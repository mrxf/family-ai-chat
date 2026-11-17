package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// UpstreamModel is what a provider's /models endpoint says about one model.
type UpstreamModel struct {
	ID               string
	Name             string // OpenRouter style "OpenAI: GPT-4o"; usually empty elsewhere
	Created          int64  // unix seconds, 0 if unknown
	ContextLength    int
	InputModalities  []string
	OutputModalities []string
	SupportedParams  []string
	PromptPrice      float64 // USD per token
	CompletionPrice  float64
	Rich             bool // capabilities above came from the provider (OpenRouter), not guessed
}

// ListModels fetches the provider's model list.
func (c *Client) ListModels(ctx context.Context) ([]UpstreamModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/models"), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Status: resp.StatusCode, Message: extractError(raw)}
	}
	var out struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Created       int64  `json:"created"`
			ContextLength int    `json:"context_length"`
			Architecture  *struct {
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
			SupportedParameters []string `json:"supported_parameters"`
			Pricing             *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("返回的内容不是模型列表，请检查接口地址")
	}
	seen := map[string]bool{}
	ms := make([]UpstreamModel, 0, len(out.Data))
	for _, d := range out.Data {
		id := strings.TrimPrefix(strings.TrimSpace(d.ID), "models/") // Gemini lists "models/gemini-2.5-flash"
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m := UpstreamModel{ID: id, Name: d.Name, Created: d.Created, ContextLength: d.ContextLength, SupportedParams: d.SupportedParameters}
		if d.Architecture != nil {
			m.InputModalities, m.OutputModalities = d.Architecture.InputModalities, d.Architecture.OutputModalities
			m.Rich = true
		}
		if d.Pricing != nil {
			m.PromptPrice, _ = strconv.ParseFloat(d.Pricing.Prompt, 64)
			m.CompletionPrice, _ = strconv.ParseFloat(d.Pricing.Completion, 64)
		}
		ms = append(ms, m)
	}
	return ms, nil
}

// KeyInfo is OpenRouter's view of an API key.
type KeyInfo struct {
	Usage float64  // USD spent with this key
	Limit *float64 // USD limit, nil = unlimited
}

