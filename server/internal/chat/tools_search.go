package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/store"
	"nuan/internal/websearch"
)

const (
	maxSearchCalls = 5
	maxFetchCalls  = 3
	searchTimeout  = 22 * time.Second
	fetchTimeout   = 25 * time.Second
	maxFetchRunes  = 8000
	maxSources     = 16
)

// WebSearchService abstracts pluggable web search and page fetching for chat tools.
type WebSearchService interface {
	ActiveStatus(ctx context.Context) (enabled bool, supportsFetch bool, kind string)
	Search(ctx context.Context, req websearch.SearchRequest) (*websearch.SearchResponse, error)
	Fetch(ctx context.Context, targetURL string) (*websearch.FetchResponse, error)
}

var webSearchTool = llm.Tool{
	Name: "web_search",
	Description: "Search the live web for real-time or recent information: breaking news, today's prices/rates/weather/schedules, " +
		"policy updates, newly released products/models/drugs, scam/rumor verification, or facts you are not 100% certain about. " +
		"Do NOT call this for greetings, casual chat, cooking recipes, schoolwork math, translation, creative writing, or the user's personal memories.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "1 to 3 concise search queries (each <= 80 chars, keyword-focused). Include the current year/date when freshness matters. " +
					"Supports operators: site:domain.com, -site:domain.com, \"exact phrase\", -exclude, lang:zh,en, after:YYYY-MM-DD, before:YYYY-MM-DD.",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Results per query (default 6, max 15).",
			},
		},
		"required": []string{"queries"},
	},
}

var webFetchTool = llm.Tool{
	Name: "web_fetch",
	Description: "Fetch and read the full Markdown content of a specific HTTP/HTTPS webpage URL. " +
		"Call this when the user sends a webpage link to read/summarize/verify, or when a web_search summary lacks exact details (such as a specific table row, policy clause, or step-by-step notice).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Full HTTP or HTTPS URL to fetch.",
			},
			"focus": map[string]any{
				"type":        "string",
				"description": "Optional keywords or question to focus on if the page is very long.",
			},
		},
		"required": []string{"url"},
	},
}

const webSearchToolPrompt = `

# 联网搜索与网页核实（按需触发，切勿滥用）
你可以调用 web_search 搜索实时网页，确保回答准确、不过时：
- 【必须主动联网搜索的情况】（不要凭旧印象瞎猜，更不要说「我的知识截止到某年」「我无法联网」）：
  1. 时效与动态信息：涉及「今天/昨天/最近/最新/今年/现在/目前」或当前年份的新闻热点、政策法规调整（医保、养老金、个税、限行、放假安排等）、油价金价汇率股价、天气预警、车次航班赛事票务等实时变动信息；
  2. 新近事物与未知专名：训练知识可能未覆盖或已更新的产品与版本发布（新手机、汽车、AI 模型、新药、新影视剧）、机构地址电话/营业时间、人物近况；
  3. 事实核查与防骗求证：核查养生传言或网传消息真伪、查询具体药品说明书禁忌/召回通报、核实可疑电话/App/网站/理财平台是否有诈骗曝光，或对某个具体数字、日期、条文没有十足把握时；
  4. 用户明确要求上网查证，或消息中直接包含 http/https 网页链接。
- 【严禁联网搜索的情况】（直接作答，不要浪费时间做无谓搜索）：
  1. 日常寒暄、道谢、闲聊倾诉、讲故事笑话，以及顺着上文的简单追问（「继续」「通俗点说」「举个例子」「为什么」）；
  2. 稳定不变的常识与基础学科：家常菜谱做法、生活妙招、中小学数学/物理/英语作业讲题、古诗词、历史定论、基础科学原理、通用代码语法；
  3. 纯文本加工与创作：翻译、润色改写、写祝福语/朋友圈/通知邮件、总结用户已经发出来的文字或图片；
  4. 涉及用户个人与家人的私事或过往对话（这类情况应调用 recall_memory 查记忆库，绝不要去公网搜用户隐私）。
- 【高效搜索与引用规范】：
  - 搜索词应精炼（关键词空格组合），查近期信息可带上当前年份或 after: 日期；多维度问题可在同一次 web_search 中并列传 1～3 组搜索词。
  - 优先采信权威来源（政府官网、权威媒体、三甲医院与专业机构），看清结果中的发布时间（date），切勿把往年旧闻当成今年近况。
  - 回答时自然融入查到的结论与关键数据，可顺带点明来源与时间（如「据澎湃新闻今日报道…」），不要在文末堆砌裸链接列表（界面底部会自动展示参考网页卡片）。`

