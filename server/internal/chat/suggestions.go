package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/store"
)

const (
	suggestionsCooldown       = 6 * time.Hour
	suggestionsLookback       = 14 * 24 * time.Hour
	suggestionsMinNewConvs    = 2
	suggestionsMinNewMsgs     = 4
	suggestionsMinInitialMsgs = 2
	suggestionsMinInitialMems = 2
	suggestionsMaxDigests     = 12
	suggestionsMaxMemories    = 15
	suggestionsPoolLimit      = 12
	suggestionsMinValidItems  = 4
)

// HomeSuggestionsResult is returned immediately by GetHomeSuggestions (stale-while-revalidate).
type HomeSuggestionsResult struct {
	Items       []store.HomeSuggestion `json:"items"`
	GeneratedAt int64                  `json:"generated_at"`
	Refreshing  bool                   `json:"refreshing"`
}

// GetHomeSuggestions returns the cached personalized suggestion pool for user immediately,
// and triggers an asynchronous background refresh when the 6-hour cooldown has elapsed and
// enough new non-private activity (or initial memories) exists.
func (s *Service) GetHomeSuggestions(ctx context.Context, user *store.User) (*HomeSuggestionsResult, error) {
	cached, err := s.Store.GetUserSuggestions(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	res := &HomeSuggestionsResult{Items: []store.HomeSuggestion{}}
	var generatedAt, lastMsgAt int64
	if cached != nil {
		res.Items = cached.Items
		res.GeneratedAt = cached.GeneratedAt
		generatedAt = cached.GeneratedAt
		lastMsgAt = cached.LastMsgAt
	}

	nowMS := store.Now()
	cooldownMS := suggestionsCooldown.Milliseconds()
	if generatedAt > 0 && nowMS-generatedAt < cooldownMS {
		return res, nil
	}

	// Check whether there is enough new activity since the last generation.
	newConvs, newMsgs, latestMsgAt, err := s.Store.CountNewUserActivitySince(ctx, user.ID, lastMsgAt)
	if err != nil {
		s.Log.Warn("suggestions activity check failed", "user", user.ID, "err", err)
		return res, nil
	}
	enough := false
	if generatedAt == 0 {
		if newMsgs >= suggestionsMinInitialMsgs {
			enough = true
		} else if s.memoryAllowedForSuggestions(ctx, user) {
			if mems, err := s.Store.ListMemories(ctx, user.ID); err == nil && len(mems) >= suggestionsMinInitialMems {
				enough = true
			}
		}
	} else {
		enough = newConvs >= suggestionsMinNewConvs || newMsgs >= suggestionsMinNewMsgs
	}
	if !enough {
		return res, nil
	}

	if s.startSuggestionsRefresh(user, latestMsgAt) {
		res.Refreshing = true
	}
	return res, nil
}

func (s *Service) startSuggestionsRefresh(user *store.User, latestMsgAt int64) bool {
	s.suggestMu.Lock()
	if s.suggestRunning == nil {
		s.suggestRunning = map[string]bool{}
	}
	if s.suggestRunning[user.ID] {
		s.suggestMu.Unlock()
		return true
	}
	s.suggestRunning[user.ID] = true
	s.suggestMu.Unlock()

	s.compactMu.Lock()
	if s.compactClosed {
		s.compactMu.Unlock()
		s.suggestMu.Lock()
		delete(s.suggestRunning, user.ID)
		s.suggestMu.Unlock()
		return false
	}
	s.compactWg.Add(1)
	s.compactMu.Unlock()

	// Copy user fields needed in the detached goroutine.
	u := *user
	go func() {
		defer s.compactWg.Done()
		defer func() {
			s.suggestMu.Lock()
			delete(s.suggestRunning, u.ID)
			s.suggestMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(s.bgCtx(), 60*time.Second)
		defer cancel()
		s.generateHomeSuggestions(ctx, &u, latestMsgAt)
	}()
	return true
}

func (s *Service) resolveSuggestionsModel(ctx context.Context) (*store.Model, *store.Provider, bool) {
	for _, key := range []string{"title_model_id", "default_model_id"} {
		if id, err := s.Store.GetSetting(ctx, key); err == nil && id != "" {
			if m, p, ok := s.usableModel(ctx, id); ok {
				return m, p, true
			}
		}
	}
	ms, err := s.Store.ListModels(ctx, true)
	if err != nil {
		return nil, nil, false
	}
	for _, m := range ms {
		if m.Kind != store.KindChat {
			continue
		}
		if um, p, ok := s.usableModel(ctx, m.ID); ok {
			return um, p, true
		}
	}
	return nil, nil, false
}

func (s *Service) memoryAllowedForSuggestions(ctx context.Context, user *store.User) bool {
	if s.Memory != nil {
		return s.Memory.Allowed(ctx, user, nil)
	}
	return true
}

func (s *Service) generateHomeSuggestions(ctx context.Context, user *store.User, latestMsgAt int64) {
	model, provider, ok := s.resolveSuggestionsModel(ctx)
	if !ok {
		return
	}
	sinceMS := time.Now().Add(-suggestionsLookback).UnixMilli()
	digests, err := s.Store.ListRecentConversationDigests(ctx, user.ID, sinceMS, suggestionsMaxDigests)
	if err != nil {
		s.Log.Warn("suggestions list digests failed", "user", user.ID, "err", err)
		return
	}
	var memories []*store.Memory
	if s.memoryAllowedForSuggestions(ctx, user) {
		if ms, err := s.Store.ListMemories(ctx, user.ID); err == nil {
			if len(ms) > suggestionsMaxMemories {
				ms = ms[:suggestionsMaxMemories]
			}
			memories = ms
		}
	}
	if len(digests) == 0 && len(memories) == 0 {
		return
	}

	userPrompt := buildSuggestionsPrompt(user.DisplayName, digests, memories, time.Now())
	req := llm.Request{
		Model: model.UpstreamModel,
		Messages: []llm.Message{
			{Role: "system", Content: suggestionsSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:       900,
		ReasoningEffort: model.DefaultEffort,
	}
	raw, _, err := s.client(provider).Complete(ctx, req)
	if err != nil {
		s.Log.Warn("suggestions generation failed", "user", user.ID, "err", err)
		return
	}
	items := parseSuggestionsResponse(raw)
	if len(items) < suggestionsMinValidItems {
		s.Log.Warn("suggestions generation produced too few valid items", "user", user.ID, "count", len(items))
		return
	}
	nowMS := store.Now()
	if err := s.Store.SaveUserSuggestions(ctx, &store.UserSuggestions{
		UserID:      user.ID,
		Items:       items,
		GeneratedAt: nowMS,
		LastMsgAt:   latestMsgAt,
		UpdatedAt:   nowMS,
	}); err != nil {
		s.Log.Warn("suggestions save failed", "user", user.ID, "err", err)
	}
}

const suggestionsSystemPrompt = `你是家庭 AI 助手「小暖」的首页推荐问题策划师。
根据用户的近期对话方向和长期记忆，生成 12 条用户接下来可能会感兴趣、愿意点开聊的中文提问。

要求：
1. 70%（约 8-9 条）围绕用户近期关注的方向和记忆做自然延伸、进阶探索或关联话题，绝对不要原样复读用户已经问过的问题。
2. 30%（约 3-4 条）提供轻松、贴合当前日期/季节的生活灵感或新鲜话题，避免信息茧房。
3. 每条提问必须口语化、自然，像用户自己会随口发给助手的一句话（10-24 个汉字），不要写成文章标题或官话。
4. 每条配一个贴切的单个 emoji。
5. 只输出严格的 JSON 数组，不要输出任何额外文字、解释或 Markdown 标记。格式示例：
[{"emoji":"🍳","text":"周末想做道简单又下饭的硬菜，有推荐吗？"}]`

var zhWeekdays = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func buildSuggestionsPrompt(displayName string, digests []store.RecentConversationDigest, memories []*store.Memory, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "当前时间：%s %s\n", now.Format("2006年1月2日"), zhWeekdays[now.Weekday()])
	if name := strings.TrimSpace(displayName); name != "" {
		fmt.Fprintf(&b, "用户称呼：%s\n", trimRunes(name, 20))
	}
	if len(digests) > 0 {
		b.WriteString("\n近期对话方向（按时间倒序）：\n")
		for _, d := range digests {
			title := strings.TrimSpace(d.Title)
			first := cleanSnippet(d.FirstUserMsg, 80)
			last := cleanSnippet(d.LastUserMsg, 80)
			if title == "" && first == "" {
				continue
			}
			b.WriteString("- ")
			if title != "" {
				fmt.Fprintf(&b, "《%s》", trimRunes(title, 24))
			}
			if first != "" {
				if title != "" {
					b.WriteString("：")
				}
				b.WriteString(first)
			}
			if last != "" {
				b.WriteString(" / 后续提到：")
				b.WriteString(last)
			}
			b.WriteByte('\n')
		}
	}
	if len(memories) > 0 {
		b.WriteString("\n关于用户的长期记忆：\n")
		for _, m := range memories {
			c := cleanSnippet(m.Content, 80)
			if c == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	b.WriteString("\n请生成 12 条推荐提问的 JSON 数组：")
	return b.String()
}

func cleanSnippet(s string, maxRunes int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " "))
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return trimRunes(s, maxRunes)
}

func trimRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes]) + "…"
}

func parseSuggestionsResponse(raw string) []store.HomeSuggestion {
	s := strings.TrimSpace(raw)
	if start := strings.IndexByte(s, '['); start >= 0 {
		if end := strings.LastIndexByte(s, ']'); end > start {
			s = s[start : end+1]
		}
	}
	var parsed []store.HomeSuggestion
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(parsed))
	out := make([]store.HomeSuggestion, 0, suggestionsPoolLimit)
	for _, item := range parsed {
		text := strings.TrimSpace(item.Text)
		text = strings.Trim(text, "\"'“”")
		n := utf8.RuneCountInString(text)
		if n < 4 || n > 42 {
			continue
		}
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		emoji := strings.TrimSpace(item.Emoji)
		if utf8.RuneCountInString(emoji) > 4 {
			emoji = ""
		}
		if emoji == "" {
			emoji = "💡"
		}
		out = append(out, store.HomeSuggestion{Emoji: emoji, Text: text})
		if len(out) >= suggestionsPoolLimit {
			break
		}
	}
	return out
}
