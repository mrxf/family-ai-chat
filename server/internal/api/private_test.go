package api

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/memory"
	"nuan/internal/store"
)

type spyMemoryWorker struct {
	kicks atomic.Int32
}

func (w *spyMemoryWorker) Kick(string, time.Duration) { w.kicks.Add(1) }
func (w *spyMemoryWorker) Shutdown(context.Context)   {}

func TestPrivateChatEndToEndIsolationAndCleanup(t *testing.T) {
	f := newAPIFixture(t)
	spy := &spyMemoryWorker{}
	f.svc.MemoryWorker = spy

	// Seed an existing user memory to verify private chat never injects it.
	if err := f.st.CreateMemories(t.Context(), []*store.Memory{
		{UserID: f.user.ID, Category: "health", Content: "用户每天早上吃氨氯地平 5mg", Pinned: true},
	}); err != nil {
		t.Fatal(err)
	}

	// Upload a private image via ?private=1 and verify Cache-Control: private, no-store.
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "secret.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(img.Bytes())
	_ = mw.Close()
	var uploaded store.File
	f.request("POST", "/api/files?private=1", f.token, mw.FormDataContentType(), &body, 201, &uploaded)
	if !uploaded.Private {
		t.Fatalf("expected uploaded file to be marked private: %+v", uploaded)
	}
	storedFile, err := f.st.GetFile(t.Context(), uploaded.ID, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", f.http.URL+"/api/files/"+uploaded.ID, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control=%q, want private, no-store", got)
	}

	// Send a private message containing a strong memory trigger word ("记住") and the image.
	res := f.send(map[string]any{
		"private":     true,
		"content":     "记住我对芒果严重过敏，这是化验单",
		"attachments": []map[string]string{{"file_id": uploaded.ID}},
	})
	if !res.Conversation.Private {
		t.Fatalf("expected private conversation: %+v", res.Conversation)
	}
	_, doneMsg, events := f.stream(res.Assistant.ID).finish()
	if doneMsg.Status != store.StatusDone {
		t.Fatalf("assistant status=%s err=%s", doneMsg.Status, doneMsg.Error)
	}
	for _, ev := range events {
		if ev == "title" {
			t.Fatal("private conversation must not emit title event")
		}
	}

	// 1. MemoryWorker.Kick must NEVER fire for private chat.
	if got := spy.kicks.Load(); got != 0 {
		t.Fatalf("expected 0 memory kicks, got %d", got)
	}
	// 2. Upstream prompt must NOT contain existing memories or user's DisplayName, and no title request was made.
	reqs := f.up.recorded("记住我对芒果严重过敏，这是化验单")
	if len(reqs) != 1 {
		t.Fatalf("expected 1 upstream chat request, got %d", len(reqs))
	}
	sys := string(reqs[0].Messages[0].Content)
	if strings.Contains(sys, "氨氯地平") || !strings.Contains(sys, "隐私对话模式") {
		t.Fatalf("unexpected system prompt in private chat: %s", sys)
	}
	f.up.mu.Lock()
	totalUpstreamReqs := len(f.up.requests)
	f.up.mu.Unlock()
	if totalUpstreamReqs != 1 {
		t.Fatalf("expected no background title LLM request, got %d total requests", totalUpstreamReqs)
	}

	// 3. Even running MemoryWorker.Process directly or scanning candidates yields zero memories.
	candidates, err := f.st.MemoryCandidates(t.Context(), store.Now()+60_000)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("MemoryCandidates=%+v err=%v", candidates, err)
	}
	w := memory.NewWorker(f.svc.Memory.(*memory.Service))
	t.Cleanup(func() { w.Shutdown(context.Background()) })
	if err := w.Process(t.Context(), res.Conversation); err != nil {
		t.Fatal(err)
	}
	mems, err := f.st.ListMemories(t.Context(), f.user.ID)
	if err != nil || len(mems) != 1 {
		t.Fatalf("memories changed after private chat: %+v err=%v", mems, err)
	}

	// 4. Follow-up turn without repeating "private": true still stays private.
	res2 := f.send(map[string]any{
		"conversation_id": res.Conversation.ID,
		"content":         "那菠萝能吃吗",
	})
	if !res2.Conversation.Private {
		t.Fatal("follow-up turn lost private flag")
	}
	f.waitRun(res2.Assistant.ID)

	// 5. History list and full-text search must NOT return the private conversation.
	var list struct {
		Items []*store.Conversation `json:"items"`
	}
	f.json("GET", "/api/conversations", nil, 200, &list)
	if len(list.Items) != 0 {
		t.Fatalf("private conversation leaked into list: %+v", list.Items)
	}
	f.json("GET", "/api/conversations?q=芒果", nil, 200, &list)
	if len(list.Items) != 0 {
		t.Fatalf("private conversation leaked into search: %+v", list.Items)
	}

	// 6. Sending private:true into a normal conversation must be rejected with 400 private_mismatch.
	normal := f.send(map[string]any{"content": "普通问候"})
	f.waitRun(normal.Assistant.ID)
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	f.json("POST", "/api/chat", map[string]any{
		"conversation_id": normal.Conversation.ID,
		"content":         "悄悄话",
		"private":         true,
	}, 400, &apiErr)
	if apiErr.Error.Code != "private_mismatch" {
		t.Fatalf("expected private_mismatch, got %+v", apiErr)
	}

	// 7. Closing (DELETE) the private conversation immediately wipes DB rows AND the on-disk image file.
	f.json("DELETE", "/api/conversations/"+res.Conversation.ID, nil, 200, nil)
	f.request("GET", "/api/conversations/"+res.Conversation.ID, f.token, "", nil, 404, nil)
	f.request("GET", "/api/files/"+uploaded.ID, f.token, "", nil, 404, nil)
	if _, err := os.Stat(storedFile.Path); !os.IsNotExist(err) {
		t.Fatalf("expected private file deleted from disk immediately, stat err=%v", err)
	}
}
