package api

import (
	"strings"
	"testing"

	"nuan/internal/store"
)

// asAdmin promotes the fixture user so admin endpoints can be exercised.
func (f *apiFixture) asAdmin() {
	f.t.Helper()
	role := "admin"
	if _, err := f.st.UpdateUser(f.t.Context(), f.user.ID, store.UserPatch{Role: &role}); err != nil {
		f.t.Fatal(err)
	}
}

type providerResp struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	BaseURL           string `json:"base_url"`
	APIKey            string `json:"api_key"`
	APIKeyHint        string `json:"api_key_hint"`
	Endpoint          string `json:"endpoint"`
	ModelCount        int    `json:"model_count"`
	EnabledModelCount int    `json:"enabled_model_count"`
}

func TestProviderCRUDKeepsKeysOnTheirHost(t *testing.T) {
	f := newAPIFixture(t)
	f.asAdmin()

	var presets []map[string]any
	f.json("GET", "/api/admin/provider-presets", nil, 200, &presets)
	if len(presets) < 3 || presets[len(presets)-1]["kind"] != "custom" {
		t.Fatalf("presets=%v", presets)
	}

	// A preset fills name and address; only the key is required.
	var p providerResp
	f.json("POST", "/api/admin/providers", map[string]any{"kind": "gemini", "api_key": "AIzaSyExampleExampleExample1234"}, 200, &p)
	if p.Name != "Google Gemini" || p.Endpoint != "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions" ||
		p.APIKeyHint != "AIz****1234" || p.APIKey != "" {
		t.Fatalf("created=%+v", p)
	}
	f.json("POST", "/api/admin/providers", map[string]any{"kind": "custom", "name": "x", "base_url": "ftp://x", "api_key": "k"}, 400, nil)
	f.json("POST", "/api/admin/providers", map[string]any{"kind": "nope", "api_key": "k"}, 400, nil)

	// Renaming keeps the key; moving it to another host without re-entering the key is refused.
	f.json("PATCH", "/api/admin/providers/"+p.ID, map[string]any{"name": "谷歌"}, 200, &p)
	var errResp struct{ Error struct{ Code string } }
	f.json("PATCH", "/api/admin/providers/"+p.ID, map[string]any{"base_url": "https://evil.example/v1"}, 400, &errResp)
	if errResp.Error.Code != "key_required" {
		t.Fatalf("error=%+v", errResp)
	}
	f.json("POST", "/api/admin/providers/test", map[string]any{"id": p.ID, "base_url": "https://evil.example/v1"}, 400, nil)
	f.json("PATCH", "/api/admin/providers/"+p.ID, map[string]any{"base_url": "https://generativelanguage.googleapis.com/v1beta/openai/"}, 200, &p)
	stored, err := f.st.GetProvider(t.Context(), p.ID)
	if err != nil || stored.APIKey != "AIzaSyExampleExampleExample1234" || stored.Name != "谷歌" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}

	var list []providerResp
	f.json("GET", "/api/admin/providers", nil, 200, &list)
	if len(list) != 2 || list[0].ID != "local" || list[0].ModelCount != 1 || list[0].EnabledModelCount != 1 || list[0].APIKeyHint != "****" {
		t.Fatalf("list=%+v", list)
	}
}

