// Package llm is a minimal client for OpenAI-compatible chat completion APIs.
// It only implements what the app needs: streaming chat completions with
// text / image input, reasoning deltas and usage reporting.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Message is a chat message. Exactly one of Content / Parts is used.
// An assistant message may carry ToolCalls (Content may then be empty); a
// "tool" message answers one call through ToolCallID.
type Message struct {
	Role       string
	Content    string
	Parts      []Part
	ToolCalls  []ToolCall
	ToolCallID string
}

// Tool is a function the model may call.
type Tool struct {
	Name        string
	Description string
	Parameters  any // JSON schema
}

func (t Tool) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "function",
		"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters},
	})
}

// ToolCall is one function call requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // raw JSON
}

func (c ToolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"id":       c.ID,
		"type":     "function",
		"function": map[string]string{"name": c.Name, "arguments": c.Arguments},
	})
}

// Part is a multimodal content part.
type Part struct {
	Type     string    `json:"type"` // "text" | "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	if m.Role == "tool" {
		return json.Marshal(struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		}{m.Role, m.ToolCallID, m.Content})
	}
	if len(m.ToolCalls) > 0 {
		var content *string // null when the model only called tools
		if m.Content != "" {
			content = &m.Content
		}
		return json.Marshal(struct {
			Role      string     `json:"role"`
			Content   *string    `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		}{m.Role, content, m.ToolCalls})
	}
	if len(m.Parts) > 0 {
		return json.Marshal(struct {
			Role    string `json:"role"`
			Content []Part `json:"content"`
		}{m.Role, m.Parts})
	}
	return json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{m.Role, m.Content})
}

// Request is a chat completion request.
type Request struct {
	Model           string
	Messages        []Message
	Temperature     *float64
	MaxTokens       int
	ReasoningEffort string // "", "low", "medium", "high"
	Tools           []Tool
}

type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"` // USD, when the upstream reports it (OpenRouter)
}

// Event is a single streamed increment.
type Event struct {
	Content   string
	Reasoning string
}

// Result is the final outcome of a completed stream.
type Result struct {
	FinishReason string
	Usage        *Usage
	ToolCalls    []ToolCall // complete calls, in index order
}

// Client talks to one OpenAI-compatible endpoint.
type Client struct {
	Kind    string // provider preset; "openrouter" switches to its request dialect
	BaseURL string // e.g. https://host (/v1 is added when there is no path)
	APIKey  string
	HTTP    *http.Client
	// ImageHTTP is used for image generation (much longer response timeout).
	ImageHTTP *http.Client
	// IdleTimeout aborts a stream when no bytes arrive for this long.
	IdleTimeout time.Duration
}

// sharedTransport is reused by all clients so connections to the same
// upstream are pooled.
var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   32,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 120 * time.Second,
}

func NewClient(kind, baseURL, apiKey string) *Client {
	return &Client{
		Kind:        kind,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		HTTP:        &http.Client{Transport: sharedTransport},
		ImageHTTP:   imageHTTP,
		IdleTimeout: 120 * time.Second,
	}
}

// APIError is a non-2xx response from upstream.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("upstream %d: %s", e.Status, e.Message) }

// Retryable reports whether the request may be retried (before any output).
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Endpoint joins an OpenAI-compatible base URL and an API path. A bare host
// ("https://api.openai.com", "https://relay.example/") gets the conventional /v1;
// a base that already has a path ("/v1", "/api/v1", "/v1beta/openai", "/api/v3")
// is used as is.
func Endpoint(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u, err := url.Parse(base); err == nil && u.Path == "" {
		base += "/v1"
	}
	return base + path
}

func (c *Client) endpoint(path string) string { return Endpoint(c.BaseURL, path) }

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if c.Kind == KindOpenRouter {
		// App attribution on openrouter.ai; harmless elsewhere but only sent there.
		req.Header.Set("HTTP-Referer", "https://github.com/mrxf/family-ai-chat")
		req.Header.Set("X-Title", "XiaoNuan")
	}
}

func (c *Client) body(r Request, stream bool) ([]byte, error) {
	payload := map[string]any{
		"model":    r.Model,
		"messages": r.Messages,
		"stream":   stream,
	}
	if stream {
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	if r.Temperature != nil {
		payload["temperature"] = *r.Temperature
	}
	if r.MaxTokens > 0 {
		payload["max_tokens"] = r.MaxTokens
	}
	if len(r.Tools) > 0 {
		payload["tools"] = r.Tools
	}
	if c.Kind == KindOpenRouter {
		// OpenRouter reports the real cost when usage accounting is requested and
		// takes reasoning settings in its own unified shape.
		payload["usage"] = map[string]any{"include": true}
		if r.ReasoningEffort != "" {
			payload["reasoning"] = map[string]any{"effort": r.ReasoningEffort}
		}
	} else if r.ReasoningEffort != "" {
		payload["reasoning_effort"] = r.ReasoningEffort
	}
	return json.Marshal(payload)
}

func (c *Client) do(ctx context.Context, r Request, stream bool) (*http.Response, error) {
	b, err := c.body(r, stream)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/chat/completions"), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, &APIError{Status: resp.StatusCode, Message: extractError(raw)}
	}
	return resp, nil
}

