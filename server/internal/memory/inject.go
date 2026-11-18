// Package memory provides shared user memories independently of the chat model.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/store"
)

type ModelResolver func(context.Context, string) (*store.Model, *llm.Client, error)
type Service struct {
	Store   *store.Store
	Resolve ModelResolver
	Log     *slog.Logger
	embedMu sync.Mutex
}

func New(st *store.Store, resolve ModelResolver, log *slog.Logger) *Service {
	return &Service{Store: st, Resolve: resolve, Log: log}
}

type Context struct{ Core, Recall string }

func Enabled(u *store.User) bool {
	var settings map[string]any
	_ = json.Unmarshal(u.Settings, &settings)
	v, ok := settings["memory_enabled"].(bool)
	return !ok || v
}
func (s *Service) allowed(ctx context.Context, u *store.User, assistant *string) (bool, error) {
	if !Enabled(u) {
		return false, nil
	}
	if assistant != nil {
		a, err := s.Store.GetAssistant(ctx, *assistant, u.ID)
		if err != nil {
			return false, err
		}
		return a.UseMemory, nil
	}
	return true, nil
}
func (s *Service) Allowed(ctx context.Context, u *store.User, assistant *string) bool {
	ok, err := s.allowed(ctx, u, assistant)
	if err != nil {
		s.Log.Warn("memory permission lookup", "err", err)
	}
	return ok && err == nil
}

func isCore(m *store.Memory, now int64) bool {
	return m.Pinned && m.Category != "episode" && !(m.Category == "ongoing" && m.ValidUntil != nil && *m.ValidUntil < now)
}

var labels = map[string]string{"health": "健康与用药", "family": "家人", "profile": "关于我", "preference": "喜好习惯", "ongoing": "近期在做"}

func (s *Service) Inject(ctx context.Context, u *store.User, assistant *string, query string) Context {
	if !s.Allowed(ctx, u, assistant) {
		return Context{}
	}
	ms, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil {
		s.Log.Warn("load memories", "err", err)
		return Context{}
	}
	var core strings.Builder
	recall := []*store.Memory{}
	category := ""
	n := 0
	for _, m := range ms {
		if m.AssistantID != nil && (assistant == nil || *m.AssistantID != *assistant) {
			continue
		}
		if !isCore(m, store.Now()) {
			recall = append(recall, m)
			continue
		}
		if n >= CoreHardLimit {
			s.Log.Warn("memory core truncated", "user", u.ID)
			continue
		}
		if core.Len() == 0 {
			core.WriteString("\n\n# 你记得的关于用户的事\n这些是以前对话里记下的，可能有过时之处。自然地用上，不要生硬地说「我记得……」，除非用户问起；和用户这次说的不一致时，以这次为准，可以顺口确认一下。\n")
		}
		if category != m.Category {
			category = m.Category
			fmt.Fprintf(&core, "【%s】\n", labels[category])
		}
		content := clip(m.Content, CoreHardLimit-n)
		fmt.Fprintf(&core, "- %s\n", content)
		n += utf8.RuneCountInString(content)
	}
	if len(recall) == 0 {
		return Context{Core: core.String()}
	}
	// The embedding round trip (~0.6-1.2 s on OpenRouter) delays the first token;
	// acknowledgements like "好的" or "谢谢" need no recall at all.
	var vector []float32
	model := ""
	if utf8.RuneCountInString(strings.TrimSpace(query)) >= minVectorQuery {
		vector, model = s.queryVector(ctx, query, liveEmbedTimeout)
	}
	selected := rank(query, vector, model, recall, RecallLimit, true)
	parts := []string{}
	n = 0
	for _, m := range selected {
		remaining := RecallRunes - n
		if remaining <= 0 {
			break
		}
		text := clip(m.Content, remaining)
		parts = append(parts, text)
		n += utf8.RuneCountInString(text) + 1
	}
	return Context{Core: core.String(), Recall: strings.Join(parts, "；")}
}

var zone = time.FixedZone("CST", 8*3600)

const (
	minVectorQuery    = 4                       // runes; shorter queries use keywords only
	liveEmbedTimeout  = 1500 * time.Millisecond // before a reply: the user is waiting
	backgroundTimeout = 10 * time.Second        // extraction runs in the background
)

func (s *Service) queryVector(ctx context.Context, query string, timeout time.Duration) ([]float32, string) {
	if strings.TrimSpace(query) == "" {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	m, c, err := s.Resolve(ctx, "embedding")
	if err != nil {
		s.Log.Warn("memory embedding model lookup", "err", err)
		return nil, ""
	}
	if m == nil {
		return nil, ""
	}
	vectors, _, err := c.Embed(ctx, m.UpstreamModel, []string{query})
	if err != nil {
		s.Log.Warn("memory query embedding", "err", err)
		return nil, m.ID
	}
	return vectors[0], m.ID
}
