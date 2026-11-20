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
	Store     *store.Store
	Resolve   ModelResolver
	Log       *slog.Logger
	embedMu   sync.Mutex
	keywordMu sync.Mutex

	importMu sync.Mutex
	imports  map[string]*ImportJob // by user id
}

func New(st *store.Store, resolve ModelResolver, log *slog.Logger) *Service {
	return &Service{Store: st, Resolve: resolve, Log: log, imports: map[string]*ImportJob{}}
}

type Context struct{ Core, Recall string }

type InjectRequest struct {
	ConversationID    string
	ConversationTitle string
	AssistantID       *string
	UserMessages      []string // chronological recent user messages in the conversation
}

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

var labels = map[string]string{
	"health":     "健康与用药",
	"family":     "家人",
	"profile":    "关于我",
	"preference": "喜好习惯",
	"ongoing":    "近期在做",
	"episode":    "聊过的事",
}

func (s *Service) Inject(ctx context.Context, u *store.User, assistant *string, query string) Context {
	var msgs []string
	if strings.TrimSpace(query) != "" {
		msgs = []string{query}
	}
	return s.InjectTurn(ctx, u, InjectRequest{AssistantID: assistant, UserMessages: msgs})
}

func (s *Service) InjectTurn(ctx context.Context, u *store.User, in InjectRequest) Context {
	if !s.Allowed(ctx, u, in.AssistantID) {
		return Context{}
	}
	ms, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil {
		s.Log.Warn("load memories", "err", err)
		return Context{}
	}
	var core strings.Builder
	coreItems := []*store.Memory{}
	recall := []*store.Memory{}
	category := ""
	n := 0
	now := store.Now()
	for _, m := range ms {
		if m.AssistantID != nil && (in.AssistantID == nil || *m.AssistantID != *in.AssistantID) {
			continue
		}
		if !isCore(m, now) {
			recall = append(recall, m)
			continue
		}
		if n >= CoreHardLimit {
			// Still reachable through retrieval until the next demotion pass.
			s.Log.Warn("memory core truncated", "user", u.ID)
			recall = append(recall, m)
			continue
		}
		coreItems = append(coreItems, m)
		if core.Len() == 0 {
			core.WriteString("\n\n# 你记得的关于用户的事\n这些是以前对话里记下的，可能有过时之处。自然地用上，不要生硬地说「我记得……」，除非用户问起；和用户这次说的不一致时，以这次为准，可以顺口确认一下。\n")
		}
		if category != m.Category {
			category = m.Category
			fmt.Fprintf(&core, "【%s】\n", labels[category])
		}
		content := clip(formatCoreEntry(m), CoreHardLimit-n)
		fmt.Fprintf(&core, "- %s\n", content)
		n += utf8.RuneCountInString(content)
	}

	queries, embedQuery := buildTurnQueries(in, coreItems)
	retrospective := hasRetrospectiveIntent(in.UserMessages)
	parts := []string{}
	usedRunes := 0
	appendPart := func(text string) bool {
		remaining := RecallRunes - usedRunes
		if remaining <= 0 {
			return false
		}
		text = clip(strings.TrimSpace(text), remaining)
		if text == "" {
			return true
		}
		parts = append(parts, text)
		usedRunes += utf8.RuneCountInString(text) + 1
		return true
	}

	if len(recall) > 0 && len(queries) > 0 {
		// Short acknowledgements ("好的", "谢谢") skip the embedding round-trip.
		var vector []float32
		model := ""
		if utf8.RuneCountInString(strings.TrimSpace(embedQuery)) >= minVectorQuery {
			vector, model = s.queryVector(ctx, embedQuery, liveEmbedTimeout)
		}
		selected := rankForInject(queries, vector, model, recall, RecallLimit, now, retrospective)
		var touchIDs []string
		for _, m := range selected {
			entry := formatRecallEntry(m, now, in.ConversationID)
			if !appendPart(entry) {
				break
			}
			if m.LastRecalledAt == nil || now-*m.LastRecalledAt >= RecallTouchCooldownMs {
				touchIDs = append(touchIDs, m.ID)
			}
		}
		if len(touchIDs) > 0 {
			if err := s.Store.TouchMemoriesRecall(ctx, u.ID, touchIDs, now, RecallTouchCooldownMs); err != nil {
				s.Log.Warn("touch memory recall", "err", err)
			}
		}
	}

	// Bridge the 60-second background extraction window when the user just switched
	// from another conversation and immediately follows up on the same topic.
	if in.ConversationID != "" && len(queries) > 0 && usedRunes < RecallRunes {
		for _, line := range s.recentUnextractedMatches(ctx, u.ID, in.ConversationID, queries) {
			if !appendPart(line) {
				break
			}
		}
	}

	return Context{Core: core.String(), Recall: strings.Join(parts, "；")}
}

