package chat

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nuan/internal/store"
)

// Run is one in-flight assistant generation. Output accumulates in memory;
// any number of subscribers can attach at any time, receive a snapshot of
// everything so far and then follow new output.
//
// Subscribers do not get a per-token queue. They hold byte offsets into the
// accumulated text and are woken through a 1-slot notify channel, so a slow
// client never blocks the generator and bursts of tokens are coalesced.
// RunStep describes one discrete task performed while preparing an assistant reply
// (such as recalling memories, searching the web, or reading a web page).
type RunStep struct {
	Kind      string `json:"kind"`             // "memory" | "history" | "search" | "fetch" | "image" | "skill" | "schedule" | "component" | "ask_user"
	Title     string `json:"title"`            // short human-readable task label
	Detail    string `json:"detail,omitempty"` // optional outcome summary
	Status    string `json:"status"`           // "running" | "done" | "error"
	StartedAt int64  `json:"started_at,omitempty"`
}

type Run struct {
	MessageID      string
	ConversationID string
	UserID         string
	UserName       string

	cancel context.CancelCauseFunc
	done   chan struct{}

	mu            sync.Mutex
	content       strings.Builder
	reasoning     strings.Builder
	title         string
	modelID       string                   // resolved/updated model ID (e.g. Auto mode or fallback)
	activity      string                   // what a tool is doing right now ("正在画图…"), "" when idle
	activityAt    int64                    // unix ms when the activity started
	steps         []RunStep                // chronological task trace for memory/search/fetch
	stepsSeq      int                      // monotonic counter incremented on step mutations
	attachments   []store.Attachment       // pictures drawn so far
	sources       []store.WebSource        // web pages referenced so far
	components    []store.MessageComponent // rich UI cards interleaved in this reply
	componentsSeq int                      // monotonic counter incremented on component additions/updates
	final         any                      // set when finished: the final message payload
	subs          map[chan struct{}]struct{}
}

// Snapshot is the complete visible state of a run at one point in time.
type Snapshot struct {
	Content       string
	Reasoning     string
	Title         string
	ModelID       string
	Activity      string
	ActivityAt    int64
	Steps         []RunStep
	StepsSeq      int
	Attachments   []store.Attachment
	Sources       []store.WebSource
	Components    []store.MessageComponent
	ComponentsSeq int
	Final         any // non-nil once the run has finished
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

func (r *Run) setModelID(id string) {
	r.mu.Lock()
	r.modelID = id
	r.notifyLocked()
	r.mu.Unlock()
}

func (r *Run) setActivity(a string) {
	r.mu.Lock()
	if r.activity == a {
		r.mu.Unlock()
		return
	}
	r.activity = a
	r.activityAt = 0
	if a != "" {
		r.activityAt = time.Now().UnixMilli()
	}
	r.notifyLocked()
	r.mu.Unlock()
}

func (r *Run) startStep(kind, title, detail string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := len(r.steps)
	r.steps = append(r.steps, RunStep{
		Kind:      kind,
		Title:     title,
		Detail:    detail,
		Status:    "running",
		StartedAt: time.Now().UnixMilli(),
	})
	r.stepsSeq++
	r.notifyLocked()
	return idx
}

func (r *Run) addDoneStep(kind, title, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, RunStep{
		Kind:   kind,
		Title:  title,
		Detail: detail,
		Status: "done",
	})
	r.stepsSeq++
	r.notifyLocked()
}

func (r *Run) finishStep(idx int, status, title, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx < 0 || idx >= len(r.steps) {
		return
	}
	if status != "" {
		r.steps[idx].Status = status
	}
	if title != "" {
		r.steps[idx].Title = title
	}
	r.steps[idx].Detail = detail
	r.stepsSeq++
	r.notifyLocked()
}

func (r *Run) setSources(sources []store.WebSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources = append([]store.WebSource(nil), sources...)
	r.notifyLocked()
}

func (r *Run) addAttachment(a store.Attachment) []store.Attachment {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attachments = append(r.attachments, a)
	r.notifyLocked()
	return append([]store.Attachment(nil), r.attachments...)
}

func (r *Run) addComponent(c store.MessageComponent) []store.MessageComponent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.Pos < 0 {
		c.Pos = utf8.RuneCountInString(r.content.String())
	}
	r.components = append(r.components, c)
	r.componentsSeq++
	r.notifyLocked()
	return append([]store.MessageComponent(nil), r.components...)
}

// UpdateComponentState synchronizes an in-flight run's component state if the user
// interacts with a rendered card while the message is still streaming.
func (r *Run) UpdateComponentState(compID string, state json.RawMessage) bool {
	return r.UpdateComponent(compID, nil, state)
}

// UpdateComponent synchronizes an in-flight run's component data and/or state.
func (r *Run) UpdateComponent(compID string, data, state json.RawMessage) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.components {
		if r.components[i].ID == compID {
			if len(data) > 0 {
				r.components[i].Data = append(json.RawMessage(nil), data...)
			}
			if len(state) > 0 {
				r.components[i].State = append(json.RawMessage(nil), state...)
			}
			r.componentsSeq++
			r.notifyLocked()
			return true
		}
	}
	return false
}

func (r *Run) contentLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.content.Len()
}

func (r *Run) reasoningLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reasoning.Len()
}

func (r *Run) text() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.content.String(), r.reasoning.String()
}

func (r *Run) finish(final any) {
	r.mu.Lock()
	r.activity = ""
	r.activityAt = 0
	r.final = final
	r.notifyLocked()
	r.mu.Unlock()
	close(r.done)
}

// Snapshot returns the current state.
func (r *Run) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{
		Content: r.content.String(), Reasoning: r.reasoning.String(), Title: r.title, ModelID: r.modelID,
		Activity: r.activity, ActivityAt: r.activityAt,
		Steps: append([]RunStep(nil), r.steps...), StepsSeq: r.stepsSeq,
		Attachments:   append([]store.Attachment(nil), r.attachments...),
		Sources:       append([]store.WebSource(nil), r.sources...),
		Components:    append([]store.MessageComponent(nil), r.components...),
		ComponentsSeq: r.componentsSeq,
		Final:         r.final,
	}
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

// registerDetached registers an externally managed background run (such as a scheduled
// automation task) so clients can attach to its live SSE stream without consuming the
// user's interactive chat concurrency slot. Call the returned cleanup func after run.finish.
func (h *Hub) registerDetached(r *Run, cancel context.CancelCauseFunc) func() {
	r.cancel = cancel
	if r.done == nil {
		r.done = make(chan struct{})
	}
	if r.subs == nil {
		r.subs = map[chan struct{}]struct{}{}
	}
	h.mu.Lock()
	h.runs[r.MessageID] = r
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.runs, r.MessageID)
		h.mu.Unlock()
	}
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
