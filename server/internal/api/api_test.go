package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"nuan/internal/chat"
	"nuan/internal/store"
)

func TestAuthenticationAndAuthorization(t *testing.T) {
	f := newAPIFixture(t)
	f.request("GET", "/api/me", "", "", nil, 401, nil)
	f.request("GET", "/api/me", "invalid", "", nil, 401, nil)
	var login struct {
		Token string
		User  store.User
	}
	f.request("POST", "/api/auth/login", "", "application/json", strings.NewReader(`{"username":" ALICE ","password":"test-password"}`), 200, &login)
	if login.Token == "" || login.User.ID != f.user.ID || login.User.PasswordHash != "" {
		t.Fatalf("login response=%+v", login)
	}
	f.request("GET", "/api/me", login.Token, "", nil, 200, nil)
	for _, path := range []string{"users", "providers", "models", "settings", "usage"} {
		f.request("GET", "/api/admin/"+path, login.Token, "", nil, 403, nil)
	}
	f.request("POST", "/api/auth/login", "", "application/json", strings.NewReader(`{"username":"alice","password":"wrong"}`), 401, nil)
	f.request("POST", "/api/auth/logout", login.Token, "application/json", strings.NewReader(`{}`), 200, nil)
	f.request("GET", "/api/me", login.Token, "", nil, 401, nil)
}

func TestLoginRateLimitAfterEightFailures(t *testing.T) {
	f := newAPIFixture(t)
	// Use a real account so all eight failures exercise password verification.
	for i := 0; i < 8; i++ {
		f.request("POST", "/api/auth/login", "", "application/json", strings.NewReader(`{"username":"alice","password":"wrong"}`), 401, nil)
	}
	f.request("POST", "/api/auth/login", "", "application/json", strings.NewReader(`{"username":"alice","password":"test-password"}`), 429, nil)
}

func TestChatLiveStreamPersistsAndReplays(t *testing.T) {
	f := newAPIFixture(t)
	steps := make(chan struct{}, 3)
	f.up.plan("live", upstreamPlan{tokens: []string{"你", "好", "🙂"}, steps: steps})
	res := f.send(map[string]any{"content": "live"}) // returns while upstream is blocked
	if res.Assistant.Status != store.StatusStreaming {
		t.Fatalf("send status=%s", res.Assistant.Status)
	}
	s := f.stream(res.Assistant.ID)
	e := s.next()
	if e.name != "snapshot" {
		t.Fatalf("first event=%s", e.name)
	}
	snap := decodeEvent[streamText](t, e)
	if snap.Content != "" || snap.Reasoning != "" {
		t.Fatalf("unexpected initial output: %+v", snap)
	}
	var content, reasoning string
	for i := 0; i < 3; i++ {
		steps <- struct{}{}
		for {
			e = s.next()
			if e.name == "title" {
				continue
			}
			if e.name != "delta" {
				t.Fatalf("want delta before done, got %s", e.name)
			}
			break
		}
		d := decodeEvent[streamText](t, e)
		content += d.Content
		reasoning += d.Reasoning
	}
	_, done, _ := s.finish()
	if done.Status != store.StatusDone || done.Content != content || content != "你好🙂" || done.Reasoning != reasoning || reasoning != "想想想" || done.PromptTokens != 11 || done.CompletionTokens != 7 {
		t.Fatalf("done=%+v deltas=%q/%q", done, content, reasoning)
	}
	stored, err := f.st.GetMessage(t.Context(), done.ID)
	if err != nil || stored.Status != store.StatusDone || !reflect.DeepEqual(stored, done) {
		t.Fatalf("persisted=%+v done=%+v err=%v", stored, done, err)
	}
	f.waitRun(done.ID)
	replay := f.stream(done.ID)
	e = replay.next()
	if e.name != "snapshot" {
		t.Fatal(e.name)
	}
	v := decodeEvent[streamText](t, e)
	if v.Content != content || v.Reasoning != reasoning || v.Status != store.StatusDone {
		t.Fatalf("replay snapshot=%+v", v)
	}
	e = replay.next()
	if e.name != "done" || !reflect.DeepEqual(decodeEvent[chat.FinalEvent](t, e).Message, stored) {
		t.Fatalf("replay done=%s", e.data)
	}
	c, err := f.st.GetConversation(t.Context(), res.Conversation.ID, f.user.ID)
	if err != nil || c.Title != "家庭测试标题" {
		t.Fatalf("title=%+v err=%v", c, err)
	}
}