// CheckKey validates the key. OpenRouter's model list is public, so it is the
// only kind that needs a dedicated call; for it the key's spend is returned.
func (c *Client) CheckKey(ctx context.Context) (*KeyInfo, error) {
	if c.Kind != KindOpenRouter {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/key"), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Status: resp.StatusCode, Message: extractError(raw)}
	}
	var out struct {
		Data struct {
			Usage float64  `json:"usage"`
			Limit *float64 `json:"limit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unexpected /key response: %w", err)
	}
	return &KeyInfo{Usage: out.Data.Usage, Limit: out.Data.Limit}, nil
}

// Suggestion is a ready-to-save model configuration inferred from an upstream model.
type Suggestion struct {
	UpstreamModel string  `json:"upstream_model"`
	DisplayName   string  `json:"display_name"`
	Description   string  `json:"description"`
	Vision        bool    `json:"vision"`
	DefaultEffort string  `json:"default_effort"`
	ThinkEffort   string  `json:"think_effort"`
	ContextWindow int     `json:"context_window"`
	InputPrice    float64 `json:"input_price"`  // USD per million tokens
	OutputPrice   float64 `json:"output_price"` // USD per million tokens
	Chat          bool    `json:"chat"`         // false for embeddings, TTS, image generation, ...
	Image         bool    `json:"image"`        // draws pictures (can be added as an image model)
	Tools         bool    `json:"tools"`        // likely supports tool calling
	Recommended   bool    `json:"recommended"`  // a current mainstream chat model
	Created       int64   `json:"created"`
}

// MaxSuggestedContext caps how much history is sent per reply by default.
// Larger windows remember longer chats but every reply then costs more.
const MaxSuggestedContext = 128000

var (
	reDated     = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}|\d{8}|\d{4})$`)
	reOSeries   = regexp.MustCompile(`^o\d`)
	reGPT       = regexp.MustCompile(`^(?:chat)?gpt-(\d+)(?:\.(\d+))?`)
	reGemini    = regexp.MustCompile(`^gemini-(\d+)(?:\.(\d+))?`)
	reGLMVision = regexp.MustCompile(`^glm-\d(?:\.\d+)?v`)
)

var nonChatMarkers = []string{
	"embed", "tts", "whisper", "transcribe", "dall-e", "gpt-image", "-image", "imagen", "veo", "sora", "lyria",
	"moderation", "rerank", "realtime", "audio", "speech", "aqa", "babbage", "davinci", "turbo-instruct", "deep-research",
}

var imageMarkers = []string{
	"gpt-image", "dall-e", "imagen", "flux", "seedream", "-image", "image-", "cogview", "kolors", "stable-diffusion", "sdxl",
	"recraft", "ideogram", "wanx", "midjourney",
}

// Chat models that are known to reject the tools parameter.
var noToolMarkers = []string{"o1-mini", "o1-preview", "reasoner", "-r1", "qwq", "gemma", "-vl", "vl-", "instruct"}

func hasAny(s string, subs []string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// Suggest infers a model configuration. Rules are deliberately simple; the admin
// can test and adjust every field afterwards.
func Suggest(kind string, um UpstreamModel) Suggestion {
	base := strings.ToLower(um.ID)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:] // "openai/gpt-4o" -> "gpt-4o"
	}
	base, _, _ = strings.Cut(base, ":") // "deepseek-chat:free" -> "deepseek-chat"

	s := Suggestion{UpstreamModel: um.ID, Created: um.Created, Chat: true}

	for _, m := range nonChatMarkers {
		if strings.Contains(base, m) {
			s.Chat = false
		}
	}
	if um.Rich && len(um.OutputModalities) > 0 && !contains(um.OutputModalities, "text") {
		s.Chat = false
	}
	s.Image = (hasAny(base, imageMarkers) && !hasAny(base, []string{"embed", "video", "veo", "sora", "vision"})) ||
		(um.Rich && contains(um.OutputModalities, "image"))
	if s.Image {
		s.Chat = false
	}
	s.Tools = s.Chat && !hasAny(base, noToolMarkers)
	if s.Chat && um.Rich && len(um.SupportedParams) > 0 {
		s.Tools = contains(um.SupportedParams, "tools")
	}

	if um.Rich {
		s.Vision = contains(um.InputModalities, "image")
	} else {
		s.Vision = guessVision(base)
	}
	s.DefaultEffort, s.ThinkEffort = guessEffort(base)
	if s.ThinkEffort == "" && um.Rich && (contains(um.SupportedParams, "reasoning") || contains(um.SupportedParams, "include_reasoning")) {
		s.ThinkEffort = "high"
	}

	s.ContextWindow = MaxSuggestedContext
	if um.ContextLength > 0 && um.ContextLength < s.ContextWindow {
		s.ContextWindow = um.ContextLength
	}
	if um.PromptPrice > 0 || um.CompletionPrice > 0 {
		s.InputPrice, s.OutputPrice = round6(um.PromptPrice*1e6), round6(um.CompletionPrice*1e6)
	}

	s.DisplayName = PrettyName(um.ID)
	if um.Name != "" {
		name := um.Name
		if i := strings.Index(name, ": "); i >= 0 && i < 24 {
			name = name[i+2:] // "OpenAI: GPT-4o" -> "GPT-4o"
		}
		s.DisplayName = strings.TrimSpace(name)
	}
	s.Description = describe(base)
	s.Recommended = s.Chat && recommended(base)
	return s
}

func guessVision(base string) bool {
	switch {
	case strings.Contains(base, "vision"), strings.Contains(base, "-vl"), strings.Contains(base, "vl-"),
		strings.HasPrefix(base, "qvq"), strings.Contains(base, "omni"), reGLMVision.MatchString(base),
		strings.HasPrefix(base, "gemini"), strings.HasPrefix(base, "gemma-3"), strings.HasPrefix(base, "llama-4"),
		strings.HasPrefix(base, "grok-4"), strings.HasPrefix(base, "pixtral"):
		return true
	case strings.HasPrefix(base, "claude"):
		return !strings.HasPrefix(base, "claude-2") && !strings.Contains(base, "instant")
	case reOSeries.MatchString(base):
		return !strings.Contains(base, "mini") || strings.HasPrefix(base, "o4")
	}
	if m := reGPT.FindStringSubmatch(base); m != nil {
		major, _ := strconv.Atoi(m[1])
		return major >= 5 || strings.HasPrefix(base, "gpt-4o") || strings.HasPrefix(base, "chatgpt-4o") ||
			strings.HasPrefix(base, "gpt-4.1") || strings.HasPrefix(base, "gpt-4.5") || strings.HasPrefix(base, "gpt-4-turbo")
	}
	return false
}

