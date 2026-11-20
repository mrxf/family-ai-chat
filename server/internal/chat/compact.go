package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/memory"
	"nuan/internal/store"
)

const (
	// ColdResumeIdle is the inactivity threshold (6 hours: half-day / overnight)
	// after which an old long conversation resumes in lightweight mode instead of
	// replaying tens or hundreds of thousands of cold uncached history tokens.
	ColdResumeIdle = 6 * time.Hour

	activeHistoryMaxCap    = 48_000 // max multi-turn history tokens on 128K..1M models
	coldResumeTailTokens   = 8_000  // pre-gap verbatim tail budget when waking a cold chat
	coldResumeTailMessages = 12     // max pre-gap messages (~6 rounds) kept on cold resume
	minProtectedUserTurns  = 2      // keep at least 2 recent user turns intact during active chat
	historyFetchLimit      = 200    // max messages fetched after summary_cursor per turn
	maxCompactSourceTokens = 26_000 // hard cap on tokens fed into background summary call
	maxSummaryRunes        = 1200   // hard cap on persisted rolling summary length
	maxHistoryCalls        = 4      // max read_earlier_messages calls per assistant reply
	compactTimeout         = 90 * time.Second
)

type contextBudgets struct {
	hardBudget     int // model physical ceiling (ContextWindow - MaxOutput - system)
	activeCap      int // multi-turn history cap during active chat
	compactTrigger int // uncompacted token level that schedules background compaction
	lowWater       int // target verbatim tail kept after compaction
	coldTail       int // pre-gap verbatim tail kept across >= 6h idle gaps
}

func computeContextBudgets(hardBudget int) contextBudgets {
	if hardBudget <= 0 {
		return contextBudgets{
			hardBudget:     hardBudget,
			activeCap:      hardBudget,
			compactTrigger: 3_000,
			lowWater:       1_200,
			coldTail:       1_200,
		}
	}
	if hardBudget <= 8_000 {
		return contextBudgets{
			hardBudget:     hardBudget,
			activeCap:      hardBudget,
			compactTrigger: max(hardBudget*3/4, 2_400),
			lowWater:       max(hardBudget*3/8, 1_000),
			coldTail:       min(hardBudget, 2_400),
		}
	}
	activeCap := min(hardBudget, max(12_000, min(hardBudget*3/5, activeHistoryMaxCap)))
	compactTrigger := activeCap * 3 / 4
	lowWater := activeCap * 3 / 8
	coldTail := min(lowWater, coldResumeTailTokens)
	return contextBudgets{
		hardBudget:     hardBudget,
		activeCap:      activeCap,
		compactTrigger: compactTrigger,
		lowWater:       lowWater,
		coldTail:       coldTail,
	}
}

var readEarlierTool = llm.Tool{
	Name: "read_earlier_messages",
	Description: "Search or read earlier messages from THIS current conversation that were automatically folded out of the active prompt window. " +
		"Call this whenever the user asks about specific earlier details, numbers, medications, clauses, code, or exact wording from earlier in this chat.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "1 to 4 keyword groups or exact terms to search in earlier messages of this conversation " +
					"(e.g. [\"氨氯地平 剂量 毫克\", \"降压药 早上\"], [\"违约金 赔偿\"]). Omit when mode is \"opening\" or \"recent_folded\".",
			},
			"mode": map[string]any{
				"type": "string",
				"enum": []string{"search", "opening", "recent_folded"},
				"description": "\"search\" (default) finds matching earlier Q&A turns and document excerpts; " +
					"\"opening\" reads the very beginning of this conversation; \"recent_folded\" reads the turns just before the visible window.",
			},
		},
	},
}

