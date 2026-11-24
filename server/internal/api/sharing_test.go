package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"nuan/internal/store"
)

func addFixtureUser(t *testing.T, f *apiFixture, username, displayName string) (*store.User, string) {
	t.Helper()
	u := &store.User{Username: username, DisplayName: displayName, Role: "member"}
	if err := f.st.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	tok, err := f.api.Sessions.Create(t.Context(), u.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return u, tok
}

func doUserJSON(t *testing.T, f *apiFixture, token, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, f.http.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(resp.Body)
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, resp.StatusCode, wantStatus, raw.String())
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
}

func TestShortCodeShareLifecycleAndComponentDetachment(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()
	alice, aliceTok := addFixtureUser(t, f, "mom_share", "妈妈")
	_, bobTok := addFixtureUser(t, f, "dad_share", "爸爸")

	conv := &store.Conversation{UserID: alice.ID, Title: "杭州周末游"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	uMsg := &store.Message{Role: "user", SenderUserID: alice.ID, Content: "帮我列一个行李清单", Status: store.StatusDone}
	aMsg := &store.Message{Role: "assistant", Content: "好的，清单如下：", Status: store.StatusDone}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{uMsg, aMsg}}); err != nil {
		t.Fatal(err)
	}

	ent := &store.ComponentEntity{
		UserID:               alice.ID,
		Kind:                 "todo",
		Title:                "行李清单",
		Data:                 json.RawMessage(`{"title":"行李清单","items":[{"id":"1","text":"雨伞"}]}`),
		OriginConversationID: conv.ID,
		OriginMessageID:      aMsg.ID,
		OriginComponentID:    "c1",
	}
	if err := f.st.CreateComponentEntity(ctx, ent, "创建清单"); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetComponents(ctx, aMsg.ID, []store.MessageComponent{
		{ID: "c1", EntityID: ent.ID, Kind: "todo", Pos: 0, Version: 1, Data: ent.Data},
	}); err != nil {
		t.Fatal(err)
	}

	// Update entity to v2 before sharing — share must capture v2.
	v2Data := json.RawMessage(`{"title":"行李清单","items":[{"id":"1","text":"雨伞"},{"id":"2","text":"充电宝"}]}`)
	if _, err := f.st.UpdateComponentEntity(ctx, ent.ID, alice.ID, v2Data, nil, store.ComponentMutationMeta{ChangeSummary: "加充电宝"}); err != nil {
		t.Fatal(err)
	}

	var created struct {
		ID            string `json:"id"`
		Code          string `json:"code"`
		Title         string `json:"title"`
		MessageCount  int    `json:"message_count"`
		HasComponents bool   `json:"has_components"`
	}
	doUserJSON(t, f, aliceTok, "POST", "/api/conversations/"+conv.ID+"/shares", map[string]any{}, 201, &created)
	if created.Code == "" || created.MessageCount != 2 || !created.HasComponents {
		t.Fatalf("unexpected share: %+v", created)
	}

	// Update entity to v3 AFTER sharing — imported copy must NOT see v3.
	v3Data := json.RawMessage(`{"title":"行李清单v3","items":[{"id":"1","text":"防晒霜"}]}`)
	if _, err := f.st.UpdateComponentEntity(ctx, ent.ID, alice.ID, v3Data, nil, store.ComponentMutationMeta{ChangeSummary: "v3"}); err != nil {
		t.Fatal(err)
	}

	doUserJSON(t, f, bobTok, "GET", "/api/shares/"+created.Code+"/preview", nil, 200, nil)
	var imported struct {
		Conversation store.Conversation `json:"conversation"`
	}
	doUserJSON(t, f, bobTok, "POST", "/api/shares/"+created.Code+"/import", nil, 201, &imported)
	if imported.Conversation.ID == "" || imported.Conversation.ID == conv.ID {
		t.Fatalf("expected cloned conversation, got %+v", imported.Conversation)
	}

	bobMsgs, err := f.st.ListMessages(ctx, imported.Conversation.ID, "", 10)
	if err != nil || len(bobMsgs) != 2 || len(bobMsgs[1].Components) != 1 {
		t.Fatalf("unexpected bob msgs: %+v err=%v", bobMsgs, err)
	}
	fc := bobMsgs[1].Components[0]
	if fc.EntityID != "" {
		t.Fatalf("expected detached component with empty EntityID, got %q", fc.EntityID)
	}
	if string(fc.Data) != string(v2Data) {
		t.Fatalf("expected v2 snapshot %s, got %s", v2Data, fc.Data)
	}

	// Revoke share -> 410 on preview.
	doUserJSON(t, f, aliceTok, "DELETE", "/api/shares/"+created.ID, nil, 200, nil)
	doUserJSON(t, f, bobTok, "GET", "/api/shares/"+created.Code+"/preview", nil, 410, nil)

	// Private conversations cannot be shared.
	priv := &store.Conversation{UserID: alice.ID, Title: "私密", Private: true}
	if err := f.st.CreateConversation(ctx, priv); err != nil {
		t.Fatal(err)
	}
	doUserJSON(t, f, aliceTok, "POST", "/api/conversations/"+priv.ID+"/shares", map[string]any{}, 400, nil)
}