func TestChatReconnectSnapshotPlusDeltas(t *testing.T) {
	f := newAPIFixture(t)
	steps := make(chan struct{}, 3)
	f.up.plan("reconnect", upstreamPlan{tokens: []string{"第一", "第二", "第三"}, steps: steps})
	res := f.send(map[string]any{"content": "reconnect"})
	s := f.stream(res.Assistant.ID)
	if e := s.next(); e.name != "snapshot" {
		t.Fatal(e.name)
	}
	steps <- struct{}{}
	for {
		e := s.next()
		if e.name == "delta" {
			if decodeEvent[streamText](t, e).Content != "第一" {
				t.Fatal(string(e.data))
			}
			break
		}
	}
	s.close()
	steps <- struct{}{}
	eventually(t, "generation after disconnect", func() bool {
		r := f.svc.Hub.Get(res.Assistant.ID)
		return r != nil && r.Snapshot().Content == "第一第二"
	})
	reconnected := f.stream(res.Assistant.ID)
	e := reconnected.next()
	if e.name != "snapshot" {
		t.Fatal(e.name)
	}
	snap := decodeEvent[streamText](t, e)
	if snap.Content != "第一第二" {
		t.Fatalf("reconnect snapshot=%+v", snap)
	}
	steps <- struct{}{}
	rest, done, names := reconnected.finish()
	if snap.Content+rest.Content != done.Content || done.Content != "第一第二第三" || snap.Reasoning+rest.Reasoning != done.Reasoning || done.Status != store.StatusDone {
		t.Fatalf("snapshot=%+v rest=%+v done=%+v events=%v", snap, rest, done, names)
	}
}

func TestChatCancelKeepsPartialAndRejectsBusyConversation(t *testing.T) {
	f := newAPIFixture(t)
	steps := make(chan struct{}, 1)
	f.up.plan("cancel", upstreamPlan{tokens: []string{"已生成"}, steps: steps, blockAfter: true})
	res := f.send(map[string]any{"content": "cancel"})
	s := f.stream(res.Assistant.ID)
	if e := s.next(); e.name != "snapshot" {
		t.Fatal(e.name)
	}
	f.json("POST", "/api/chat", map[string]any{"conversation_id": res.Conversation.ID, "content": "busy"}, 409, nil)
	f.json("POST", "/api/conversations/"+res.Conversation.ID+"/regenerate", map[string]any{}, 409, nil)
	steps <- struct{}{}
	for {
		e := s.next()
		if e.name == "delta" {
			if decodeEvent[streamText](t, e).Content != "已生成" {
				t.Fatal(string(e.data))
			}
			break
		}
	}
	var cancelled struct{ OK bool }
	f.json("POST", "/api/messages/"+res.Assistant.ID+"/cancel", map[string]any{}, 200, &cancelled)
	if !cancelled.OK {
		t.Fatal("cancel returned false")
	}
	_, done, _ := s.finish()
	if done.Status != store.StatusCancelled || done.Content != "已生成" || done.Reasoning != "想" {
		t.Fatalf("cancelled=%+v", done)
	}
	stored, err := f.st.GetMessage(t.Context(), done.ID)
	if err != nil || stored.Status != store.StatusCancelled || stored.Content != done.Content {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	select {
	case prompt := <-f.up.cancelled:
		if prompt != "cancel" {
			t.Fatal(prompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream not cancelled")
	}
	f.waitRun(done.ID)
	f.json("POST", "/api/messages/"+done.ID+"/cancel", map[string]any{}, 200, &cancelled)
	if cancelled.OK {
		t.Fatal("finished run cancelled again")
	}
}

func TestChatRetryBeforeOutputAndErrorAfterOutput(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		plan                    upstreamPlan
		wantStatus, wantContent string
		attempts                int
	}{
		{"500-once", upstreamPlan{tokens: []string{"recovered"}, failStatus: 500, failCount: 1}, store.StatusDone, "recovered", 2},
		{"429-once", upstreamPlan{tokens: []string{"recovered"}, failStatus: 429, failCount: 1}, store.StatusDone, "recovered", 2},
		{"500-twice", upstreamPlan{failStatus: 500, failCount: 2}, store.StatusError, "", 2},
		{"400-no-retry", upstreamPlan{failStatus: 400, failCount: 1}, store.StatusError, "", 1},
		{"partial-error", upstreamPlan{tokens: []string{"保留内容"}, errorAfter: true}, store.StatusError, "保留内容", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t)
			f.up.plan(tc.name, tc.plan)
			res := f.send(map[string]any{"content": tc.name})
			text, done, _ := f.stream(res.Assistant.ID).finish()
			if done.Status != tc.wantStatus || done.Content != tc.wantContent || text.Content != done.Content || (done.Status == store.StatusError && done.Error == "") {
				t.Fatalf("text=%+v done=%+v", text, done)
			}
			if n := len(f.up.recorded(tc.name)); n != tc.attempts {
				t.Fatalf("attempts=%d want=%d", n, tc.attempts)
			}
			stored, err := f.st.GetMessage(t.Context(), done.ID)
			if err != nil || stored.Status != done.Status || stored.Content != done.Content {
				t.Fatalf("stored=%+v err=%v", stored, err)
			}
		})
	}
}

