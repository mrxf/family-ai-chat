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
	"strings"
	"time"
)

// Message is a chat message. Exactly one of Content / Parts is used.
type Message struct {
	Role    string
	Content string
	Parts   []Part
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
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
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
}

// Client talks to one OpenAI-compatible endpoint.
type Client struct {
	BaseURL string // e.g. https://host (with or without /v1)
	APIKey  string
	HTTP    *http.Client
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

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		HTTP:        &http.Client{Transport: sharedTransport},
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

func (c *Client) endpoint(path string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
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
	if r.ReasoningEffort != "" {
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
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
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
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens            int `json:"prompt_tokens"`
		CompletionTokens        int `json:"completion_tokens"`
		TotalTokens             int `json:"total_tokens"`
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
					return res, nil
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
					if choice.FinishReason != nil && *choice.FinishReason != "" {
						res.FinishReason = *choice.FinishReason
					}
				}
				if ch.Usage != nil {
					res.Usage = &Usage{
						PromptTokens:     ch.Usage.PromptTokens,
						CompletionTokens: ch.Usage.CompletionTokens,
						TotalTokens:      ch.Usage.TotalTokens,
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
				return res, nil
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
