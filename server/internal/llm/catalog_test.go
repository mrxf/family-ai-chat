package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestEndpoint(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.openai.com":                                   "https://api.openai.com/v1/chat/completions",
		"https://cc.miaojiai.com/":                                 "https://cc.miaojiai.com/v1/chat/completions",
		"https://api.openai.com/v1":                                "https://api.openai.com/v1/chat/completions",
		"https://openrouter.ai/api/v1/":                            "https://openrouter.ai/api/v1/chat/completions",
		"https://generativelanguage.googleapis.com/v1beta/openai/": "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
		"https://ark.cn-beijing.volces.com/api/v3":                 "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		" http://127.0.0.1:8080 ":                                  "http://127.0.0.1:8080/v1/chat/completions",
	} {
		if got := Endpoint(base, "/chat/completions"); got != want {
			t.Errorf("Endpoint(%q)=%q want %q", base, got, want)
		}
	}
}

func TestSuggest(t *testing.T) {
	type want struct {
		vision         bool
		def, think     string
		chat, recommen bool
		name           string
	}
	for id, w := range map[string]want{
		"gpt-6.1-terra":          {true, "none", "high", true, true, "GPT-6.1 Terra"},
		"gpt-6.1-sol":            {true, "low", "high", true, true, "GPT-6.1 Sol"},
		"gpt-5-mini":             {true, "minimal", "high", true, true, "GPT-5 Mini"},
		"gpt-4o":                 {true, "", "", true, true, "GPT-4o"},
		"gpt-4o-2024-08-06":      {true, "", "", true, false, "GPT-4o (2024-08-06)"},
		"gpt-3.5-turbo":          {false, "", "", true, false, "GPT-3.5 Turbo"},
		"o4-mini":                {true, "low", "high", true, true, "O4 Mini"},
		"gemini-2.5-flash":       {true, "none", "high", true, true, "Gemini 2.5 Flash"},
		"gemini-2.5-pro":         {true, "low", "high", true, true, "Gemini 2.5 Pro"},
		"gemini-2.0-flash":       {true, "", "", true, false, "Gemini 2.0 Flash"},
		"claude-sonnet-4-5":      {true, "", "", true, true, "Claude Sonnet 4.5"},
		"deepseek-chat":          {false, "", "", true, true, "Deepseek Chat"},
		"Qwen/Qwen2.5-VL-72B":    {true, "", "", true, false, "Qwen2.5 VL 72B"},
		"text-embedding-3-small": {false, "", "", false, false, "Text Embedding 3 Small"},
		"gpt-image-1":            {false, "", "", false, false, "GPT-Image 1"},
		"tts-1":                  {false, "", "", false, false, "Tts 1"},
	} {
		s := Suggest(KindCustom, UpstreamModel{ID: id})
		got := want{s.Vision, s.DefaultEffort, s.ThinkEffort, s.Chat, s.Recommended, s.DisplayName}
		if got != w {
			t.Errorf("Suggest(%q)=%+v want %+v", id, got, w)
		}
		if s.ContextWindow != MaxSuggestedContext {
			t.Errorf("Suggest(%q) context=%d", id, s.ContextWindow)
		}
	}

	// OpenRouter metadata wins over guessing.
	s := Suggest(KindOpenRouter, UpstreamModel{
		ID: "anthropic/claude-sonnet-4.5", Name: "Anthropic: Claude Sonnet 4.5", Rich: true, ContextLength: 64000,
		InputModalities: []string{"text"}, OutputModalities: []string{"text"}, SupportedParams: []string{"reasoning"},
		PromptPrice: 0.000003, CompletionPrice: 0.000015,
	})
	if s.DisplayName != "Claude Sonnet 4.5" || s.Vision || s.ThinkEffort != "high" || s.DefaultEffort != "" || s.ContextWindow != 64000 ||
		s.InputPrice != 3 || s.OutputPrice != 15 || !s.Chat {
		t.Fatalf("openrouter suggestion=%+v", s)
	}
	if s := Suggest(KindOpenRouter, UpstreamModel{ID: "x/image-gen", Rich: true, OutputModalities: []string{"image"}}); s.Chat {
		t.Fatal("image-only output treated as chat")
	}
}

func TestListModelsStripsGeminiPrefixAndParsesOpenRouterMeta(t *testing.T) {
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request %s", r.URL)
		}
		io.WriteString(w, `{"data":[{"id":"models/gemini-2.5-flash"},{"id":"gemini-2.5-flash"},
			{"id":"openai/gpt-4o","name":"OpenAI: GPT-4o","created":5,"context_length":128000,
			 "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
			 "pricing":{"prompt":"0.0000025","completion":"0.00001"}}]}`)
	})
	ms, err := c.ListModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].ID != "gemini-2.5-flash" || !ms[1].Rich || ms[1].PromptPrice != 0.0000025 || ms[1].Created != 5 {
		t.Fatalf("models=%+v", ms)
	}
}

func TestOpenRouterDialectAndCost(t *testing.T) {
	var body map[string]any
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Title") == "" {
			t.Error("missing attribution header")
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5,\"cost\":0.00042}}\n\ndata: [DONE]\n\n")
	})
	c.Kind = KindOpenRouter
	res, err := c.Stream(t.Context(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}, ReasoningEffort: "high"}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage == nil || res.Usage.Cost != 0.00042 {
		t.Fatalf("usage=%+v", res.Usage)
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	usage, _ := body["usage"].(map[string]any)
	if _, ok := body["reasoning_effort"]; ok || reasoning["effort"] != "high" || usage["include"] != true {
		t.Fatalf("body=%v", body)
	}
}
