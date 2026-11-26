package chat

import (
	"log/slog"
	"strings"
	"testing"

	"nuan/internal/skills"
	"nuan/internal/store"
)

func TestAppGuideSkillRegistrationAndDynamicDeviceHint(t *testing.T) {
	svc := NewService(nil, "", slog.Default())
	if svc.Skills == nil {
		t.Fatal("expected Skills registry")
	}

	normalCtx := skills.Context{
		UserID:        "u1",
		Private:       false,
		ClientContext: &ClientContext{DeviceKind: "desktop"},
	}
	def, ok := svc.Skills.Get(skillKeyAppGuide, normalCtx)
	if !ok {
		t.Fatal("expected app_guide skill to be available in normal chat")
	}
	rendered := def.RenderContent(normalCtx)
	if !strings.Contains(rendered, "`memories`") || !strings.Contains(rendered, "电脑端") {
		t.Fatalf("rendered guide missing catalog or desktop hint: %s", rendered)
	}

	mobileCtx := skills.Context{
		UserID:        "u1",
		Private:       false,
		ClientContext: &ClientContext{DeviceKind: "mobile"},
	}
	if !strings.Contains(def.RenderContent(mobileCtx), "手机端") {
		t.Fatalf("expected mobile device hint in rendered guide")
	}

	if _, ok := svc.Skills.Get(skillKeyAppGuide, skills.Context{UserID: "u1", Private: true}); ok {
		t.Fatal("expected app_guide skill to be unavailable in private chat")
	}
}

func TestExecAppGuideReadSkillAndTool(t *testing.T) {
	svc := NewService(nil, "", slog.Default())
	turn := &toolTurn{
		user:   &store.User{ID: "u1"},
		convID: "c1",
		run:    &Run{MessageID: "m1"},
	}

	out, unlocked := svc.execReadSkillCall(turn, `{"skill_key":"app_guide"}`)
	if !strings.Contains(out, "小暖使用指南") || len(unlocked) != 1 || unlocked[0].Name != readAppGuideTool.Name {
		t.Fatalf("unexpected read_skill result: unlocked=%v out=%q", unlocked, out)
	}

	doc, handled := svc.execAppGuideTool(turn, readAppGuideTool.Name, `{"topics":["memories"]}`)
	if !handled {
		t.Fatal("expected read_app_guide to be handled")
	}
	if !strings.Contains(doc, "从其他 AI 导入") || !strings.Contains(doc, "30,000") {
		t.Fatalf("expected memories guide content, got: %q", doc)
	}

	singleDoc, handled := svc.execAppGuideTool(turn, readAppGuideTool.Name, `{"topic":"settings_and_shortcuts"}`)
	if !handled || !strings.Contains(singleDoc, "快捷键") {
		t.Fatalf("expected single topic fallback to work, got: %q", singleDoc)
	}

	errDoc, handled := svc.execAppGuideTool(turn, readAppGuideTool.Name, `{}`)
	if !handled || !strings.Contains(errDoc, "参数错误") {
		t.Fatalf("expected parameter error message, got: %q", errDoc)
	}

	unknownDoc, handled := svc.execAppGuideTool(turn, readAppGuideTool.Name, `{"topics":["does_not_exist"]}`)
	if !handled || !strings.Contains(unknownDoc, "未找到") {
		t.Fatalf("expected unknown topic message, got: %q", unknownDoc)
	}
}