func TestChatEditTruncatesAndRegenerateReplacesAssistant(t *testing.T) {
	f := newAPIFixture(t)
	first := f.send(map[string]any{"content": "first"})
	f.stream(first.Assistant.ID).finish()
	f.waitRun(first.Assistant.ID)
	second := f.send(map[string]any{"conversation_id": first.Conversation.ID, "content": "second"})
	f.stream(second.Assistant.ID).finish()
	f.waitRun(second.Assistant.ID)
	edited := f.send(map[string]any{"conversation_id": first.Conversation.ID, "edit_of": first.UserMessage.ID, "content": "edited"})
	f.stream(edited.Assistant.ID).finish()
	f.waitRun(edited.Assistant.ID)
	for _, m := range []*store.Message{first.UserMessage, first.Assistant, second.UserMessage, second.Assistant} {
		if _, err := f.st.GetMessage(t.Context(), m.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("old message %s still exists: %v", m.ID, err)
		}
	}
	ms, err := f.st.ListMessages(t.Context(), first.Conversation.ID, "", 100)
	if err != nil || len(ms) != 2 || ms[0].ID != edited.UserMessage.ID || ms[0].Content != "edited" || ms[1].ID != edited.Assistant.ID {
		t.Fatalf("edited history=%+v err=%v", ms, err)
	}
	var redo chat.SendResult
	f.json("POST", "/api/conversations/"+first.Conversation.ID+"/regenerate", map[string]any{}, 200, &redo)
	if redo.Assistant == nil || redo.Assistant.ID == edited.Assistant.ID {
		t.Fatalf("regenerate=%+v", redo)
	}
	f.stream(redo.Assistant.ID).finish()
	f.waitRun(redo.Assistant.ID)
	if _, err := f.st.GetMessage(t.Context(), edited.Assistant.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old assistant remains: %v", err)
	}
	ms, err = f.st.ListMessages(t.Context(), first.Conversation.ID, "", 100)
	if err != nil || len(ms) != 2 || ms[0].ID != edited.UserMessage.ID || ms[1].ID != redo.Assistant.ID {
		t.Fatalf("regenerated history=%+v err=%v", ms, err)
	}
}

func TestUploadedImageAndReasoningEffortReachUpstream(t *testing.T) {
	f := newAPIFixture(t)
	file, png := f.uploadPNG()
	var downloaded bytes.Buffer
	req, err := f.http.Client().Get(f.http.URL + "/api/files/" + file.ID + "?access_token=" + f.token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = downloaded.ReadFrom(req.Body)
	req.Body.Close()
	if err != nil || req.StatusCode != 200 || !bytes.Equal(downloaded.Bytes(), png) {
		t.Fatalf("download status=%d err=%v", req.StatusCode, err)
	}
	for _, vision := range []bool{true, false} {
		f.model.Vision = vision
		if err := f.st.UpsertModel(t.Context(), f.model); err != nil {
			t.Fatal(err)
		}
		for _, think := range []bool{false, true} {
			prompt := "image"
			if !vision {
				prompt += "-text"
			}
			if think {
				prompt += "-think"
			}
			res := f.send(map[string]any{"content": prompt, "think": think, "attachments": []store.Attachment{{FileID: file.ID, Mime: "forged"}}})
			f.stream(res.Assistant.ID).finish()
			f.waitRun(res.Assistant.ID)
			key := prompt
			if !vision {
				key = "[用户发送了 1 张图片，当前已不可见]\n" + prompt
			}
			reqs := f.up.recorded(key)
			if len(reqs) != 1 {
				t.Fatalf("upstream requests for %q: %d", key, len(reqs))
			}
			r := reqs[0]
			effort := f.model.DefaultEffort
			if think {
				effort = f.model.ThinkEffort
			}
			if r.Effort != effort || r.Model != f.model.UpstreamModel {
				t.Fatalf("effort=%q model=%q", r.Effort, r.Model)
			}
			last := r.Messages[len(r.Messages)-1]
			if vision {
				var parts []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL *struct {
						URL string `json:"url"`
					} `json:"image_url"`
				}
				if err := json.Unmarshal(last.Content, &parts); err != nil {
					t.Fatal(err)
				}
				if len(parts) != 2 || parts[0].Type != "image_url" || parts[0].ImageURL == nil || parts[0].ImageURL.URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png) || parts[1].Text != prompt {
					t.Fatalf("vision parts=%s", last.Content)
				}
			} else {
				var text string
				if err := json.Unmarshal(last.Content, &text); err != nil || text != key || bytes.Contains(last.Content, []byte("image_url")) {
					t.Fatalf("nonvision content=%s err=%v", last.Content, err)
				}
			}
			if res.UserMessage.Attachments[0].Mime != "image/png" {
				t.Fatal("attachment metadata was not verified")
			}
		}
	}
}