func TestProviderTestAndUpstreamModelPicker(t *testing.T) {
	f := newAPIFixture(t)
	f.asAdmin()
	var res map[string]any
	f.json("POST", "/api/admin/providers/test", map[string]any{"kind": "custom", "name": "t", "base_url": f.up.server.URL, "api_key": "local-test-key"}, 200, &res)
	if res["ok"] != true || res["model_count"] != float64(4) || res["chat_model_count"] != float64(3) {
		t.Fatalf("test=%v", res)
	}
	f.json("POST", "/api/admin/providers/test", map[string]any{"kind": "custom", "name": "t", "base_url": f.up.server.URL, "api_key": "wrong"}, 200, &res)
	if res["ok"] != false || !strings.Contains(res["message"].(string), "API Key") {
		t.Fatalf("bad key test=%v", res)
	}
	// Testing a saved provider reuses its stored key.
	f.json("POST", "/api/admin/providers/test", map[string]any{"id": "local"}, 200, &res)
	if res["ok"] != true {
		t.Fatalf("saved test=%v", res)
	}

	var picker struct {
		Items []struct {
			UpstreamModel   string `json:"upstream_model"`
			DisplayName     string `json:"display_name"`
			Chat            bool   `json:"chat"`
			Recommended     bool   `json:"recommended"`
			DefaultEffort   string `json:"default_effort"`
			ExistingModelID string `json:"existing_model_id"`
		}
	}
	f.json("GET", "/api/admin/providers/local/upstream-models", nil, 200, &picker)
	got := []string{}
	for _, it := range picker.Items {
		got = append(got, it.UpstreamModel)
	}
	// Recommended chat models first, then other chat models (newest first), non-chat last.
	if strings.Join(got, ",") != "gpt-6.1-terra,gpt-4o-2024-08-06,fake,text-embedding-3-small" {
		t.Fatalf("order=%v", got)
	}
	if picker.Items[0].DisplayName != "GPT-6.1 Terra" || picker.Items[0].DefaultEffort != "none" || picker.Items[2].ExistingModelID != "test-model" || picker.Items[3].Chat {
		t.Fatalf("items=%+v", picker.Items)
	}

	// Batch add skips models that already exist.
	var batch struct {
		Items []store.Model `json:"items"`
	}
	f.json("POST", "/api/admin/models/batch", map[string]any{"provider_id": "local", "models": []map[string]any{
		{"upstream_model": "gpt-6.1-terra", "display_name": "GPT-6.1 Terra", "vision": true, "default_effort": "none", "think_effort": "high", "context_window": 128000},
		{"upstream_model": "fake"},
	}}, 200, &batch)
	if len(batch.Items) != 1 || batch.Items[0].UpstreamModel != "gpt-6.1-terra" || batch.Items[0].Sort != 1 {
		t.Fatalf("batch=%+v", batch.Items)
	}

	// Reorder puts the new model first.
	var ordered []store.Model
	f.json("PUT", "/api/admin/models/order", map[string]any{"ids": []string{batch.Items[0].ID, "test-model"}}, 200, &ordered)
	if len(ordered) != 2 || ordered[0].ID != batch.Items[0].ID {
		t.Fatalf("ordered=%+v", ordered)
	}

	// Model test sends a real request with the configured effort.
	f.json("POST", "/api/admin/models/test-model/test", map[string]any{"think": true}, 200, &res)
	if res["ok"] != true || res["effort"] != "high" || res["reply"] == "" {
		t.Fatalf("model test=%v", res)
	}
}

func TestSettingsRepairAfterDeletingDefaultProvider(t *testing.T) {
	f := newAPIFixture(t)
	f.asAdmin()
	var p providerResp
	f.json("POST", "/api/admin/providers", map[string]any{"kind": "custom", "name": "备用", "base_url": f.up.server.URL, "api_key": "local-test-key"}, 200, &p)
	var m store.Model
	f.json("POST", "/api/admin/models", map[string]any{"provider_id": p.ID, "upstream_model": "backup", "vision": true}, 200, &m)

	f.json("PUT", "/api/admin/settings", map[string]any{"fallback_model_id": "nope"}, 400, nil)
	f.json("PUT", "/api/admin/settings", map[string]any{"default_model_id": ""}, 400, nil)
	var settings map[string]string
	f.json("PUT", "/api/admin/settings", map[string]any{"fallback_model_id": m.ID, "title_model_id": "test-model"}, 200, &settings)

	// Disabling the backup provider clears the fallback; deleting the default's provider moves the default.
	f.json("PATCH", "/api/admin/providers/"+p.ID, map[string]any{"enabled": false}, 200, nil)
	f.json("GET", "/api/admin/settings", nil, 200, &settings)
	if settings["fallback_model_id"] != "" || settings["default_model_id"] != "test-model" {
		t.Fatalf("after disable=%v", settings)
	}
	f.json("PATCH", "/api/admin/providers/"+p.ID, map[string]any{"enabled": true}, 200, nil)
	f.json("DELETE", "/api/admin/providers/local", nil, 200, &settings)
	if settings["default_model_id"] != m.ID || settings["title_model_id"] != "" {
		t.Fatalf("after delete=%v", settings)
	}
}

