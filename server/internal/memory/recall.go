package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/store"
)

type RecallRequest struct {
	Queries  []string `json:"queries"`
	Category string   `json:"category"` // "" / "all" or a valid memory category
	Mode     string   `json:"mode"`     // "search" (default) | "recent"
	Limit    int      `json:"limit"`
}

// Recall executes an active memory & historical conversation search on behalf of
// the chat model. It always stays strictly inside the user's own non-private data.
func (s *Service) Recall(ctx context.Context, u *store.User, assistant *string, currentConvID string, req RecallRequest) string {
	if !s.Allowed(ctx, u, assistant) {
		return "当前对话未开启记忆功能。"
	}
	ms, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil {
		s.Log.Warn("recall load memories", "err", err)
		return "暂时无法读取记忆库，请稍后再试。"
	}

	visible := make([]*store.Memory, 0, len(ms))
	coreItems := []*store.Memory{}
	counts := map[string]int{}
	now := store.Now()
	for _, m := range ms {
		if m.AssistantID != nil && (assistant == nil || *m.AssistantID != *assistant) {
			continue
		}
		visible = append(visible, m)
		counts[m.Category]++
		if isCore(m, now) {
			coreItems = append(coreItems, m)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "记忆库统计：共 %d 条（健康与用药 %d、家人 %d、关于我 %d、喜好习惯 %d、近期在做 %d、聊过的事 %d）。\n",
		len(visible), counts["health"], counts["family"], counts["profile"], counts["preference"], counts["ongoing"], counts["episode"])

	cleanQueries := []string{}
	seenQ := map[string]bool{}
	for _, q := range req.Queries {
		q = strings.TrimSpace(q)
		if q != "" && !seenQ[q] {
			seenQ[q] = true
			cleanQueries = append(cleanQueries, clip(q, 200))
			if len(cleanQueries) >= 4 {
				break
			}
		}
	}

	cat := strings.TrimSpace(strings.ToLower(req.Category))
	if cat == "all" {
		cat = ""
	}
	// Browse / meta-query mode: list chronologically when mode="recent" or no search query is given.
	if strings.EqualFold(strings.TrimSpace(req.Mode), "recent") || len(cleanQueries) == 0 {
		candidates := visible
		if cat != "" {
			filtered := make([]*store.Memory, 0, len(visible))
			for _, m := range visible {
				if m.Category == cat {
					filtered = append(filtered, m)
				}
			}
			candidates = filtered
		}
		limit := req.Limit
		if limit <= 0 || limit > 50 {
			limit = 30
		}
		sorted := append([]*store.Memory(nil), candidates...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].CreatedAt != sorted[j].CreatedAt {
				return sorted[i].CreatedAt > sorted[j].CreatedAt
			}
			return sorted[i].ID > sorted[j].ID
		})
		if len(sorted) == 0 {
			b.WriteString("该分类下目前没有已保存的记忆条目。\n")
		} else {
			shown := min(len(sorted), limit)
			fmt.Fprintf(&b, "\n【记忆列表（按时间由新到旧，显示 %d/%d 条）】\n", shown, len(sorted))
			for _, m := range sorted[:shown] {
				writeMemoryLine(&b, m, currentConvID)
			}
		}
		s.appendRecentUnextracted(&b, ctx, u.ID, currentConvID, 2*time.Hour)
		return strings.TrimSpace(b.String())
	}

	// Search mode: hybrid retrieval over memories + raw historical conversation messages.
	limit := req.Limit
	if limit <= 0 || limit > 25 {
		limit = 12
	}
	wQueries := make([]WeightedQuery, 0, len(cleanQueries)+3)
	for _, q := range cleanQueries {
		wQueries = append(wQueries, WeightedQuery{Text: q, Weight: 1.0})
	}
	wQueries = bridgeCoreEntities(wQueries, coreItems)

	var vector []float32
	embedModel := ""
	joined := strings.Join(cleanQueries, " ")
	if utf8.RuneCountInString(joined) >= 2 {
		vector, embedModel = s.queryVector(ctx, joined, liveEmbedTimeout)
	}

	// In search mode, search across all visible categories (models often guess "health"
	// for a pet/episode item), prioritizing exact category matches first if specified.
	scored := rankScored(wQueries, vector, embedModel, visible, limit, false)
	if cat != "" {
		sort.SliceStable(scored, func(i, j int) bool {
			si, sj := scored[i].score, scored[j].score
			if scored[i].m.Category == cat {
				si *= 1.15
			}
			if scored[j].m.Category == cat {
				sj *= 1.15
			}
			return si > sj
		})
	}
	var strictHits, softHits []*store.Memory
	for _, it := range scored {
		if it.ok {
			strictHits = append(strictHits, it.m)
		} else if it.score > 0.045 && len(softHits) < 3 {
			softHits = append(softHits, it.m)
		}
	}

	if len(strictHits) > 0 {
		b.WriteString("\n【匹配的记忆条目】\n")
		for _, m := range strictHits {
			writeMemoryLine(&b, m, currentConvID)
		}
	} else if len(softHits) > 0 {
		b.WriteString("\n【可能相关的候选记忆（低置信，请甄别）】\n")
		for _, m := range softHits {
			writeMemoryLine(&b, m, currentConvID)
		}
	} else {
		b.WriteString("\n【匹配的记忆条目】\n（没有搜到直接匹配的摘要记忆）\n")
	}

	// Always search raw historical non-private messages as well so unextracted details
	// and just-finished conversations are never missed.
	expandedStrings := make([]string, 0, len(wQueries))
	for _, wq := range wQueries {
		expandedStrings = append(expandedStrings, wq.Text)
	}
	terms := SearchTerms(expandedStrings...)
	if len(terms) > 0 {
		hits, err := s.Store.SearchUserMessages(ctx, u.ID, currentConvID, terms, 100)
		if err != nil {
			s.Log.Warn("recall search messages", "err", err)
		} else {
			excerpts := rankMessageHits(wQueries, hits, 6)
			if len(excerpts) > 0 {
				b.WriteString("\n【匹配的历史对话原文片段（如需完整上下文可调用 read_past_conversation）】\n")
				for _, ex := range excerpts {
					role := "用户"
					if ex.Role == "assistant" {
						role = "小暖"
					}
					title := strings.TrimSpace(ex.ConversationTitle)
					if title == "" {
						title = "未命名对话"
					}
					ts := time.UnixMilli(ex.CreatedAt).In(zone).Format("2006-01-02 15:04")
					fmt.Fprintf(&b, "- [conv_id: %s] 《%s》(%s) %s：%s\n", ex.ConversationID, title, ts, role, excerptAround(ex.Content, terms, 140))
				}
			}
		}
	}
	s.appendRecentUnextracted(&b, ctx, u.ID, currentConvID, 30*time.Minute)
	return strings.TrimSpace(b.String())
}