func buildTurnQueries(in InjectRequest, coreItems []*store.Memory) ([]WeightedQuery, string) {
	var qs []WeightedQuery
	var embedParts []string
	// Walk recent user messages from newest to oldest: latest carries full weight,
	// up to two preceding turns provide entity/topic continuity for follow-ups.
	weights := []float64{1.0, 0.55, 0.35}
	wIdx := 0
	for i := len(in.UserMessages) - 1; i >= 0 && wIdx < len(weights); i-- {
		t := strings.TrimSpace(in.UserMessages[i])
		if t == "" {
			continue
		}
		qs = append(qs, WeightedQuery{Text: t, Weight: weights[wIdx]})
		if wIdx < 2 {
			embedParts = append([]string{t}, embedParts...)
		}
		wIdx++
	}
	if title := strings.TrimSpace(in.ConversationTitle); title != "" && len(qs) > 0 {
		qs = append(qs, WeightedQuery{Text: title, Weight: 0.65})
		embedParts = append([]string{title}, embedParts...)
	}
	if len(qs) == 0 {
		return nil, ""
	}
	qs = bridgeCoreEntities(qs, coreItems)
	return qs, strings.Join(embedParts, "\n")
}

// bridgeCoreEntities links colloquial references in the user's message (e.g. "小狗")
// to specific entities already known in core memories (e.g. "五岁的柯基") with zero latency.
func bridgeCoreEntities(queries []WeightedQuery, coreItems []*store.Memory) []WeightedQuery {
	if len(coreItems) == 0 || len(queries) == 0 {
		return queries
	}
	docs := make([]docIndex, len(coreItems))
	for i, m := range coreItems {
		docs[i] = buildDocIndex(m.Content, m.Keywords)
	}
	_, ok := bm25Corpus(queries, docs)
	added := 0
	for i, hit := range ok {
		if !hit {
			continue
		}
		qs := cleanDocContent(coreItems[i].Content)
		if qs == "" {
			continue
		}
		queries = append(queries, WeightedQuery{Text: qs, Weight: 0.55})
		added++
		if added >= 3 {
			break
		}
	}
	return queries
}

func (s *Service) recentUnextractedMatches(ctx context.Context, userID, excludeConvID string, queries []WeightedQuery) []string {
	convs, err := s.Store.RecentUnextractedConversations(ctx, userID, excludeConvID, store.Now()-15*60*1000, 3)
	if err != nil || len(convs) == 0 {
		return nil
	}
	var out []string
	for _, c := range convs {
		msgs, err := s.Store.ListMessages(ctx, c.ID, "", 4)
		if err != nil || len(msgs) == 0 {
			continue
		}
		var text strings.Builder
		if c.Title != "" {
			text.WriteString(c.Title + " ")
		}
		lastUser := ""
		for _, m := range msgs {
			if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
				lastUser = strings.TrimSpace(m.Content)
				text.WriteString(lastUser + " ")
			}
		}
		if lastUser == "" {
			continue
		}
		_, ok := bm25Corpus(queries, []docIndex{buildDocIndex(text.String(), "")})
		if len(ok) == 0 || !ok[0] {
			continue
		}
		title := c.Title
		if title == "" {
			title = clip(lastUser, 24)
		}
		out = append(out, fmt.Sprintf("刚才聊过《%s》：%s [conv:%s]", title, clip(lastUser, 60), c.ID))
		if len(out) >= 2 {
			break
		}
	}
	return out
}

var zone = time.FixedZone("CST", 8*3600)

const (
	minVectorQuery    = 4                       // runes; shorter queries use lexical BM25 only
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
