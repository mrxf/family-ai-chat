package store_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

func TestMemoriesCRUDIsolationAndConversationDelete(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	a := &store.User{Role: "member", Username: "a"}
	b := &store.User{Role: "member", Username: "b"}
	for _, u := range []*store.User{a, b} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	c := &store.Conversation{UserID: a.ID}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	fact := &store.Memory{UserID: a.ID, Category: "health", Content: "用户有高血压", Source: "manual", Pinned: true, SourceConvID: &c.ID, Embedding: []float32{1, -0.25, 0.125}, EmbeddingModel: "e"}
	ep := &store.Memory{UserID: a.ID, Category: "episode", Content: "聊过血压", SourceConvID: &c.ID, Pinned: true}
	if err := s.CreateMemories(ctx, []*store.Memory{fact, ep}); err != nil {
		t.Fatal(err)
	}
	if ep.Pinned {
		t.Fatal("episode pinned")
	}
	ms, err := s.ListMemories(ctx, a.ID)
	if err != nil || len(ms) != 2 || ms[0].ID != fact.ID || !reflect.DeepEqual(ms[0].Embedding, fact.Embedding) {
		t.Fatalf("list=%+v err=%v", ms, err)
	}
	if ms, err := s.ListMemories(ctx, b.ID); err != nil || len(ms) != 0 {
		t.Fatal(ms, err)
	}
	if _, err := s.GetMemory(ctx, fact.ID, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	text := "用户遵医嘱停药"
	category := "ongoing"
	if _, err := s.UpdateMemory(ctx, fact.ID, b.ID, nil, &text); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	m, err := s.UpdateMemory(ctx, fact.ID, a.ID, &category, &text)
	if err != nil || m.Content != text || len(m.Embedding) > 0 || m.EmbeddingModel != "" {
		t.Fatal(m, err)
	}
	pending, err := s.PendingEmbeddings(ctx, "new")
	if err != nil || len(pending) != 2 {
		t.Fatal(pending, err)
	}
	if err := s.SetMemoryEmbedding(ctx, m, []float32{2, 3}, "new"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMemory(ctx, fact.ID, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.DeleteConversation(ctx, c.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	ms, err = s.ListMemories(ctx, a.ID)
	if err != nil || len(ms) != 1 || ms[0].SourceConvID != nil {
		t.Fatal(ms, err)
	}
	if err := s.DeleteMemory(ctx, fact.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMemories(ctx, []*store.Memory{{UserID: a.ID, Category: "profile", Content: "用户退休"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearMemories(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	ms, err = s.ListMemories(ctx, a.ID)
	if err != nil || len(ms) != 0 {
		t.Fatal(ms, err)
	}
}
func TestMemoryExtractionSupersedeAndStaleSnapshot(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	u := &store.User{Role: "member", Username: "a"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	c := &store.Conversation{UserID: u.ID}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	msg := &store.Message{Role: "user", Content: "用户搬家", Status: store.StatusDone}
	if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{msg}}); err != nil {
		t.Fatal(err)
	}
	old := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户住在上海", Pinned: true}
	if err := s.CreateMemories(ctx, []*store.Memory{old}); err != nil {
		t.Fatal(err)
	}
	next := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户住在杭州", Pinned: true}
	ms, _ := s.MemoryMessages(ctx, c.ID, "")
	snap := store.MemorySnapshot{ConversationID: c.ID, UserID: u.ID, Messages: ms}
	changes := []store.MemoryChange{{Op: "update", RefID: old.ID, Memory: next}}
	bad := snap
	bad.Cursor = "wrong"
	if err := s.ApplyMemoryExtraction(ctx, bad, changes, ""); !errors.Is(err, store.ErrMemoryStale) {
		t.Fatal(err)
	}
	if cursor, _ := s.MemoryCursor(ctx, c.ID); cursor != "" {
		t.Fatal(cursor)
	}
	if err := s.ApplyMemoryExtraction(ctx, snap, changes, "2026-11-17 搬家了"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMemory(ctx, old.ID, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if m, err := s.GetMemory(ctx, next.ID, u.ID); err != nil || m.Content != next.Content {
		t.Fatal(m, err)
	}
	if err := s.ApplyMemoryExtraction(ctx, snap, nil, ""); !errors.Is(err, store.ErrMemoryStale) {
		t.Fatal(err)
	}
	cursor, _ := s.MemoryCursor(ctx, c.ID)
	if cursor != msg.ID {
		t.Fatal(cursor)
	}
	// Editing/deleting an input after inference invalidates the entire result.
	more := &store.Message{Role: "user", Content: "用户的血压正常", Status: store.StatusDone}
	if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{more}}); err != nil {
		t.Fatal(err)
	}
	ms, _ = s.MemoryMessages(ctx, c.ID, cursor)
	snap = store.MemorySnapshot{ConversationID: c.ID, UserID: u.ID, Cursor: cursor, Messages: ms}
	if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, TruncateFrom: more.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyMemoryExtraction(ctx, snap, []store.MemoryChange{{Op: "remove", RefID: next.ID}}, ""); !errors.Is(err, store.ErrMemoryStale) {
		t.Fatal(err)
	}
	if _, err := s.GetMemory(ctx, next.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MemoryCursor(ctx, c.ID); got != cursor {
		t.Fatal(got)
	}
}

func TestMemoryCandidatesIncludeErrorRepliesAndExcludeStreaming(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	u := &store.User{Username: "u", Role: "member"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	cs := []*store.Conversation{}
	for i := 0; i < 3; i++ {
		c := &store.Conversation{UserID: u.ID}
		if err := s.CreateConversation(ctx, c); err != nil {
			t.Fatal(err)
		}
		status := []string{store.StatusError, store.StatusInterrupted, store.StatusStreaming}[i]
		if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{{Role: "user", Content: "真实事实", Status: store.StatusDone}, {Role: "assistant", Status: status}}}); err != nil {
			t.Fatal(err)
		}
		cs = append(cs, c)
	}
	candidates, err := s.MemoryCandidates(ctx, store.Now())
	if err != nil || len(candidates) != 2 || candidates[0].ID != cs[0].ID || candidates[1].ID != cs[1].ID {
		t.Fatal(candidates, err)
	}
	candidates, err = s.MemoryCandidates(ctx, store.Now()-60000)
	if err != nil || len(candidates) != 0 {
		t.Fatal(candidates, err)
	}
	fact := &store.Memory{UserID: u.ID, Category: "health", Content: "用户有高血压"}
	if err := s.CreateMemories(ctx, []*store.Memory{fact}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	ms, err := s.ListMemories(ctx, u.ID)
	if err != nil || len(ms) != 0 {
		t.Fatal(ms, err)
	}
}

func TestSettingsToggleWhileExtractingDiscardsWrites(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	u := &store.User{Username: "u", Role: "member"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	c := &store.Conversation{UserID: u.ID}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{{Role: "user", Content: "记住我退休了", Status: store.StatusDone}}}); err != nil {
		t.Fatal(err)
	}
	ms, err := s.MemoryMessages(ctx, c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	snap := store.MemorySnapshot{ConversationID: c.ID, UserID: u.ID, Messages: ms}
	if _, err := s.UpdateUser(ctx, u.ID, store.UserPatch{Settings: json.RawMessage(`{"memory_enabled":false}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyMemoryExtraction(ctx, snap, []store.MemoryChange{{Op: "add", Memory: &store.Memory{UserID: u.ID, Category: "profile", Content: "用户退休了"}}}, "退休了"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListMemories(ctx, u.ID)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	cursor, err := s.MemoryCursor(ctx, c.ID)
	if err != nil || cursor != ms[0].ID {
		t.Fatal(cursor, err)
	}
}

func TestMemoryArchiveAndLittleEndianVector(t *testing.T) {
	if got := store.EncodeEmbedding([]float32{1, -2}); !reflect.DeepEqual(got, []byte{0, 0, 128, 63, 0, 0, 0, 192}) {
		t.Fatal(got)
	}
	s := storetest.Open(t)
	ctx := t.Context()
	u := &store.User{Username: "u", Role: "member"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	c := &store.Conversation{UserID: u.ID}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	old := &store.Memory{UserID: u.ID, Category: "ongoing", Content: "用户在装修"}
	if err := s.CreateMemories(ctx, []*store.Memory{old}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{{Role: "user", Content: "装修完成了", Status: store.StatusDone}}}); err != nil {
		t.Fatal(err)
	}
	ms, err := s.MemoryMessages(ctx, c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyMemoryExtraction(ctx, store.MemorySnapshot{ConversationID: c.ID, UserID: u.ID, Messages: ms}, []store.MemoryChange{{Op: "remove", RefID: old.ID}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMemory(ctx, old.ID, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	// Physical deletion still affects the archived row, proving extraction retained it.
	if err := s.DeleteMemory(ctx, old.ID, u.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConversationDeleteSerializesWithExtraction(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	u := &store.User{Username: "u", Role: "member"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		c := &store.Conversation{UserID: u.ID}
		if err := s.CreateConversation(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{{Role: "user", Content: "聊一聊", Status: store.StatusDone}}}); err != nil {
			t.Fatal(err)
		}
		ms, err := s.MemoryMessages(ctx, c.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		extracted := make(chan error, 1)
		deleted := make(chan error, 1)
		go func() {
			<-start
			extracted <- s.ApplyMemoryExtraction(ctx, store.MemorySnapshot{ConversationID: c.ID, UserID: u.ID, Messages: ms}, nil, "2026-11-17 聊了健康")
		}()
		go func() { <-start; deleted <- s.DeleteConversation(ctx, c.ID, u.ID) }()
		close(start)
		if err := <-extracted; err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		if err := <-deleted; err != nil {
			t.Fatal(err)
		}
		memories, err := s.ListMemories(ctx, u.ID)
		if err != nil || len(memories) != 0 {
			t.Fatalf("orphan episode: %+v %v", memories, err)
		}
	}
}