// guessEffort returns (reasoning effort for everyday replies, effort for "deep thinking").
// "" means: do not send the parameter.
func guessEffort(base string) (string, string) {
	slow := strings.Contains(base, "pro") || strings.Contains(base, "sol")
	if reOSeries.MatchString(base) {
		return "low", "high" // o-series always reasons and rejects "none"
	}
	if m := reGPT.FindStringSubmatch(base); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		switch {
		case major < 5 || strings.Contains(base, "-chat"):
			return "", "" // classic chat models reject reasoning_effort
		case slow:
			return "low", "high"
		case major == 5 && minor == 0:
			return "minimal", "high" // gpt-5 has no "none"
		default:
			return "none", "high" // gpt-5.1+ can switch thinking off for fast first tokens
		}
	}
	if m := reGemini.FindStringSubmatch(base); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		switch {
		case major < 2 || (major == 2 && minor < 5):
			return "", ""
		case major == 2 && strings.Contains(base, "flash"):
			return "none", "high"
		default:
			return "low", "high" // Pro and Gemini 3 cannot switch thinking off
		}
	}
	return "", ""
}

func describe(base string) string {
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(base, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("sol", "opus", "reasoner", "-r1", "qwq", "-max", "-pro") || reOSeries.MatchString(base):
		return "最聪明，适合复杂问题，稍慢一些"
	case has("mini", "flash", "lite", "nano", "haiku", "turbo", "-air"):
		return "反应快，适合日常问答"
	case strings.HasPrefix(base, "claude"):
		return "写作细腻，擅长分析和长文"
	case strings.HasPrefix(base, "gemini"):
		return "反应快，知识面广"
	case strings.HasPrefix(base, "deepseek"):
		return "中文好，性价比高"
	case strings.HasPrefix(base, "qwen"), strings.HasPrefix(base, "glm"), strings.HasPrefix(base, "kimi"), strings.HasPrefix(base, "doubao"):
		return "中文好，国内模型"
	case strings.Contains(base, "gpt"):
		return "聪明均衡，日常首选"
	}
	return ""
}

func recommended(base string) bool {
	if reDated.MatchString(base) {
		return false // pinned snapshots duplicate the alias
	}
	for _, m := range []string{"preview", "exp", "beta", "alpha", "test", "search", "-latest", "codex", "computer"} {
		if strings.Contains(base, m) {
			return false
		}
	}
	if m := reGPT.FindStringSubmatch(base); m != nil {
		major, _ := strconv.Atoi(m[1])
		return major >= 5 || strings.HasPrefix(base, "gpt-4o") || strings.HasPrefix(base, "gpt-4.1")
	}
	if m := reGemini.FindStringSubmatch(base); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		return major > 2 || (major == 2 && minor >= 5)
	}
	switch {
	case strings.HasPrefix(base, "claude"):
		return !strings.HasPrefix(base, "claude-2") && !strings.HasPrefix(base, "claude-3") && !strings.Contains(base, "instant")
	case strings.HasPrefix(base, "o3"), strings.HasPrefix(base, "o4"):
		return true
	}
	for _, p := range []string{"deepseek", "qwen3", "qwen-max", "qwen-plus", "qwen-turbo", "qwen-vl-max", "glm-4.5", "glm-4.6", "glm-5", "kimi-k2", "grok-4", "doubao-seed"} {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	return false
}

// PrettyName turns an upstream id into a readable name: "openai/gpt-5-5-mini" -> "GPT-5.5 Mini".
func PrettyName(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id, _, _ = strings.Cut(id, ":")
	snapshot := ""
	if loc := reDated.FindStringIndex(id); loc != nil {
		snapshot, id = id[loc[0]+1:], id[:loc[0]] // "gpt-4o-2024-08-06" -> "GPT-4o (2024-08-06)"
	}
	parts := strings.Split(id, "-")
	// Drop effort suffixes like -low / -medium / -high; they are an implementation detail.
	if n := len(parts); n > 1 {
		switch parts[n-1] {
		case "low", "medium", "high", "xhigh":
			parts = parts[:n-1]
		}
	}
	out := []string{}
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		// "5-5" -> "5.5"
		if isDigits(p) && len(p) <= 2 && i+1 < len(parts) && isDigits(parts[i+1]) && len(parts[i+1]) <= 2 {
			p = p + "." + parts[i+1]
			i++
		}
		switch {
		case strings.EqualFold(p, "gpt"):
			p = "GPT"
		case len(p) > 0 && p[0] >= 'a' && p[0] <= 'z':
			p = strings.ToUpper(p[:1]) + p[1:]
		}
		out = append(out, p)
	}
	name := strings.Replace(strings.Join(out, " "), "GPT ", "GPT-", 1)
	if snapshot != "" {
		name += " (" + snapshot + ")"
	}
	return name
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func round6(f float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 6, 64), 64)
	return v
}
