package memory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func memoryFixture(t *testing.T) (*Service, *store.User, *store.Conversation) {
	s := storetest.Open(t)
	u := &store.User{Role: "member", Username: "user", DisplayName: "家人"}
	if err := s.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	c := &store.Conversation{UserID: u.ID}
	if err := s.CreateConversation(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	return New(s, func(context.Context, string) (*store.Model, *llm.Client, error) {
		return nil, nil, errors.New("no model")
	}, logger()), u, c
}
func appendMessage(t *testing.T, s *Service, c *store.Conversation, text string) *store.Message {
	m := &store.Message{Role: "user", Content: text, Status: store.StatusDone}
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{ConversationID: c.ID, Append: []*store.Message{m}}); err != nil {
		t.Fatal(err)
	}
	return m
}
func snapshot(t *testing.T, s *Service, c *store.Conversation) store.MemorySnapshot {
	cursor, err := s.Store.MemoryCursor(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := s.Store.MemoryMessages(t.Context(), c.ID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	return store.MemorySnapshot{ConversationID: c.ID, UserID: c.UserID, Cursor: cursor, Messages: ms}
}
func TestScoresAndFallback(t *testing.T) {
	if got := KeywordScore("高血压 药物", "用户有高血压。 高血压 降压药"); got <= KeywordThreshold {
		t.Fatal(got, Terms("高血压 药物"))
	}
	if got := KeywordScore("BLOOD 120", "blood pressure 120 and many other words"); got <= KeywordThreshold {
		t.Fatal(got)
	}
	m := &store.Memory{Content: "高血压", EmbeddingModel: "old", Embedding: []float32{0, 1}}
	if score, ok := Score("高血压", []float32{1, 0}, "new", m); !ok || score <= 0 {
		t.Fatal(score, ok)
	}
	m.Content = "无关"
	if _, ok := Score("血压", nil, "", m); ok {
		t.Fatal("unrelated keyword hit")
	}
	// Weak modifier unigram ("高") must never falsely match "高血压" on "我今天很高兴".
	if _, ok := Score("我今天很高兴", nil, "", &store.Memory{Content: "用户有高血压"}); ok {
		t.Fatal("weak modifier unigram caused false hit")
	}
	// Polite natural-language sentence must still hit "柯基" without denominator collapse.
	if _, ok := Score("你还记得上次说的柯基是怎么回事吗", nil, "", &store.Memory{Content: "2026-11-17 用户询问5岁柯基雨后反复瘙痒的处理办法"}); !ok {
		t.Fatal("natural sentence missed exact entity")
	}
	// Pure semantic match at moderate cosine (0.50) with k=0 must not be killed by lexical weight.
	m.EmbeddingModel = "e"
	m.Embedding = []float32{0.5, 0.8660254} // cos([1,0], m.Embedding) == 0.50
	if _, ok := Score("毛孩子老毛病又犯了", []float32{1, 0}, "e", m); !ok {
		t.Fatal("moderate semantic match (cos=0.50, k=0) lost")
	}
	m.Embedding = []float32{0, 1}
	if _, ok := Score("血压", []float32{1, 0}, "e", m); ok {
		t.Fatal("orthogonal hit")
	}
	if Cosine([]float32{0}, []float32{1}) != 0 || Cosine([]float32{1}, nil) != 0 {
		t.Fatal("invalid vectors")
	}
}
func TestParseToleratesWrappersAndInvalidItems(t *testing.T) {
	text := "解释\n```json\n" + `{"actions":[{"op":"add","category":"health","content":"用户有高血压"},{"op":"add","category":"episode","content":"禁止"},{"op":"update","ref":0,"content":"错"},{"op":"add","category":"ongoing","content":"旅行","valid_until":"wrong"},7,{"op":"remove","ref":1}],"episode":"聊了健康"}` + "\n```多余文字"
	out, err := Parse(text, logger())
	if err != nil || len(out.Actions) != 2 || out.Episode != "聊了健康" {
		t.Fatal(out, err)
	}
	out, err = Parse(`{"actions":[{"op":"add","category":"profile","content":"`+strings.Repeat("字", 120)+`"}],"episode":""}`, logger())
	if err != nil || len([]rune(out.Actions[0].Content)) != 80 {
		t.Fatal(out, err)
	}
	for _, text := range []string{"invalid", `{"actions":null,"episode":""}`, `{"actions":[]}`, `{"actions":{},"episode":""}`} {
		if _, err := Parse(text, logger()); !errors.Is(err, ErrParse) {
			t.Fatal(text, err)
		}
	}
}
func TestApplyDeletedTargetAndConsistency(t *testing.T) {
	s, u, c := memoryFixture(t)
	m := &store.Memory{UserID: u.ID, Category: "health", Content: "旧药", Pinned: true}
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{m}); err != nil {
		t.Fatal(err)
	}
	appendMessage(t, s, c, "换药")
	snap := snapshot(t, s, c)
	if err := s.Store.DeleteMemory(t.Context(), m.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	out := Extraction{Actions: []Action{{Op: "update", Ref: 1, Content: "新药"}}}
	if err := s.apply(t.Context(), snap, out, []*store.Memory{m}); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.Store.ListMemories(t.Context(), u.ID)
	if len(ms) != 0 {
		t.Fatal("resurrected", ms)
	}
	appendMessage(t, s, c, "新事实")
	snap = snapshot(t, s, c)
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{ConversationID: c.ID, TruncateFrom: snap.Messages[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(t.Context(), snap, Extraction{Actions: []Action{{Op: "add", Category: "profile", Content: "不应出现"}}}, nil); !errors.Is(err, store.ErrMemoryStale) {
		t.Fatal(err)
	}
	cursor, _ := s.Store.MemoryCursor(t.Context(), c.ID)
	if cursor != snap.Cursor {
		t.Fatal(cursor)
	}
	ms, _ = s.Store.ListMemories(t.Context(), u.ID)
	if len(ms) != 0 {
		t.Fatal(ms)
	}
}
func TestDemotionExpirationAndCoreHardLimit(t *testing.T) {
	s, u, c := memoryFixture(t)
	past := store.Now() - 1000
	ms := []*store.Memory{{UserID: u.ID, Category: "health", Content: strings.Repeat("健", 900), Pinned: true}, {UserID: u.ID, Category: "profile", Content: strings.Repeat("我", 500), Pinned: true}, {UserID: u.ID, Category: "preference", Content: strings.Repeat("喜", 200), Pinned: true}, {UserID: u.ID, Category: "ongoing", Content: "用户正在装修", Pinned: true, ValidUntil: &past}, {UserID: u.ID, Category: "ongoing", Content: strings.Repeat("旅", 200), Pinned: true}}
	if err := s.Store.CreateMemories(t.Context(), ms); err != nil {
		t.Fatal(err)
	}
	appendMessage(t, s, c, "你好")
	if err := s.apply(t.Context(), snapshot(t, s, c), Extraction{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[2:] {
		got, err := s.Store.GetMemory(t.Context(), m.ID, u.ID)
		if err != nil || got.Pinned {
			t.Fatal(got, err)
		}
	}
	ctx := s.Inject(t.Context(), u, nil, "装修")
	if strings.Contains(ctx.Core, "装修") || !strings.Contains(ctx.Recall, "装修") {
		t.Fatal(ctx)
	}
	// An expired ongoing item is retrieval-only even before a demotion pass.
	expired := &store.Memory{UserID: u.ID, Category: "ongoing", Content: "用户准备搬家", Pinned: true, ValidUntil: &past}
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{expired}); err != nil {
		t.Fatal(err)
	}
	ctx = s.Inject(t.Context(), u, nil, "搬家")
	if strings.Contains(ctx.Core, "搬家") || !strings.Contains(ctx.Recall, "搬家") {
		t.Fatal(ctx)
	}
	huge := &store.Memory{UserID: u.ID, Category: "health", Content: strings.Repeat("病", 4000), Pinned: true}
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{huge}); err != nil {
		t.Fatal(err)
	}
	ctx = s.Inject(t.Context(), u, nil, "")
	if strings.Count(ctx.Core, "病") > 3000 {
		t.Fatal("hard limit exceeded")
	}
}
func TestWorkerExtractionFailureClassesAndEpisodeMerge(t *testing.T) {
	s, u, c := memoryFixture(t)
	appendMessage(t, s, c, "记住，我有高血压")
	var body atomic.Value
	body.Store(`{"actions":[{"op":"add","category":"health","content":"用户有高血压"}],"episode":"聊了血压"}`)
	var status atomic.Int32
	status.Store(200)
	client := llm.NewClient("", "https://test.invalid/v1", "key")
	var prompts []string
	client.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var in struct {
			Messages  []struct{ Content string }
			MaxTokens int    `json:"max_tokens"`
			Effort    string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.MaxTokens != 0 || in.Effort != "low" {
			t.Error(in)
		}
		prompts = append(prompts, in.Messages[1].Content)
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": body.Load().(string)}}}})
		return &http.Response{StatusCode: int(status.Load()), Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(_ context.Context, kind string) (*store.Model, *llm.Client, error) {
		if kind == "embedding" {
			return nil, nil, nil
		}
		return &store.Model{UpstreamModel: "fake", DefaultEffort: "low"}, client, nil
	}
	w := &Worker{service: s, failures: map[string]int{}}
	if err := w.Process(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.Store.ListMemories(t.Context(), u.ID)
	if len(ms) != 2 {
		t.Fatal(ms)
	}
	appendMessage(t, s, c, "血压控制很好")
	body.Store(`{"actions":[],"episode":"聊了血压，控制很好"}`)
	if err := w.Process(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompts[1], "聊了血压") || !strings.Contains(prompts[1], "#1 [health]") {
		t.Fatal(prompts[1])
	}
	ms, _ = s.Store.ListMemories(t.Context(), u.ID)
	if len(ms) != 2 {
		t.Fatal("episode duplicated", ms)
	}
	appendMessage(t, s, c, "再聊聊")
	before, _ := s.Store.MemoryCursor(t.Context(), c.ID)
	status.Store(429)
	for i := 0; i < 4; i++ {
		if err := w.Process(t.Context(), c); err == nil {
			t.Fatal("expected API error")
		}
	}
	if cursor, _ := s.Store.MemoryCursor(t.Context(), c.ID); cursor != before {
		t.Fatal("API errors skipped", cursor)
	}
	status.Store(200)
	body.Store("not JSON")
	for i := 0; i < 2; i++ {
		if err := w.Process(t.Context(), c); !errors.Is(err, ErrParse) {
			t.Fatal(err)
		}
	}
	if cursor, _ := s.Store.MemoryCursor(t.Context(), c.ID); cursor != before {
		t.Fatal(cursor)
	}
	if err := w.Process(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if cursor, _ := s.Store.MemoryCursor(t.Context(), c.ID); cursor == before {
		t.Fatal("third parse failure not skipped")
	}
}
func TestDisabledMemoryOnlyAdvancesCursor(t *testing.T) {
	s, u, c := memoryFixture(t)
	_, err := s.Store.UpdateUser(t.Context(), u.ID, store.UserPatch{Settings: json.RawMessage(`{"memory_enabled":false}`)})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(t, s, c, "记住我退休了")
	w := &Worker{service: s, failures: map[string]int{}}
	if err := w.Process(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if cursor, _ := s.Store.MemoryCursor(t.Context(), c.ID); cursor == "" {
		t.Fatal("not advanced")
	}
}
func TestValidUntilAndTriggers(t *testing.T) {
	s, u, c := memoryFixture(t)
	appendMessage(t, s, c, "准备出行")
	d := "2026-12-31"
	out := Extraction{Actions: []Action{{Op: "add", Category: "ongoing", Content: "用户准备旅行", ValidUntil: &d}}}
	if err := s.apply(t.Context(), snapshot(t, s, c), out, nil); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.Store.ListMemories(t.Context(), u.ID)
	expected := time.Date(2027, 1, 1, 0, 0, 0, 0, zone).UnixMilli() - 1
	if len(ms) != 1 || ms[0].ValidUntil == nil || *ms[0].ValidUntil != expected {
		t.Fatal(ms)
	}
	for _, v := range []string{"记住", "别忘", "以后", "停药", "换药", "不吃了", "过敏", "搬家", "生日", "去世", "住院", "出院", "怀孕", "忘掉", "别记"} {
		if !Trigger(v) {
			t.Fatal(v)
		}
	}
}

func TestEmbeddingBackfillBatchesAndEditGuard(t *testing.T) {
	s, u, _ := memoryFixture(t)
	ms := []*store.Memory{}
	for i := 0; i < 35; i++ {
		ms = append(ms, &store.Memory{UserID: u.ID, Category: "profile", Content: "用户退休", Embedding: []float32{1}, EmbeddingModel: "old"})
	}
	if err := s.Store.CreateMemories(t.Context(), ms); err != nil {
		t.Fatal(err)
	}
	client := llm.NewClient("", "https://test.invalid", "")
	batches := []int{}
	client.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var in struct{ Input []string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		batches = append(batches, len(in.Input))
		data := []map[string]any{}
		for i := range in.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float32{1, 2}})
		}
		out, _ := json.Marshal(map[string]any{"data": data})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(out))), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(context.Context, string) (*store.Model, *llm.Client, error) {
		return &store.Model{ID: "new", UpstreamModel: "new"}, client, nil
	}
	for i := 0; i < 2; i++ {
		if err := s.FillEmbeddings(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(batches) != 2 || batches[0] != 32 || batches[1] != 3 {
		t.Fatal(batches)
	}
	got, err := s.Store.GetMemory(t.Context(), ms[0].ID, u.ID)
	if err != nil || got.EmbeddingModel != "new" || len(got.Embedding) != 2 {
		t.Fatal(got, err)
	}
	text := "用户仍在工作"
	if _, err := s.Store.UpdateMemory(t.Context(), got.ID, u.ID, nil, &text); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetMemoryEmbedding(t.Context(), got, []float32{5, 6}, "new"); err != nil {
		t.Fatal(err)
	}
	got, err = s.Store.GetMemory(t.Context(), got.ID, u.ID)
	if err != nil || len(got.Embedding) > 0 {
		t.Fatal("stale vector overwrote edit", got, err)
	}
}
func TestWorkerSerializesUsersAndCapsGlobalConcurrency(t *testing.T) {
	s, u, c := memoryFixture(t)
	c2 := &store.Conversation{UserID: u.ID}
	if err := s.Store.CreateConversation(t.Context(), c2); err != nil {
		t.Fatal(err)
	}
	b := &store.User{Username: "b", Role: "member"}
	if err := s.Store.CreateUser(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	c3 := &store.Conversation{UserID: b.ID}
	if err := s.Store.CreateConversation(t.Context(), c3); err != nil {
		t.Fatal(err)
	}
	d := &store.User{Username: "d", Role: "member"}
	if err := s.Store.CreateUser(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	c4 := &store.Conversation{UserID: d.ID}
	if err := s.Store.CreateConversation(t.Context(), c4); err != nil {
		t.Fatal(err)
	}
	convs := []*store.Conversation{c, c2, c3, c4}
	for i, cv := range convs {
		appendMessage(t, s, cv, []string{"FIRST", "SECOND", "OTHER", "THIRD"}[i])
	}
	entered := make(chan string, 10)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	client := llm.NewClient("", "https://test.invalid", "")
	client.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var in struct{ Messages []struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		n := active.Add(1)
		for {
			old := maxActive.Load()
			if n <= old || maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- in.Messages[1].Content
		select {
		case <-release:
		case <-r.Context().Done():
			active.Add(-1)
			return nil, r.Context().Err()
		}
		active.Add(-1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"actions\":[],\"episode\":\"\"}"}}]}`)), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(_ context.Context, kind string) (*store.Model, *llm.Client, error) {
		if kind == "embedding" {
			return nil, nil, nil
		}
		return &store.Model{UpstreamModel: "fake"}, client, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &Worker{service: s, ctx: ctx, busy: map[string]bool{}, failures: map[string]int{}, slots: make(chan struct{}, 2)}
	immediate := map[string]bool{}
	for _, c := range convs {
		immediate[c.ID] = true
	}
	w.scan(immediate)
	wait := func() string {
		select {
		case prompt := <-entered:
			return prompt
		case <-time.After(2 * time.Second):
			t.Fatal("extraction never started")
			return ""
		}
	}
	first, second := wait(), wait()
	if strings.Contains(first, "SECOND") || strings.Contains(second, "SECOND") {
		t.Fatal("same user ran concurrently", first, second)
	}
	w.scan(immediate)
	if active.Load() != 2 {
		t.Fatal(active.Load())
	}
	close(release)
	w.wg.Wait()
	if maxActive.Load() != 2 {
		t.Fatal(maxActive.Load())
	}
	w.scan(immediate)
	w.wg.Wait()
	for _, c := range convs {
		cursor, err := s.Store.MemoryCursor(t.Context(), c.ID)
		if err != nil || cursor == "" {
			t.Fatal(c.ID, cursor, err)
		}
	}
}
func TestSegmentChronologyAndCharacterBudget(t *testing.T) {
	a := &store.Message{Role: "user", Content: strings.Repeat("字", 5000)}
	b := &store.Message{Role: "assistant", Content: strings.Repeat("答", 5000)}
	c := &store.Message{Role: "user", Content: strings.Repeat("字", 4000)}
	got := segment([]*store.Message{a, b, c})
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatal(got)
	}
}

func TestOversizedMessageChunksCommitAtomically(t *testing.T) {
	s, u, c := memoryFixture(t)
	appendMessage(t, s, c, strings.Repeat("甲", 14000)+"最后一条事实")
	client := llm.NewClient("", "https://test.invalid", "")
	calls := 0
	client.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		var in struct{ Messages []struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		prompt := in.Messages[1].Content
		if len([]rune(prompt)) > 8000 {
			t.Fatal("oversized extraction request", len([]rune(prompt)))
		}
		if calls > 1 && !strings.Contains(prompt, "#1 [health] 用户有高血压") {
			t.Fatal("draft lost", prompt)
		}
		out := `{"actions":[{"op":"add","category":"health","content":"用户有高血压"}],"episode":"聊了健康"}`
		if calls > 1 {
			out = `{"actions":[],"episode":"聊了健康"}`
		}
		if calls == 3 && !strings.Contains(prompt, "最后一条事实") {
			t.Fatal("tail lost")
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": out}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(_ context.Context, kind string) (*store.Model, *llm.Client, error) {
		if kind == "embedding" {
			return nil, nil, nil
		}
		return &store.Model{UpstreamModel: "fake"}, client, nil
	}
	w := &Worker{service: s, failures: map[string]int{}}
	if err := w.Process(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	ms, err := s.Store.ListMemories(t.Context(), u.ID)
	if err != nil || calls != 3 || len(ms) != 2 {
		t.Fatal(calls, ms, err)
	}
}

func TestEpisodeDateStripped(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-18 2026-11-18 聊了按摩椅": "聊了按摩椅",
		"2026-11-18：聊了按摩椅":            "聊了按摩椅",
		"聊了 2026-11-18 的体检":           "聊了 2026-11-18 的体检",
	} {
		if got := episodeDate.ReplaceAllString(in, ""); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestCorgiColloquialAndMultiTurnRecallWithoutEmbeddings(t *testing.T) {
	s, u, oldConv := memoryFixture(t)
	// Seed core fact + 24 episodes (legacy state: keywords == "", embedding == nil).
	mems := []*store.Memory{
		{UserID: u.ID, Category: "family", Content: "用户家养了一只五岁的柯基。", Pinned: true},
		{UserID: u.ID, Category: "episode", Content: "2026-11-17 用户询问无糖可乐的甜味来源、糖精是什么及其制造方式，并追问是否有化学家尝过氢氟酸；讨论提到舍勒，但具体事迹缺乏可靠证据。"},
		{UserID: u.ID, Category: "episode", Content: "2026-11-17 用户询问5岁柯基雨后反复瘙痒的处理办法，因上次检查和药浴费用较高不想再就医；讨论了雨后吹干、暂不自行用药，以及先问清检查内容和剃毛范围。", SourceConvID: &oldConv.ID},
		{UserID: u.ID, Category: "episode", Content: "2026-11-17 用户请小暖讲有意思的历史故事；小暖讲了法国药剂师巴门蒂耶推广土豆的轶事。"},
	}
	for i := 0; i < 21; i++ {
		mems = append(mems, &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-11-10 用户询问家常菜谱与周末购物安排。"})
	}
	if err := s.Store.CreateMemories(t.Context(), mems); err != nil {
		t.Fatal(err)
	}

	// Turn 1: "小狗痒痒还在继续" (pure SQLite, zero embeddings, empty keywords) must recall the Corgi episode!
	turn1 := s.InjectTurn(t.Context(), u, InjectRequest{
		ConversationID:    "new-conv",
		ConversationTitle: "",
		UserMessages:      []string{"小狗痒痒还在继续"},
	})
	if !strings.Contains(turn1.Recall, "雨后反复瘙痒") || !strings.Contains(turn1.Recall, "[conv:"+oldConv.ID+"]") {
		t.Fatalf("Turn 1 missed Corgi itching episode: %s", turn1.Recall)
	}

	// Turn 2: pronoun follow-up "你还记得它为啥痒痒啊？" (>8 runes) retains multi-turn + title context.
	turn2 := s.InjectTurn(t.Context(), u, InjectRequest{
		ConversationID:    "new-conv",
		ConversationTitle: "小狗持续瘙痒",
		UserMessages:      []string{"小狗痒痒还在继续", "你还记得它为啥痒痒啊？"},
	})
	if !strings.Contains(turn2.Recall, "雨后反复瘙痒") {
		t.Fatalf("Turn 2 pronoun follow-up missed Corgi episode: %s", turn2.Recall)
	}

	// Turn 3: meta-query "我看你的记忆里聊过的事有24条，你看不到？" -> Recall(mode="recent") lists all 24!
	recent := s.Recall(t.Context(), u, nil, "new-conv", RecallRequest{Mode: "recent", Category: "episode", Limit: 30})
	if !strings.Contains(recent, "聊过的事 24") || !strings.Contains(recent, "显示 24/24 条") || !strings.Contains(recent, "巴门蒂耶") {
		t.Fatalf("recent browse missed 24 episodes: %s", recent)
	}

	// When the model guesses category="health" in search mode for a pet symptom stored as "episode",
	// search mode boosts rather than hides episodes so the Corgi episode still surfaces.
	healthSearch := s.Recall(t.Context(), u, nil, "new-conv", RecallRequest{Mode: "search", Category: "health", Queries: []string{"五岁柯基 雨后 反复瘙痒"}})
	if !strings.Contains(healthSearch, "雨后反复瘙痒") {
		t.Fatalf("search with guessed category dropped matching episode: %s", healthSearch)
	}
}

func TestMultiEpisodeExtractionAndKeywordBackfill(t *testing.T) {
	s, u, c := memoryFixture(t)
	// Create a legacy episode with empty keywords (KeywordsChecked=false).
	legacy := &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-11-17 用户询问5岁柯基雨后反复瘙痒的处理办法。"}
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{legacy}); err != nil {
		t.Fatal(err)
	}
	if legacy.KeywordsChecked {
		t.Fatal("legacy empty-keyword memory should start unchecked")
	}

	// Multi-topic conversation produces 2 distinct episodes with keywords.
	appendMessage(t, s, c, "无糖可乐为什么甜？另外我家柯基淋雨又痒了")
	parsed, err := Parse(`{"actions":[],"episodes":[{"content":"询问无糖可乐的甜味来源与代糖原理。","keywords":"可乐 零度 阿斯巴甜 甜味剂"},{"content":"询问5岁柯基淋雨后瘙痒的家庭护理。","keywords":"小狗 狗狗 柯基 痒 痒痒 淋雨"}]}`, logger())
	if err != nil || len(parsed.Episodes) != 2 {
		t.Fatalf("parse episodes: %+v %v", parsed, err)
	}
	if err := s.apply(t.Context(), snapshot(t, s, c), parsed, nil); err != nil {
		t.Fatal(err)
	}
	all, err := s.Store.ListMemories(t.Context(), u.ID)
	if err != nil || len(all) != 3 {
		t.Fatalf("expected 3 episodes (1 legacy + 2 new), got %d (%v)", len(all), err)
	}

	// Background FillKeywords enriches only the 1 unchecked legacy episode, then FillEmbeddings embeds EmbedText (content + keywords).
	var embedInputs []string
	memClient := llm.NewClient("", "https://test.invalid", "")
	memClient.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			var in struct{ Input []string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			embedInputs = append(embedInputs, in.Input...)
			data := []map[string]any{}
			for i := range in.Input {
				data = append(data, map[string]any{"index": i, "embedding": []float32{0.6, 0.8}})
			}
			raw, _ := json.Marshal(map[string]any{"data": data})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
		}
		resp := `{"choices":[{"message":{"content":"{\"items\":[{\"ref\":1,\"keywords\":\"小狗 狗狗 柯基 痒 痒痒 皮肤病\"}]}"}}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(resp)), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(_ context.Context, kind string) (*store.Model, *llm.Client, error) {
		return &store.Model{ID: "m-" + kind, UpstreamModel: kind}, memClient, nil
	}
	if err := s.FillKeywords(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotLegacy, err := s.Store.GetMemory(t.Context(), legacy.ID, u.ID)
	if err != nil || !gotLegacy.KeywordsChecked || !strings.Contains(gotLegacy.Keywords, "小狗") {
		t.Fatalf("backfilled legacy=%+v err=%v", gotLegacy, err)
	}
	if err := s.FillEmbeddings(t.Context()); err != nil {
		t.Fatal(err)
	}
	joinedEmbed := strings.Join(embedInputs, "\n")
	if !strings.Contains(joinedEmbed, "小狗 狗狗 柯基") || strings.Contains(joinedEmbed, "2026-11-17") {
		t.Fatalf("EmbedText should include keywords and strip date boilerplate: %v", embedInputs)
	}
}

func TestRecallToolRawMessagesUnextractedAndIsolation(t *testing.T) {
	s, u, pastConv := memoryFixture(t)
	if _, err := s.Store.UpdateConversation(t.Context(), pastConv.ID, u.ID, store.ConversationPatch{Title: new("狗狗护理讨论")}); err != nil {
		t.Fatal(err)
	}
	// Unextracted detail only present in raw messages (never summarized into memories table!).
	if err := s.Store.ApplyTurn(t.Context(), store.Turn{
		ConversationID: pastConv.ID,
		Append: []*store.Message{
			{Role: "user", Content: "我家柯基平时吃的是渴望六种鱼狗粮，洗澡用的是维克药浴香波", Status: store.StatusDone},
			{Role: "assistant", Content: "维克药浴香波使用时要在皮肤上停留 5 到 10 分钟再冲干净。", Status: store.StatusDone},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Private conversation and another user's conversation must NEVER appear in Recall or ReadConversation.
	priv := &store.Conversation{UserID: u.ID, Private: true, Title: "隐私秘密"}
	if err := s.Store.CreateConversation(t.Context(), priv); err != nil {
		t.Fatal(err)
	}
	_ = s.Store.ApplyTurn(t.Context(), store.Turn{
		ConversationID: priv.ID,
		Append:         []*store.Message{{Role: "user", Content: "维克药浴香波的隐私备注999", Status: store.StatusDone}},
	})
	other := &store.User{Username: "other", DisplayName: "别人", Role: "member"}
	_ = s.Store.CreateUser(t.Context(), other)
	otherConv := &store.Conversation{UserID: other.ID, Title: "别人的对话"}
	_ = s.Store.CreateConversation(t.Context(), otherConv)
	_ = s.Store.ApplyTurn(t.Context(), store.Turn{
		ConversationID: otherConv.ID,
		Append:         []*store.Message{{Role: "user", Content: "别人家的维克药浴香波888", Status: store.StatusDone}},
	})

	res := s.Recall(t.Context(), u, nil, "current-conv", RecallRequest{Queries: []string{"狗粮 药浴 香波"}})
	if !strings.Contains(res, "渴望六种鱼") || !strings.Contains(res, "维克药浴香波") || !strings.Contains(res, pastConv.ID) {
		t.Fatalf("Recall missed unextracted raw message details: %s", res)
	}
	if strings.Contains(res, "999") || strings.Contains(res, "888") {
		t.Fatalf("Recall leaked private or cross-user messages: %s", res)
	}

	full := s.ReadConversation(t.Context(), u, nil, "conv:"+pastConv.ID, "香波")
	if !strings.Contains(full, "停留 5 到 10 分钟") {
		t.Fatalf("ReadConversation missed assistant detail: %s", full)
	}
	if denied := s.ReadConversation(t.Context(), u, nil, priv.ID, ""); !strings.Contains(denied, "未找到") {
		t.Fatalf("private conversation must not be readable: %s", denied)
	}
	if denied := s.ReadConversation(t.Context(), u, nil, otherConv.ID, ""); !strings.Contains(denied, "未找到") {
		t.Fatalf("other user's conversation must not be readable: %s", denied)
	}
}