const webFetchPromptAddon = `
  - web_search 返回的 summary 已包含较完整的长摘要，足够回答时直接作答，不要对每条结果机械调用 web_fetch；仅在用户直接发来链接，或摘要缺少关键细节（如具体表格项、完整办事流程、细则原文）时，再挑选 1～2 个最权威链接调用 web_fetch 读取全文。`

const noWebSearchPrompt = `

你目前不能联网搜索实时网页。如果用户询问今天的新闻、实时金价/股价/油价/天气或最新政策变动，请温和说明管理员还没有开启联网搜索功能，可以提供通用背景知识，但不要编造实时数据。`

const otherModelSearchesPrompt = `

当前这个模型不支持联网搜索（其他模型可以）。如果用户需要查询实时新闻、今日行情或网页内容，温和地告诉他：点屏幕顶部的模型名称换一个模型再问就能联网查。`

// SearchAvailable reports whether live web search is enabled and configured by an admin.
func (s *Service) SearchAvailable(ctx context.Context) bool {
	if s.Search == nil {
		return false
	}
	en, _, _ := s.Search.ActiveStatus(ctx)
	return en
}

func (s *Service) searchPrompt(ctx context.Context, ts toolSet) string {
	switch {
	case ts.canSearch && ts.canFetch:
		return webSearchToolPrompt + webFetchPromptAddon
	case ts.canSearch:
		return webSearchToolPrompt
	case s.SearchAvailable(ctx):
		return otherModelSearchesPrompt
	default:
		return noWebSearchPrompt
	}
}

func parseSearchArgs(raw string) ([]string, int) {
	var args struct {
		Queries []string `json:"queries"`
		Query   string   `json:"query"`
		Count   int      `json:"count"`
	}
	raw = strings.TrimSpace(raw)
	if raw != "" && json.Unmarshal([]byte(raw), &args) != nil {
		// Tolerate {"queries": "single string"} from weaker models.
		var alt struct {
			Queries string `json:"queries"`
			Count   int    `json:"count"`
		}
		if json.Unmarshal([]byte(raw), &alt) == nil {
			args.Query = alt.Queries
			args.Count = alt.Count
		}
	}
	seen := map[string]bool{}
	var qs []string
	add := func(q string) {
		q = websearch.SanitizeQuery(q)
		if q == "" || seen[strings.ToLower(q)] || len(qs) >= 3 {
			return
		}
		seen[strings.ToLower(q)] = true
		qs = append(qs, q)
	}
	for _, q := range args.Queries {
		add(q)
	}
	if len(qs) == 0 && args.Query != "" {
		add(args.Query)
	}
	count := args.Count
	if count <= 0 {
		count = 6
	} else if count > 15 {
		count = 15
	}
	return qs, count
}

