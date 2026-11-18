package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nuan/internal/store"
)

// drawUpstream is an OpenAI-compatible fake that asks for one picture and
// then wraps up once the tool result comes back.
type drawUpstream struct {
	mu        sync.Mutex
	chats     []map[string]any // streamed chat requests
	images    []map[string]any
	edits     []map[string]string // form fields of /images/edits, plus "image_mime" and "image_bytes"
	editFail  int                 // answer edits with this status
	rejectAll bool                // answer 400 to any request that carries tools
}

func (d *drawUpstream) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			d.edit(t, w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/images/generations":
			d.mu.Lock()
			d.images = append(d.images, body)
			d.mu.Unlock()
			var buf bytes.Buffer
			_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 6)))
			fmt.Fprintf(w, `{"data":[{"b64_json":%q}],"usage":{"input_tokens":10,"output_tokens":500}}`, base64.StdEncoding.EncodeToString(buf.Bytes()))
			return
		case "/v1/chat/completions":
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		if body["stream"] != true {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"画小狗"}}]}`)
			return
		}
		d.mu.Lock()
		d.chats = append(d.chats, body)
		reject := d.rejectAll
		d.mu.Unlock()
		_, hasTools := body["tools"]
		if reject && hasTools {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"tools is not supported"}}`)
			return
		}
		msgs, _ := body["messages"].([]any)
		last, _ := msgs[len(msgs)-1].(map[string]any)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := func(delta map[string]any, finish string) {
			ch := map[string]any{"delta": delta}
			if finish != "" {
				ch["finish_reason"] = finish
			}
			b, _ := json.Marshal(map[string]any{"choices": []any{ch}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		if hasTools && last["role"] == "user" && strings.Contains(fmt.Sprint(last["content"]), "四张") {
			var calls []any
			for i, a := range []string{"square", "landscape", "portrait", "square"} {
				calls = append(calls, map[string]any{"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
					"function": map[string]any{"name": "generate_image", "arguments": fmt.Sprintf(`{"prompt":"pic %d","aspect":%q}`, i, a)}})
			}
			chunk(map[string]any{"tool_calls": calls}, "tool_calls")
		} else if hasTools && last["role"] == "user" && strings.Contains(fmt.Sprint(last["content"]), "改") {
			chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_e", "type": "function",
				"function": map[string]any{"name": "generate_image", "arguments": `{"prompt":"make it black","image":"图1"}`}}}}, "tool_calls")
		} else if hasTools && last["role"] == "user" && strings.Contains(fmt.Sprint(last["content"]), "画") {
			chunk(map[string]any{"content": "我来画一只。"}, "")
			chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_a", "type": "function",
				"function": map[string]any{"name": "generate_image", "arguments": `{"prompt":"a cute puppy","aspect":"portrait"}`}}}}, "")
			chunk(map[string]any{}, "tool_calls")
		} else if last["role"] == "tool" {
			chunk(map[string]any{"content": "画好啦！"}, "stop")
		} else {
			chunk(map[string]any{"content": "好的"}, "stop")
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	}
}

func (d *drawUpstream) edit(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		t.Errorf("edit form: %v", err)
		return
	}
	form := map[string]string{}
	for k, v := range r.MultipartForm.Value {
		form[k] = v[0]
	}
	if fh := r.MultipartForm.File["image"]; len(fh) == 1 {
		form["image_mime"] = fh[0].Header.Get("Content-Type")
		form["image_bytes"] = fmt.Sprint(fh[0].Size)
	}
	d.mu.Lock()
	d.edits = append(d.edits, form)
	fail := d.editFail
	d.mu.Unlock()
	if fail != 0 {
		w.WriteHeader(fail)
		fmt.Fprint(w, `{"error":{"message":"edits not supported for this model"}}`)
		return
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	fmt.Fprintf(w, `{"data":[{"b64_json":%q}],"usage":{"input_tokens":10,"output_tokens":500}}`, base64.StdEncoding.EncodeToString(buf.Bytes()))
}

func (d *drawUpstream) chatRequests() []map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]map[string]any(nil), d.chats...)
}

