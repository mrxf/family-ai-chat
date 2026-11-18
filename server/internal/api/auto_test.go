package api

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"nuan/internal/chat"
	"nuan/internal/llm"
	"nuan/internal/store"
)

func TestAutoModeGatingAdminSettingsAndRoutingNotes(t *testing.T) {
	f := newAPIFixture(t)
	f.asAdmin()

	// By default Auto mode is off.
	var boot struct {
		DefaultModelID string         `json:"default_model_id"`
		Features       map[string]any `json:"features"`
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features["auto_model"] != false || boot.Features["auto_dynamic"] != false {
		t.Fatalf("expected auto off by default, got features=%v", boot.Features)
	}

	// Cannot enable Auto without choosing a judge model, nor set default_model_id="auto" while off.
	f.json("PUT", "/api/admin/settings", map[string]string{"auto_enabled": "1"}, 400, nil)
	f.json("PUT", "/api/admin/settings", map[string]string{"default_model_id": "auto"}, 400, nil)
	f.json("PUT", "/api/admin/settings", map[string]string{"auto_router_kind": "jev"}, 400, nil)

	// Add models with admin routing_note and verify server built-in profile enrichment.
	var carbon struct {
		store.Model
		BuiltinRoutingNote string `json:"builtin_routing_note"`
		AutoTier           int    `json:"auto_tier"`
	}
	f.json("POST", "/api/admin/models", map[string]any{
		"provider_id":    "local",
		"upstream_model": "gemini-4-carbon",
		"display_name":   "Gemini 4 Carbon",
		"routing_note":   "医疗问题和深度科学研究",
		"vision":         true,
	}, 200, &carbon)
	if carbon.RoutingNote != "医疗问题和深度科学研究" || carbon.AutoTier != llm.TierFlagship || !strings.Contains(carbon.BuiltinRoutingNote, "Gemini Carbon") {
		t.Fatalf("carbon view=%+v", carbon)
	}

	// Enable Auto mode with judge model and set family default to Auto.
	var settings map[string]string
	f.json("PUT", "/api/admin/settings", map[string]string{
		"auto_enabled":     "1",
		"auto_model_id":    "test-model",
		"default_model_id": "auto",
	}, 200, &settings)
	if settings["auto_enabled"] != "1" || settings["auto_model_id"] != "test-model" || settings["default_model_id"] != "auto" {
		t.Fatalf("settings=%v", settings)
	}

	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features["auto_model"] != true || boot.DefaultModelID != "auto" {
		t.Fatalf("bootstrap after enable=%+v", boot)
	}

	// Turning Auto off repairs default_model_id back to a concrete usable chat model.
	f.json("PUT", "/api/admin/settings", map[string]string{"auto_enabled": "0"}, 200, &settings)
	if settings["auto_enabled"] != "" || settings["default_model_id"] == "auto" || settings["default_model_id"] == "" {
		t.Fatalf("settings after disable=%v", settings)
	}
}

func TestAutoModeFirstTurnLockAndDynamicCapabilityShift(t *testing.T) {
	f := newAPIFixture(t)
	f.asAdmin()
	ctx := t.Context()

	// Seed mainstream & specialized models: Fable 5.0, Fable 5.2, Carbon, Opus 4.6, Opus 5.5, DeepSeek, Gemini Hydrogen.
	addModel := func(id, upstream, name, note string, vision bool) *store.Model {
		m := &store.Model{
			ID: id, ProviderID: "local", Kind: store.KindChat,
			UpstreamModel: upstream, DisplayName: name, RoutingNote: note,
			Vision: vision, ContextWindow: 128000, Enabled: true,
		}
		if err := f.st.UpsertModel(ctx, m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	fable50 := addModel("m-fable50", "fable-5.0", "Fable 5.0", "", true)
	fable52 := addModel("m-fable52", "fable-5.2", "Fable 5.2", "物理与硬核数理推导", true)
	carbon := addModel("m-carbon", "gemini-4-carbon", "Carbon", "医疗问题和研究", true)
	_ = addModel("m-opus46", "claude-opus-4.6", "Opus 4.6", "", true)
	opus55 := addModel("m-opus55", "claude-opus-5.5", "Opus 5.5", "", true)
	deepseek := addModel("m-deepseek", "deepseek-chat", "DeepSeek", "日常聊天", false)
	hydrogen := addModel("m-hydrogen", "gemini-4-hydrogen", "Gemini 氢气", "孩子中小学数学题与快速问答", true)
	_ = fable50
	_ = opus55

	var judgeCalls atomic.Int32
	var lastJudgeUserPrompt string
	f.up.mu.Lock()
	f.up.routerFunc = func(req upstreamRequest) string {
		judgeCalls.Add(1)
		var uText string
		if len(req.Messages) > 1 {
			uText = string(req.Messages[1].Content)
		}
		lastJudgeUserPrompt = uText
		latest := uText
		if idx := strings.Index(uText, "用户最新提问："); idx >= 0 {
			latest = uText[idx:]
		}
		switch {
		case strings.Contains(latest, "三年级数学"):
			return fmt.Sprintf(`{"action": "switch", "pick": %q, "reason": "转为辅导孩子数学题，降级氢气模型"}`, hydrogen.ID)
		case strings.Contains(latest, "洛伦兹因子"):
			return fmt.Sprintf(`{"action": "keep", "pick": %q, "reason": "仍在讨论物理"}`, fable52.ID)
		case strings.Contains(latest, "狭义相对论"):
			return fmt.Sprintf(`{"pick": %q, "reason": "硬核物理问题选 Fable 5.2"}`, fable52.ID)
		case strings.Contains(latest, "血常规"):
			return fmt.Sprintf(`{"pick": %q, "reason": "医疗体检选 Carbon"}`, carbon.ID)
		default:
			return fmt.Sprintf(`{"pick": %q, "reason": "日常聊天"}`, deepseek.ID)
		}
	}
	f.up.mu.Unlock()

	// 1. Enable Auto mode WITHOUT dynamic switching (first-question lock for KV cache hit).
	f.json("PUT", "/api/admin/settings", map[string]string{
		"auto_enabled":  "1",
		"auto_model_id": "test-model",
		"auto_dynamic":  "0",
	}, 200, nil)

	res1 := f.send(map[string]any{"model_id": "auto", "content": "帮我看看血常规白细胞偏高是怎么回事"})
	_, done1, _ := f.stream(res1.Assistant.ID).finish()
	if done1.ModelID == nil || *done1.ModelID != carbon.ID {
		t.Fatalf("turn 1 expected Carbon, got %+v", done1.ModelID)
	}
	// Verify prompt passed admin routing_note and Opus 5.5 newest-in-series hint to the Judge LLM.
	if !strings.Contains(lastJudgeUserPrompt, "医疗问题和研究") || !strings.Contains(lastJudgeUserPrompt, "同系最新版(v5.5") || !strings.Contains(lastJudgeUserPrompt, "同系已有新版 v5.5") {
		t.Fatalf("judge prompt missing admin note or version hierarchy:\n%s", lastJudgeUserPrompt)
	}
	callsAfterTurn1 := judgeCalls.Load()
	if callsAfterTurn1 != 1 {
		t.Fatalf("expected 1 judge call, got %d", callsAfterTurn1)
	}

	// Turn 2 with auto_dynamic=0: even when topic changes to kid's math, model stays locked on Carbon (0 extra judge calls!).
	res2 := f.send(map[string]any{"conversation_id": res1.Conversation.ID, "model_id": "auto", "content": "帮我给孩子讲一道三年级数学应用题"})
	_, done2, _ := f.stream(res2.Assistant.ID).finish()
	if done2.ModelID == nil || *done2.ModelID != carbon.ID || judgeCalls.Load() != callsAfterTurn1 {
		t.Fatalf("expected locked model Carbon with 0 judge calls, got model=%v calls=%d", done2.ModelID, judgeCalls.Load())
	}

	// 2. Turn ON auto_dynamic ("模型能力动态更新开关")!
	f.json("PUT", "/api/admin/settings", map[string]string{"auto_dynamic": "1"}, 200, nil)

	// Start a physics conversation -> picks Fable 5.2.
	phys1 := f.send(map[string]any{"model_id": "auto", "content": "请推导狭义相对论中时间膨胀的物理原理"})
	_, physDone1, _ := f.stream(phys1.Assistant.ID).finish()
	if physDone1.ModelID == nil || *physDone1.ModelID != fable52.ID {
		t.Fatalf("expected Fable 5.2 on physics turn 1, got %v", physDone1.ModelID)
	}
	callsBeforeFollowup := judgeCalls.Load()

	// Short continuation ("为什么？") skips judge call and keeps Fable 5.2!
	phys2 := f.send(map[string]any{"conversation_id": phys1.Conversation.ID, "model_id": "auto", "content": "为什么？"})
	_, physDone2, _ := f.stream(phys2.Assistant.ID).finish()
	if physDone2.ModelID == nil || *physDone2.ModelID != fable52.ID || judgeCalls.Load() != callsBeforeFollowup {
		t.Fatalf("short follow-up should keep Fable 5.2 without judge call: model=%v calls=%d", physDone2.ModelID, judgeCalls.Load())
	}

	// Same-topic substantive question ("那洛伦兹因子在低速极限下怎么近似展开？") -> Judge returns keep -> stays Fable 5.2.
	phys3 := f.send(map[string]any{"conversation_id": phys1.Conversation.ID, "model_id": "auto", "content": "那洛伦兹因子在低速极限下怎么近似展开？"})
	_, physDone3, _ := f.stream(phys3.Assistant.ID).finish()
	if physDone3.ModelID == nil || *physDone3.ModelID != fable52.ID {
		t.Fatalf("same-topic physics question should stay on Fable 5.2, got %v", physDone3.ModelID)
	}

	// Big topic/complexity shift ("对了，后面帮我看看孩子这道三年级数学题怎么做") -> dynamically downgrades to Gemini Hydrogen!
	mathTurn := f.send(map[string]any{"conversation_id": phys1.Conversation.ID, "model_id": "auto", "content": "对了，后面帮我看看孩子这道三年级数学题怎么做：鸡兔同笼共10个头28只脚"})
	_, mathDone, _ := f.stream(mathTurn.Assistant.ID).finish()
	if mathDone.ModelID == nil || *mathDone.ModelID != hydrogen.ID {
		t.Fatalf("expected dynamic downgrade to Gemini Hydrogen, got %v", mathDone.ModelID)
	}

	// Conversation stored on server retains auto_mode=true and updated model_id=hydrogen.ID.
	convStored, err := f.st.GetConversation(ctx, phys1.Conversation.ID, f.user.ID)
	if err != nil || !convStored.AutoMode || convStored.ModelID == nil || *convStored.ModelID != hydrogen.ID {
		t.Fatalf("stored conv=%+v err=%v", convStored, err)
	}
}

type stubJEVRouter struct {
	pickID string
}

func (r stubJEVRouter) Kind() string { return chat.RouterKindJEV }
func (r stubJEVRouter) Route(_ context.Context, req chat.RouteRequest) (*chat.RouteDecision, error) {
	for _, c := range req.Candidates {
		if c.Model.ID == r.pickID {
			return &chat.RouteDecision{Model: c.Model, Provider: c.Provider, Switched: true, Reason: "jev_classifier", RouterKind: chat.RouterKindJEV}, nil
		}
	}
	return chat.HeuristicRoute(req), nil
}

func TestHeuristicFallbackAndPluggableJEVRouter(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()

	carbon := &store.Model{ID: "m-carbon", ProviderID: "local", Kind: store.KindChat, UpstreamModel: "gemini-4-carbon", DisplayName: "Carbon", RoutingNote: "医疗问题和研究", Vision: true, ContextWindow: 128000, Enabled: true}
	dsFlash := &store.Model{ID: "m-ds-flash", ProviderID: "local", Kind: store.KindChat, UpstreamModel: "deepseek-flash", DisplayName: "DeepSeek Flash", RoutingNote: "日常聊天", Vision: false, ContextWindow: 128000, Enabled: true}
	for _, m := range []*store.Model{carbon, dsFlash} {
		if err := f.st.UpsertModel(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.st.SetSetting(ctx, chat.SettingAutoEnabled, "1")
	_ = f.st.SetSetting(ctx, chat.SettingAutoModelID, "test-model")

	// When judge returns non-JSON text (default fakeUpstream title reply), HeuristicRoute still routes accurately!
	medRes := f.send(map[string]any{"model_id": "auto", "content": "体检报告显示甘油三酯偏高，需要吃药吗？"})
	_, medDone, _ := f.stream(medRes.Assistant.ID).finish()
	if medDone.ModelID == nil || *medDone.ModelID != carbon.ID {
		t.Fatalf("heuristic medical expected Carbon, got %v", medDone.ModelID)
	}

	dailyRes := f.send(map[string]any{"model_id": "auto", "content": "早上好，今天中午吃什么家常菜好？"})
	_, dailyDone, _ := f.stream(dailyRes.Assistant.ID).finish()
	if dailyDone.ModelID == nil || *dailyDone.ModelID != dsFlash.ID {
		t.Fatalf("heuristic daily expected DeepSeek Flash, got %v", dailyDone.ModelID)
	}

	// Image upload in Auto mode never picks a non-vision model like DeepSeek Flash!
	file, _ := f.uploadPNG()
	imgRes := f.send(map[string]any{"model_id": "auto", "content": "早上好，看看这张图", "attachments": []store.Attachment{{FileID: file.ID}}})
	_, imgDone, _ := f.stream(imgRes.Assistant.ID).finish()
	if imgDone.ModelID == nil || *imgDone.ModelID == dsFlash.ID {
		t.Fatalf("image turn must not pick blind model, got %v", imgDone.ModelID)
	}

	// Registering a JEV router implementation plugs right into chat.Service without changing callers.
	f.svc.RegisterRouter(stubJEVRouter{pickID: dsFlash.ID})
	_ = f.st.SetSetting(ctx, chat.SettingAutoRouterKind, chat.RouterKindJEV)
	jevRes := f.send(map[string]any{"model_id": "auto", "content": "测试接入 JEV 路由"})
	_, jevDone, _ := f.stream(jevRes.Assistant.ID).finish()
	if jevDone.ModelID == nil || *jevDone.ModelID != dsFlash.ID {
		t.Fatalf("expected JEV router pick %s, got %v", dsFlash.ID, jevDone.ModelID)
	}
}
