package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunSnapshotNotificationsAndFinish(t *testing.T) {
	h := NewHub()
	r := &Run{MessageID: "message", ConversationID: "conversation", UserID: "user"}
	release := make(chan struct{})
	h.start(r, func(ctx context.Context) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		r.finish("persisted")
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		h.Shutdown(ctx, errors.New("cleanup"))
	})
	if h.Get(r.MessageID) != r || h.ActiveInConversation(r.ConversationID) != r || h.ActiveForUser(r.UserID) != 1 {
		t.Fatal("run not indexed")
	}
	wake, unsub := r.Subscribe()
	for i := 0; i < 1000; i++ {
		r.appendContent("字")
		r.appendReasoning("想")
	}
	r.setTitle("title")
	if len(wake) != 1 || cap(wake) != 1 {
		t.Fatalf("notifications not coalesced: len=%d cap=%d", len(wake), cap(wake))
	}
	<-wake
	snap := r.Snapshot()
	if snap.Content != strings.Repeat("字", 1000) || snap.Reasoning != strings.Repeat("想", 1000) || snap.Title != "title" || snap.Final != nil {
		t.Fatalf("snapshot=%+v", snap)
	}
	unsub()
	unsub() // removing an existing subscription twice is safe
	r.appendContent("!")
	if len(wake) != 0 {
		t.Fatal("unsubscribed channel was notified")
	}
	close(release)
	select {
	case <-r.Done():
	case <-time.After(time.Second):
		t.Fatal("finish did not close Done")
	}
	if r.Snapshot().Final != "persisted" {
		t.Fatal("Done closed before final state became visible")
	}
	late, stop := r.Subscribe()
	defer stop()
	if cap(late) != 1 || r.Snapshot().Final != "persisted" {
		t.Fatal("late subscriber cannot replay final")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	h.Shutdown(ctx, errors.New("shutdown"))
	if h.Get(r.MessageID) != nil || h.ActiveInConversation(r.ConversationID) != nil || h.ActiveForUser(r.UserID) != 0 {
		t.Fatal("completed run still indexed")
	}
}

func TestHubConcurrentSubscribersReadByByteOffset(t *testing.T) {
	h := NewHub()
	r := &Run{MessageID: "m", ConversationID: "c", UserID: "u"}
	start := make(chan struct{})
	h.start(r, func(ctx context.Context) {
		select {
		case <-start:
		case <-ctx.Done():
			r.finish("cancelled")
			return
		}
		for i := 0; i < 500; i++ {
			r.appendContent("你🙂")
			r.appendReasoning("想")
		}
		r.setTitle("并发标题")
		r.finish("done")
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	t.Cleanup(func() { h.Shutdown(ctx, errors.New("cleanup")) })
	var wg sync.WaitGroup
	ready := make(chan struct{}, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wake, unsub := r.Subscribe()
			defer unsub()
			ready <- struct{}{}
			var content, reasoning string
			offC, offR := 0, 0
			for {
				snap := r.Snapshot()
				content += snap.Content[offC:]
				reasoning += snap.Reasoning[offR:]
				offC, offR = len(snap.Content), len(snap.Reasoning)
				if snap.Final != nil {
					if content != strings.Repeat("你🙂", 500) || reasoning != strings.Repeat("想", 500) || snap.Title != "并发标题" || snap.Final != "done" {
						t.Errorf("subscriber lost output: content bytes=%d reasoning bytes=%d final=%v", len(content), len(reasoning), snap.Final)
					}
					return
				}
				select {
				case <-wake:
				case <-r.Done():
				case <-ctx.Done():
					t.Error("subscriber timed out")
					return
				}
			}
		}()
	}
	for i := 0; i < 32; i++ {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("subscribers failed to start")
		}
	}
	close(start)
	wg.Wait()
	h.Shutdown(ctx, errors.New("shutdown"))
}

func TestHubShutdownCancelsEveryRunAndWaits(t *testing.T) {
	h := NewHub()
	cause := errors.New("shutdown cause")
	runs := make([]*Run, 6)
	for i := range runs {
		r := &Run{MessageID: fmt.Sprint(i), ConversationID: fmt.Sprint(i), UserID: fmt.Sprint(i % 2)}
		runs[i] = r
		h.start(r, func(ctx context.Context) { <-ctx.Done(); r.finish(context.Cause(ctx)) })
	}
	if h.ActiveForUser("0") != 3 || h.ActiveForUser("1") != 3 {
		t.Fatal("wrong per-user counts")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	h.Shutdown(ctx, cause)
	for _, r := range runs {
		select {
		case <-r.Done():
		default:
			t.Fatal("shutdown returned before finish")
		}
		if r.Snapshot().Final != cause || h.Get(r.MessageID) != nil || h.ActiveForUser(r.UserID) != 0 {
			t.Fatalf("run not cancelled and removed: %+v", r.Snapshot())
		}
	}
}
