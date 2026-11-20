package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"nuan/internal/llm"
	"nuan/internal/memory"
	"nuan/internal/store"
)

func TestMemoryInjectionImageAndOptOut(t *testing.T) {
	s, u, c := contextFixture(t)
	ms := []*store.Memory{{UserID: u.ID, Category: "health", Content: "用户服用氨氯地平", Pinned: true}, {UserID: u.ID, Category: "episode", Content: "2026-11-02 聊过按摩椅", Pinned: false}}
	if err := s.Store.CreateMemories(t.Context(), ms); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.DataDir, "image.png")
	if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	f := &store.File{UserID: u.ID, Mime: "image/png", Path: path}
	if err := s.Store.CreateFile(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	msg := &store.Message{Role: "user", Content: "按摩椅", Status: store.StatusDone, Attachments: []store.Attachment{{FileID: f.ID}}}
	id := contextHistory(t, s, c, msg)
	m := &store.Model{UpstreamModel: "fake", Vision: true, ContextWindow: 128000}
	req, err := s.buildRequest(t.Context(), u, c, id, m, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Messages[0].Content, "用户服用氨氯地平") || regexp.MustCompile(`\d\d:\d\d`).MatchString(req.Messages[0].Content) {
		t.Fatal(req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[1].Parts[1].Text, "现在是") || !strings.Contains(req.Messages[1].Parts[1].Text, "聊过按摩椅") {
		t.Fatal(req.Messages[1])
	}
	persisted, err := s.Store.GetMessage(t.Context(), msg.ID)
	if err != nil || persisted.Content != "按摩椅" {
		t.Fatal(persisted, err)
	}
	u.Settings = json.RawMessage(`{"memory_enabled":false}`)
	req, err = s.buildRequest(t.Context(), u, c, id, m, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(req.Messages[0].Content, "用户服用") || strings.Contains(req.Messages[1].Parts[1].Text, "按摩椅；") || strings.Contains(req.Messages[1].Parts[1].Text, "可能相关的往事") {
		t.Fatal(req.Messages)
	}
	u.Settings = json.RawMessage(`{}`)
	a := &store.Assistant{Name: "翻译官", SystemPrompt: "请翻译", UseMemory: false}
	if err := s.Store.UpsertAssistant(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	c.AssistantID = &a.ID
	req, err = s.buildRequest(t.Context(), u, c, id, m, false)
	if err != nil || strings.Contains(req.Messages[0].Content, "用户服用") {
		t.Fatal(req, err)
	}
}

type memoryTransport func(*http.Request) (*http.Response, error)

func (f memoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestEmbeddingTimeoutFallsBackAndChatSurvivesStoreFailure(t *testing.T) {
	s, u, c := contextFixture(t)
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{{UserID: u.ID, Category: "episode", Content: "用户上次聊了按摩椅"}}); err != nil {
		t.Fatal(err)
	}
	client := llm.NewClient("", "https://fake.invalid", "")
	client.HTTP = &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	s.Memory.(*memory.Service).Resolve = func(context.Context, string) (*store.Model, *llm.Client, error) {
		return &store.Model{ID: "embed", UpstreamModel: "embed"}, client, nil
	}
	id := contextHistory(t, s, c, &store.Message{Role: "user", Content: "按摩椅", Status: store.StatusDone})
	start := time.Now()
	req, err := s.buildRequest(t.Context(), u, c, id, &store.Model{ContextWindow: 128000}, false)
	if err != nil || time.Since(start) > 2*time.Second || !strings.Contains(req.Messages[1].Content, "可能相关的往事") {
		t.Fatal(req, err, time.Since(start))
	}
	// A separate closed memory store simulates a memory-only persistence failure.
	other, _, _ := contextFixture(t)
	s.Memory.(*memory.Service).Store = other.Store
	if err := other.Store.Close(); err != nil {
		t.Fatal(err)
	}
	req, err = s.buildRequest(t.Context(), u, c, id, &store.Model{ContextWindow: 128000}, false)
	if err != nil || !strings.HasSuffix(req.Messages[1].Content, "按摩椅") {
		t.Fatal(req, err)
	}
}

func TestPrivateConversationSkipsMemoryInjectEmbedAndCleanup(t *testing.T) {
	s, u, _ := contextFixture(t)
	ms := []*store.Memory{
		{UserID: u.ID, Category: "health", Content: "用户服用氨氯地平", Pinned: true},
		{UserID: u.ID, Category: "episode", Content: "2026-11-02 聊过按摩椅", Pinned: false},
	}
	if err := s.Store.CreateMemories(t.Context(), ms); err != nil {
		t.Fatal(err)
	}

	embedCalls := 0
	client := llm.NewClient("", "https://fake.invalid", "")
	client.HTTP = &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		embedCalls++
		return nil, context.Canceled
	})}
	s.Memory.(*memory.Service).Resolve = func(context.Context, string) (*store.Model, *llm.Client, error) {
		return &store.Model{ID: "embed", UpstreamModel: "embed"}, client, nil
	}

	priv := &store.Conversation{UserID: u.ID, Private: true}
	if err := s.Store.CreateConversation(t.Context(), priv); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(s.DataDir, "priv.jpg")
	if err := os.WriteFile(imgPath, []byte("secret-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	privFile := &store.File{UserID: u.ID, Mime: "image/jpeg", Path: imgPath, Private: true}
	if err := s.Store.CreateFile(t.Context(), privFile); err != nil {
		t.Fatal(err)
	}
	id := contextHistory(t, s, priv, &store.Message{
		Role:        "user",
		Content:     "按摩椅推荐一下，记住我喜欢深灰色",
		Status:      store.StatusDone,
		Attachments: []store.Attachment{{FileID: privFile.ID, Mime: "image/jpeg"}},
	})
	req, err := s.buildRequest(t.Context(), u, priv, id, &store.Model{UpstreamModel: "fake", Vision: true, ContextWindow: 128000}, false)
	if err != nil {
		t.Fatal(err)
	}
	if embedCalls != 0 {
		t.Fatalf("expected zero embedding calls for private chat, got %d", embedCalls)
	}
	sys := req.Messages[0].Content
	if strings.Contains(sys, "用户服用氨氯地平") || strings.Contains(sys, u.DisplayName) || !strings.Contains(sys, "隐私对话模式") {
		t.Fatalf("unexpected private system prompt: %s", sys)
	}
	if strings.Contains(req.Messages[1].Parts[1].Text, "可能相关的往事") || strings.Contains(req.Messages[1].Parts[1].Text, "按摩椅；") {
		t.Fatalf("unexpected recall note in private chat: %+v", req.Messages[1])
	}

	// Fresh private chat (< TTL) must NOT be deleted by CleanupPrivate.
	if cCount, fCount, err := s.CleanupPrivate(t.Context(), 2*time.Hour, 15*time.Minute); err != nil || cCount != 0 || fCount != 0 {
		t.Fatalf("fresh private chat cleaned prematurely: convs=%d files=%d err=%v", cCount, fCount, err)
	}

	// Orphan private file older than grace is removed; normal orphan file stays for 24h cleanup.
	orphanPrivPath := filepath.Join(s.DataDir, "orphan_priv.jpg")
	orphanNormPath := filepath.Join(s.DataDir, "orphan_norm.jpg")
	_ = os.WriteFile(orphanPrivPath, []byte("p"), 0600)
	_ = os.WriteFile(orphanNormPath, []byte("n"), 0600)
	op := &store.File{UserID: u.ID, Mime: "image/jpeg", Path: orphanPrivPath, Private: true}
	on := &store.File{UserID: u.ID, Mime: "image/jpeg", Path: orphanNormPath, Private: false}
	if err := s.Store.CreateFile(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.CreateFile(t.Context(), on); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	// With zero TTL/grace, both the idle private conversation (+ its attachment) and the orphan private file are purged.
	cCount, fCount, err := s.CleanupPrivate(t.Context(), 0, 0)
	if err != nil || cCount != 1 || fCount != 2 {
		t.Fatalf("CleanupPrivate: convs=%d files=%d err=%v", cCount, fCount, err)
	}
	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		t.Fatalf("expected private conversation file removed from disk, err=%v", err)
	}
	if _, err := os.Stat(orphanPrivPath); !os.IsNotExist(err) {
		t.Fatalf("expected orphan private file removed from disk, err=%v", err)
	}
	if _, err := os.Stat(orphanNormPath); err != nil {
		t.Fatalf("normal orphan file should wait for FileGrace, err=%v", err)
	}
	if _, err := s.Store.GetConversation(t.Context(), priv.ID, u.ID); err != store.ErrNotFound {
		t.Fatalf("expected private conversation deleted, got err=%v", err)
	}
}

func TestTemporalAwarePromptBlocks30DayChatterAndFormatsRelativeDates(t *testing.T) {
	s, u, c := contextFixture(t)
	now := store.Now()
	const dayMs = int64(24 * 3600 * 1000)
	until := now + 14*dayMs

	ms := []*store.Memory{
		{
			UserID: u.ID, Category: "ongoing", Content: "2026年11月用户在准备去三亚过冬",
			Pinned: true, ValidUntil: &until, EventAt: now - 4*dayMs, CreatedAt: now - 4*dayMs,
		},
		{
			UserID: u.ID, Category: "episode", Content: "2026-11-16 聊了挑羽绒服的充绒量参数",
			Pinned: false, EventAt: now - 3*dayMs, CreatedAt: now - 3*dayMs,
		},
		{
			UserID: u.ID, Category: "episode", Content: "2026-10-01 聊了想吃铜锅涮肉但牙龈肿痛",
			Pinned: false, EventAt: now - 48*dayMs, CreatedAt: now - 48*dayMs,
		},
	}
	if err := s.Store.CreateMemories(t.Context(), ms); err != nil {
		t.Fatal(err)
	}

	// 1. Forward-looking message mentioning both 3d-old ("羽绒服") and 48d-old ("铜锅涮肉"):
	//    3d-old injects with [3天前] + anti-continuation guidance; 48d-old stays silent!
	id1 := contextHistory(t, s, c, &store.Message{
		Role:    "user",
		Content: "穿上羽绒服去吃铜锅涮肉真暖和",
		Status:  store.StatusDone,
	})
	req1, err := s.buildRequest(t.Context(), u, c, id1, &store.Model{UpstreamModel: "fake", ContextWindow: 128000}, false)
	if err != nil {
		t.Fatal(err)
	}
	sys := req1.Messages[0].Content
	if !strings.Contains(sys, "记于 ") || !strings.Contains(sys, "预计至 ") {
		t.Fatalf("system prompt core ongoing missing recorded/expiry date: %s", sys)
	}
	userWithNote := req1.Messages[1].Content
	if !strings.Contains(userWithNote, "[3天前]") || !strings.Contains(userWithNote, "充绒量") {
		t.Fatalf("missing [3天前] fresh episode in note: %s", userWithNote)
	}
	if !strings.Contains(userWithNote, "不要把旧事当成正在发生的新事追问或续聊") {
		t.Fatalf("missing anti-stale-continuation instruction in note: %s", userWithNote)
	}
	if strings.Contains(userWithNote, "牙龈肿痛") {
		t.Fatalf("48-day-old episode leaked into forward-looking chat note: %s", userWithNote)
	}

	// 2. Explicit retrospective turn ("还记得之前吃铜锅涮肉那次吗") unlocks the 48d-old episode with [1个月前]!
	c2 := &store.Conversation{UserID: u.ID}
	if err := s.Store.CreateConversation(t.Context(), c2); err != nil {
		t.Fatal(err)
	}
	id2 := contextHistory(t, s, c2, &store.Message{
		Role:    "user",
		Content: "你还记得之前聊铜锅涮肉那次吗",
		Status:  store.StatusDone,
	})
	req2, err := s.buildRequest(t.Context(), u, c2, id2, &store.Model{UpstreamModel: "fake", ContextWindow: 128000}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req2.Messages[1].Content, "[1个月前]") || !strings.Contains(req2.Messages[1].Content, "牙龈肿痛") {
		t.Fatalf("retrospective query should recall 48d episode with [1个月前]: %s", req2.Messages[1].Content)
	}
}
