package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"nuan/internal/store/storetest/testdb"
)

func sqliteStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "copy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestMigrationVersionsMatch(t *testing.T) {
	versions := func(dir string) []string {
		entries, err := migrationFS.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			out = append(out, strings.SplitN(e.Name(), "_", 2)[0])
		}
		sort.Strings(out)
		return out
	}
	a, b := versions("migrations/sqlite"), versions("migrations/postgres")
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("migration versions: sqlite=%v postgres=%v", a, b)
	}
}

func TestBusinessTablesCoverSchema(t *testing.T) {
	s := sqliteStore(t)
	rows, err := s.query(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name <> 'schema_migrations' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	expected := strings.Fields(businessTables)
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("tables=%v copy list=%v", actual, expected)
	}
}

func populateCopyStore(t *testing.T, s *Store) {
	t.Helper()
	ctx := t.Context()
	u := testUser(t, s, "搬家用户Alice")
	if err := s.CreateSession(ctx, []byte{0, 1, 128, 255}, Session{UserID: u.ID, Device: "测试", CreatedAt: Now(), LastSeenAt: Now(), ExpiresAt: Now() + 10000}); err != nil {
		t.Fatal(err)
	}
	p := &Provider{Name: "upstream", Enabled: true}
	if err := s.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	m := &Model{ProviderID: p.ID, Enabled: true, Vision: true, InputPrice: 0.125}
	if err := s.UpsertModel(ctx, m); err != nil {
		t.Fatal(err)
	}
	temp := 0.75
	a := &Assistant{UseMemory: true, OwnerID: &u.ID, Name: "助手", ModelID: &m.ID, Temperature: &temp, Suggestions: []string{"你好"}}
	if err := s.UpsertAssistant(ctx, a); err != nil {
		t.Fatal(err)
	}
	c := &Conversation{UserID: u.ID, AssistantID: &a.ID, ModelID: &m.ID, Title: "迁移", Pinned: true}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	recalled := Now() - 3600000
	if err := s.CreateMemories(ctx, []*Memory{
		{UserID: u.ID, Category: "health", Content: "用户有高血压", Pinned: true, Embedding: []float32{1, -0.5, 0.25}, EmbeddingModel: "bge", SourceConvID: &c.ID, LastRecalledAt: &recalled, RecallCount: 2},
		{UserID: u.ID, Category: "episode", Content: "2026-11-17 聊了健康", SourceConvID: &c.ID, Compressed: true},
	}); err != nil {
		t.Fatal(err)
	}
	f := &File{UserID: u.ID, Mime: "image/png", Size: 123, Path: "files/picture.png", SHA256: "abc", Width: 16, Height: 32}
	if err := s.CreateFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	pf := &File{UserID: u.ID, Mime: "image/jpeg", Size: 64, Path: "files/private.jpg", SHA256: "def", Width: 8, Height: 8, Private: true}
	if err := s.CreateFile(ctx, pf); err != nil {
		t.Fatal(err)
	}
	pc := &Conversation{UserID: u.ID, ModelID: &m.ID, Private: true}
	if err := s.CreateConversation(ctx, pc); err != nil {
		t.Fatal(err)
	}
	var ms []*Message
	for i := 0; i < 503; i++ {
		ms = append(ms, &Message{Role: "assistant", Content: "你好 ? ' \\ 中文", Status: StatusDone, ModelID: &m.ID, Attachments: []Attachment{{FileID: f.ID, Mime: f.Mime}}})
	}
	if err := s.ApplyTurn(ctx, Turn{ConversationID: c.ID, Append: ms}); err != nil {
		t.Fatal(err)
	}
	ms[0].Cost, ms[0].PromptTokens, ms[0].CompletionTokens = 0.125, 100, 20
	ms[0].Sources = []WebSource{{Title: "示例新闻", URL: "https://example.com/news", SiteName: "示例网"}}
	if err := s.FinishMessage(ctx, ms[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.LogImage(ctx, u.ID, m.ID, 0.25); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "test-setting", `{"value":"测试"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertASRConfig(ctx, &ASRConfig{Kind: "openai", SecretKey: "test", Extra: map[string]string{"region": "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSearchConfig(ctx, &SearchConfig{Kind: "deepseek", APIKey: "test", BaseURL: "https://api.deepseek.com", Extra: map[string]string{"mode": "auto"}}); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, s *Store) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range strings.Fields(businessTables) {
		rows, err := s.query(t.Context(), "SELECT * FROM "+table)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals, ptrs := make([]any, len(cols)), make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			mapped := map[string]any{}
			for i, col := range cols {
				mapped[col] = vals[i]
			}
			b, err := json.Marshal(mapped)
			if err != nil {
				t.Fatal(err)
			}
			out[table] = append(out[table], string(b))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(out[table])
	}
	return out
}

func TestCopySQLite(t *testing.T) {
	source, target := sqliteStore(t), sqliteStore(t)
	populateCopyStore(t, source)
	counts, err := source.CopyTo(t.Context(), target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != len(strings.Fields(businessTables)) {
		t.Fatalf("counts=%v", counts)
	}
	want := snapshot(t, source)
	if got := snapshot(t, target); !reflect.DeepEqual(want, got) {
		t.Fatal("copy changed data")
	}
	if _, err := source.CopyTo(t.Context(), target, false); err == nil {
		t.Fatal("expected nonempty destination error")
	}
	testUser(t, target, "to-be-cleared")
	if _, err := source.CopyTo(t.Context(), target, true); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, target); !reflect.DeepEqual(want, got) {
		t.Fatal("forced copy changed data")
	}
}

// A destination that ran once has settings / built-in assistants but no users yet.
func TestCopyRefusesAnyNonEmptyTable(t *testing.T) {
	source, target := sqliteStore(t), testStore(t)
	populateCopyStore(t, source)
	if err := target.SetSetting(t.Context(), "extra-target-setting", "extra"); err != nil {
		t.Fatal(err)
	}
	want := snapshot(t, target)
	if _, err := source.CopyTo(t.Context(), target, false); err == nil || !strings.Contains(err.Error(), "settings is not empty") {
		t.Fatalf("expected non-empty destination error, got %v", err)
	}
	if got := snapshot(t, target); !reflect.DeepEqual(want, got) {
		t.Fatal("refused copy changed the destination")
	}
}

func TestCopyPostgresRoundTrip(t *testing.T) {
	raw := os.Getenv("NUAN_TEST_PG_URL")
	if raw == "" {
		t.Skip("NUAN_TEST_PG_URL is not set")
	}
	pg, err := Open(testdb.PostgresDSN(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pg.Close(); err != nil {
			t.Error(err)
		}
	})
	source, target := sqliteStore(t), sqliteStore(t)
	populateCopyStore(t, source)
	if _, err := source.CopyTo(t.Context(), pg, false); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.CopyTo(t.Context(), target, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot(t, source), snapshot(t, target)) {
		t.Fatal("round trip changed data")
	}
	users, err := pg.ListUsers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.LogImage(t.Context(), users[0].ID, "image-model", 0); err != nil {
		t.Fatalf("identity not advanced: %v", err)
	}
	if _, err := source.CopyTo(t.Context(), pg, false); err == nil {
		t.Fatal("expected nonempty destination error")
	}
	if _, err := source.CopyTo(t.Context(), pg, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot(t, source), snapshot(t, pg)) {
		t.Fatal("forced Postgres copy changed data")
	}
}
