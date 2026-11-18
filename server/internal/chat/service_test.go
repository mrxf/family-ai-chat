package chat

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nuan/internal/memory"
	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

func contextFixture(t *testing.T) (*Service, *store.User, *store.Conversation) {
	t.Helper()
	dir := t.TempDir()
	st := storetest.Open(t)
	u := &store.User{Username: "owner", DisplayName: "家人", Role: "member"}
	if err := st.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	c := &store.Conversation{UserID: u.ID, Title: "existing"}
	if err := st.CreateConversation(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(st, dir, log)
	svc.Memory = memory.New(st, svc.ResolveMemoryModel, log)
	return svc, u, c
}

func contextHistory(t *testing.T, s *Service, c *store.Conversation, ms ...*store.Message) string {
	t.Helper()
	placeholder := &store.Message{Role: "assistant", Status: store.StatusStreaming}
	ms = append(ms, placeholder)
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{ConversationID: c.ID, Append: ms}); err != nil {
		t.Fatal(err)
	}
	return placeholder.ID
}

func TestBuildRequestKeepsOnlyFourRecentImageMessages(t *testing.T) {
	s, u, c := contextFixture(t)
	path := filepath.Join(s.DataDir, "image.png")
	if err := os.WriteFile(path, []byte("test image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	f := &store.File{UserID: u.ID, Mime: "image/png", Path: path}
	if err := s.Store.CreateFile(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	var history []*store.Message
	for i := 0; i < 6; i++ {
		// Two attachments in one message count as one image-bearing message.
		history = append(history, &store.Message{Role: "user", Content: strings.Repeat("图", i+1), Status: store.StatusDone, Attachments: []store.Attachment{{FileID: f.ID}, {FileID: f.ID}}})
	}
	id := contextHistory(t, s, c, history...)
	model := &store.Model{UpstreamModel: "fake", Vision: true, ContextWindow: 128000, MaxOutput: 100}
	req, err := s.buildRequest(t.Context(), u, c, id, model, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 7 || req.Messages[0].Role != "system" {
		t.Fatalf("messages=%+v", req.Messages)
	}
	for i, m := range req.Messages[1:] {
		if i < 2 {
			if len(m.Parts) != 0 || !strings.Contains(m.Content, "2 张图片，当前已不可见") {
				t.Errorf("old images retained: %+v", m)
			}
		} else {
			if len(m.Parts) != 3 || m.Parts[0].Type != "image_url" || m.Parts[1].Type != "image_url" || m.Parts[0].ImageURL.URL != "data:image/png;base64,dGVzdCBpbWFnZSBieXRlcw==" || !strings.HasSuffix(m.Parts[2].Text, history[i].Content) {
				t.Errorf("recent image message=%+v", m)
			}
		}
	}
	model.Vision = false
	req, err = s.buildRequest(t.Context(), u, c, id, model, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range req.Messages[1:] {
		if len(m.Parts) != 0 || !strings.Contains(m.Content, "当前已不可见") {
			t.Errorf("nonvision message=%+v", m)
		}
	}
}

func TestBuildRequestTrimsBudgetAndDropsLeadingAssistant(t *testing.T) {
	s, u, c := contextFixture(t)
	old := &store.Message{Role: "user", Content: strings.Repeat("旧", 100), Status: store.StatusDone}
	answer := &store.Message{Role: "assistant", Content: "上轮答复", Status: store.StatusDone}
	latest := &store.Message{Role: "user", Content: "最新问题", Status: store.StatusDone}
	id := contextHistory(t, s, c, old, answer, latest)
	reserve := 100
	model := &store.Model{UpstreamModel: "fake", MaxOutput: reserve, ContextWindow: reserve + estimateTokens(s.systemPrompt(t.Context(), u, c, false)) + estimateTokens(answer.Content) + estimateTokens(latest.Content)}
	req, err := s.buildRequest(t.Context(), u, c, id, model, false)
	if err != nil {
		t.Fatal(err)
	}
	// The older user cannot fit. Its now-leading assistant must also be discarded.
	if len(req.Messages) != 2 || req.Messages[1].Role != "user" || !strings.HasSuffix(req.Messages[1].Content, latest.Content) || req.MaxTokens != reserve {
		t.Fatalf("trimmed request=%+v", req)
	}
	if !strings.Contains(req.Messages[0].Content, u.DisplayName) || !strings.Contains(req.Messages[0].Content, "当前日期") {
		t.Fatalf("missing user/time system context: %s", req.Messages[0].Content)
	}
	model.ContextWindow = 1
	req, err = s.buildRequest(t.Context(), u, c, id, model, false)
	if err != nil || len(req.Messages) != 2 || !strings.HasSuffix(req.Messages[1].Content, latest.Content) {
		t.Fatalf("newest user must survive small budget: %+v err=%v", req, err)
	}
}

func TestBuildRequestSkipsFailedAndEmptyAssistantMessages(t *testing.T) {
	s, u, c := contextFixture(t)
	id := contextHistory(t, s, c,
		&store.Message{Role: "user", Content: "first", Status: store.StatusDone},
		&store.Message{Role: "assistant", Content: "failed partial", Status: store.StatusError},
		&store.Message{Role: "assistant", Content: "  ", Status: store.StatusCancelled},
		&store.Message{Role: "user", Content: "second", Status: store.StatusDone},
		&store.Message{Role: "assistant", Content: "useful partial", Status: store.StatusCancelled},
		&store.Message{Role: "user", Content: "latest", Status: store.StatusDone},
	)
	req, err := s.buildRequest(t.Context(), u, c, id, &store.Model{ContextWindow: 128000}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 5 {
		t.Fatalf("messages=%+v", req.Messages)
	}
	for i, want := range []string{"first", "second", "useful partial", "latest"} {
		if !strings.HasSuffix(req.Messages[i+1].Content, want) {
			t.Fatalf("message %d=%+v want %q", i+1, req.Messages[i+1], want)
		}
	}
}