func writeMemoryLine(b *strings.Builder, m *store.Memory, currentConvID string) {
	label := labels[m.Category]
	if label == "" {
		label = m.Category
	}
	date := time.UnixMilli(m.CreatedAt).In(zone).Format("2006-01-02")
	convTag := ""
	if m.SourceConvID != nil && *m.SourceConvID != "" && *m.SourceConvID != currentConvID {
		convTag = fmt.Sprintf(" (conv_id: %s)", *m.SourceConvID)
	}
	content := strings.TrimSpace(m.Content)
	if m.Category == "episode" && episodeDate.MatchString(content) {
		fmt.Fprintf(b, "- [%s] %s%s\n", label, content, convTag)
		return
	}
	fmt.Fprintf(b, "- [%s | %s] %s%s\n", label, date, content, convTag)
}

func (s *Service) appendRecentUnextracted(b *strings.Builder, ctx context.Context, userID, excludeConvID string, window time.Duration) {
	convs, err := s.Store.RecentUnextractedConversations(ctx, userID, excludeConvID, store.Now()-window.Milliseconds(), 3)
	if err != nil || len(convs) == 0 {
		return
	}
	var lines []string
	for _, c := range convs {
		msgs, err := s.Store.ListMessages(ctx, c.ID, "", 4)
		if err != nil || len(msgs) == 0 {
			continue
		}
		var preview []string
		for _, m := range msgs {
			txt := strings.TrimSpace(m.Content)
			if txt == "" {
				continue
			}
			who := "用户"
			if m.Role == "assistant" {
				who = "小暖"
			}
			preview = append(preview, who+"："+clip(strings.Join(strings.Fields(txt), " "), 60))
		}
		if len(preview) == 0 {
			continue
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = "近期对话"
		}
		ts := time.UnixMilli(c.UpdatedAt).In(zone).Format("2006-01-02 15:04")
		lines = append(lines, fmt.Sprintf("- [conv_id: %s] 《%s》(%s) %s", c.ID, title, ts, strings.Join(preview, " / ")))
	}
	if len(lines) > 0 {
		b.WriteString("\n【刚聊过尚未归档的对话】\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
}

func rankMessageHits(queries []WeightedQuery, hits []*store.MessageHit, limit int) []*store.MessageHit {
	if len(hits) == 0 || limit <= 0 {
		return nil
	}
	docs := make([]docIndex, len(hits))
	for i, h := range hits {
		docs[i] = buildDocIndex(h.ConversationTitle+" "+h.Content, "")
	}
	scores, ok := bm25Corpus(queries, docs)
	type item struct {
		h *store.MessageHit
		s float64
	}
	var valid []item
	for i, h := range hits {
		if ok[i] || scores[i] >= 0.45 {
			valid = append(valid, item{h: h, s: scores[i]})
		}
	}
	sort.SliceStable(valid, func(i, j int) bool {
		if valid[i].s != valid[j].s {
			return valid[i].s > valid[j].s
		}
		return valid[i].h.CreatedAt > valid[j].h.CreatedAt
	})
	perConv := map[string]int{}
	var out []*store.MessageHit
	for _, it := range valid {
		if perConv[it.h.ConversationID] >= 2 {
			continue
		}
		perConv[it.h.ConversationID]++
		out = append(out, it.h)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func excerptAround(content string, terms []string, maxRunes int) string {
	clean := strings.Join(strings.Fields(content), " ")
	rs := []rune(clean)
	if len(rs) <= maxRunes {
		return clean
	}
	lower := strings.ToLower(clean)
	bestByte := -1
	for _, t := range terms {
		if idx := strings.Index(lower, strings.ToLower(t)); idx >= 0 && (bestByte < 0 || idx < bestByte) {
			bestByte = idx
		}
	}
	pos := 0
	if bestByte > 0 {
		pos = utf8.RuneCountInString(clean[:bestByte])
	}
	start := max(0, pos-24)
	end := min(len(rs), start+maxRunes)
	if end-start < maxRunes {
		start = max(0, end-maxRunes)
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	b.WriteString(string(rs[start:end]))
	if end < len(rs) {
		b.WriteString("…")
	}
	return b.String()
}

// ReadConversation loads the dialogue messages of a past non-private conversation
// owned by the user, focusing around optional keywords when the thread is long.
func (s *Service) ReadConversation(ctx context.Context, u *store.User, assistant *string, convID, focus string) string {
	if !s.Allowed(ctx, u, assistant) {
		return "当前对话未开启记忆功能。"
	}
	convID = strings.TrimSpace(convID)
	for _, prefix := range []string{"conv_id:", "conv:", "#"} {
		if strings.HasPrefix(strings.ToLower(convID), prefix) {
			convID = strings.TrimSpace(convID[len(prefix):])
		}
	}
	if convID == "" {
		return "需要提供有效的 conversation_id。"
	}
	c, err := s.Store.GetConversation(ctx, convID, u.ID)
	if err != nil || c.Private {
		return "未找到这个历史对话（可能已删除，或属于隐私对话）。"
	}
	msgs, err := s.Store.ListMessages(ctx, c.ID, "", 200)
	if err != nil || len(msgs) == 0 {
		return "这个对话里还没有消息内容。"
	}

	type line struct {
		idx  int
		text string
		raw  string
	}
	var lines []line
	totalRunes := 0
	for i, m := range msgs {
		if m.Role == "assistant" && m.Status == store.StatusError && len(m.Attachments) == 0 {
			continue
		}
		body := strings.TrimSpace(m.Content)
		if body == "" && len(m.Attachments) == 0 {
			continue
		}
		who := "用户"
		if m.Role == "assistant" {
			who = "小暖"
			body = clip(body, 420)
		} else {
			body = clip(body, 600)
		}
		if len(m.Attachments) > 0 {
			body += fmt.Sprintf("（含 %d 张图片）", len(m.Attachments))
		}
		ts := time.UnixMilli(m.CreatedAt).In(zone).Format("2006-01-02 15:04")
		formatted := fmt.Sprintf("[%s] %s：%s", ts, who, body)
		lines = append(lines, line{idx: i, text: formatted, raw: m.Content})
		totalRunes += utf8.RuneCountInString(formatted) + 1
	}
	if len(lines) == 0 {
		return "这个对话里没有可读取的文本消息。"
	}

	var b strings.Builder
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = "未命名对话"
	}
	fmt.Fprintf(&b, "历史对话《%s》(conv_id: %s，共 %d 条有效消息)：\n", title, c.ID, len(lines))

	const maxBudget = 2800
	if totalRunes <= maxBudget {
		for _, l := range lines {
			b.WriteString(l.text + "\n")
		}
		return strings.TrimSpace(b.String())
	}

	// Long thread: pick windows around focus matches (plus opening and closing context).
	keep := make([]bool, len(lines))
	keep[0] = true
	if len(lines) > 1 {
		keep[1] = true
	}
	keep[len(lines)-1] = true
	if len(lines) > 2 {
		keep[len(lines)-2] = true
	}

	if strings.TrimSpace(focus) != "" {
		docs := make([]docIndex, len(lines))
		for i, l := range lines {
			docs[i] = buildDocIndex(l.raw, "")
		}
		scores, _ := bm25Corpus([]WeightedQuery{{Text: focus, Weight: 1.0}}, docs)
		order := make([]int, len(lines))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool { return scores[order[i]] > scores[order[j]] })
		picked := 0
		for _, idx := range order {
			if scores[idx] <= 0 || picked >= 4 {
				break
			}
			picked++
			for w := max(0, idx-1); w <= min(len(lines)-1, idx+2); w++ {
				keep[w] = true
			}
		}
	} else {
		// Without focus, keep recent tail within budget.
		for i := max(0, len(lines)-12); i < len(lines); i++ {
			keep[i] = true
		}
	}

	used := 0
	skipped := false
	for i, l := range lines {
		if !keep[i] {
			skipped = true
			continue
		}
		cost := utf8.RuneCountInString(l.text) + 1
		if used+cost > maxBudget {
			b.WriteString("……（后续内容已截断，可传入更具体的 focus 关键词定位）\n")
			break
		}
		if skipped {
			b.WriteString("……（中间省略无关轮次）……\n")
			skipped = false
		}
		b.WriteString(l.text + "\n")
		used += cost
	}
	return strings.TrimSpace(b.String())
}
