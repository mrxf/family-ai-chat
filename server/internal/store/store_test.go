package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"nuan/internal/store/storetest/testdb"
	"reflect"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(testdb.DSN(t))
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

func testUser(t *testing.T, s *Store, name string) *User {
	t.Helper()
	u := &User{Username: name, DisplayName: name, Role: "member", PasswordHash: "test-only"}
	if err := s.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func testConv(t *testing.T, s *Store, u *User, title string) *Conversation {
	t.Helper()
	c := &Conversation{UserID: u.ID, Title: title}
	if err := s.CreateConversation(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func appendMessages(t *testing.T, s *Store, c *Conversation, count int) []*Message {
	t.Helper()
	ms := make([]*Message, count)
	for i := range ms {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		ms[i] = &Message{Role: role, Content: fmt.Sprintf("message %d", i), Status: StatusDone}
	}
	if err := s.ApplyTurn(t.Context(), Turn{ConversationID: c.ID, Append: ms}); err != nil {
		t.Fatal(err)
	}
	return ms
}

func messageIDs(ms []*Message) []string {
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	return ids
}

func TestMigrationsRepeatableAndPersistData(t *testing.T) {
	path := testdb.DSN(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u := testUser(t, s, "preserved")
	for i := 0; i < 4; i++ {
		if err := s.migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.queryRow(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 19 {
		t.Errorf("migration count=%d err=%v", n, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetUser(t.Context(), u.ID)
	if err != nil || got.Username != u.Username {
		t.Fatalf("reopen: user=%+v err=%v", got, err)
	}
}

func TestUsernameConflictCaseInsensitive(t *testing.T) {
	s := testStore(t)
	u := testUser(t, s, "Alice")
	for _, name := range []string{"Alice", "alice", "ALICE"} {
		err := s.CreateUser(t.Context(), &User{Username: name, Role: "member"})
		if !errors.Is(err, ErrConflict) {
			t.Errorf("username=%q error=%v", name, err)
		}
	}
	got, err := s.GetUserByUsername(t.Context(), "aLiCe")
	if err != nil || got.ID != u.ID {
		t.Fatalf("case insensitive lookup=%+v err=%v", got, err)
	}
}

func TestApplyTurnTruncateInclusiveAndAtomic(t *testing.T) {
	s := testStore(t)
	u := testUser(t, s, "owner")
	c := testConv(t, s, u, "")
	ms := appendMessages(t, s, c, 6)
	other := testConv(t, s, u, "other")
	untouched := appendMessages(t, s, other, 2)
	// A failure after truncating must roll back the deletion and all inserts.
	err := s.ApplyTurn(t.Context(), Turn{ConversationID: c.ID, TruncateFrom: ms[2].ID, Append: []*Message{{ID: ms[0].ID, Role: "user", Status: StatusDone}}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict: %v", err)
	}
	got, err := s.ListMessages(t.Context(), c.ID, "", 100)
	if err != nil || !reflect.DeepEqual(messageIDs(got), messageIDs(ms)) {
		t.Fatalf("rollback: %v err=%v", messageIDs(got), err)
	}
	replacement := &Message{Role: "user", Content: "edited", Status: StatusDone}
	if err := s.ApplyTurn(t.Context(), Turn{ConversationID: c.ID, TruncateFrom: ms[2].ID, Append: []*Message{replacement}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListMessages(t.Context(), c.ID, "", 100)
	want := []string{ms[0].ID, ms[1].ID, replacement.ID}
	if err != nil || !reflect.DeepEqual(messageIDs(got), want) {
		t.Fatalf("truncate: got=%v want=%v err=%v", messageIDs(got), want, err)
	}
	for _, m := range ms[2:] {
		if _, err := s.GetMessage(t.Context(), m.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleted %s: %v", m.ID, err)
		}
	}
	got, err = s.ListMessages(t.Context(), other.ID, "", 100)
	if err != nil || !reflect.DeepEqual(messageIDs(got), messageIDs(untouched)) {
		t.Fatalf("other conversation changed: %v %v", got, err)
	}
}

func TestListMessagesCursorAscending(t *testing.T) {
	s := testStore(t)
	c := testConv(t, s, testUser(t, s, "owner"), "")
	ms := appendMessages(t, s, c, 7)
	for _, tc := range []struct {
		before string
		limit  int
		want   []*Message
	}{
		{"", 3, ms[4:]}, {ms[4].ID, 3, ms[1:4]}, {ms[1].ID, 3, ms[:1]}, {ms[0].ID, 3, ms[:0]}, {"", 0, ms},
	} {
		got, err := s.ListMessages(t.Context(), c.ID, tc.before, tc.limit)
		if err != nil || !reflect.DeepEqual(messageIDs(got), messageIDs(tc.want)) {
			t.Errorf("before=%s limit=%d got=%v want=%v err=%v", tc.before, tc.limit, messageIDs(got), messageIDs(tc.want), err)
		}
	}
}

func TestUserIsolation(t *testing.T) {
	s := testStore(t)
	a := testUser(t, s, "alice")
	b := testUser(t, s, "bob")
	c := testConv(t, s, a, "private")
	ms := appendMessages(t, s, c, 2)
	for _, uid := range []string{b.ID, "missing"} {
		if _, err := s.GetConversation(t.Context(), c.ID, uid); !errors.Is(err, ErrNotFound) {
			t.Errorf("conversation leaked: %v", err)
		}
		if _, err := s.GetMessageForUser(t.Context(), ms[0].ID, uid); !errors.Is(err, ErrNotFound) {
			t.Errorf("message leaked: %v", err)
		}
		cs, err := s.ListConversations(t.Context(), ConversationQuery{UserID: uid})
		if err != nil || len(cs) != 0 {
			t.Errorf("list leaked: %v %v", cs, err)
		}
	}
	if _, err := s.GetConversation(t.Context(), c.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMessageForUser(t.Context(), ms[0].ID, a.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMarkInterruptedAndTitlePreservation(t *testing.T) {
	s := testStore(t)
	c := testConv(t, s, testUser(t, s, "owner"), "")
	var ms []*Message
	for _, status := range []string{StatusStreaming, StatusDone, StatusCancelled, StatusError, StatusInterrupted} {
		ms = append(ms, &Message{Role: "assistant", Content: "partial", Reasoning: "reason", Status: status})
	}
	if err := s.ApplyTurn(t.Context(), Turn{ConversationID: c.ID, Append: ms}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{1, 0} {
		if n, err := s.MarkInterrupted(t.Context()); err != nil || n != want {
			t.Errorf("call %d: n=%d err=%v", i, n, err)
		}
	}
	for i, m := range ms {
		got, err := s.GetMessage(t.Context(), m.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := m.Status
		if i == 0 {
			want = StatusInterrupted
		}
		if got.Status != want || got.Content != "partial" || got.Reasoning != "reason" {
			t.Errorf("message changed unexpectedly: %+v", got)
		}
	}
	if ok, err := s.SetConversationTitle(t.Context(), c.ID, "auto"); err != nil || !ok {
		t.Fatalf("first title: %v %v", ok, err)
	}
	manual := "user title"
	if _, err := s.UpdateConversation(t.Context(), c.ID, c.UserID, ConversationPatch{Title: &manual}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SetConversationTitle(t.Context(), c.ID, "late auto"); err != nil || ok {
		t.Fatalf("overwrite: %v %v", ok, err)
	}
	got, err := s.GetConversation(t.Context(), c.ID, c.UserID)
	if err != nil || got.Title != manual {
		t.Fatalf("title=%+v err=%v", got, err)
	}
}

// Every nonempty search must run, and LIKE metacharacters must be literal.
func TestListConversationsSearch(t *testing.T) {
	s := testStore(t)
	u := testUser(t, s, "owner")
	other := testUser(t, s, "other")
	a := testConv(t, s, u, "标题 needle 100% a_b C:\\docs")
	b := testConv(t, s, u, "消息匹配")
	if err := s.ApplyTurn(t.Context(), Turn{ConversationID: b.ID, Append: []*Message{{Role: "user", Content: "body needle 100% a_b C:\\docs", Status: StatusDone}}}); err != nil {
		t.Fatal(err)
	}
	testConv(t, s, u, "100x axb")
	testConv(t, s, other, a.Title)
	for _, tc := range []struct {
		query string
		ids   []string
	}{
		{"标题", []string{a.ID}}, {"body", []string{b.ID}}, {"needle", []string{a.ID, b.ID}}, {"%", []string{a.ID, b.ID}}, {"_", []string{a.ID, b.ID}}, {`C:\docs`, []string{a.ID, b.ID}}, {"absent", nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			cs, err := s.ListConversations(context.Background(), ConversationQuery{UserID: u.ID, Search: tc.query})
			if err != nil {
				t.Fatalf("search %q must succeed: %v", tc.query, err)
			}
			want := map[string]bool{}
			for _, id := range tc.ids {
				want[id] = true
			}
			if len(cs) != len(want) {
				t.Fatalf("search %q: got %d want %d", tc.query, len(cs), len(want))
			}
			for _, c := range cs {
				if !want[c.ID] {
					t.Errorf("unexpected conversation %s", c.ID)
				}
			}
		})
	}
	// Verify snippet behavior: title-only match has empty snippet; long message match extracts windowed excerpt.
	longConv := testConv(t, s, u, "周末饮食")
	longBody := strings.Repeat("这是一段前置铺垫文字，用来测试长文本摘要截断。", 3) + "\n关键重点是动物肝脏能有效补铁。\n" + strings.Repeat("后面还有一些补充说明注意事项。", 4)
	if err := s.ApplyTurn(t.Context(), Turn{ConversationID: longConv.ID, Append: []*Message{{Role: "assistant", Content: longBody, Status: StatusDone}}}); err != nil {
		t.Fatal(err)
	}
	cs, err := s.ListConversations(t.Context(), ConversationQuery{UserID: u.ID, Search: "动物肝脏"})
	if err != nil || len(cs) != 1 {
		t.Fatalf("long search: got=%v err=%v", cs, err)
	}
	if !strings.Contains(cs[0].Snippet, "动物肝脏") || !strings.HasPrefix(cs[0].Snippet, "…") || !strings.HasSuffix(cs[0].Snippet, "…") {
		t.Errorf("unexpected snippet: %q", cs[0].Snippet)
	}
	titleOnly, err := s.ListConversations(t.Context(), ConversationQuery{UserID: u.ID, Search: "标题"})
	if err != nil || len(titleOnly) != 1 || titleOnly[0].Snippet != "" {
		t.Errorf("title-only match should have empty snippet: %+v err=%v", titleOnly, err)
	}
}

func TestPrivateConversationStoreInvariants(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	u := testUser(t, s, "private_tester")

	normal := &Conversation{UserID: u.ID, Title: "周年礼物参考"}
	if err := s.CreateConversation(ctx, normal); err != nil {
		t.Fatal(err)
	}
	priv := &Conversation{UserID: u.ID, Title: "周年礼物惊喜项链", Pinned: true, Private: true}
	if err := s.CreateConversation(ctx, priv); err != nil {
		t.Fatal(err)
	}
	if priv.Pinned {
		t.Fatal("private conversation must not be created pinned")
	}
	pin := true
	updated, err := s.UpdateConversation(ctx, priv.ID, u.ID, ConversationPatch{Pinned: &pin})
	if err != nil || updated.Pinned {
		t.Fatalf("private conversation must ignore pin=true: %+v err=%v", updated, err)
	}

	privFile := &File{UserID: u.ID, Mime: "image/jpeg", Path: "/tmp/priv_ring.jpg", Private: true}
	if err := s.CreateFile(ctx, privFile); err != nil {
		t.Fatal(err)
	}
	msg := &Message{Role: "user", Content: "记住我要买黄金项链", Status: StatusDone, Attachments: []Attachment{{FileID: privFile.ID, Mime: "image/jpeg"}}}
	if err := s.ApplyTurn(ctx, Turn{ConversationID: priv.ID, Append: []*Message{msg}}); err != nil {
		t.Fatal(err)
	}

	// Excluded from normal list and search.
	list, err := s.ListConversations(ctx, ConversationQuery{UserID: u.ID})
	if err != nil || len(list) != 1 || list[0].ID != normal.ID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	search, err := s.ListConversations(ctx, ConversationQuery{UserID: u.ID, Search: "项链"})
	if err != nil || len(search) != 0 {
		t.Fatalf("search leaked private conversation: %+v err=%v", search, err)
	}

	// Excluded from memory candidates and blocked in ApplyMemoryExtraction.
	cands, err := s.MemoryCandidates(ctx, Now()+60_000)
	if err != nil || len(cands) != 0 {
		t.Fatalf("MemoryCandidates=%+v err=%v", cands, err)
	}
	err = s.ApplyMemoryExtraction(ctx, MemorySnapshot{
		ConversationID: priv.ID,
		UserID:         u.ID,
		Cursor:         "",
		Messages:       []*Message{msg},
	}, []MemoryChange{{Op: "add", Memory: &Memory{UserID: u.ID, Category: "ongoing", Content: "用户在挑项链", Pinned: true}}}, "聊了买项链")
	if err != nil {
		t.Fatal(err)
	}
	mems, err := s.ListMemories(ctx, u.ID)
	if err != nil || len(mems) != 0 {
		t.Fatalf("ApplyMemoryExtraction wrote memories for private chat: %+v err=%v", mems, err)
	}

	// ExpiredPrivateConversations finds it once cutoff is past updated_at.
	expired, err := s.ExpiredPrivateConversations(ctx, Now()+60_000)
	if err != nil || len(expired) != 1 || expired[0].ID != priv.ID {
		t.Fatalf("ExpiredPrivateConversations=%+v err=%v", expired, err)
	}

	// DeleteConversationCleanup deletes the private conversation AND its unreferenced file row.
	paths, err := s.DeleteConversationCleanup(ctx, priv.ID, u.ID)
	if err != nil || len(paths) != 1 || paths[0] != privFile.Path {
		t.Fatalf("DeleteConversationCleanup paths=%v err=%v", paths, err)
	}
	if _, err := s.GetFile(ctx, privFile.ID, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected private file row deleted, got err=%v", err)
	}
}

func TestModelRoutingNoteAndConversationAutoMode(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	u := testUser(t, s, "autoroute")
	p := &Provider{Name: "p", BaseURL: "https://example.com/v1", APIKey: "k", Enabled: true}
	if err := s.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	m := &Model{ProviderID: p.ID, UpstreamModel: "gemini-4-carbon", DisplayName: "Carbon", RoutingNote: "医疗问题和深度研究", Enabled: true}
	if err := s.UpsertModel(ctx, m); err != nil {
		t.Fatal(err)
	}
	gotM, err := s.GetModel(ctx, m.ID)
	if err != nil || gotM.RoutingNote != "医疗问题和深度研究" {
		t.Fatalf("model=%+v err=%v", gotM, err)
	}

	conv := &Conversation{UserID: u.ID, AutoMode: true, Title: "自动测试"}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	gotC, err := s.GetConversation(ctx, conv.ID, u.ID)
	if err != nil || !gotC.AutoMode || gotC.ModelID != nil {
		t.Fatalf("conv=%+v err=%v", gotC, err)
	}
	off := false
	updated, err := s.UpdateConversation(ctx, conv.ID, u.ID, ConversationPatch{AutoMode: &off, ModelID: &m.ID})
	if err != nil || updated.AutoMode || updated.ModelID == nil || *updated.ModelID != m.ID {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
}

func TestConversationSummaryAndEarlierSearch(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	u := testUser(t, s, "compactor")
	conv := testConv(t, s, u, "长期用药咨询")
	ms := []*Message{
		{Role: "user", Content: "父亲每天早上吃氨氯地平 5mg", Status: StatusDone},
		{Role: "assistant", Content: "氨氯地平 5mg 建议固定早晨服用。", Status: StatusDone},
		{Role: "user", Content: "那晚饭后还能吃硝苯地平吗？", Status: StatusDone},
		{Role: "assistant", Content: "同属钙通道阻滞剂，不要自行叠加。", Status: StatusDone},
		{Role: "user", Content: "今天晚饭吃西红柿炒鸡蛋可以吗？", Status: StatusDone},
		{Role: "assistant", Content: "完全可以，注意少盐。", Status: StatusDone},
	}
	if err := s.ApplyTurn(ctx, Turn{ConversationID: conv.ID, Append: ms}); err != nil {
		t.Fatal(err)
	}

	// Before compaction: SearchUserMessages excludes active uncompacted messages of conv.ID.
	hitsBefore, err := s.SearchUserMessages(ctx, u.ID, conv.ID, []string{"氨氯地平"}, 10)
	if err != nil || len(hitsBefore) != 0 {
		t.Fatalf("uncompacted current conv should be excluded, got=%+v err=%v", hitsBefore, err)
	}

	beforeUpdatedAt := conv.UpdatedAt
	gotC, _ := s.GetConversation(ctx, conv.ID, u.ID)
	beforeUpdatedAt = gotC.UpdatedAt

	ok, err := s.CommitConversationSummary(ctx, conv.ID, "", ms[3].ID, "父亲早餐服氨氯地平5mg；已提醒勿叠加硝苯地平。")
	if err != nil || !ok {
		t.Fatalf("CommitConversationSummary ok=%v err=%v", ok, err)
	}
	// Stale expectedCursor or non-advancing cursor is rejected cleanly.
	if ok, err := s.CommitConversationSummary(ctx, conv.ID, "", ms[5].ID, "stale"); err != nil || ok {
		t.Fatalf("stale cursor should not commit: ok=%v err=%v", ok, err)
	}
	gotAfterCommit, _ := s.GetConversation(ctx, conv.ID, u.ID)
	if gotAfterCommit.UpdatedAt != beforeUpdatedAt {
		t.Fatalf("compaction must not bump updated_at: got %d want %d", gotAfterCommit.UpdatedAt, beforeUpdatedAt)
	}

	sum, cur, err := s.GetConversationSummary(ctx, conv.ID)
	if err != nil || cur != ms[3].ID || sum == "" {
		t.Fatalf("summary=%q cursor=%q err=%v", sum, cur, err)
	}

	// ListMessagesAfter returns only messages strictly after summary_cursor and sets hasOlder=true.
	tail, hasOlder, err := s.ListMessagesAfter(ctx, conv.ID, cur, "", 50)
	if err != nil || !hasOlder || len(tail) != 2 || tail[0].ID != ms[4].ID {
		t.Fatalf("ListMessagesAfter tail=%+v hasOlder=%v err=%v", tail, hasOlder, err)
	}

	// SearchUserMessages now finds compacted early messages (<= summary_cursor) of conv.ID,
	// while still excluding active tail messages (> summary_cursor).
	hitsCompacted, err := s.SearchUserMessages(ctx, u.ID, conv.ID, []string{"氨氯地平"}, 10)
	if err != nil || len(hitsCompacted) == 0 {
		t.Fatalf("expected compacted message searchable via SearchUserMessages, got=%+v err=%v", hitsCompacted, err)
	}
	hitsTail, err := s.SearchUserMessages(ctx, u.ID, conv.ID, []string{"西红柿炒鸡蛋"}, 10)
	if err != nil || len(hitsTail) != 0 {
		t.Fatalf("active uncompacted tail must stay excluded, got=%+v err=%v", hitsTail, err)
	}

	// SearchConversationMessages searches strictly before upperBound.
	convHits, err := s.SearchConversationMessages(ctx, conv.ID, ms[4].ID, []string{"硝苯地平"}, 10)
	if err != nil || len(convHits) != 1 || convHits[0].ID != ms[2].ID {
		t.Fatalf("SearchConversationMessages=%+v err=%v", convHits, err)
	}
	around, err := s.ListMessagesAround(ctx, conv.ID, ms[2].ID, ms[4].ID, 1, 1)
	if err != nil || len(around) != 3 || around[0].ID != ms[1].ID || around[2].ID != ms[3].ID {
		t.Fatalf("ListMessagesAround=%+v err=%v", around, err)
	}

	// Regenerating / truncating AFTER summary_cursor preserves summary.
	if err := s.ApplyTurn(ctx, Turn{
		ConversationID: conv.ID,
		TruncateFrom:   ms[5].ID,
		Append:         []*Message{{Role: "assistant", Content: "可以吃，少放盐即可。", Status: StatusDone}},
	}); err != nil {
		t.Fatal(err)
	}
	if sum2, cur2, _ := s.GetConversationSummary(ctx, conv.ID); cur2 != ms[3].ID || sum2 == "" {
		t.Fatalf("truncate after summary_cursor must preserve summary, got cur=%q sum=%q", cur2, sum2)
	}

	// Editing / truncating AT OR BEFORE summary_cursor invalidates stale summary atomically.
	if err := s.ApplyTurn(ctx, Turn{
		ConversationID: conv.ID,
		TruncateFrom:   ms[2].ID,
		Append:         []*Message{{Role: "user", Content: "改问别的问题", Status: StatusDone}},
	}); err != nil {
		t.Fatal(err)
	}
	if sum3, cur3, _ := s.GetConversationSummary(ctx, conv.ID); cur3 != "" || sum3 != "" {
		t.Fatalf("truncate inside compacted range must clear summary, got cur=%q sum=%q", cur3, sum3)
	}
}

func TestMessageComponentsAndStateUpdate(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	u := testUser(t, s, "comp_user")
	other := testUser(t, s, "comp_other")
	conv := &Conversation{UserID: u.ID}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	uMsg := &Message{Role: "user", Content: "周末露营要带什么？", Status: StatusDone}
	aMsg := &Message{Role: "assistant", Status: StatusStreaming}
	if err := s.ApplyTurn(ctx, Turn{ConversationID: conv.ID, Append: []*Message{uMsg, aMsg}}); err != nil {
		t.Fatal(err)
	}

	comps := []MessageComponent{{
		ID:    "comp_1",
		Kind:  "todo",
		Pos:   12,
		Data:  json.RawMessage(`{"title":"露营清单","items":[{"id":"i1","text":"帐篷"},{"id":"i2","text":"睡袋"}]}`),
		State: json.RawMessage(`{"done_ids":["i1"]}`),
	}}
	if err := s.SetComponents(ctx, aMsg.ID, comps); err != nil {
		t.Fatal(err)
	}
	aMsg.Content = "先给你整理一份清单：\n\n出发前记得看天气哦～"
	aMsg.Components = comps
	aMsg.Status = StatusDone
	if err := s.FinishMessage(ctx, aMsg); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMessageForUser(ctx, aMsg.ID, u.ID)
	if err != nil || len(got.Components) != 1 || got.Components[0].Pos != 12 || got.Components[0].Kind != "todo" {
		t.Fatalf("GetMessageForUser components=%+v err=%v", got, err)
	}

	// Other user cannot mutate component state.
	if _, _, err := s.UpdateMessageComponentState(ctx, aMsg.ID, other.ID, "comp_1", json.RawMessage(`{"done_ids":["i1","i2"]}`)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for other user, got %v", err)
	}

	// Owner updates component state atomically.
	updatedMsg, updatedComp, err := s.UpdateMessageComponentState(ctx, aMsg.ID, u.ID, "comp_1", json.RawMessage(`{"done_ids":["i1","i2"]}`))
	if err != nil || updatedComp == nil || string(updatedComp.State) != `{"done_ids":["i1","i2"]}` {
		t.Fatalf("UpdateMessageComponentState comp=%+v msg=%+v err=%v", updatedComp, updatedMsg, err)
	}
}
