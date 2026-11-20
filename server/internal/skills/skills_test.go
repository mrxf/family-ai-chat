package skills_test

import (
	"strings"
	"testing"

	"nuan/internal/llm"
	"nuan/internal/skills"
)

func TestRegistryCatalogAndVisibility(t *testing.T) {
	reg := skills.NewRegistry(skills.Definition{
		Key:         "schedule",
		Title:       "日程与提醒管理",
		Description: "为用户管理日程安排与提醒。",
		WhenToUse:   "用户提到未来的安排或要求设提醒时。",
		ToolHints: []skills.ToolHint{
			{Name: "list_schedules", Brief: "查询日程"},
			{Name: "create_schedule", Brief: "新建提醒"},
		},
		Tools: []llm.Tool{
			{Name: "list_schedules"},
			{Name: "create_schedule"},
		},
		Content: "# 日程指南\n先查后建。",
		Dynamic: func(ctx skills.Context) string {
			return "当前用户：" + ctx.UserID
		},
		Available: func(ctx skills.Context) bool {
			return !ctx.Private
		},
	})

	pubCtx := skills.Context{UserID: "u1", Private: false}
	privCtx := skills.Context{UserID: "u1", Private: true}

	if list := reg.List(privCtx); len(list) != 0 {
		t.Fatalf("expected 0 skills in private chat, got %d", len(list))
	}
	if _, ok := reg.Get("schedule", privCtx); ok {
		t.Fatal("expected schedule skill unavailable in private chat")
	}

	list := reg.List(pubCtx)
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	cat := skills.FormatCatalog(list)
	for _, want := range []string{"`schedule`（日程与提醒管理）", "`list_schedules`（查询日程）", "`create_schedule`（新建提醒）", "read_skill"} {
		if !strings.Contains(cat, want) {
			t.Fatalf("catalog missing %q:\n%s", want, cat)
		}
	}

	def, ok := reg.Get("  SCHEDULE ", pubCtx)
	if !ok {
		t.Fatal("expected case-insensitive skill lookup")
	}
	rendered := def.RenderContent(pubCtx)
	if !strings.Contains(rendered, "先查后建") || !strings.Contains(rendered, "当前用户：u1") {
		t.Fatalf("unexpected rendered content: %s", rendered)
	}

	owner, ok := reg.OwnerSkill("create_schedule")
	if !ok || owner.Key != "schedule" {
		t.Fatalf("expected OwnerSkill(create_schedule)=schedule, got %+v (%v)", owner, ok)
	}
}
