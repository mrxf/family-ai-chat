package llm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStreamCollectsToolCalls(t *testing.T) {
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 || !strings.Contains(fmt.Sprint(tools[0]), "generate_image") {
			t.Errorf("tools not sent: %#v", body["tools"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range []string{
			`{"choices":[{"delta":{"content":"我来画"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"generate_image","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"prompt\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a dog\"}"}}]}}]}`,
			// Gemini style: a second complete call that reuses index 0.
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"generate_image","arguments":"{\"prompt\":\"a cat\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", d)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	var content strings.Builder
	res, err := c.Stream(t.Context(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "画狗"}},
		Tools: []Tool{{Name: "generate_image", Description: "draw", Parameters: map[string]any{"type": "object"}}}},
		func(e Event) { content.WriteString(e.Content) })
	if err != nil {
		t.Fatal(err)
	}
	if content.String() != "我来画" || res.FinishReason != "tool_calls" || len(res.ToolCalls) != 2 {
		t.Fatalf("content=%q result=%+v", content.String(), res)
	}
	if c0 := res.ToolCalls[0]; c0.ID != "call_1" || c0.Name != "generate_image" || c0.Arguments != `{"prompt":"a dog"}` {
		t.Fatalf("call 0: %+v", c0)
	}
	if c1 := res.ToolCalls[1]; c1.ID != "call_2" || c1.Arguments != `{"prompt":"a cat"}` {
		t.Fatalf("call 1: %+v", c1)
	}
}

func TestToolMessagesMarshal(t *testing.T) {
	b, _ := json.Marshal([]Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "generate_image", Arguments: `{"prompt":"x"}`}}},
		{Role: "tool", ToolCallID: "c1", Content: "done"},
	})
	want := `[{"role":"assistant","content":null,"tool_calls":[{"function":{"arguments":"{\"prompt\":\"x\"}","name":"generate_image"},"id":"c1","type":"function"}]},{"role":"tool","tool_call_id":"c1","content":"done"}]`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

var tinyPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

func imageClient(t *testing.T, kind string, h http.HandlerFunc) *Client {
	c := streamClient(t, h)
	c.Kind = kind
	c.ImageHTTP = c.HTTP
	return c
}

func TestGenerateImageOpenAI(t *testing.T) {
	c := imageClient(t, KindCustom, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/images/generations" || body["model"] != "gpt-image" || body["size"] != "1024x1536" || body["quality"] != "medium" {
			t.Errorf("request: %s %#v", r.URL.Path, body)
		}
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}],"usage":{"input_tokens":19,"output_tokens":515,"total_tokens":534}}`, base64.StdEncoding.EncodeToString(tinyPNG))
	})
	res, err := c.GenerateImage(t.Context(), ImageRequest{Model: "gpt-image", Prompt: "dog", Size: "1024x1536", Quality: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mime != "image/png" || len(res.Data) != len(tinyPNG) || res.Usage == nil || res.Usage.PromptTokens != 19 || res.Usage.CompletionTokens != 515 {
		t.Fatalf("result: mime=%s len=%d usage=%+v", res.Mime, len(res.Data), res.Usage)
	}
}

func TestGenerateImageURLAndErrors(t *testing.T) {
	var c *Client
	c = imageClient(t, KindCustom, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["prompt"] == "bad" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"message":"Your request was rejected by the safety system"}}`)
				return
			}
			fmt.Fprintf(w, `{"data":[{"url":%q}]}`, c.BaseURL+"/img.png")
		case "/img.png":
			w.Write(tinyPNG)
		}
	})
	res, err := c.GenerateImage(t.Context(), ImageRequest{Model: "m", Prompt: "ok"})
	if err != nil || res.Mime != "image/png" {
		t.Fatalf("url image: %v %+v", err, res)
	}
	_, err = c.GenerateImage(t.Context(), ImageRequest{Model: "m", Prompt: "bad"})
	if ae, ok := err.(*APIError); !ok || ae.Status != 400 || !strings.Contains(ae.Message, "safety") {
		t.Fatalf("want APIError 400, got %v", err)
	}
}

func TestGenerateImageOpenRouter(t *testing.T) {
	c := imageClient(t, KindOpenRouter, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/chat/completions" || fmt.Sprint(body["modalities"]) != "[image text]" {
			t.Errorf("request: %s %#v", r.URL.Path, body)
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}}]}}],"usage":{"prompt_tokens":5,"completion_tokens":1290,"cost":0.039}}`,
			base64.StdEncoding.EncodeToString(tinyPNG))
	})
	res, err := c.GenerateImage(t.Context(), ImageRequest{Model: "google/gemini-image", Prompt: "dog", Size: "1024x1024"})
	if err != nil || res.Mime != "image/png" || res.Usage == nil || res.Usage.Cost != 0.039 {
		t.Fatalf("openrouter image: %v %+v", err, res)
	}
}

func TestEditImageOpenAIMultipart(t *testing.T) {
	c := imageClient(t, KindCustom, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" || r.Header.Get("Authorization") == "" {
			t.Errorf("request: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		fh := r.MultipartForm.File["image"]
		if r.FormValue("model") != "gpt-image" || r.FormValue("prompt") != "make it red" || r.FormValue("size") != "" ||
			len(fh) != 1 || fh[0].Header.Get("Content-Type") != "image/jpeg" || fh[0].Filename != "image.jpg" || fh[0].Size != 3 {
			t.Errorf("form: %#v files=%#v", r.MultipartForm.Value, fh)
		}
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(tinyPNG))
	})
	res, err := c.GenerateImage(t.Context(), ImageRequest{Model: "gpt-image", Prompt: "make it red", Image: []byte{1, 2, 3}, ImageMime: "image/jpeg"})
	if err != nil || res.Mime != "image/png" {
		t.Fatalf("edit: %v %+v", err, res)
	}
}

func TestEditImageOpenRouterSendsPicture(t *testing.T) {
	c := imageClient(t, KindOpenRouter, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"url":"data:image/jpeg;base64,AQID"`) || !strings.Contains(string(raw), `"text":"make it red"`) {
			t.Errorf("body: %s", raw)
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,%s"}}]}}]}`, base64.StdEncoding.EncodeToString(tinyPNG))
	})
	if _, err := c.GenerateImage(t.Context(), ImageRequest{Model: "g", Prompt: "make it red", Image: []byte{1, 2, 3}, ImageMime: "image/jpeg"}); err != nil {
		t.Fatal(err)
	}
}
