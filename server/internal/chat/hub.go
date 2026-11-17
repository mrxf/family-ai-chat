package chat

import (
	"context"
	"strings"
	"sync"
)

// Run is one in-flight assistant generation. Output accumulates in memory;
// any number of subscribers can attach at any time, receive a snapshot of
// everything so far and then follow new output.
//
// Subscribers do not get a per-token queue. They hold byte offsets into the
// accumulated text and are woken through a 1-slot notify channel, so a slow
// client never blocks the generator and bursts of tokens are coalesced.
type Run struct {
	MessageID      string
	ConversationID string
	UserID         string

	cancel context.CancelCauseFunc
	done   chan struct{}

	mu        sync.Mutex
	content   strings.Builder
	reasoning strings.Builder
	title     string
	final     any // set when finished: the final message payload
	subs      map[chan struct{}]struct{}
}

// Snapshot is the complete visible state of a run at one point in time.
type Snapshot struct {
	Content   string
	Reasoning string
	Title     string
	Final     any // non-nil once the run has finished
}

func (r *Run) notifyLocked() {
	for ch := range r.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (r *Run) appendContent(s string) {
	r.mu.Lock()
	r.content.WriteString(s)
	r.notifyLocked()
	r.mu.Unlock()
}

func (r *Run) appendReasoning(s string) {
	r.mu.Lock()
	r.reasoning.WriteString(s)
	r.notifyLocked()
	r.mu.Unlock()
}

func (r *Run) setTitle(t string) {
	r.mu.Lock()
	r.title = t
	r.notifyLocked()
	r.mu.Unlock()
}

func (r *Run) text() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.content.String(), r.reasoning.String()
}

func (r *Run) finish(final any) {
	r.mu.Lock()
	r.final = final
	r.notifyLocked()
	r.mu.Unlock()
	close(r.done)
}

// Snapshot returns the current state.
func (r *Run) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{Content: r.content.String(), Reasoning: r.reasoning.String(), Title: r.title, Final: r.final}
}

// Subscribe registers a wake-up channel. Call the returned func to unsubscribe.
func (r *Run) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}
}

// Cancel stops the generation; the partial output is kept.
func (r *Run) Cancel(cause error) { r.cancel(cause) }

// Done is closed when the run has finished and been persisted.
func (r *Run) Done() <-chan struct{} { return r.done }

// Hub indexes in-flight runs by assistant message id.
type Hub struct {
	mu     sync.Mutex
	runs   map[string]*Run
	byUser map[string]int
	wg     sync.WaitGroup
}

func NewHub() *Hub {
	return &Hub{runs: map[string]*Run{}, byUser: map[string]int{}}
}

func (h *Hub) Get(messageID string) *Run {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runs[messageID]
}

// ActiveForUser reports how many runs the user has in flight.
func (h *Hub) ActiveForUser(userID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.byUser[userID]
}

// ActiveInConversation returns the running generation of a conversation, if any.
func (h *Hub) ActiveInConversation(convID string) *Run {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.runs {
		if r.ConversationID == convID {
			return r
		}
	}
	return nil
}

// start registers a run and executes fn in its own goroutine, detached from
// any HTTP request. fn must call run.finish when done.
func (h *Hub) start(r *Run, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	r.subs = map[chan struct{}]struct{}{}

	h.mu.Lock()
	h.runs[r.MessageID] = r
	h.byUser[r.UserID]++
	h.mu.Unlock()

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer cancel(nil)
		defer func() {
			h.mu.Lock()
			delete(h.runs, r.MessageID)
			if h.byUser[r.UserID]--; h.byUser[r.UserID] <= 0 {
				delete(h.byUser, r.UserID)
			}
			h.mu.Unlock()
		}()
		fn(ctx)
	}()
}

// Shutdown cancels every run and waits for them to persist their partial output.
func (h *Hub) Shutdown(ctx context.Context, cause error) {
	h.mu.Lock()
	for _, r := range h.runs {
		r.cancel(cause)
	}
	h.mu.Unlock()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
