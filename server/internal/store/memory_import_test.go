package store_test

import (
	"strings"
	"testing"

	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

func TestImportMemoriesDedupReplaceIsolationAndDemotion(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	a := &store.User{Role: "member", Username: "a"}
	b := &store.User{Role: "member", Username: "b"}
	for _, u := range []*store.User{a, b} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	old := &store.Memory{UserID: a.ID, Category: "health", Content: "用户每天吃氨氯地平 5mg", Source: "auto", Pinned: true}
	have := &store.Memory{UserID: a.ID, Category: "profile", Content: "用户住在上海。", Source: "manual", Pinned: true}
	other := &store.Memory{UserID: b.ID, Category: "family", Content: "用户有女儿", Source: "auto", Pinned: true}
	if err := s.CreateMemories(ctx, []*store.Memory{old, have, other}); err != nil {
		t.Fatal(err)
	}
	items := []store.MemoryImport{
		{Memory: &store.Memory{Category: "health", Content: "用户 2025 年起改吃缬沙坦", Keywords: "降压药", Source: "import", Pinned: true}, ReplacesID: old.ID},
		{Memory: &store.Memory{Category: "profile", Content: " 用户住在上海 ", Source: "import", Pinned: true}}, // existing
		{Memory: &store.Memory{Category: "preference", Content: "用户不吃香菜", Source: "import", Pinned: true}},
		{Memory: &store.Memory{Category: "preference", Content: "用户不吃香菜！", Source: "import", Pinned: true}},                 // same batch
		{Memory: &store.Memory{Category: "family", Content: "用户有儿子", Source: "import", Pinned: true}, ReplacesID: other.ID}, // not theirs
	}
	saved, skipped, err := s.ImportMemories(ctx, a.ID, items)
	if err != nil || len(saved) != 3 || skipped != 2 {
		t.Fatal(saved, skipped, err)
	}
	if saved[0].UserID != a.ID || saved[0].Keywords != "降压药" || saved[0].Source != "import" {
		t.Fatal(saved[0])
	}
	if _, err := s.GetMemory(ctx, old.ID, a.ID); err == nil {
		t.Fatal("replaced memory still active")
	}
	if got, err := s.GetMemory(ctx, other.ID, b.ID); err != nil || got.Status != "active" {
		t.Fatal("other user's memory touched", got, err)
	}
	ms, _ := s.ListMemories(ctx, a.ID)
	if len(ms) != 4 {
		t.Fatal(ms)
	}
	// Importing the same thing again is a no-op.
	if saved, skipped, err := s.ImportMemories(ctx, a.ID, []store.MemoryImport{{Memory: &store.Memory{Category: "preference", Content: "用户不吃香菜", Source: "import", Pinned: true}}}); err != nil || len(saved) != 0 || skipped != 1 {
		t.Fatal(saved, skipped, err)
	}
	// A large import is demoted to the retrieval layer in the same transaction.
	big := []store.MemoryImport{}
	for i := range 20 {
		big = append(big, store.MemoryImport{Memory: &store.Memory{Category: "preference", Content: strings.Repeat("喜", 100) + string(rune('A'+i)), Source: "import", Pinned: true}})
	}
	if _, _, err := s.ImportMemories(ctx, a.ID, big); err != nil {
		t.Fatal(err)
	}
	ms, _ = s.ListMemories(ctx, a.ID)
	pinned := 0
	for _, m := range ms {
		if m.Pinned {
			pinned += len([]rune(m.Content))
		}
	}
	if pinned > 1500 {
		t.Fatal("not demoted", pinned)
	}
	if _, _, err := s.ImportMemories(ctx, "missing", nil); err == nil {
		t.Fatal("unknown user accepted")
	}
}

func TestMemoryKey(t *testing.T) {
	if store.MemoryKey("用户 对花生过敏。") != store.MemoryKey("对花生过敏") || store.MemoryKey("Likes TEA!") != "likestea" {
		t.Fatal(store.MemoryKey("用户 对花生过敏。"))
	}
}
