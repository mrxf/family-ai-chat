package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nuan/internal/memory"
	"nuan/internal/store"
)

func TestMemoryAPIAndSettingsMerge(t *testing.T) {
	f := newAPIFixture(t)
	var created struct {
		ID, Category, Content, Source string
		IsNew                         bool `json:"is_new"`
	}
	f.json("POST", "/api/memories", map[string]any{"category": "health", "content": "  用户有高血压  "}, 201, &created)
	if created.Content != "用户有高血压" || created.Source != "manual" || created.IsNew {
		t.Fatal(created)
	}
	f.json("POST", "/api/memories", map[string]any{"category": "bad", "content": "x"}, 400, nil)
	f.json("POST", "/api/memories", map[string]any{"category": "health", "content": strings.Repeat("字", 201)}, 400, nil)
	var list struct {
		Memories   []map[string]any
		Total, New int
		Enabled    bool
	}
	f.json("GET", "/api/memories", nil, 200, &list)
	if list.Total != 1 || !list.Enabled {
		t.Fatal(list)
	}
	for _, key := range []string{"embedding", "keywords", "status"} {
		if _, ok := list.Memories[0][key]; ok {
			t.Fatal(key)
		}
	}
	f.json("PATCH", "/api/memories/"+created.ID, map[string]any{"content": "用户血压正常", "category": "profile"}, 200, &created)
	if created.Category != "profile" || created.Content != "用户血压正常" {
		t.Fatal(created)
	}
	b := &store.User{Username: "b", Role: "admin"}
	if err := f.st.CreateUser(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	bm := &store.Memory{UserID: b.ID, Category: "family", Content: "用户有女儿", Source: "auto", Pinned: true}
	if err := f.st.CreateMemories(t.Context(), []*store.Memory{bm}); err != nil {
		t.Fatal(err)
	}
	f.json("PATCH", "/api/memories/"+bm.ID, map[string]any{"content": "越权"}, 404, nil)
	f.json("DELETE", "/api/memories/"+bm.ID, nil, 404, nil)
	var imported struct {
		Memories []map[string]any
		Skipped  int
	}
	f.json("POST", "/api/memories/import", map[string]any{"items": []map[string]string{{"category": "preference", "content": "用户不喜欢辣"}, {"category": "preference", "content": "用户不喜欢辣。"}}}, 200, &imported)
	if len(imported.Memories) != 1 || imported.Skipped != 1 {
		t.Fatal(imported)
	}
	f.json("POST", "/api/memories/import", map[string]any{"items": []map[string]string{{"category": "episode", "content": "不允许"}}}, 400, nil)
	f.json("POST", "/api/memories/import", map[string]any{"items": []map[string]string{{"category": "ongoing", "content": "用户在装修", "valid_until": "明年"}}}, 400, nil)
	many := make([]map[string]string, memory.MaxImportItems+1)
	f.json("POST", "/api/memories/import", map[string]any{"items": many}, 400, nil)
	// Replacing someone else's memory only adds the new one.
	f.json("POST", "/api/memories/import", map[string]any{"items": []map[string]string{{"category": "family", "content": "用户有儿子", "replaces_id": bm.ID}}}, 200, nil)
	if got, err := f.st.GetMemory(t.Context(), bm.ID, b.ID); err != nil || got.Status != "active" {
		t.Fatal(got, err)
	}

	f.json("GET", "/api/memories/import/preview", nil, 404, nil)
	f.up.mu.Lock()
	f.up.extraction = `{"items":[{"category":"health","content":"用户有高血压","keywords":"血压"},{"category":"preference","content":"用户不喜欢辣"}],"duplicates":[]}`
	f.up.mu.Unlock()
	var job memory.ImportJob
	f.json("POST", "/api/memories/import/preview", map[string]string{"text": "I have high blood pressure.\nI don't like spicy food."}, 202, &job)
	if job.Status != "running" || job.Total != 1 {
		t.Fatal(job)
	}
	deadline := time.Now().Add(5 * time.Second)
	for job.Status == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		f.json("GET", "/api/memories/import/preview", nil, 200, &job)
	}
	// "不喜欢辣" was imported above, so only the new fact is proposed.
	if job.Status != "done" || len(job.Items) != 1 || job.Items[0].Keywords != "血压" || job.Skipped != 1 {
		t.Fatalf("%+v", job)
	}
	f.json("DELETE", "/api/memories/import/preview", nil, 204, nil)
	f.json("GET", "/api/memories/import/preview", nil, 404, nil)
	f.json("POST", "/api/memories/import/preview", map[string]string{"text": strings.Repeat("字", memory.MaxImportRunes+1)}, 400, nil)
	f.json("POST", "/api/memories/import/preview", map[string]string{"text": "  "}, 400, nil)
	f.json("GET", "/api/memories", nil, 200, &list)
	if list.Total != 3 {
		t.Fatal("preview wrote memories", list)
	}
	f.json("PATCH", "/api/me", map[string]any{"settings": map[string]any{"font_size": 20, "theme": "dark"}}, 200, nil)
	f.json("PATCH", "/api/me", map[string]any{"settings": map[string]any{"memory_enabled": false, "theme": nil}}, 200, nil)
	u, err := f.st.GetUser(t.Context(), f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(u.Settings, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["font_size"] != float64(20) || settings["memory_enabled"] != false {
		t.Fatal(settings)
	}
	if _, ok := settings["theme"]; ok {
		t.Fatal(settings)
	}
	f.json("GET", "/api/memories", nil, 200, &list)
	if list.Enabled {
		t.Fatal(list)
	}
	for _, bad := range []any{nil, []string{"bad"}, true} {
		f.json("PATCH", "/api/me", map[string]any{"settings": bad}, 400, nil)
	}
	f.json("DELETE", "/api/memories/"+created.ID, nil, 204, nil)
	f.json("DELETE", "/api/memories", nil, 204, nil)
	f.json("GET", "/api/memories", nil, 200, &list)
	if list.Total != 0 {
		t.Fatal(list)
	}
	if _, err := f.st.GetMemory(t.Context(), bm.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	// Even an admin's ordinary endpoint only sees that admin's own memories.
	token, err := f.api.Sessions.Create(t.Context(), b.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	f.request("DELETE", "/api/memories/"+bm.ID, f.token, "", nil, 404, nil)
	f.request("GET", "/api/memories", token, "", nil, 200, &list)
	if list.Total != 1 || list.New != 1 {
		t.Fatal(list)
	}
}
func TestMemoryExtractionEndToEnd(t *testing.T) {
	f := newAPIFixture(t)
	f.up.mu.Lock()
	f.up.extraction = `{"actions":[{"op":"add","category":"health","content":"用户服用氨氯地平 5mg","keywords":"降压药 高血压 血压"}],"episode":"聊了降压药"}`
	f.up.mu.Unlock()
	result := f.send(map[string]any{"content": "记住，我每天吃氨氯地平 5mg"})
	f.waitRun(result.Assistant.ID)
	w := memory.NewWorker(f.svc.Memory.(*memory.Service))
	f.svc.MemoryWorker = w
	t.Cleanup(func() { w.Shutdown(context.Background()) })
	w.Kick(result.Conversation.ID, 0)
	eventually(t, "extracted memory", func() bool { ms, err := f.st.ListMemories(t.Context(), f.user.ID); return err == nil && len(ms) == 2 })
	next := f.send(map[string]any{"conversation_id": result.Conversation.ID, "content": "能不能吃西柚"})
	f.waitRun(next.Assistant.ID)
	requests := f.up.recorded("能不能吃西柚")
	if len(requests) == 0 || !strings.Contains(string(requests[0].Messages[0].Content), "用户服用氨氯地平") {
		t.Fatal(requests)
	}
}
func TestEmbeddingModelAdminLifecycle(t *testing.T) {
	f := newAPIFixture(t)
	role := "admin"
	if _, err := f.st.UpdateUser(t.Context(), f.user.ID, store.UserPatch{Role: &role}); err != nil {
		t.Fatal(err)
	}
	var m store.Model
	f.json("POST", "/api/admin/models", map[string]any{"provider_id": "local", "kind": "embedding", "upstream_model": "bge-m3", "context_window": 0}, 200, &m)
	if m.Kind != "embedding" {
		t.Fatal(m)
	}
	f.json("PUT", "/api/admin/settings", map[string]string{"embedding_model_id": m.ID, "memory_model_id": f.model.ID}, 200, nil)
	f.json("PUT", "/api/admin/settings", map[string]string{"memory_model_id": m.ID}, 400, nil)
	var test struct {
		OK    bool
		Reply string
	}
	f.json("POST", "/api/admin/models/"+m.ID+"/test", map[string]any{}, 200, &test)
	if !test.OK || test.Reply != "向量维度 3" {
		t.Fatal(test)
	}
	var models []struct {
		ID string `json:"id"`
	}
	f.json("GET", "/api/models", nil, 200, &models)
	if len(models) != 1 || models[0].ID != f.model.ID {
		t.Fatal(models)
	}
	f.json("PATCH", "/api/admin/models/"+m.ID, map[string]any{"kind": "chat", "context_window": 4096}, 200, &m)
	if m.Kind != "chat" {
		t.Fatal(m)
	}
	v, err := f.st.GetSetting(t.Context(), "embedding_model_id")
	if err != nil || v != "" {
		t.Fatal(v, err)
	}
	f.json("PATCH", "/api/admin/models/"+m.ID, map[string]any{"kind": "embedding"}, 200, nil)
	f.json("PUT", "/api/admin/settings", map[string]string{"embedding_model_id": m.ID}, 200, nil)
	f.json("PATCH", "/api/admin/models/"+m.ID, map[string]any{"enabled": false}, 200, nil)
	v, err = f.st.GetSetting(t.Context(), "embedding_model_id")
	if err != nil || v != "" {
		t.Fatal(v, err)
	}
}

type recordedKick struct {
	conv  string
	delay time.Duration
}
type recordingMemoryWorker struct{ kicks chan recordedKick }

func (w *recordingMemoryWorker) Kick(conv string, delay time.Duration) {
	w.kicks <- recordedKick{conv, delay}
}
func (w *recordingMemoryWorker) Shutdown(context.Context) {}
func TestMemoryKickOnlyAfterSuccessfulTriggeredReply(t *testing.T) {
	f := newAPIFixture(t)
	w := &recordingMemoryWorker{kicks: make(chan recordedKick, 4)}
	f.svc.MemoryWorker = w
	result := f.send(map[string]any{"content": "记住我退休了"})
	f.waitRun(result.Assistant.ID)
	select {
	case k := <-w.kicks:
		if k.conv != result.Conversation.ID || k.delay != 5*time.Second {
			t.Fatal(k)
		}
		msg, err := f.st.GetMessage(t.Context(), result.Assistant.ID)
		if err != nil || msg.Status != store.StatusDone {
			t.Fatal(msg, err)
		}
	default:
		t.Fatal("trigger did not kick")
	}
	plain := f.send(map[string]any{"content": "你好"})
	f.waitRun(plain.Assistant.ID)
	f.up.plan("记住失败的消息", upstreamPlan{tokens: []string{"partial"}, errorAfter: true})
	failed := f.send(map[string]any{"content": "记住失败的消息"})
	f.waitRun(failed.Assistant.ID)
	select {
	case k := <-w.kicks:
		t.Fatal("unexpected kick", k)
	default:
	}
}
func TestAssistantMemoryDefaultsAndToggle(t *testing.T) {
	f := newAPIFixture(t)
	var a store.Assistant
	f.json("POST", "/api/assistants", map[string]any{"name": "新角色"}, 201, &a)
	if !a.UseMemory {
		t.Fatal(a)
	}
	f.json("PATCH", "/api/assistants/"+a.ID, map[string]any{"use_memory": false}, 200, &a)
	if a.UseMemory {
		t.Fatal(a)
	}
}
