package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// ImageRequest asks an image model for one picture.
type ImageRequest struct {
	Model   string
	Prompt  string
	Size    string // "1024x1024", "1024x1536", "1536x1024"; "" = model default
	Quality string // "low" | "medium" | "high" | "" (model default)
	// Image, when set, is the picture to modify (images API /images/edits).
	Image     []byte
	ImageMime string
}

// ImageResult is one generated picture.
type ImageResult struct {
	Data  []byte
	Mime  string
	Usage *Usage // token usage when the upstream reports it
}

const maxImageBytes = 40 << 20

// imageHTTP waits much longer for the first byte: one picture can take a minute or two.
var imageHTTP = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConnsPerHost:   8,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 300 * time.Second,
}}

// GenerateImage draws one picture. OpenRouter returns images from chat
// completions (modalities); everything else uses the OpenAI images API.
func (c *Client) GenerateImage(ctx context.Context, r ImageRequest) (*ImageResult, error) {
	if c.Kind == KindOpenRouter {
		return c.generateViaChat(ctx, r)
	}
	var raw []byte
	var err error
	if len(r.Image) > 0 {
		raw, err = c.postEdit(ctx, r)
	} else {
		payload := map[string]any{"model": r.Model, "prompt": r.Prompt, "n": 1}
		if r.Size != "" {
			payload["size"] = r.Size
		}
		if r.Quality != "" {
			payload["quality"] = r.Quality
		}
		raw, err = c.postJSON(ctx, "/images/generations", payload)
	}
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
		Usage *struct {
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			TotalTokens  int     `json:"total_tokens"`
			Cost         float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode image response: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, errors.New("upstream returned no image")
	}
	res := &ImageResult{}
	if u := out.Usage; u != nil {
		res.Usage = &Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.TotalTokens, Cost: u.Cost}
	}
	d := out.Data[0]
	switch {
	case d.B64 != "":
		res.Data, err = base64.StdEncoding.DecodeString(d.B64)
	case d.URL != "":
		res.Data, err = c.fetchImage(ctx, d.URL)
	default:
		err = errors.New("upstream returned an empty image")
	}
	if err != nil {
		return nil, err
	}
	res.Mime = http.DetectContentType(res.Data)
	return res, nil
}

// generateViaChat uses chat completions with image output (OpenRouter, Gemini image models).
func (c *Client) generateViaChat(ctx context.Context, r ImageRequest) (*ImageResult, error) {
	msg := Message{Role: "user", Content: r.Prompt}
	if len(r.Image) > 0 {
		msg = Message{Role: "user", Parts: []Part{
			{Type: "image_url", ImageURL: &ImageURL{URL: "data:" + r.ImageMime + ";base64," + base64.StdEncoding.EncodeToString(r.Image)}},
			{Type: "text", Text: r.Prompt},
		}}
	}
	payload := map[string]any{
		"model":      r.Model,
		"messages":   []Message{msg},
		"modalities": []string{"image", "text"},
		"usage":      map[string]any{"include": true},
	}
	if ar := aspectRatio(r.Size); ar != "" {
		payload["image_config"] = map[string]any{"aspect_ratio": ar}
	}
	raw, err := c.postJSON(ctx, "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Images  []struct {
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode image response: %w", err)
	}
	if len(out.Choices) == 0 || len(out.Choices[0].Message.Images) == 0 {
		msg := "upstream returned no image"
		if len(out.Choices) > 0 && strings.TrimSpace(out.Choices[0].Message.Content) != "" {
			msg = strings.TrimSpace(out.Choices[0].Message.Content)
		}
		return nil, &APIError{Status: 422, Message: truncateStr(msg, 300)}
	}
	u := out.Choices[0].Message.Images[0].ImageURL.URL
	var data []byte
	if strings.HasPrefix(u, "data:") {
		i := strings.Index(u, ",")
		if i < 0 {
			return nil, errors.New("bad image data url")
		}
		data, err = base64.StdEncoding.DecodeString(u[i+1:])
	} else {
		data, err = c.fetchImage(ctx, u)
	}
	if err != nil {
		return nil, err
	}
	return &ImageResult{Data: data, Mime: http.DetectContentType(data), Usage: out.Usage}, nil
}

func aspectRatio(size string) string {
	switch size {
	case "1024x1024":
		return "1:1"
	case "1024x1536":
		return "2:3"
	case "1536x1024":
		return "3:2"
	}
	return ""
}

// postEdit sends the multipart /images/edits request.
func (c *Client) postEdit(ctx context.Context, r ImageRequest) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("model", r.Model)
	_ = w.WriteField("prompt", r.Prompt)
	_ = w.WriteField("n", "1")
	if r.Size != "" {
		_ = w.WriteField("size", r.Size)
	}
	if r.Quality != "" {
		_ = w.WriteField("quality", r.Quality)
	}
	ext := map[string]string{"image/png": "png", "image/webp": "webp"}[r.ImageMime]
	if ext == "" {
		ext = "jpg"
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="image.%s"`, ext))
	h.Set("Content-Type", r.ImageMime)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(r.Image); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return c.post(ctx, "/images/edits", w.FormDataContentType(), &body)
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return c.post(ctx, path, "application/json", bytes.NewReader(b))
}

func (c *Client) post(ctx context.Context, path, contentType string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.ImageHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes*2))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Status: resp.StatusCode, Message: extractError(raw)}
	}
	return raw, nil
}

func (c *Client) fetchImage(ctx context.Context, u string) ([]byte, error) {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return nil, errors.New("bad image url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.ImageHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, errors.New("image too large")
	}
	return data, nil
}

func truncateStr(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