func TestFamilySharedConversationLifecycle(t *testing.T) {
	f := newAPIFixture(t)
	ctx := t.Context()
	alice, aliceTok := addFixtureUser(t, f, "mom_fam", "妈妈")
	bob, bobTok := addFixtureUser(t, f, "dad_fam", "爸爸")
	carol, carolTok := addFixtureUser(t, f, "kid_fam", "女儿")

	var fam struct {
		Items []store.FamilyMember `json:"items"`
	}
	doUserJSON(t, f, aliceTok, "GET", "/api/family/members", nil, 200, &fam)
	if len(fam.Items) < 2 {
		t.Fatalf("expected at least 2 other family members, got %d", len(fam.Items))
	}

	conv := &store.Conversation{UserID: alice.ID, Title: "家庭旅行计划"}
	if err := f.st.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	m1 := &store.Message{Role: "user", SenderUserID: alice.ID, Content: "我们去三亚吧", Status: store.StatusDone}
	m2 := &store.Message{Role: "assistant", Content: "三亚行程如下", Status: store.StatusDone}
	if err := f.st.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, Append: []*store.Message{m1, m2}}); err != nil {
		t.Fatal(err)
	}
	ent := &store.ComponentEntity{
		UserID:               alice.ID,
		Kind:                 "todo",
		Title:                "三亚清单",
		Data:                 json.RawMessage(`{"title":"三亚清单","items":[{"id":"1","text":"泳衣"}]}`),
		OriginConversationID: conv.ID,
		OriginMessageID:      m2.ID,
		OriginComponentID:    "c1",
	}
	if err := f.st.CreateComponentEntity(ctx, ent, "创建"); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetComponents(ctx, m2.ID, []store.MessageComponent{
		{ID: "c1", EntityID: ent.ID, Kind: "todo", Pos: 0, Version: 1, Data: ent.Data},
	}); err != nil {
		t.Fatal(err)
	}

	// Share with Bob & Carol.
	doUserJSON(t, f, aliceTok, "POST", "/api/conversations/"+conv.ID+"/members", map[string]any{
		"user_ids": []string{bob.ID, carol.ID},
	}, 200, nil)

	// Bob sees the shared conversation in his list.
	var listResp struct {
		Items []store.Conversation `json:"items"`
	}
	doUserJSON(t, f, bobTok, "GET", "/api/conversations", nil, 200, &listResp)
	if len(listResp.Items) != 1 || !listResp.Items[0].IsShared || listResp.Items[0].MemberCount != 3 || listResp.Items[0].IsOwner {
		t.Fatalf("unexpected bob conv list: %+v", listResp.Items)
	}

	// Bob collaboratively updates the component in the shared conversation.
	doUserJSON(t, f, bobTok, "PATCH", "/api/components/"+ent.ID, map[string]any{
		"data":           json.RawMessage(`{"title":"三亚清单","items":[{"id":"1","text":"泳衣"},{"id":"2","text":"墨镜"}]}`),
		"change_summary": "爸爸加了墨镜",
	}, 200, nil)

	// Carol fetches the conversation and sees v2.
	var carolConv struct {
		Messages []store.Message `json:"messages"`
	}
	doUserJSON(t, f, carolTok, "GET", "/api/conversations/"+conv.ID, nil, 200, &carolConv)
	if len(carolConv.Messages) != 2 || len(carolConv.Messages[1].Components) != 1 || carolConv.Messages[1].Components[0].LatestVersion != 2 {
		t.Fatalf("carol did not see updated component v2: %+v", carolConv.Messages)
	}

	// Bob cannot delete Alice's message, but Alice (owner) can.
	doUserJSON(t, f, bobTok, "DELETE", "/api/conversations/"+conv.ID+"/messages/"+m1.ID, nil, 403, nil)
	doUserJSON(t, f, aliceTok, "DELETE", "/api/conversations/"+conv.ID+"/messages/"+m1.ID, nil, 200, nil)

	// Carol deletes the conversation -> only leaves; Bob still has access.
	doUserJSON(t, f, carolTok, "DELETE", "/api/conversations/"+conv.ID, nil, 200, nil)
	doUserJSON(t, f, carolTok, "GET", "/api/conversations/"+conv.ID, nil, 404, nil)
	doUserJSON(t, f, bobTok, "GET", "/api/conversations/"+conv.ID, nil, 200, nil)

	// Alice lists her owned shared conversations and revokes all sharing.
	doUserJSON(t, f, aliceTok, "GET", "/api/shared-conversations", nil, 200, nil)
	doUserJSON(t, f, aliceTok, "DELETE", "/api/conversations/"+conv.ID+"/members", nil, 200, nil)
	doUserJSON(t, f, bobTok, "GET", "/api/conversations/"+conv.ID, nil, 404, nil)
	doUserJSON(t, f, aliceTok, "GET", "/api/conversations/"+conv.ID, nil, 200, nil)
}