func extractError(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Error.Message != "" {
			return e.Error.Message
		}
		if e.Message != "" {
			return e.Message
		}
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens            int     `json:"prompt_tokens"`
		CompletionTokens        int     `json:"completion_tokens"`
		TotalTokens             int     `json:"total_tokens"`
		Cost                    float64 `json:"cost"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Stream runs a streaming completion and calls onEvent for every increment.
// It returns once the stream has finished, failed, or ctx is cancelled.
func (c *Client) Stream(ctx context.Context, r Request, onEvent func(Event)) (*Result, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	resp, err := c.do(ctx, r, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Idle watchdog: cancel if the upstream goes silent.
	var activity = make(chan struct{}, 1)
	if c.IdleTimeout > 0 {
		go func() {
			t := time.NewTimer(c.IdleTimeout)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-activity:
					t.Reset(c.IdleTimeout) // Go 1.23+ timers: Reset needs no drain
				case <-t.C:
					cancel(errIdle)
					return
				}
			}
		}()
	}

	res := &Result{}
	calls := map[int]*ToolCall{} // by arrival order
	slot := map[int]int{}        // upstream index -> key in calls
	var order []int
	finish := func() *Result {
		for _, i := range order {
			if c := calls[i]; c.Name != "" {
				res.ToolCalls = append(res.ToolCalls, *c)
			}
		}
		return res
	}
	// Report activity on every raw read (not per line) so a long line that is
	// still arriving never trips the idle watchdog.
	br := bufio.NewReaderSize(activityReader{resp.Body, activity}, 32<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSpace(line)
			if bytes.HasPrefix(line, []byte("data:")) {
				data := bytes.TrimSpace(line[5:])
				if bytes.Equal(data, []byte("[DONE]")) {
					return finish(), nil
				}
				var ch chunk
				if json.Unmarshal(data, &ch) != nil {
					continue
				}
				if ch.Error != nil && ch.Error.Message != "" {
					return res, &APIError{Status: 502, Message: ch.Error.Message}
				}
				for _, choice := range ch.Choices {
					ev := Event{Content: choice.Delta.Content, Reasoning: choice.Delta.ReasoningContent}
					if ev.Reasoning == "" {
						ev.Reasoning = choice.Delta.Reasoning
					}
					if ev.Content != "" || ev.Reasoning != "" {
						onEvent(ev)
					}
					for _, tc := range choice.Delta.ToolCalls {
						key, ok := slot[tc.Index]
						// Some upstreams send every complete call with index 0: a new id means a new call.
						if ok && tc.ID != "" && calls[key].ID != "" && calls[key].ID != tc.ID {
							ok = false
						}
						if !ok {
							key = len(order)
							slot[tc.Index] = key
							calls[key] = &ToolCall{}
							order = append(order, key)
						}
						c := calls[key]
						if c.ID == "" {
							c.ID = tc.ID
						}
						c.Name += tc.Function.Name
						c.Arguments += tc.Function.Arguments
					}
					if choice.FinishReason != nil && *choice.FinishReason != "" {
						res.FinishReason = *choice.FinishReason
					}
				}
				if ch.Usage != nil {
					res.Usage = &Usage{
						PromptTokens:     ch.Usage.PromptTokens,
						CompletionTokens: ch.Usage.CompletionTokens,
						TotalTokens:      ch.Usage.TotalTokens,
						Cost:             ch.Usage.Cost,
					}
					if ch.Usage.CompletionTokensDetails != nil {
						res.Usage.ReasoningTokens = ch.Usage.CompletionTokensDetails.ReasoningTokens
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if res.FinishReason == "" && res.Usage == nil {
					return res, io.ErrUnexpectedEOF
				}
				return finish(), nil
			}
			if cause := context.Cause(ctx); cause != nil {
				return res, cause
			}
			return res, err
		}
	}
}

var errIdle = errors.New("upstream stream idle timeout")

// Complete runs a non-streaming completion and returns the text.
func (c *Client) Complete(ctx context.Context, r Request) (string, *Usage, error) {
	resp, err := c.do(ctx, r, false)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", nil, err
	}
	if len(out.Choices) == 0 {
		return "", out.Usage, errors.New("upstream returned no choices")
	}
	return out.Choices[0].Message.Content, out.Usage, nil
}

type activityReader struct {
	r  io.Reader
	ch chan<- struct{}
}

func (a activityReader) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if n > 0 {
		select {
		case a.ch <- struct{}{}:
		default:
		}
	}
	return n, err
}