// drawFixture points the default chat model at a drawUpstream and, when
// withImage is set, configures an image model on the same provider.
func drawFixture(t *testing.T, withImage bool) (*apiFixture, *drawUpstream) {
	f := newAPIFixture(t)
	d := &drawUpstream{}
	srv := newHTTPTestServer(d.handler(t))
	if _, inMemory := srv.Listener.(*memoryListener); inMemory {
		srv.Close()
		t.Skip("image client uses its own transport")
	}
	t.Cleanup(srv.Close)
	ctx := t.Context()
	p := &store.Provider{ID: "draw", Kind: "custom", Name: "draw", BaseURL: srv.URL, APIKey: "k", Enabled: true}
	if err := f.st.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	cm := &store.Model{ID: "draw-chat", ProviderID: p.ID, Kind: store.KindChat, UpstreamModel: "gpt-6.1-terra", DisplayName: "Terra", Tools: true, Vision: true, ContextWindow: 128000, Enabled: true}
	if err := f.st.UpsertModel(ctx, cm); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSetting(ctx, "default_model_id", cm.ID); err != nil {
		t.Fatal(err)
	}
	if withImage {
		im := &store.Model{ID: "draw-image", ProviderID: p.ID, Kind: store.KindImage, UpstreamModel: "gpt-image-2.6-sunburst", DisplayName: "画图",
			ContextWindow: 128000, InputPrice: 5, OutputPrice: 40, Enabled: true}
		if err := f.st.UpsertModel(ctx, im); err != nil {
			t.Fatal(err)
		}
		if err := f.st.SetSetting(ctx, "image_model_id", im.ID); err != nil {
			t.Fatal(err)
		}
	}
	return f, d
}

func TestChatDrawsPictureThroughTool(t *testing.T) {
	f, d := drawFixture(t, true)
	res := f.send(map[string]any{"content": "帮我画一只可爱的小狗"})
	s := f.stream(res.Assistant.ID)
	var activities []string
	var sawAttachments bool
	var final *store.Message
	for final == nil {
		e := s.next()
		switch e.name {
		case "activity":
			// An empty text clears the indicator; a late subscriber may only see that.
			if text := decodeEvent[struct {
				Text string `json:"text"`
			}](t, e).Text; text != "" {
				activities = append(activities, text)
			}
		case "attachments":
			sawAttachments = true
		case "done":
			final = decodeEvent[struct{ Message *store.Message }](t, e).Message
		}
	}
	if final.Status != store.StatusDone || final.Content != "我来画一只。\n\n画好啦！" {
		t.Fatalf("final message: status=%s content=%q err=%q", final.Status, final.Content, final.Error)
	}
	if len(final.Attachments) != 1 || final.Attachments[0].Prompt != "a cute puppy" || final.Attachments[0].Mime != "image/jpeg" || final.Attachments[0].Width != 4 {
		t.Fatalf("attachments: %+v", final.Attachments)
	}
	_ = sawAttachments // may be folded into the snapshot when the stream attaches late
	if len(activities) > 0 && activities[0] != "正在画图…" {
		t.Fatalf("activities: %v", activities)
	}
	if len(d.images) != 1 || d.images[0]["size"] != "1024x1536" || d.images[0]["model"] != "gpt-image-2.6-sunburst" {
		t.Fatalf("image requests: %+v", d.images)
	}

	// Stored message keeps the picture; the file belongs to the user and is a JPEG.
	stored, err := f.st.GetMessage(t.Context(), final.ID)
	if err != nil || len(stored.Attachments) != 1 {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), "GET", f.http.URL+"/api/files/"+stored.Attachments[0].FileID, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.http.Client().Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("file: %v %v", resp, err)
	}
	resp.Body.Close()

	// The tool round trip: second request carries the call and its result.
	chats := d.chatRequests()
	if len(chats) != 2 {
		t.Fatalf("chat requests: %d", len(chats))
	}
	msgs := chats[1]["messages"].([]any)
	asst := msgs[len(msgs)-2].(map[string]any)
	tool := msgs[len(msgs)-1].(map[string]any)
	if asst["role"] != "assistant" || asst["tool_calls"] == nil || tool["role"] != "tool" || tool["tool_call_id"] != "call_a" ||
		!strings.Contains(fmt.Sprint(tool["content"]), "图片已经画好") {
		t.Fatalf("round trip messages: %v / %v", asst, tool)
	}
	sys := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "generate_image") {
		t.Fatal("system prompt misses the drawing instructions")
	}

	// Cost: chat tokens have no price here, the picture is 10*5 + 500*40 per million.
	rows, err := f.st.ModelUsageSince(t.Context(), 0)
	var cost float64
	for _, r := range rows {
		if r.ModelID == "draw-chat" {
			cost = r.Cost
		}
	}
	if err != nil || cost < 0.02 || cost > 0.0201 {
		t.Fatalf("cost=%v err=%v", cost, err)
	}

	// Next turn: the model is told what it drew before.
	res2 := f.send(map[string]any{"conversation_id": res.Conversation.ID, "content": "谢谢"})
	f.waitRun(res2.Assistant.ID)
	chats = d.chatRequests()
	hist := fmt.Sprint(chats[len(chats)-1]["messages"])
	if !strings.Contains(hist, "画了 1 张图") || !strings.Contains(hist, "a cute puppy") {
		t.Fatalf("history misses the drawn picture: %s", hist)
	}
}