func TestDeleteConversationCancelsGeneration(t *testing.T) {
	f := newAPIFixture(t)
	f.up.plan("delete", upstreamPlan{tokens: []string{"partial"}, blockAfter: true})
	res := f.send(map[string]any{"content": "delete"})
	eventually(t, "partial output", func() bool {
		r := f.svc.Hub.Get(res.Assistant.ID)
		return r != nil && r.Snapshot().Content == "partial"
	})
	f.json("DELETE", "/api/conversations/"+res.Conversation.ID, nil, 200, nil)
	select {
	case <-f.up.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("deleting conversation did not cancel upstream")
	}
	f.waitRun(res.Assistant.ID)
	if _, err := f.st.GetConversation(t.Context(), res.Conversation.ID, f.user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := f.st.GetMessage(t.Context(), res.Assistant.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestChatStreamsTitleEvent(t *testing.T) {
	f := newAPIFixture(t)
	titleGate := make(chan struct{}, 1)
	steps := make(chan struct{}, 1)
	t.Cleanup(func() { close(titleGate) })
	f.up.plan("title-event", upstreamPlan{tokens: []string{"answer"}, steps: steps, titleSteps: titleGate})
	var res chat.SendResult
	f.json("POST", "/api/chat", map[string]any{"content": "title-event"}, 200, &res)
	if res.Assistant == nil {
		t.Fatal("missing assistant")
	}
	s := f.stream(res.Assistant.ID)
	e := s.next()
	if e.name != "snapshot" || decodeEvent[streamText](t, e).Title != "" {
		t.Fatalf("initial event=%s %s", e.name, e.data)
	}
	// Release title generation while the main completion remains in flight.
	titleGate <- struct{}{}
	e = s.next()
	if e.name != "title" {
		t.Fatalf("expected title event, got %s %s", e.name, e.data)
	}
	var title struct {
		ConversationID string `json:"conversation_id"`
		Title          string `json:"title"`
	}
	if err := json.Unmarshal(e.data, &title); err != nil {
		t.Fatal(err)
	}
	if title.Title != "家庭测试标题" || title.ConversationID != res.Conversation.ID {
		t.Fatalf("title event=%+v", title)
	}
	c, err := f.st.GetConversation(t.Context(), res.Conversation.ID, f.user.ID)
	if err != nil || c.Title != title.Title {
		t.Fatalf("title not persisted before event: %+v %v", c, err)
	}
	steps <- struct{}{}
	text, done, _ := s.finish()
	if text.Content != "answer" || done.Status != store.StatusDone {
		t.Fatalf("text=%+v done=%+v", text, done)
	}
}

func TestChatEndpointsIsolateUsers(t *testing.T) {
	f := newAPIFixture(t)
	f.up.plan("private", upstreamPlan{tokens: []string{"private answer"}, blockAfter: true})
	res := f.send(map[string]any{"content": "private"})
	other := &store.User{Username: "bob", DisplayName: "Bob", Role: "member", PasswordHash: passwordHash}
	if err := f.st.CreateUser(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	token, err := f.api.Sessions.Create(t.Context(), other.ID, "other device")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/conversations/" + res.Conversation.ID,
		"/api/conversations/" + res.Conversation.ID + "/messages",
		"/api/messages/" + res.Assistant.ID + "/stream",
	} {
		f.request("GET", path, token, "", nil, 404, nil)
	}
	var cancelled struct{ OK bool }
	f.request("POST", "/api/messages/"+res.Assistant.ID+"/cancel", token, "application/json", strings.NewReader(`{}`), 200, &cancelled)
	if cancelled.OK {
		t.Fatal("another user cancelled the run")
	}
	f.request("DELETE", "/api/conversations/"+res.Conversation.ID, token, "", nil, 404, nil)
	if f.svc.Hub.Get(res.Assistant.ID) == nil {
		t.Fatal("unauthorized deletion cancelled the run")
	}
	f.json("POST", "/api/messages/"+res.Assistant.ID+"/cancel", map[string]any{}, 200, nil)
	_, done, _ := f.stream(res.Assistant.ID).finish()
	if done.Status != store.StatusCancelled {
		t.Fatalf("owner could not cancel: %+v", done)
	}
}
