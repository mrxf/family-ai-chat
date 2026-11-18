package memory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
)

func TestCleanAndSplitImport(t *testing.T) {
	text := "```markdown\r\n以下是我记得的：\r\n\r\n\r\n\r\n## 健康与用药\r\n- 高血压\r\n```"
	if got := cleanImport(text); got != "以下是我记得的：\n\n## 健康与用药\n- 高血压" {
		t.Fatalf("%q", got)
	}
	if splitImport(" \n ") != nil {
		t.Fatal("blank text split")
	}
	var b strings.Builder
	b.WriteString("## 健康与用药\n")
	for range 130 {
		b.WriteString("- 用户每天早上吃一种药\n")
	}
	chunks := splitImport(b.String())
	if len(chunks) != 3 || !strings.HasPrefix(chunks[1], "（接上文「健康与用药」）") {
		t.Fatal(len(chunks), chunks[1][:40])
	}
	for _, c := range chunks {
		if n := strings.Count(c, "\n- "); n > importChunkLines {
			t.Fatal("too many lines", n)
		}
	}
	long := splitImport(strings.Repeat("字", importChunkRunes*2+10))
	if len(long) != 3 {
		t.Fatal(len(long))
	}
}

func TestParseImport(t *testing.T) {
	future := time.Now().In(zone).AddDate(0, 1, 0).Format("2006-01-02")
	text := "好的：\n```json\n" + `{"items":[{"category":"health","content":"用户有高血压","keywords":"血压","replaces":2},{"category":"ongoing","content":"用户去年去过日本","valid_until":"2020-01-01"},{"category":"ongoing","content":"用户准备旅行","valid_until":"` + future + `"},{"category":"episode","content":"不允许"},{"category":"profile","content":""},{"category":"profile","content":"用户退休","replaces":9}],"duplicates":[1,1,2,7]}` + "\n```"
	items, skipped, err := parseImport(text, 2)
	if err != nil || !slices.Equal(skipped, []int{1, 2}) || len(items) != 3 {
		t.Fatal(items, skipped, err)
	}
	if items[0].ref != 2 || items[0].Keywords != "血压" || items[1].ValidUntil != future || items[2].ref != 0 {
		t.Fatal(items)
	}
	// A truncated answer keeps every complete item.
	items, _, err = parseImport(`{"items":[{"category":"health","content":"用户对花生过敏"},{"category":"family","content":"用户的女`, 0)
	if err != nil || len(items) != 1 || items[0].Content != "用户对花生过敏" {
		t.Fatal(items, err)
	}
	// The chat extraction format is not an import answer.
	if _, _, err := parseImport(`{"actions":[],"episode":""}`, 0); err == nil {
		t.Fatal("accepted extraction JSON")
	}
	if items, _, err := parseImport(`{"items":[]}`, 0); err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
}

type importUpstream struct {
	mu       sync.Mutex
	prompts  []string
	merges   int
	respond  func(system, user string) (int, string)
	inFlight int
	peak     int
}

func (f *importUpstream) client(t *testing.T) *llm.Client {
	c := llm.NewClient("", "https://test.invalid/v1", "key")
	c.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var in struct{ Messages []struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		f.inFlight++
		f.peak = max(f.peak, f.inFlight)
		f.prompts = append(f.prompts, in.Messages[1].Content)
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		status, body := f.respond(in.Messages[0].Content, in.Messages[1].Content)
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": body}}}})
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})}
	return c
}

func waitImport(t *testing.T, s *Service, user string) ImportJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := s.ImportStatus(user)
		if !ok {
			t.Fatal("job vanished")
		}
		if job.Status != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("import did not finish")
	return ImportJob{}
}