func formatSummarySection(summary string, foldedEarlier, canReadEarlier bool) string {
	summary = strings.TrimSpace(summary)
	if summary == "" && !(foldedEarlier && canReadEarlier) {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n# 本次对话的前情提要\n")
	if summary != "" {
		b.WriteString("以下是本次对话较早轮次的要点摘要（早期原话已自动精简折叠）：\n")
		b.WriteString(summary)
		if canReadEarlier {
			b.WriteString("\n如果用户追问前面聊过的具体细节、原话、数字、药名、条款或代码而上方提要未完整涵盖，请先调用 read_earlier_messages 工具查阅本对话更早的聊天记录，不要凭印象猜测。")
		}
		return b.String()
	}
	b.WriteString("本次对话较早的历史消息已自动折叠精简（未全部展开在当前上下文中）。如果用户提到或追问前面聊过的具体细节、原话、数字或结论，请先调用 read_earlier_messages 工具查阅本对话更早的聊天记录，不要未经查阅就说不记得或看不到。")
	return b.String()
}

// execReadEarlierCall executes the read_earlier_messages tool strictly inside the
// folded portion (id < t.foldedBeforeID) of the current conversation.
func (s *Service) execReadEarlierCall(ctx context.Context, t *toolTurn, rawArgs string) string {
	if t.historyCalls >= maxHistoryCalls {
		return "本轮查阅前面聊天记录的次数已达上限，请根据已有内容回答。"
	}
	t.historyCalls++

	var parsed struct {
		Queries []string `json:"queries"`
		Query   string   `json:"query"`
		Mode    string   `json:"mode"`
	}
	if sArgs := strings.TrimSpace(rawArgs); sArgs != "" {
		if json.Unmarshal([]byte(sArgs), &parsed) != nil {
			parsed.Query = sArgs
		}
	}
	if len(parsed.Queries) == 0 && strings.TrimSpace(parsed.Query) != "" {
		parsed.Queries = []string{parsed.Query}
	}
	cleanQueries := cleanSearchQueries(parsed.Queries)
	mode := strings.ToLower(strings.TrimSpace(parsed.Mode))
	if mode == "" {
		if len(cleanQueries) > 0 {
			mode = "search"
		} else {
			mode = "recent_folded"
		}
	}

	stepIdx := t.run.startStep("history", "翻看前面的聊天记录", "")
	t.run.setActivity("正在翻看前面的聊天记录…")
	t.postActivity = "正在结合前面的聊天内容整理回答…"
	defer t.run.finishStep(stepIdx, "done", "已翻看前面的聊天记录", "")

	conv, err := s.Store.GetConversation(ctx, t.convID, t.user.ID)
	if err != nil || conv.Private != t.private {
		return "无法读取当前对话的早期记录。"
	}
	upper := t.foldedBeforeID
	if upper == "" {
		upper = t.asstID
	}

	switch mode {
	case "opening":
		msgs, err := s.Store.ListFirstMessages(ctx, conv.ID, upper, 8)
		if err != nil || len(msgs) == 0 {
			return "前面没有更早的聊天记录了。"
		}
		return formatEarlierMessages("本对话开头的聊天记录", msgs, nil, 2600)

	case "recent_folded":
		msgs, err := s.Store.ListMessages(ctx, conv.ID, upper, 10)
		if err != nil || len(msgs) == 0 {
			return "前面没有更早的聊天记录了。"
		}
		return formatEarlierMessages("紧邻当前窗口之前的聊天记录", msgs, nil, 2600)

	default: // "search"
		terms := buildConversationSearchTerms(cleanQueries)
		var hits []*store.Message
		if len(terms) > 0 {
			hits, err = s.Store.SearchConversationMessages(ctx, conv.ID, upper, terms, 100)
			if err != nil {
				s.Log.Warn("search earlier conversation messages", "conv", conv.ID, "err", err)
			}
		}
		if len(hits) == 0 {
			// Fallback so abstract queries still surface concrete context instead of nothing.
			recent, _ := s.Store.ListMessages(ctx, conv.ID, upper, 6)
			opening, _ := s.Store.ListFirstMessages(ctx, conv.ID, upper, 4)
			merged := mergeUniqueMessages(opening, recent)
			if len(merged) == 0 {
				return "没有搜到匹配的早期记录。"
			}
			return formatEarlierMessages("未搜到精确关键词，以下为本对话开头与折叠分界附近的聊天记录", merged, nil, 2400)
		}

		ranked := rankEarlierHits(cleanQueries, terms, hits, 4)
		var expanded []*store.Message
		for _, h := range ranked {
			around, aErr := s.Store.ListMessagesAround(ctx, conv.ID, h.ID, upper, 1, 1)
			if aErr != nil || len(around) == 0 {
				expanded = append(expanded, h)
				continue
			}
			expanded = append(expanded, around...)
		}
		merged := mergeUniqueMessages(expanded)
		return formatEarlierMessages("匹配到的本对话早期聊天记录", merged, terms, 3000)
	}
}

func cleanSearchQueries(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, q := range raw {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		out = append(out, clip(q, 160))
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func buildConversationSearchTerms(queries []string) []string {
	seen := map[string]bool{}
	var terms []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		terms = append(terms, s)
	}
	// Exact space-split tokens first (catches numbers like "145/90", "5mg", "第12条").
	for _, q := range queries {
		for _, part := range strings.Fields(q) {
			if utf8.RuneCountInString(part) >= 2 && utf8.RuneCountInString(part) <= 24 {
				add(part)
			}
		}
	}
	for _, t := range memory.SearchTerms(queries...) {
		add(t)
	}
	if len(terms) > 16 {
		terms = terms[:16]
	}
	return terms
}

func rankEarlierHits(queries, terms []string, hits []*store.Message, topK int) []*store.Message {
	texts := make([]string, len(hits))
	for i, h := range hits {
		texts[i] = h.Content
	}
	scores, _ := memory.RankTextDocs(queries, texts)
	for i, h := range hits {
		lower := strings.ToLower(h.Content)
		for _, t := range terms {
			if utf8.RuneCountInString(t) >= 2 && strings.Contains(lower, strings.ToLower(t)) {
				scores[i] += 0.65
			}
		}
	}
	order := make([]int, len(hits))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if scores[ia] != scores[ib] {
			return scores[ia] > scores[ib]
		}
		return hits[ia].ID > hits[ib].ID
	})
	if len(order) > topK {
		order = order[:topK]
	}
	out := make([]*store.Message, len(order))
	for i, idx := range order {
		out[i] = hits[idx]
	}
	return out
}