func TestChatWithoutImageModelSendsNoTools(t *testing.T) {
	f, d := drawFixture(t, false)
	res := f.send(map[string]any{"content": "帮我画一只小狗"})
	f.waitRun(res.Assistant.ID)
	chats := d.chatRequests()
	if len(chats) != 1 || chats[0]["tools"] != nil {
		t.Fatalf("requests: %+v", chats)
	}
	sys := chats[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "不能画图") {
		t.Fatal("system prompt should say drawing is unavailable")
	}
	var boot struct {
		Models   []map[string]any `json:"models"`
		Features map[string]any   `json:"features"`
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features["image_generation"] != false {
		t.Fatalf("features: %v", boot.Features)
	}
}

func TestChatRetriesWithoutToolsWhenRejected(t *testing.T) {
	f, d := drawFixture(t, true)
	d.rejectAll = true
	if err := f.st.CreateMemories(t.Context(), []*store.Memory{{UserID: f.user.ID, Category: "preference", Content: "用户喜欢小狗", Pinned: true}}); err != nil {
		t.Fatal(err)
	}
	res := f.send(map[string]any{"content": "帮我画一只小狗"})
	f.waitRun(res.Assistant.ID)
	m, err := f.st.GetMessage(t.Context(), res.Assistant.ID)
	if err != nil || m.Status != store.StatusDone || m.Content != "好的" {
		t.Fatalf("message: %+v %v", m, err)
	}
	chats := d.chatRequests()
	if len(chats) != 2 || chats[0]["tools"] == nil || chats[1]["tools"] != nil {
		t.Fatalf("expected one rejected request with tools and one retry without: %d", len(chats))
	}
	for _, req := range chats {
		messages := req["messages"].([]any)
		sys := messages[0].(map[string]any)["content"].(string)
		if !strings.Contains(sys, "用户喜欢小狗") {
			t.Fatal("retry lost core", sys)
		}
	}
}

func TestImageModelsStayOutOfFamilyList(t *testing.T) {
	f, _ := drawFixture(t, true)
	var boot struct {
		Models   []map[string]any `json:"models"`
		Features map[string]any   `json:"features"`
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	for _, m := range boot.Models {
		if m["id"] == "draw-image" {
			t.Fatal("image model leaked into the family's model list")
		}
	}
	if boot.Features["image_generation"] != true {
		t.Fatalf("features: %v", boot.Features)
	}
	// A conversation cannot be pinned to the image model.
	res := f.send(map[string]any{"content": "你好", "model_id": "draw-image"})
	f.waitRun(res.Assistant.ID)
	if m, _ := f.st.GetMessage(t.Context(), res.Assistant.ID); m.ModelID == nil || *m.ModelID == "draw-image" {
		t.Fatalf("reply used the image model: %+v", m)
	}
}

func TestChatDrawsSeveralPicturesInParallelWithLimit(t *testing.T) {
	f, d := drawFixture(t, true)
	res := f.send(map[string]any{"content": "画四张不同的风景"})
	f.waitRun(res.Assistant.ID)
	m, err := f.st.GetMessage(t.Context(), res.Assistant.ID)
	if err != nil || m.Status != store.StatusDone || len(m.Attachments) != 3 {
		t.Fatalf("message: %+v %v", m, err)
	}
	d.mu.Lock()
	n := len(d.images)
	d.mu.Unlock()
	if n != 3 {
		t.Fatalf("image requests: %d", n)
	}
	chats := d.chatRequests()
	if len(chats) != 2 || chats[1]["tools"] != nil {
		t.Fatalf("second round must not offer tools: %d requests", len(chats))
	}
	msgs := chats[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["tool_call_id"] != "call_3" || !strings.Contains(fmt.Sprint(last["content"]), "最多画 3 张") {
		t.Fatalf("fourth call should be refused: %v", last)
	}
}

func TestToolRejectionIsRemembered(t *testing.T) {
	f, d := drawFixture(t, true)
	d.rejectAll = true
	if err := f.st.CreateMemories(t.Context(), []*store.Memory{{UserID: f.user.ID, Category: "preference", Content: "用户喜欢小狗", Pinned: true}}); err != nil {
		t.Fatal(err)
	}
	res := f.send(map[string]any{"content": "你好"})
	f.waitRun(res.Assistant.ID)
	res = f.send(map[string]any{"conversation_id": res.Conversation.ID, "content": "再来"})
	f.waitRun(res.Assistant.ID)
	chats := d.chatRequests()
	if len(chats) != 3 || chats[2]["tools"] != nil {
		t.Fatalf("second reply should skip tools straight away: %d requests", len(chats))
	}
	sys := chats[2]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "换一个模型") {
		t.Fatal("system prompt should point to another model")
	}
}

func TestAddingImageModelAdoptsItAndSettingsAreKindChecked(t *testing.T) {
	f, _ := drawFixture(t, false)
	f.asAdmin()
	var out struct {
		Items []store.Model `json:"items"`
	}
	f.json("POST", "/api/admin/models/batch", map[string]any{"provider_id": "draw", "models": []map[string]any{
		{"upstream_model": "gpt-image-2.6-sunburst", "kind": "image"},
		{"upstream_model": "gpt-6.1-sol", "kind": "chat"},
	}}, 200, &out)
	if len(out.Items) != 2 || out.Items[0].Kind != store.KindImage || out.Items[0].Tools || out.Items[1].Kind != store.KindChat || !out.Items[1].Tools {
		t.Fatalf("added: %+v", out.Items)
	}
	settings := map[string]string{}
	f.json("GET", "/api/admin/settings", nil, 200, &settings)
	if settings["image_model_id"] != out.Items[0].ID {
		t.Fatalf("image model not adopted: %v", settings)
	}
	// Chat slots refuse image models and vice versa.
	f.json("PUT", "/api/admin/settings", map[string]string{"default_model_id": out.Items[0].ID}, 400, nil)
	f.json("PUT", "/api/admin/settings", map[string]string{"image_model_id": out.Items[1].ID}, 400, nil)
	// Turning drawing off sticks.
	f.json("PUT", "/api/admin/settings", map[string]string{"image_model_id": ""}, 200, &settings)
	if settings["image_model_id"] != "" {
		t.Fatalf("settings: %v", settings)
	}
}

// drawOne asks for a puppy and returns the finished reply.
func drawOne(t *testing.T, f *apiFixture, convID string) *store.Message {
	t.Helper()
	body := map[string]any{"content": "帮我画一只可爱的小狗"}
	if convID != "" {
		body["conversation_id"] = convID
	}
	res := f.send(body)
	f.waitRun(res.Assistant.ID)
	m, err := f.st.GetMessage(t.Context(), res.Assistant.ID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func lastToolResult(t *testing.T, d *drawUpstream) string {
	t.Helper()
	chats := d.chatRequests()
	msgs := chats[len(chats)-1]["messages"].([]any)
	return fmt.Sprint(msgs[len(msgs)-1].(map[string]any)["content"])
}

func TestChatEditsPictureInPlace(t *testing.T) {
	f, d := drawFixture(t, true)
	first := drawOne(t, f, "")
	if len(first.Attachments) != 1 {
		t.Fatalf("first: %+v", first)
	}
	res := f.send(map[string]any{"conversation_id": first.ConversationID, "content": "把图1改成黑色的"})
	f.waitRun(res.Assistant.ID)
	m, _ := f.st.GetMessage(t.Context(), res.Assistant.ID)
	if m.Status != store.StatusDone || len(m.Attachments) != 1 || m.Attachments[0].Source != first.Attachments[0].FileID || m.Attachments[0].Width != 8 {
		t.Fatalf("edited reply: %+v", m)
	}
	if len(d.edits) != 1 || d.edits[0]["prompt"] != "make it black" || d.edits[0]["model"] != "gpt-image-2.6-sunburst" ||
		d.edits[0]["image_mime"] != "image/jpeg" || d.edits[0]["image_bytes"] == "0" || d.edits[0]["size"] != "" {
		t.Fatalf("edit requests: %+v", d.edits)
	}
	if len(d.images) != 1 {
		t.Fatalf("an edit must not draw anew: %d generations", len(d.images))
	}
	chats := d.chatRequests()
	if !strings.Contains(fmt.Sprint(chats[2]["messages"]), "第 1 张是图1") {
		t.Fatalf("history does not number the picture: %v", chats[2]["messages"])
	}
	if !strings.Contains(lastToolResult(t, d), "在原图基础上改好") {
		t.Fatalf("tool result: %s", lastToolResult(t, d))
	}
	res = f.send(map[string]any{"conversation_id": first.ConversationID, "content": "谢谢"})
	f.waitRun(res.Assistant.ID)
	chats = d.chatRequests()
	if !strings.Contains(fmt.Sprint(chats[len(chats)-1]["messages"]), "第 1 张是图2，由图1 修改而来") {
		t.Fatalf("history misses the edit: %v", chats[len(chats)-1]["messages"])
	}
}

func TestChatEditsUploadedPhoto(t *testing.T) {
	f, d := drawFixture(t, true)
	file, _ := f.uploadPNG()
	res := f.send(map[string]any{"content": "把这张照片改成卡通风格", "attachments": []map[string]any{{"file_id": file.ID}}})
	f.waitRun(res.Assistant.ID)
	m, _ := f.st.GetMessage(t.Context(), res.Assistant.ID)
	if len(m.Attachments) != 1 || m.Attachments[0].Source != file.ID {
		t.Fatalf("reply: %+v", m)
	}
	// The PNG upload is sent as JPEG (alpha could be read as a mask).
	if len(d.edits) != 1 || d.edits[0]["image_mime"] != "image/jpeg" {
		t.Fatalf("edits: %+v", d.edits)
	}
	if !strings.Contains(fmt.Sprint(d.chatRequests()[0]["messages"]), "这条消息里的图片：图1") {
		t.Fatal("the photo is not numbered for the model")
	}
}

func TestEditFallsBackToRedrawWhenUnsupported(t *testing.T) {
	f, d := drawFixture(t, true)
	d.editFail = 404
	first := drawOne(t, f, "")
	res := f.send(map[string]any{"conversation_id": first.ConversationID, "content": "把它改成黑色的"})
	f.waitRun(res.Assistant.ID)
	m, _ := f.st.GetMessage(t.Context(), res.Assistant.ID)
	if m.Status != store.StatusDone || len(m.Attachments) != 1 {
		t.Fatalf("reply: %+v", m)
	}
	if len(d.edits) != 1 || len(d.images) != 2 {
		t.Fatalf("edits=%d generations=%d", len(d.edits), len(d.images))
	}
	if p := fmt.Sprint(d.images[1]["prompt"]); !strings.Contains(p, "a cute puppy") || !strings.Contains(p, "Change: make it black") {
		t.Fatalf("redraw prompt: %s", p)
	}
	if !strings.Contains(lastToolResult(t, d), "重新画了一张") {
		t.Fatalf("tool result: %s", lastToolResult(t, d))
	}
}

func TestEditWithUnknownNumberIsRefused(t *testing.T) {
	f, d := drawFixture(t, true)
	res := f.send(map[string]any{"content": "把图1改成黑色"}) // no pictures yet
	f.waitRun(res.Assistant.ID)
	if len(d.edits) != 0 || len(d.images) != 0 {
		t.Fatalf("edits=%d generations=%d", len(d.edits), len(d.images))
	}
	if !strings.Contains(lastToolResult(t, d), "还没有图片") {
		t.Fatalf("tool result: %s", lastToolResult(t, d))
	}
}

func TestDailyImageLimit(t *testing.T) {
	f, d := drawFixture(t, true)
	if err := f.st.SetSetting(t.Context(), "image_daily_limit", "1"); err != nil {
		t.Fatal(err)
	}
	first := drawOne(t, f, "")
	if len(first.Attachments) != 1 {
		t.Fatalf("first: %+v", first)
	}
	var boot struct {
		Features map[string]any `json:"features"`
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features["images_left"] != float64(0) {
		t.Fatalf("features: %v", boot.Features)
	}
	// Deleting the conversation does not give the picture back.
	f.json("DELETE", "/api/conversations/"+first.ConversationID, nil, 200, nil)
	second := drawOne(t, f, "")
	if len(second.Attachments) != 0 || len(d.images) != 1 || second.Status != store.StatusDone {
		t.Fatalf("over the limit: %+v, generations=%d", second, len(d.images))
	}
	if !strings.Contains(lastToolResult(t, d), "今天的画图次数已经用完了（每人每天 1 张）") {
		t.Fatalf("tool result: %s", lastToolResult(t, d))
	}
	// The admin pays the bills and is not limited.
	f.asAdmin()
	if third := drawOne(t, f, ""); len(third.Attachments) != 1 {
		t.Fatalf("admin: %+v", third)
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features["images_left"] != float64(-1) {
		t.Fatalf("admin features: %v", boot.Features)
	}
	// The setting is validated; 0 means no limit.
	f.json("PUT", "/api/admin/settings", map[string]string{"image_daily_limit": "abc"}, 400, nil)
	settings := map[string]string{}
	f.json("PUT", "/api/admin/settings", map[string]string{"image_daily_limit": "0"}, 200, &settings)
	if settings["image_daily_limit"] != "" {
		t.Fatalf("settings: %v", settings)
	}
}

func TestCleanupRemovesFilesNoMessageUses(t *testing.T) {
	f, _ := drawFixture(t, true)
	ctx := t.Context()
	unsent, _ := f.uploadPNG()
	sent, _ := f.uploadPNG()
	res := f.send(map[string]any{"content": "看看", "attachments": []map[string]any{{"file_id": sent.ID}}})
	f.waitRun(res.Assistant.ID)
	drawn := drawOne(t, f, "")
	stray := filepath.Join(f.svc.DataDir, "files", "2020-01", "0190-stray.jpg")
	if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(stray, old, old)

	// Fresh files are left alone; the stray one without a row goes.
	if n, err := f.svc.CleanupFiles(ctx, time.Hour); err != nil || n != 1 {
		t.Fatalf("grace: removed=%d err=%v", n, err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatal("stray file kept")
	}
	if _, err := f.st.GetFile(ctx, unsent.ID, f.user.ID); err != nil {
		t.Fatal("fresh upload removed")
	}

	if n, err := f.svc.CleanupFiles(ctx, -time.Minute); err != nil || n != 1 {
		t.Fatalf("removed=%d err=%v", n, err)
	}
	if _, err := f.st.GetFile(ctx, unsent.ID, f.user.ID); err == nil {
		t.Fatal("unsent upload kept")
	}
	pic, err := f.st.GetFile(ctx, drawn.Attachments[0].FileID, f.user.ID)
	if err != nil {
		t.Fatal("drawn picture removed while its message exists")
	}
	if _, err := f.st.GetFile(ctx, sent.ID, f.user.ID); err != nil {
		t.Fatal("sent photo removed")
	}

	f.json("DELETE", "/api/conversations/"+drawn.ConversationID, nil, 200, nil)
	if n, err := f.svc.CleanupFiles(ctx, -time.Minute); err != nil || n != 1 {
		t.Fatalf("after delete: removed=%d err=%v", n, err)
	}
	if _, err := os.Stat(pic.Path); !os.IsNotExist(err) {
		t.Fatal("picture of the deleted conversation still on disk")
	}
}