func TestImportJobDedupMergeAndPartialFailure(t *testing.T) {
	s, u, _ := memoryFixture(t)
	old := &store.Memory{UserID: u.ID, Category: "health", Content: "用户每天吃氨氯地平 5mg", Pinned: true}
	have := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户住在上海", Pinned: true}
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{old, have}); err != nil {
		t.Fatal(err)
	}
	up := &importUpstream{}
	up.respond = func(system, user string) (int, string) {
		if strings.Contains(system, "段与段之间") {
			up.mu.Lock()
			up.merges++
			up.mu.Unlock()
			if !strings.Contains(user, "#1 [health] 用户 2025 年起改吃缬沙坦") {
				t.Error(user)
			}
			return 200, `{"drop":[3],"update":[{"ref":1,"content":"用户 2025 年起改吃缬沙坦 80mg"},{"ref":99,"content":"越界"},{"ref":2,"content":"  "}]}`
		}
		switch {
		case strings.Contains(user, "CHUNK-FAIL"):
			return 500, "boom"
		case strings.Contains(user, "CHUNK-B"):
			// "用户住在上海。" duplicates an existing memory, "不吃香菜" duplicates chunk A.
			return 200, `{"items":[{"category":"profile","content":"用户住在上海。"},{"category":"preference","content":"用户不吃香菜！"},{"category":"preference","content":"Likes tea"}],"duplicates":[2]}`
		default:
			return 200, `{"items":[{"category":"health","content":"用户 2025 年起改吃缬沙坦","keywords":"降压药","replaces":1},{"category":"preference","content":"用户不吃香菜"},{"category":"preference","content":"用户爱喝茶"}],"duplicates":[2]}`
		}
	}
	client := up.client(t)
	s.Resolve = func(context.Context, string) (*store.Model, *llm.Client, error) {
		return &store.Model{UpstreamModel: "fake", DefaultEffort: "low"}, client, nil
	}
	text := "CHUNK-A\n" + strings.Repeat("a\n", importChunkLines) + "CHUNK-B\n" + strings.Repeat("b\n", importChunkLines-1) + "CHUNK-FAIL\n"
	job, err := s.StartImport(u, text)
	if err != nil || job.Status != "running" || job.Total != 3 {
		t.Fatal(job, err)
	}
	job = waitImport(t, s, u.ID)
	// Skipped counts distinct existing memories: #2 is reported by both chunks and
	// matched by "住在上海。". The repeated "不吃香菜！" is dropped without counting.
	if job.Status != "done" || job.Done != 3 || job.Failed != 1 || job.Skipped != 1 || up.merges != 1 {
		t.Fatalf("%+v merges=%d", job, up.merges)
	}
	if len(job.Items) != 3 || job.Items[0].Content != "用户 2025 年起改吃缬沙坦 80mg" || job.Items[1].Content != "用户不吃香菜" || job.Items[0].ReplacesID != old.ID || job.Items[0].Replaces != old.Content || job.Items[0].Keywords != "降压药" {
		t.Fatalf("%+v", job.Items)
	}
	for _, item := range job.Items {
		if item.Content == "用户爱喝茶" {
			t.Fatal("merge drop ignored")
		}
	}
	if !strings.Contains(up.prompts[0], "#1 [health] 用户每天吃氨氯地平 5mg") || !strings.Contains(up.prompts[0], "不要把它当成记忆写出来）：家人") || !strings.Contains(up.prompts[0], "<import_text>") {
		t.Fatal(up.prompts[0])
	}
	if up.peak > importConcurrency {
		t.Fatal("concurrency", up.peak)
	}
	// Nothing is stored before the user confirms.
	if ms, _ := s.Store.ListMemories(t.Context(), u.ID); len(ms) != 2 {
		t.Fatal(ms)
	}
	if _, ok := s.ImportStatus("someone-else"); ok {
		t.Fatal("job visible to another user")
	}
}

func TestImportJobFailureCancelAndReplace(t *testing.T) {
	s, u, _ := memoryFixture(t)
	if _, err := s.StartImport(u, " \n```\n```"); err != ErrEmptyImport {
		t.Fatal(err)
	}
	release := make(chan struct{})
	up := &importUpstream{}
	up.respond = func(_, user string) (int, string) {
		if strings.Contains(user, "SLOW") {
			<-release
		}
		if strings.Contains(user, "BAD") {
			return 200, "not JSON"
		}
		return 200, `{"items":[{"category":"profile","content":"用户退休了"}]}`
	}
	client := up.client(t)
	s.Resolve = func(context.Context, string) (*store.Model, *llm.Client, error) {
		return &store.Model{UpstreamModel: "fake"}, client, nil
	}
	if _, err := s.StartImport(u, "BAD"); err != nil {
		t.Fatal(err)
	}
	if job := waitImport(t, s, u.ID); job.Status != "failed" || job.Error == "" || len(up.prompts) != 2 {
		t.Fatal(job, len(up.prompts)) // one retry
	}
	first, _ := s.StartImport(u, "SLOW")
	second, _ := s.StartImport(u, "fast")
	close(release)
	job := waitImport(t, s, u.ID)
	if job.ID != second.ID || job.ID == first.ID || job.Status != "done" || len(job.Items) != 1 {
		t.Fatal(job)
	}
	time.Sleep(50 * time.Millisecond) // the replaced job finishing must not overwrite
	if job, _ := s.ImportStatus(u.ID); job.ID != second.ID {
		t.Fatal(job)
	}
	s.CancelImport(u.ID)
	if _, ok := s.ImportStatus(u.ID); ok {
		t.Fatal("cancelled job still listed")
	}
}

func TestCoreOverflowStaysRetrievable(t *testing.T) {
	s, u, _ := memoryFixture(t)
	huge := &store.Memory{UserID: u.ID, Category: "health", Content: strings.Repeat("病", 3000), Pinned: true}
	tail := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户爱好钓鱼", Pinned: true}
	if err := s.Store.CreateMemories(t.Context(), []*store.Memory{huge, tail}); err != nil {
		t.Fatal(err)
	}
	ctx := s.Inject(t.Context(), u, nil, "钓鱼")
	if strings.Contains(ctx.Core, "钓鱼") || !strings.Contains(ctx.Recall, "钓鱼") {
		t.Fatal(ctx.Recall)
	}
}

// The fixed private topics must stay in every prompt that writes memories.
func TestPromptsExcludePrivateTopics(t *testing.T) {
	for name, prompt := range map[string]string{"extraction": extractionPrompt, "import": importPrompt} {
		if !strings.Contains(prompt, privateTopics) || !strings.Contains(privateTopics, "炮友") || !strings.Contains(privateTopics, "不想让家人知道") {
			t.Fatal(name, "prompt lost the private topics")
		}
	}
	if !strings.Contains(extractionPrompt, `{"actions":[],"episode":""}`) {
		t.Fatal("private conversations must not get an episode")
	}
}
