package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"nuan/internal/store"
)

type kick struct {
	conv string
	due  time.Time
}
type Worker struct {
	service  *Service
	ctx      context.Context
	cancel   context.CancelFunc
	kicks    chan kick
	wg       sync.WaitGroup
	mu       sync.Mutex
	busy     map[string]bool
	failures map[string]int
	slots    chan struct{}
}

func NewWorker(s *Service) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{service: s, ctx: ctx, cancel: cancel, kicks: make(chan kick, 128), busy: map[string]bool{}, failures: map[string]int{}, slots: make(chan struct{}, 2)}
	w.wg.Add(1)
	go w.loop()
	return w
}
func Trigger(text string) bool {
	for _, term := range []string{"记住", "别忘", "以后", "停药", "换药", "不吃了", "过敏", "搬家", "生日", "去世", "住院", "出院", "怀孕", "忘掉", "别记"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
func (w *Worker) Kick(convID string, delay time.Duration) {
	select {
	case w.kicks <- kick{convID, time.Now().Add(delay)}:
	case <-w.ctx.Done():
	default:
		w.service.Log.Warn("memory kick queue full", "conv", convID)
	}
}
func (w *Worker) Shutdown(ctx context.Context) {
	w.cancel()
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
func (w *Worker) loop() {
	defer w.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	pending := map[string]time.Time{}
	reset := func() {
		d := time.Hour
		for _, due := range pending {
			d = min(d, max(0, time.Until(due)))
		}
		timer.Reset(d)
	}
	w.scan(nil)
	for {
		select {
		case <-w.ctx.Done():
			return
		case k := <-w.kicks:
			pending[k.conv] = k.due
			reset()
		case <-ticker.C:
			w.scan(nil)
		case <-timer.C:
			due := map[string]bool{}
			for id, t := range pending {
				if !time.Now().Before(t) {
					due[id] = true
					delete(pending, id)
				}
			}
			w.scan(due)
			reset()
		}
	}
}
func (w *Worker) scan(immediate map[string]bool) {
	cs, err := w.service.Store.MemoryCandidates(w.ctx, store.Now())
	if err != nil {
		if w.ctx.Err() == nil {
			w.service.Log.Warn("memory scan", "err", err)
		}
		return
	}
	groups := map[string][]*store.Conversation{}
	order := []string{}
	for _, c := range cs {
		if !immediate[c.ID] && c.UpdatedAt > time.Now().Add(-time.Minute).UnixMilli() {
			continue
		}
		if len(groups[c.UserID]) == 0 {
			order = append(order, c.UserID)
		}
		groups[c.UserID] = append(groups[c.UserID], c)
	}
	retryKick := func(convs []*store.Conversation) {
		for _, c := range convs {
			if immediate[c.ID] {
				w.Kick(c.ID, time.Second)
			}
		}
	}
	for _, user := range order {
		w.mu.Lock()
		busy := w.busy[user]
		if !busy {
			w.busy[user] = true
		}
		w.mu.Unlock()
		if busy {
			retryKick(groups[user])
			continue
		}
		select {
		case w.slots <- struct{}{}:
		default:
			w.mu.Lock()
			delete(w.busy, user)
			w.mu.Unlock()
			retryKick(groups[user])
			continue
		}
		w.wg.Add(1)
		go func(user string, convs []*store.Conversation) {
			defer w.wg.Done()
			defer func() { <-w.slots; w.mu.Lock(); delete(w.busy, user); w.mu.Unlock() }()
			for _, c := range convs {
				if err := w.Process(w.ctx, c); err != nil {
					w.service.Log.Warn("memory extraction", "conv", c.ID, "err", err)
					break
				}
			}
		}(user, groups[user])
	}
	// Embedding writes use content/version guards and can run alongside extraction.
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		if err := w.service.FillEmbeddings(w.ctx); err != nil && w.ctx.Err() == nil {
			w.service.Log.Warn("memory embedding backfill", "err", err)
		}
	}()
}

// Process drains a conversation in chronological segments. The scheduler owns the
// per-user lock; callers using Process directly must serialize the same user.
func (w *Worker) Process(ctx context.Context, c *store.Conversation) error {
	for {
		u, err := w.service.Store.GetUser(ctx, c.UserID)
		if err != nil {
			return err
		}
		cursor, err := w.service.Store.MemoryCursor(ctx, c.ID)
		if err != nil {
			return err
		}
		ms, err := w.service.Store.MemoryMessages(ctx, c.ID, cursor)
		if err != nil || len(ms) == 0 {
			return err
		}
		for _, m := range ms {
			if m.Status == store.StatusStreaming {
				return nil
			}
		}
		snap := store.MemorySnapshot{ConversationID: c.ID, UserID: c.UserID, Cursor: cursor, Messages: segment(ms)}
		key := c.ID + ":" + cursor + ":" + snap.Messages[len(snap.Messages)-1].ID
		allowed, err := w.service.allowed(ctx, u, c.AssistantID)
		if err != nil {
			return err
		}
		if !allowed {
			if err := w.service.Store.ApplyMemoryExtraction(ctx, snap, nil, ""); err != nil {
				return err
			}
			continue
		}
		ectx, cancel := context.WithTimeout(ctx, 90*time.Second)
		out, existing, err := w.service.extract(ectx, u, c, snap)
		cancel()
		if errors.Is(err, ErrParse) {
			w.mu.Lock()
			w.failures[key]++
			failures := w.failures[key]
			w.mu.Unlock()
			if failures < 3 {
				return err
			}
			w.service.Log.Warn("skip unparseable memory segment", "conv", c.ID)
			if err := w.service.Store.ApplyMemoryExtraction(ctx, snap, nil, ""); err != nil {
				return err
			}
		} else if err != nil {
			// A transport/API error interrupts a consecutive JSON failure streak.
			w.mu.Lock()
			delete(w.failures, key)
			w.mu.Unlock()
			return err
		} else if err := w.service.apply(ctx, snap, out, existing); err != nil {
			return err
		}
		w.mu.Lock()
		delete(w.failures, key)
		w.mu.Unlock()
		if err := w.service.FillEmbeddings(ctx); err != nil {
			w.service.Log.Warn("embed extracted memories", "err", err)
		}
	}
}
func (s *Service) FillEmbeddings(ctx context.Context) error {
	if !s.embedMu.TryLock() {
		return nil
	}
	defer s.embedMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	m, c, err := s.Resolve(ctx, "embedding")
	if err != nil || m == nil {
		return err
	}
	ms, err := s.Store.PendingEmbeddings(ctx, m.ID)
	if err != nil || len(ms) == 0 {
		return err
	}
	inputs := make([]string, len(ms))
	for i, m := range ms {
		inputs[i] = m.Content
	}
	vectors, _, err := c.Embed(ctx, m.UpstreamModel, inputs)
	if err != nil {
		return err
	}
	for i, item := range ms {
		if err := s.Store.SetMemoryEmbedding(ctx, item, vectors[i], m.ID); err != nil {
			return err
		}
	}
	return nil
}