func (s *Service) execWebSearchCall(ctx context.Context, t *toolTurn, rawArgs string) string {
	if s.Search == nil {
		return "当前服务器未开启联网搜索功能，请根据已有知识如实回答并说明未联网核实。"
	}
	if t.searchCalls >= maxSearchCalls {
		return "本轮对话的联网搜索次数已达上限，请根据前面已搜到的内容回答。"
	}
	queries, count := parseSearchArgs(rawArgs)
	if len(queries) == 0 {
		return "参数错误：请提供非空的搜索词 queries。"
	}
	t.searchCalls++

	labelQ := joinShortQueries(queries, 34)
	stepIdx := t.run.startStep("search", "联网搜索："+labelQ, "")
	t.run.setActivity("正在联网搜索「" + clip(queries[0], 22) + "」…")
	t.postActivity = "正在结合搜索结果整理回答…"

	oriQ := ""
	if !t.private {
		oriQ = websearch.SanitizeQuery(t.userQuestion)
	}

	type qResult struct {
		q   string
		res *websearch.SearchResponse
		err error
	}
	outs := make([]qResult, len(queries))
	var wg sync.WaitGroup
	for idx, q := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, searchTimeout)
			defer cancel()
			res, err := s.Search.Search(sctx, websearch.SearchRequest{
				Query:            q,
				Count:            count,
				OriginalQuestion: oriQ,
			})
			outs[idx] = qResult{q: q, res: res, err: err}
		}()
	}
	wg.Wait()

	type mergedHit struct {
		hit     websearch.SearchResult
		queries []string
	}
	byURL := map[string]*mergedHit{}
	var order []string
	var firstErr error
	anyOK := false

	for _, o := range outs {
		if o.err != nil {
			if firstErr == nil {
				firstErr = o.err
			}
			s.Log.Warn("web search failed", "message_id", t.asstID, "query", o.q, "err", o.err)
			continue
		}
		anyOK = true
		t.cost += o.res.CostUSD
		for _, r := range o.res.Results {
			norm := normalizeURLKey(r.URL)
			if norm == "" {
				continue
			}
			if existing, ok := byURL[norm]; ok {
				if r.RerankScore > existing.hit.RerankScore {
					existing.hit = r
				}
				existing.queries = append(existing.queries, o.q)
				continue
			}
			byURL[norm] = &mergedHit{hit: r, queries: []string{o.q}}
			order = append(order, norm)
		}
	}

	if !anyOK {
		if ctx.Err() != nil {
			t.run.finishStep(stepIdx, "error", "联网搜索已停止", "")
			return "用户停止了搜索。"
		}
		msg := websearch.FriendlyError(firstErr, false)
		t.run.finishStep(stepIdx, "error", "联网搜索未能完成", msg)
		return "联网搜索失败：" + msg + "。请温和说明暂时没连上搜索服务，不要编造实时数据。"
	}

	hits := make([]*mergedHit, 0, len(order))
	for _, k := range order {
		hits = append(hits, byURL[k])
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].hit.RerankScore > hits[j].hit.RerankScore
	})
	const maxReturnedHits = 12
	if len(hits) > maxReturnedHits {
		hits = hits[:maxReturnedHits]
	}

	if len(hits) == 0 {
		t.run.finishStep(stepIdx, "done", "已搜索："+labelQ, "未找到直接匹配的网页")
		return fmt.Sprintf("搜索「%s」未找到匹配的网页结果。你可以换更简短通用的关键词再搜一次，或如实告知用户未查到公开报道。", strings.Join(queries, " / "))
	}

	for _, mh := range hits {
		t.recordWebSource(store.WebSource{
			Title:    mh.hit.Title,
			URL:      mh.hit.URL,
			SiteName: siteLabel(mh.hit.SiteName, mh.hit.URL),
			Date:     formatSourceDate(mh.hit.Date),
			Snippet:  clip(firstNonEmpty(mh.hit.Snippet, mh.hit.Summary), 160),
		})
	}
	s.persistSources(t)
	t.run.finishStep(stepIdx, "done", "已搜索："+labelQ, fmt.Sprintf("找到 %d 篇相关网页", len(hits)))

	var b strings.Builder
	fmt.Fprintf(&b, "联网搜索结果（查询：%s，共 %d 条，按相关性排序）：\n", strings.Join(queries, "；"), len(hits))
	for idx, mh := range hits {
		r := mh.hit
		site := siteLabel(r.SiteName, r.URL)
		date := formatSourceDate(r.Date)
		meta := []string{}
		if site != "" {
			meta = append(meta, site)
		}
		if date != "" {
			meta = append(meta, date)
		}
		if r.Authority >= 3 {
			meta = append(meta, "高权威")
		}
		fmt.Fprintf(&b, "\n[%d] %s", idx+1, firstNonEmpty(r.Title, site, "网页结果"))
		if len(meta) > 0 {
			fmt.Fprintf(&b, "（%s）", strings.Join(meta, " · "))
		}
		fmt.Fprintf(&b, "\n链接：%s\n", r.URL)
		body := firstNonEmpty(r.Summary, r.Snippet)
		if body != "" {
			fmt.Fprintf(&b, "摘要：%s\n", clip(body, 900))
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) execWebFetchCall(ctx context.Context, t *toolTurn, rawArgs string) string {
	if s.Search == nil {
		return "当前服务器未开启网页读取功能。"
	}
	if t.fetchCalls >= maxFetchCalls {
		return "本轮对话读取网页正文的次数已达上限，请结合已有信息作答。"
	}
	var args struct {
		URL   string `json:"url"`
		Focus string `json:"focus"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	targetURL := strings.TrimSpace(args.URL)
	if targetURL == "" {
		return "参数错误：请提供要读取的网页 url。"
	}
	t.fetchCalls++

	label := t.sourceNameForURL(targetURL)
	stepIdx := t.run.startStep("fetch", "阅读网页："+label, "")
	t.run.setActivity("正在阅读网页「" + clip(label, 20) + "」…")
	t.postActivity = "正在结合网页内容整理回答…"

	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	res, err := s.Search.Fetch(fctx, targetURL)
	if err != nil {
		if ctx.Err() != nil {
			t.run.finishStep(stepIdx, "error", "网页阅读已停止", "")
			return "用户停止了读取网页。"
		}
		s.Log.Warn("web fetch failed", "message_id", t.asstID, "url", targetURL, "err", err)
		msg := websearch.FriendlyError(err, true)
		t.run.finishStep(stepIdx, "done", "尝试读取："+label, "站点受限，改用搜索摘要")
		return fmt.Sprintf("未能直接抓取该网页正文（%s）。请直接利用前面的搜索摘要作答，或挑选其他权威链接，不必向用户报错。", msg)
	}
	t.cost += res.CostUSD

	title := strings.TrimSpace(res.Title)
	displayTitle := firstNonEmpty(clip(title, 28), label)
	if strings.TrimSpace(res.Content) == "" {
		t.run.finishStep(stepIdx, "done", "尝试读取："+displayTitle, "页面无可提取正文")
		return "该页面未提取到可用的正文文本，请参考已有搜索摘要回答。"
	}

	t.recordWebSource(store.WebSource{
		Title:    firstNonEmpty(title, label),
		URL:      targetURL,
		SiteName: siteLabel("", targetURL),
		Snippet:  clip(cleanExcerptLine(res.Content), 160),
	})
	s.persistSources(t)
	t.run.finishStep(stepIdx, "done", "已阅读："+displayTitle, "")

	excerpt, truncated := excerptFetchedMarkdown(res.Content, firstNonEmpty(args.Focus, t.userQuestion), maxFetchRunes)
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "网页标题：%s\n", title)
	}
	fmt.Fprintf(&b, "链接：%s\n", targetURL)
	if truncated {
		b.WriteString("（注：网页较长，已提取最相关的正文片段）\n\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(excerpt)
	return strings.TrimSpace(b.String())
}

func (t *toolTurn) recordWebSource(src store.WebSource) {
	norm := normalizeURLKey(src.URL)
	if norm == "" {
		return
	}
	if t.seenSourceURL == nil {
		t.seenSourceURL = map[string]int{}
	}
	if idx, exists := t.seenSourceURL[norm]; exists {
		cur := &t.sources[idx]
		if cur.Title == "" && src.Title != "" {
			cur.Title = src.Title
		}
		if cur.SiteName == "" && src.SiteName != "" {
			cur.SiteName = src.SiteName
		}
		if cur.Date == "" && src.Date != "" {
			cur.Date = src.Date
		}
		if cur.Snippet == "" && src.Snippet != "" {
			cur.Snippet = src.Snippet
		}
		return
	}
	if len(t.sources) >= maxSources {
		return
	}
	t.seenSourceURL[norm] = len(t.sources)
	t.sources = append(t.sources, src)
}

func (t *toolTurn) sourceNameForURL(rawURL string) string {
	norm := normalizeURLKey(rawURL)
	if t.seenSourceURL != nil {
		if idx, ok := t.seenSourceURL[norm]; ok {
			src := t.sources[idx]
			if src.SiteName != "" {
				return src.SiteName
			}
			if src.Title != "" {
				return clip(src.Title, 24)
			}
		}
	}
	return siteLabel("", rawURL)
}

func (s *Service) persistSources(t *toolTurn) {
	if len(t.sources) == 0 {
		return
	}
	t.run.setSources(t.sources)
	pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.Store.SetSources(pctx, t.asstID, t.sources); err != nil {
		s.Log.Error("save web sources", "message_id", t.asstID, "err", err)
	}
	cancel()
}

// excerptFetchedMarkdown keeps up to maxRunes of Markdown, prioritizing the opening
// overview plus paragraphs matching focus keywords when the page is very long.
func excerptFetchedMarkdown(content, focus string, maxRunes int) (string, bool) {
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) <= maxRunes {
		return content, false
	}
	terms := extractFocusTerms(focus)
	if len(terms) == 0 {
		return clip(content, maxRunes), true
	}

	blocks := strings.Split(content, "\n\n")
	headBudget := maxRunes * 2 / 5
	var picked []string
	used := 0
	headCount := 0
	for i, blk := range blocks {
		blk = strings.TrimSpace(blk)
		if blk == "" {
			continue
		}
		n := utf8.RuneCountInString(blk)
		if used+n > headBudget && headCount > 0 {
			break
		}
		if n > headBudget {
			picked = append(picked, clip(blk, headBudget))
			used = headBudget
			headCount = i + 1
			break
		}
		picked = append(picked, blk)
		used += n + 2
		headCount = i + 1
	}

	type scoredBlock struct {
		idx   int
		score int
		text  string
	}
	var cands []scoredBlock
	for i := headCount; i < len(blocks); i++ {
		blk := strings.TrimSpace(blocks[i])
		if blk == "" {
			continue
		}
		low := strings.ToLower(blk)
		sc := 0
		for _, term := range terms {
			if strings.Contains(low, term) {
				sc += 2
			}
		}
		if sc > 0 {
			cands = append(cands, scoredBlock{idx: i, score: sc, text: blk})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].idx < cands[j].idx
	})

	var selected []scoredBlock
	for _, c := range cands {
		rem := maxRunes - used
		if rem < 80 {
			break
		}
		txt := clip(c.text, rem)
		selected = append(selected, scoredBlock{idx: c.idx, text: txt})
		used += utf8.RuneCountInString(txt) + 4
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].idx < selected[j].idx })
	for _, sel := range selected {
		picked = append(picked, "……\n"+sel.text)
	}
	if len(selected) == 0 && used < maxRunes {
		return clip(content, maxRunes), true
	}
	return strings.Join(picked, "\n\n"), true
}

func extractFocusTerms(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r <= ' ' || strings.ContainsRune("，。？！、；：“”‘’（）()[]【】《》,.;:?!\"'/-", r)
	}) {
		if utf8.RuneCountInString(f) >= 2 && !seen[f] {
			seen[f] = true
			out = append(out, f)
			if len(out) >= 10 {
				break
			}
		}
	}
	return out
}

func normalizeURLKey(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/")
}

func siteLabel(siteName, rawURL string) string {
	if s := strings.TrimSpace(siteName); s != "" {
		return s
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return "网页"
	}
	h := strings.ToLower(u.Hostname())
	for _, p := range []string{"www.", "m.", "wap."} {
		if strings.HasPrefix(h, p) && len(h) > len(p)+3 {
			h = h[len(p):]
			break
		}
	}
	return h
}

func formatSourceDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(shanghai).Format("2006-01-02")
	}
	if len(raw) >= 10 && raw[4] == '-' && raw[7] == '-' {
		return raw[:10]
	}
	return clip(raw, 16)
}

func joinShortQueries(qs []string, maxRunes int) string {
	joined := strings.Join(qs, "、")
	if utf8.RuneCountInString(joined) <= maxRunes {
		return joined
	}
	return string([]rune(joined)[:maxRunes]) + "…"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

func cleanExcerptLine(md string) string {
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>* -"))
		if utf8.RuneCountInString(line) >= 8 {
			return line
		}
	}
	return strings.Join(strings.Fields(md), " ")
}