func TestFallbackModelAnswersWhenPrimaryFails(t *testing.T) {
	f := newAPIFixture(t)
	fb := &store.Model{ID: "fallback", ProviderID: "local", UpstreamModel: "backup-model", DisplayName: "备用", ContextWindow: 128000, Enabled: true, InputPrice: 1, OutputPrice: 2}
	if err := f.st.UpsertModel(t.Context(), fb); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSetting(t.Context(), "fallback_model_id", fb.ID); err != nil {
		t.Fatal(err)
	}
	// Primary fails twice (initial + retry); the third request goes to the fallback.
	f.up.plan("fb", upstreamPlan{tokens: []string{"备用回答"}, failStatus: 503, failCount: 2})
	res := f.send(map[string]any{"content": "fb"})
	_, done, _ := f.stream(res.Assistant.ID).finish()
	reqs := f.up.recorded("fb")
	if done.Status != store.StatusDone || done.Content != "备用回答" || done.ModelID == nil || *done.ModelID != fb.ID {
		t.Fatalf("done=%+v", done)
	}
	if len(reqs) != 3 || reqs[0].Model != "fake" || reqs[2].Model != "backup-model" || reqs[2].Effort != "" {
		t.Fatalf("requests=%+v", reqs)
	}
	// Cost is estimated from the fallback's prices (11 prompt + 7 completion tokens).
	f.waitRun(res.Assistant.ID)
	var usage struct {
		Total  store.UsageRow        `json:"total"`
		Models []store.ModelUsageRow `json:"models"`
	}
	f.asAdmin()
	f.json("GET", "/api/admin/usage?days=7", nil, 200, &usage)
	want := (11*1.0 + 7*2.0) / 1e6
	if usage.Total.Messages != 1 || usage.Total.Cost < want*0.999 || usage.Total.Cost > want*1.001 || len(usage.Models) != 1 || usage.Models[0].ModelID != fb.ID {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestImagesSwitchToVisionModel(t *testing.T) {
	f := newAPIFixture(t)
	text := &store.Model{ID: "text-only", ProviderID: "local", UpstreamModel: "text-only", DisplayName: "文字", ContextWindow: 128000, Enabled: true}
	if err := f.st.UpsertModel(t.Context(), text); err != nil {
		t.Fatal(err)
	}
	file, _ := f.uploadPNG()
	res := f.send(map[string]any{"content": "看图", "model_id": text.ID, "attachments": []store.Attachment{{FileID: file.ID}}})
	f.stream(res.Assistant.ID).finish()
	reqs := f.up.recorded("看图")
	if len(reqs) != 1 || reqs[0].Model != "fake" || res.Assistant.ModelID == nil || *res.Assistant.ModelID != "test-model" {
		t.Fatalf("requests=%+v assistant=%+v", reqs, res.Assistant)
	}
	// A text message in the same conversation stays on the chosen model.
	res = f.send(map[string]any{"conversation_id": res.Conversation.ID, "content": "继续"})
	f.stream(res.Assistant.ID).finish()
	if reqs := f.up.recorded("继续"); len(reqs) != 1 || reqs[0].Model != "text-only" {
		t.Fatalf("text requests=%+v", reqs)
	}
}

func TestFirstRunSetup(t *testing.T) {
	f := newAPIFixture(t)
	// The fixture already has a user: setup is closed.
	var status map[string]bool
	f.request("GET", "/api/setup", "", "", nil, 200, &status)
	if status["needs_setup"] {
		t.Fatal("setup open with existing users")
	}
	if err := f.st.DeleteUser(t.Context(), f.user.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.api.InitSetup(t.Context()); err != nil {
		t.Fatal(err)
	}
	code := f.api.setup.code
	if len(code) != 8 {
		t.Fatalf("code=%q", code)
	}
	f.request("GET", "/api/setup", "", "", nil, 200, &status)
	if !status["needs_setup"] {
		t.Fatal("setup closed on empty database")
	}
	body := func(c, pw string) *strings.Reader {
		return strings.NewReader(`{"code":"` + c + `","username":"爸爸的管理员","password":"` + pw + `"}`)
	}
	f.request("POST", "/api/setup", "", "application/json", body("WRONG-CODE", "long-password"), 400, nil)
	f.request("POST", "/api/setup", "", "application/json", body(code, "short"), 400, nil)
	var login struct {
		Token string
		User  store.User
	}
	pretty := strings.ToLower(code[:4] + "-" + code[4:])
	f.request("POST", "/api/setup", "", "application/json", body(pretty, "long-password"), 200, &login)
	if login.Token == "" || !login.User.IsAdmin() {
		t.Fatalf("setup login=%+v", login)
	}
	f.request("GET", "/api/admin/providers", login.Token, "", nil, 200, nil)
	f.request("POST", "/api/setup", "", "application/json", body(code, "long-password"), 409, nil)
}