func mergeUniqueMessages(groups ...[]*store.Message) []*store.Message {
	seen := map[string]bool{}
	var out []*store.Message
	for _, g := range groups {
		for _, m := range g {
			if m == nil || seen[m.ID] {
				continue
			}
			if m.Role == "assistant" && m.Status == store.StatusError && len(m.Attachments) == 0 {
				continue
			}
			if strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func formatEarlierMessages(header string, msgs []*store.Message, highlightTerms []string, maxRunes int) string {
	var b strings.Builder
	b.WriteString("【" + header + "】\n")
	used := utf8.RuneCountInString(b.String())
	perMsgCap := max(420, min(950, maxRunes/max(1, len(msgs))))

	for _, m := range msgs {
		who := "用户"
		if m.Role == "assistant" {
			who = "小暖"
		}
		body := strings.TrimSpace(m.Content)
		if utf8.RuneCountInString(body) > perMsgCap {
			if len(highlightTerms) > 0 {
				body = excerptAroundTerms(body, highlightTerms, perMsgCap)
			} else {
				body = clipHeadTail(body, perMsgCap*2/3, perMsgCap/3)
			}
		}
		if len(m.Attachments) > 0 {
			body += fmt.Sprintf("（含 %d 张图片）", len(m.Attachments))
		}
		ts := time.UnixMilli(m.CreatedAt).In(shanghai).Format("2006-01-02 15:04")
		line := fmt.Sprintf("[%s] %s：%s\n", ts, who, body)
		cost := utf8.RuneCountInString(line)
		if used+cost > maxRunes && used > 120 {
			b.WriteString("……（更多匹配内容已省略，可缩小关键词继续检索）\n")
			break
		}
		b.WriteString(line)
		used += cost
	}
	return strings.TrimSpace(b.String())
}

// excerptAroundTerms extracts up to 3 windows centered around matching terms inside a long message
// (e.g. a 100K-character pasted contract), so clauses buried deep in the middle are surfaced intact.
func excerptAroundTerms(content string, terms []string, budgetRunes int) string {
	rs := []rune(content)
	if len(rs) <= budgetRunes {
		return content
	}
	lower := strings.ToLower(content)
	type span struct{ start, end int }
	var spans []span
	winSize := max(180, budgetRunes/2)
	for _, t := range terms {
		tl := strings.ToLower(strings.TrimSpace(t))
		if tl == "" {
			continue
		}
		idx := strings.Index(lower, tl)
		if idx < 0 {
			continue
		}
		pos := utf8.RuneCountInString(content[:idx])
		st := max(0, pos-winSize/3)
		en := min(len(rs), st+winSize)
		if en-st < winSize {
			st = max(0, en-winSize)
		}
		merged := false
		for i := range spans {
			if st <= spans[i].end+20 && en >= spans[i].start-20 {
				spans[i].start = min(spans[i].start, st)
				spans[i].end = max(spans[i].end, en)
				merged = true
				break
			}
		}
		if !merged {
			spans = append(spans, span{st, en})
		}
		if len(spans) >= 3 {
			break
		}
	}
	if len(spans) == 0 {
		return clipHeadTail(content, budgetRunes*2/3, budgetRunes/3)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out strings.Builder
	used := 0
	for i, sp := range spans {
		rem := budgetRunes - used
		if rem <= 40 {
			break
		}
		end := min(sp.end, sp.start+rem)
		if i > 0 || sp.start > 0 {
			out.WriteString("…")
		}
		out.WriteString(string(rs[sp.start:end]))
		used += end - sp.start
		if end < len(rs) && i == len(spans)-1 {
			out.WriteString("…")
		}
	}
	return out.String()
}

func clipHeadTail(s string, headRunes, tailRunes int) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= headRunes+tailRunes+16 {
		return string(rs)
	}
	return string(rs[:headRunes]) + "\n……[中间省略]……\n" + string(rs[len(rs)-tailRunes:])
}

// ---- Background Rolling Conversation Compactor ----

const rollingSummaryPrompt = `你是家用 AI 助手「小暖」的对话前情提要整理员。同一个对话已经聊了较长篇幅，请把给定的「旧前情提要（如有）」与「新归档的对话片段」合并整理成一份简明、准确的前情提要，供后续对话接续使用。

输出要求（直接输出提要正文，不要代码块、不要「好的/以下是提要」等客套话，总字数控制在 300～850 字以内）：
1. 【话题与背景】：概括用户在这段对话里主要讨论了什么事情、涉及的对象或发过的文档/图片主题。
2. 【关键事实与数字】：准确保留对话中出现过的具体数值、指标、药名与用法剂量、金额、日期时间、专有名词、图片编号（如 图1、图2）或明确约束条件，严禁编造对话未提及的事实。
3. 【结论与待续进展】：记录小暖已经给出的核心建议结论、用户已确认或否定的选择，以及最近正在追问或尚未聊完的事项（越近的内容保留越清晰，已经翻篇的闲聊寒暄直接略去）。`

type compactHint struct {
	conv           *store.Conversation
	user           *store.User
	turnModel      *store.Model
	turnProvider   *store.Provider
	lowWaterTokens int
	coldTailTokens int
}

// maybeCompact launches an asynchronous, deduplicated rolling compaction pass for conv.
func (s *Service) maybeCompact(h compactHint) {
	if h.conv == nil || h.user == nil {
		return
	}
	if h.conv.Private && (h.turnModel == nil || h.turnProvider == nil) {
		return
	}
	s.compactMu.Lock()
	if s.compactClosed {
		s.compactMu.Unlock()
		return
	}
	if s.compactRunning == nil {
		s.compactRunning = map[string]bool{}
	}
	if s.compactSlots == nil {
		s.compactSlots = make(chan struct{}, 2)
	}
	if s.compactRunning[h.conv.ID] {
		s.compactMu.Unlock()
		return
	}
	s.compactRunning[h.conv.ID] = true
	slots := s.compactSlots
	baseCtx := s.bgCtx()
	s.compactWg.Add(1)
	s.compactMu.Unlock()

	go func() {
		defer s.compactWg.Done()
		defer func() {
			s.compactMu.Lock()
			delete(s.compactRunning, h.conv.ID)
			s.compactMu.Unlock()
		}()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-baseCtx.Done():
			return
		}
		ctx, cancel := context.WithTimeout(baseCtx, compactTimeout)
		defer cancel()
		if err := s.compactConversation(ctx, h); err != nil && baseCtx.Err() == nil {
			s.Log.Warn("conversation compaction failed", "conv", h.conv.ID, "private", h.conv.Private, "err", err)
		}
	}()
}

func (s *Service) compactConversation(ctx context.Context, h compactHint) error {
	prevSummary, curCursor, err := s.Store.GetConversationSummary(ctx, h.conv.ID)
	if err != nil {
		return nil // deleted conversation
	}

	msgs, hasOlder, err := s.Store.ListMessagesAfter(ctx, h.conv.ID, curCursor, "", 240)
	if err != nil || len(msgs) == 0 {
		return err
	}
	// Never compact past an active streaming placeholder.
	for i, m := range msgs {
		if m.Status == store.StatusStreaming {
			msgs = msgs[:i]
			break
		}
	}
	if len(msgs) < 2 {
		return nil
	}

	lowWater := h.lowWaterTokens
	if lowWater <= 0 {
		lowWater = 18_000
	}
	coldTail := h.coldTailTokens
	if coldTail <= 0 {
		coldTail = coldResumeTailTokens
	}

	keepFrom := selectCompactionSplit(msgs, lowWater, coldTail)
	if keepFrom <= 0 {
		return nil
	}
	toCompact := msgs[:keepFrom]
	// Avoid pointless micro-compactions unless there is older history before this slice
	// or the chunk itself already carries meaningful substance.
	chunkTokens := 0
	userMsgsInChunk := 0
	for _, m := range toCompact {
		chunkTokens += estimateTokens(m.Content)
		if m.Role == "user" {
			userMsgsInChunk++
		}
	}
	if !hasOlder && curCursor == "" && chunkTokens < 1_500 && userMsgsInChunk < 2 {
		return nil
	}

	var sumModel *store.Model
	var sumClient *llm.Client
	if h.conv.Private {
		// Strict privacy invariant: private chats self-summarize ONLY with the exact
		// model & provider selected by the user in this conversation.
		sumModel = h.turnModel
		sumClient = s.client(h.turnProvider)
	} else {
		m, c, rErr := s.ResolveMemoryModel(ctx, "memory")
		if rErr != nil || m == nil || c == nil {
			if h.turnModel == nil || h.turnProvider == nil {
				return rErr
			}
			sumModel, sumClient = h.turnModel, s.client(h.turnProvider)
		} else {
			sumModel, sumClient = m, c
		}
	}

	seedSummary := strings.TrimSpace(prevSummary)
	var openingAnchor []*store.Message
	if !h.conv.Private && seedSummary == "" {
		if eps, eErr := s.Store.ConversationEpisodes(ctx, h.conv.ID, h.user.ID); eErr == nil && len(eps) > 0 {
			seedSummary = strings.Join(eps, "\n- ")
			if len(eps) > 1 {
				seedSummary = "- " + seedSummary
			}
		}
	}
	if curCursor == "" && hasOlder {
		openingAnchor, _ = s.Store.ListFirstMessages(ctx, h.conv.ID, toCompact[0].ID, 4)
	}

	userPrompt := buildCompactionInput(seedSummary, openingAnchor, toCompact, maxCompactSourceTokens)
	req := llm.Request{
		Model:           sumModel.UpstreamModel,
		ReasoningEffort: sumModel.DefaultEffort,
		MaxTokens:       1400,
		Messages: []llm.Message{
			{Role: "system", Content: rollingSummaryPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	text, _, err := sumClient.Complete(ctx, req)
	if err != nil {
		return err
	}
	cleaned := cleanRollingSummary(text)
	if cleaned == "" {
		return nil
	}
	newCursor := toCompact[len(toCompact)-1].ID
	committed, err := s.Store.CommitConversationSummary(ctx, h.conv.ID, curCursor, newCursor, cleaned)
	if err == nil && committed {
		s.Log.Info("compacted conversation context",
			"conv", h.conv.ID,
			"private", h.conv.Private,
			"messages_folded", len(toCompact),
			"summary_runes", utf8.RuneCountInString(cleaned),
		)
	}
	return err
}

// selectCompactionSplit chooses the index keepFrom in msgs such that msgs[:keepFrom]
// is rolled into summary and msgs[keepFrom:] remains verbatim (starting with a user turn).
func selectCompactionSplit(msgs []*store.Message, lowWater, coldTail int) int {
	n := len(msgs)
	if n < 2 {
		return 0
	}
	keptTokens := 0
	keptUserTurns := 0
	inColdPast := false
	coldMsgs := 0
	keepFrom := n

	for i := n - 1; i >= 0; i-- {
		if i < n-1 && isColdGap(msgs[i], msgs[i+1]) {
			inColdPast = true
		}
		m := msgs[i]
		cost := estimateTokens(m.Content) + len(m.Attachments)*200
		if inColdPast {
			if (keptTokens+cost > coldTail && coldMsgs >= 2) || coldMsgs >= coldResumeTailMessages {
				break
			}
			coldMsgs++
			keptTokens += cost
			if m.Role == "user" {
				keptUserTurns++
			}
			keepFrom = i
			continue
		}
		// Active region (< 6h gaps): protect at least minProtectedUserTurns recent user turns
		// so a freshly pasted long document stays verbatim for immediate follow-up questions.
		if keptUserTurns >= minProtectedUserTurns && keptTokens+cost > lowWater {
			break
		}
		keptTokens += cost
		if m.Role == "user" {
			keptUserTurns++
		}
		keepFrom = i
	}

	// Ensure the kept verbatim slice starts cleanly with a user message.
	for keepFrom < n && msgs[keepFrom].Role == "assistant" {
		keepFrom++
	}
	if keepFrom >= n {
		// Always leave at least the final user turn verbatim.
		for i := n - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				keepFrom = i
				break
			}
		}
	}
	if keepFrom <= 0 || keepFrom >= n {
		return 0
	}
	return keepFrom
}

func isColdGap(older, newer *store.Message) bool {
	if older == nil || newer == nil || older.CreatedAt <= 0 || newer.CreatedAt <= 0 {
		return false
	}
	return newer.CreatedAt-older.CreatedAt >= ColdResumeIdle.Milliseconds()
}

func buildCompactionInput(seedSummary string, opening, toCompact []*store.Message, tokenCap int) string {
	var b strings.Builder
	if seedSummary != "" {
		b.WriteString("【已有的前情提要】\n")
		b.WriteString(seedSummary)
		b.WriteString("\n\n")
	}
	budget := tokenCap - estimateTokens(b.String())
	if len(opening) > 0 {
		var ob strings.Builder
		ob.WriteString("【对话最早的开场话题】\n")
		for _, m := range opening {
			formatCompactLine(&ob, m, 600)
		}
		ob.WriteString("\n")
		if cost := estimateTokens(ob.String()); cost < budget/4 {
			b.WriteString(ob.String())
			budget -= cost
		}
	}

	// Walk toCompact from newest to oldest within tokenCap so even a 900K legacy
	// backlog never sends more than ~26K tokens to the summary model.
	var lines []string
	for i := len(toCompact) - 1; i >= 0; i-- {
		m := toCompact[i]
		if m.Role == "assistant" && m.Status == store.StatusError && len(m.Attachments) == 0 {
			continue
		}
		if strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 {
			continue
		}
		var lb strings.Builder
		maxRunes := 2800
		if m.Role == "assistant" {
			maxRunes = 1600
		}
		formatCompactLine(&lb, m, maxRunes)
		line := lb.String()
		cost := estimateTokens(line)
		if budget-cost < 0 && len(lines) > 0 {
			lines = append(lines, "……（更早中间轮次已省略）……\n")
			break
		}
		budget -= cost
		lines = append(lines, line)
	}
	b.WriteString("【本次新归档的对话片段（按时间从早到晚）】\n")
	for i := len(lines) - 1; i >= 0; i-- {
		b.WriteString(lines[i])
	}
	return b.String()
}

func formatCompactLine(b *strings.Builder, m *store.Message, maxRunes int) {
	who := "用户"
	if m.Role == "assistant" {
		who = "小暖"
	}
	body := strings.TrimSpace(m.Content)
	if utf8.RuneCountInString(body) > maxRunes {
		body = clipHeadTail(body, maxRunes*2/3, maxRunes/3)
	}
	if len(m.Attachments) > 0 {
		if m.Role == "assistant" {
			var prompts []string
			for _, a := range m.Attachments {
				if a.Prompt != "" {
					prompts = append(prompts, clip(a.Prompt, 120))
				}
			}
			if len(prompts) > 0 {
				body = fmt.Sprintf("[画了 %d 张图：%s] %s", len(m.Attachments), strings.Join(prompts, "；"), body)
			} else {
				body = fmt.Sprintf("[画了 %d 张图] %s", len(m.Attachments), body)
			}
		} else {
			body = fmt.Sprintf("[发送了 %d 张图片] %s", len(m.Attachments), body)
		}
	}
	ts := ""
	if m.CreatedAt > 0 {
		ts = "[" + time.UnixMilli(m.CreatedAt).In(shanghai).Format("2006-01-02 15:04") + "] "
	}
	fmt.Fprintf(b, "%s%s：%s\n", ts, who, body)
}

func cleanRollingSummary(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = strings.TrimSpace(s[nl+1:])
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	for _, prefix := range []string{"以下是整理后的前情提要：", "以下是前情提要：", "前情提要：\n", "前情提要："} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
		}
	}
	return clip(s, maxSummaryRunes)
}
