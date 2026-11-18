package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/store"
)

// AutoModelID is the virtual model identifier representing server-side Auto mode.
const AutoModelID = "auto"

// Settings keys controlling Auto mode.
const (
	SettingAutoEnabled    = "auto_enabled"     // "1" = family can use Auto mode
	SettingAutoModelID    = "auto_model_id"    // chat model ID used as the LLM judge
	SettingAutoRouterKind = "auto_router_kind" // "llm" (default) | "jev" (reserved for future JEV router)
	SettingAutoDynamic    = "auto_dynamic"     // "1" = dynamically upgrade/downgrade on big mid-chat shifts
)

// Router engine kinds.
const (
	RouterKindLLM = "llm"
	RouterKindJEV = "jev" // reserved extension point for specialized JEV routing models
)

// RouteMode distinguishes first-turn selection from mid-conversation dynamic checks.
type RouteMode string

const (
	RouteInitial RouteMode = "initial"
	RouteDynamic RouteMode = "dynamic"
)

// RouteCandidate pairs an enabled chat model with its server-side fallback profile
// and relative version standing among enabled siblings.
type RouteCandidate struct {
	Index            int
	Model            *store.Model
	Provider         *store.Provider
	Profile          llm.ModelProfile
	IsLatestInSeries bool
	NewerVersion     string // non-empty when an enabled sibling in the same series has a higher version
}

// RouteTurnSummary is a compact excerpt of an earlier user turn for dynamic routing.
type RouteTurnSummary struct {
	UserText string
	ModelID  string
}

// RouteRequest holds everything a ModelRouter needs to pick (or keep) a model.
type RouteRequest struct {
	Mode         RouteMode
	Candidates   []RouteCandidate
	CurrentModel *store.Model // non-nil in RouteDynamic
	Assistant    *store.Assistant
	RecentTurns  []RouteTurnSummary
	UserMessage  string
	HasImages    bool
	DeepThink    bool
}

// RouteDecision is the outcome of a routing evaluation.
type RouteDecision struct {
	Model      *store.Model
	Provider   *store.Provider
	Switched   bool
	Reason     string
	Cost       float64
	UsedJudge  bool
	RouterKind string
}

// ModelRouter abstracts the Auto mode model-selection engine so the server can
// start with an LLM judge today and plug in specialized routers (such as JEV) later.
type ModelRouter interface {
	Kind() string
	Route(ctx context.Context, req RouteRequest) (*RouteDecision, error)
}

const judgeTimeout = 6 * time.Second

// AutoEnabled reports whether the admin has enabled Auto mode and there is at least
// one usable chat model to route to.
func (s *Service) AutoEnabled(ctx context.Context) bool {
	on, err := s.Store.GetSetting(ctx, SettingAutoEnabled)
	if err != nil || on != "1" {
		return false
	}
	ms, err := s.Store.ListModels(ctx, true)
	return err == nil && len(ms) > 0
}

// AutoDynamic reports whether mid-conversation dynamic model switching is enabled.
func (s *Service) AutoDynamic(ctx context.Context) bool {
	v, _ := s.Store.GetSetting(ctx, SettingAutoDynamic)
	return v == "1"
}

// resolveAutoJudge returns the model & provider used to evaluate Auto routing.
// Prefers auto_model_id, falling back to title_model_id or the fastest usable chat model
// if the configured judge's provider was temporarily disabled.
func (s *Service) resolveAutoJudge(ctx context.Context) (*store.Model, *store.Provider) {
	for _, key := range []string{SettingAutoModelID, "title_model_id"} {
		if id, _ := s.Store.GetSetting(ctx, key); id != "" && id != AutoModelID {
			if m, p, ok := s.usableModel(ctx, id); ok {
				return m, p
			}
		}
	}
	ms, err := s.Store.ListModels(ctx, true)
	if err != nil || len(ms) == 0 {
		return nil, nil
	}
	// Pick the lightest/fastest usable chat model as emergency judge fallback.
	var bestM *store.Model
	var bestP *store.Provider
	bestTier := 99
	for _, cand := range ms {
		m, p, ok := s.usableModel(ctx, cand.ID)
		if !ok {
			continue
		}
		tier := llm.ProfileModel(m.UpstreamModel, m.DisplayName).Tier
		if bestM == nil || tier < bestTier {
			bestM, bestP, bestTier = m, p, tier
		}
	}
	return bestM, bestP
}

// buildRouteCandidates loads usable chat models, filters for vision when required,
// and enriches each model with server-side hierarchy and version annotations.
func (s *Service) buildRouteCandidates(ctx context.Context, needVision bool) ([]RouteCandidate, error) {
	ms, err := s.Store.ListModels(ctx, true)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, ErrNoModel
	}
	var raw []RouteCandidate
	hasVision := false
	for _, m := range ms {
		um, p, ok := s.usableModel(ctx, m.ID)
		if !ok {
			continue
		}
		if um.Vision {
			hasVision = true
		}
		raw = append(raw, RouteCandidate{
			Model:    um,
			Provider: p,
			Profile:  llm.ProfileModel(um.UpstreamModel, um.DisplayName),
		})
	}
	if len(raw) == 0 {
		return nil, ErrNoModel
	}
	if needVision && hasVision {
		filtered := raw[:0]
		for _, c := range raw {
			if c.Model.Vision {
				filtered = append(filtered, c)
			}
		}
		raw = filtered
	}

	// Track the newest version within each family series (e.g. Opus 5.5 vs 4.6, Fable 5.2 vs 5.0).
	maxVerScore := map[string]int{}
	maxVerLabel := map[string]string{}
	seriesCount := map[string]int{}
	for _, c := range raw {
		fk := c.Profile.FamilyKey
		seriesCount[fk]++
		if vs := c.Profile.Version.Score(); vs > maxVerScore[fk] {
			maxVerScore[fk] = vs
			maxVerLabel[fk] = c.Profile.Version.String()
		}
	}
	for i := range raw {
		raw[i].Index = i + 1
		fk := raw[i].Profile.FamilyKey
		if seriesCount[fk] > 1 && maxVerScore[fk] > 0 {
			vs := raw[i].Profile.Version.Score()
			if vs == maxVerScore[fk] {
				raw[i].IsLatestInSeries = true
			} else if vs > 0 {
				raw[i].NewerVersion = maxVerLabel[fk]
			}
		}
	}
	return raw, nil
}

// activeRouter returns the configured ModelRouter (defaulting to LLMRouter).
func (s *Service) activeRouter(ctx context.Context) ModelRouter {
	kind, _ := s.Store.GetSetting(ctx, SettingAutoRouterKind)
	if kind == "" {
		kind = RouterKindLLM
	}
	s.routersMu.RLock()
	r := s.routers[kind]
	if r == nil {
		r = s.routers[RouterKindLLM]
	}
	s.routersMu.RUnlock()
	if r != nil {
		return r
	}
	return &llmModelRouter{svc: s}
}

// RegisterRouter registers a ModelRouter implementation (e.g. a future JEV router).
func (s *Service) RegisterRouter(r ModelRouter) {
	if r == nil {
		return
	}
	s.routersMu.Lock()
	if s.routers == nil {
		s.routers = map[string]ModelRouter{}
	}
	s.routers[r.Kind()] = r
	s.routersMu.Unlock()
}

// isShortContinuation detects brief follow-up replies ("为什么？", "继续", "谢谢", "再讲讲")
// that are clearly continuations of the current topic so dynamic mode skips unnecessary judge calls.
func isShortContinuation(msg string) bool {
	t := strings.TrimSpace(msg)
	n := utf8.RuneCountInString(t)
	if n == 0 || n > 14 {
		return false
	}
	// If it explicitly mentions switching topics or new domains, do not skip.
	if hasAnyStr(t, "换个", "另外", "顺便问", "对了", "再问个别的", "数学题", "作业", "体检", "报告", "物理") {
		return false
	}
	for _, kw := range []string{
		"为什么", "怎么说", "继续", "接着说", "展开", "详细", "举个例子", "举例",
		"没看懂", "没懂", "不懂", "再说一遍", "通俗点", "简单点", "然后呢", "还有吗",
		"真的吗", "不对", "确定吗", "谢谢", "好的", "明白了", "懂了", "行", "ok", "OK",
		"第1", "第2", "第3", "第一", "第二", "第三", "上面", "刚才",
	} {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return n <= 4
}

func hasAnyStr(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// llmModelRouter uses a configured chat LLM + server built-in model profiles to route queries.
type llmModelRouter struct {
	svc *Service
}

func (r *llmModelRouter) Kind() string { return RouterKindLLM }

func (r *llmModelRouter) Route(ctx context.Context, req RouteRequest) (*RouteDecision, error) {
	if len(req.Candidates) == 0 {
		return nil, ErrNoModel
	}
	findByID := func(id string) *RouteCandidate {
		for i := range req.Candidates {
			if req.Candidates[i].Model.ID == id {
				return &req.Candidates[i]
			}
		}
		return nil
	}

	// Single candidate: zero-latency pick.
	if len(req.Candidates) == 1 {
		c := req.Candidates[0]
		return &RouteDecision{
			Model:      c.Model,
			Provider:   c.Provider,
			Switched:   req.CurrentModel == nil || req.CurrentModel.ID != c.Model.ID,
			Reason:     "single_candidate",
			RouterKind: RouterKindLLM,
		}, nil
	}

	// Dynamic mode fast-path: short follow-up stays on current model (preserves KV cache).
	var curCand *RouteCandidate
	if req.Mode == RouteDynamic && req.CurrentModel != nil {
		curCand = findByID(req.CurrentModel.ID)
		if curCand != nil && !req.HasImages && isShortContinuation(req.UserMessage) {
			return &RouteDecision{
				Model:      curCand.Model,
				Provider:   curCand.Provider,
				Switched:   false,
				Reason:     "short_followup_keep",
				RouterKind: RouterKindLLM,
			}, nil
		}
	}

	judge, judgeProv := r.svc.resolveAutoJudge(ctx)
	if judge == nil || judgeProv == nil {
		fb := HeuristicRoute(req)
		return fb, nil
	}

	sysPrompt, userPrompt := buildJudgePrompts(req, curCand)
	temp := 0.0
	llmReq := llm.Request{
		Model: judge.UpstreamModel,
		Messages: []llm.Message{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature:     &temp,
		MaxTokens:       160,
		ReasoningEffort: judge.DefaultEffort,
	}

	jctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	raw, usage, err := r.svc.client(judgeProv).Complete(jctx, llmReq)
	var judgeCost float64
	if usage != nil {
		judgeCost = usage.Cost
		if judgeCost <= 0 {
			judgeCost = judge.EstimateCost(usage.PromptTokens, usage.CompletionTokens)
		}
	}

	if err != nil {
		r.svc.Log.Warn("auto judge failed, using fallback rules", "judge", judge.UpstreamModel, "mode", req.Mode, "err", err)
		if req.Mode == RouteDynamic && curCand != nil {
			return &RouteDecision{
				Model: curCand.Model, Provider: curCand.Provider, Switched: false,
				Reason: "judge_error_keep", Cost: judgeCost, RouterKind: RouterKindLLM,
			}, nil
		}
		fb := HeuristicRoute(req)
		fb.Cost += judgeCost
		return fb, nil
	}

	picked, action, reason, ok := parseJudgeReply(raw, req.Candidates)
	if !ok {
		r.svc.Log.Warn("auto judge output unparseable, using fallback rules", "judge", judge.UpstreamModel, "raw", clipRunes(raw, 120))
		if req.Mode == RouteDynamic && curCand != nil {
			return &RouteDecision{
				Model: curCand.Model, Provider: curCand.Provider, Switched: false,
				Reason: "judge_bad_json_keep", Cost: judgeCost, UsedJudge: true, RouterKind: RouterKindLLM,
			}, nil
		}
		fb := HeuristicRoute(req)
		fb.Cost += judgeCost
		return fb, nil
	}

	if req.Mode == RouteDynamic && curCand != nil && strings.EqualFold(action, "keep") {
		picked = curCand
	}
	switched := req.CurrentModel == nil || req.CurrentModel.ID != picked.Model.ID
	return &RouteDecision{
		Model:      picked.Model,
		Provider:   picked.Provider,
		Switched:   switched,
		Reason:     reason,
		Cost:       judgeCost,
		UsedJudge:  true,
		RouterKind: RouterKindLLM,
	}, nil
}

const judgeInitialSystem = `你是家用 AI 助手「小暖」的服务端模型路由器。家人不懂各种复杂模型代号，请根据用户的问题，从候选模型列表中挑选最合适的一个模型。

选模核心原则：
1. 【最优先看管理员说明】：如果某个候选模型有「管理员场景说明」，且用户问题符合该说明（如管理员标了日常聊天、医疗研究等），优先选它。
2. 【按需配模，兼顾质量与速度】：
   - 严肃复杂问题（疾病症状、体检报告、用药解读、物理/高阶数学/科研硬核推导、防诈骗研判、复杂长文方案，或用户开启了深度思考）：选匹配的顶级旗舰(T5)或强力进阶(T4)模型（例如医疗与深度研究优先 Carbon，硬核物理数理优先 Fable，深度剖析与严肃写作优先 Opus / Astra 等）。
   - 日常轻松问题（问候闲聊、家常菜谱、生活常识、简单翻译、给孩子辅导中小学数学应用题/基础作业）：选快速日常(T2)、极速轻量(T1)或高性价比均衡主力(T3)模型（如 DeepSeek、DeepSeek Flash、Gemini Hydrogen 氢气模型 / Flash、Bailu、GPT Mini 等），秒回且省钱。
   - 长文章阅读总结或地道中文写作：选擅长中文或长文本的均衡/强力模型（如 Kimi、Sonnet、DeepSeek、Qwen 等）。
3. 【同系选新、高阶优先】：同一系列有多个版本时优先选最新版（如 Opus 5.5 > 5.0 > 4.7 > 4.6，Fable 5.2 > 5.0，Kimi K3 > K2）；需要强能力时遵循 Opus > Sonnet > Haiku，Carbon > Pro > Flash > Hydrogen。

只输出一行紧凑 JSON，不要代码块或任何解释：
{"pick": 候选序号数字, "reason": "10字以内简短原因"}`

const judgeDynamicSystem = `你是家用 AI 助手「小暖」的服务端动态模型路由器。当前对话已经确定了一个模型，切换模型会损失上下文前缀缓存，请审慎判断是否需要切换模型。

判断原则：
1. 【默认保持不变（keep）】：只要仍在延续同一类话题、追问细节、或新问题难度档位与之前相近，必须保持当前模型，保护缓存命中！
2. 【仅在话题或难度发生明显跨档变化时切换（switch）】：
   - 明显降级：之前在聊高深物理、硬核科研、复杂医疗研究（当前用 Fable、Carbon、Opus、Astra 等 T5/T4 重型模型），现在话题已明显转变为日常闲聊、家常琐事、或辅导孩子的中小学数学题/基础作业 → 切换到更轻快省钱的日常/轻量模型（如 DeepSeek、DeepSeek Flash、Gemini Hydrogen 氢气模型 / Flash 等），优先参考管理员场景说明。
   - 明显升级：之前在闲聊或问简单日常问题（当前用 T1/T2/T3 轻量模型），现在突然开始问严肃医疗报告/用药、硬核数理推导或高难度深度分析 → 升级到匹配的 T5/T4 旗舰模型（遵循管理员说明、Opus > Sonnet、新版优于旧版）。

只输出一行紧凑 JSON，不要代码块或任何解释：
保持不变：{"action": "keep", "pick": 当前序号数字, "reason": "简短原因"}
需要切换：{"action": "switch", "pick": 新模型序号数字, "reason": "简短原因"}`

func buildJudgePrompts(req RouteRequest, curCand *RouteCandidate) (string, string) {
	var b strings.Builder
	b.WriteString("候选模型列表：\n")
	for _, c := range req.Candidates {
		m := c.Model
		fmt.Fprintf(&b, "[%d] 名称=%q (id=%s, upstream=%s) | 档位=%s(T%d)",
			c.Index, m.DisplayName, m.ID, m.UpstreamModel, llm.TierLabel(c.Profile.Tier), c.Profile.Tier)
		var caps []string
		if m.Vision {
			caps = append(caps, "能看图")
		}
		if m.ThinkEffort != "" {
			caps = append(caps, "可深度思考")
		}
		if len(caps) > 0 {
			fmt.Fprintf(&b, " | 能力=%s", strings.Join(caps, ","))
		}
		if m.OutputPrice > 0 {
			fmt.Fprintf(&b, " | 输出单价=$%.2f/M", m.OutputPrice)
		}
		if c.IsLatestInSeries {
			fmt.Fprintf(&b, " | 版本=同系最新版(v%s，优先于旧版)", c.Profile.Version.String())
		} else if c.NewerVersion != "" {
			fmt.Fprintf(&b, " | 版本=较早版本(v%s，同系已有新版 v%s)", c.Profile.Version.String(), c.NewerVersion)
		}
		if note := strings.TrimSpace(m.RoutingNote); note != "" {
			fmt.Fprintf(&b, " | ★管理员场景说明=%q", clipRunes(note, 200))
		} else if desc := strings.TrimSpace(m.Description); desc != "" {
			fmt.Fprintf(&b, " | 简介=%q", clipRunes(desc, 60))
		}
		fmt.Fprintf(&b, " | 服务端兜底认知=%q\n", c.Profile.Summary())
	}

	if req.Assistant != nil && strings.TrimSpace(req.Assistant.Name) != "" {
		fmt.Fprintf(&b, "\n当前使用的助手角色：%s（%s）\n", req.Assistant.Name, clipRunes(req.Assistant.Description, 80))
	}
	var flags []string
	if req.HasImages {
		flags = append(flags, "本条消息附带了图片")
	}
	if req.DeepThink {
		flags = append(flags, "用户主动开启了「深度思考」开关（倾向强推理模型）")
	}
	if len(flags) > 0 {
		fmt.Fprintf(&b, "附加信号：%s\n", strings.Join(flags, "；"))
	}

	if req.Mode == RouteDynamic && curCand != nil {
		fmt.Fprintf(&b, "\n当前对话正在使用的模型：[%d] %s（%s, T%d）\n",
			curCand.Index, curCand.Model.DisplayName, llm.TierLabel(curCand.Profile.Tier), curCand.Profile.Tier)
		if len(req.RecentTurns) > 0 {
			b.WriteString("前序用户提问摘录：\n")
			for i, t := range req.RecentTurns {
				fmt.Fprintf(&b, "- 第%d轮：%s\n", i+1, clipRunes(t.UserText, 180))
			}
		}
		fmt.Fprintf(&b, "\n用户最新提问：\n%s\n", clipRunes(req.UserMessage, 600))
		return judgeDynamicSystem, b.String()
	}

	msg := strings.TrimSpace(req.UserMessage)
	if msg == "" && req.HasImages {
		msg = "（用户发送了图片请小暖查看）"
	}
	fmt.Fprintf(&b, "\n用户首个提问：\n%s\n", clipRunes(msg, 600))
	return judgeInitialSystem, b.String()
}

func parseJudgeReply(raw string, candidates []RouteCandidate) (*RouteCandidate, string, string, bool) {
	s := strings.TrimSpace(raw)
	// Strip optional markdown code fences or surrounding prose.
	if start := strings.IndexByte(s, '{'); start >= 0 {
		if end := strings.LastIndexByte(s, '}'); end > start {
			s = s[start : end+1]
		}
	}
	var parsed struct {
		Action  string `json:"action"`
		Pick    any    `json:"pick"`
		ModelID string `json:"model_id"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return nil, "", "", false
	}
	var chosen *RouteCandidate
	switch v := parsed.Pick.(type) {
	case float64:
		idx := int(v)
		for i := range candidates {
			if candidates[i].Index == idx {
				chosen = &candidates[i]
				break
			}
		}
	case string:
		v = strings.TrimSpace(v)
		for i := range candidates {
			if fmt.Sprintf("%d", candidates[i].Index) == v || candidates[i].Model.ID == v || strings.EqualFold(candidates[i].Model.DisplayName, v) {
				chosen = &candidates[i]
				break
			}
		}
	}
	if chosen == nil && parsed.ModelID != "" {
		for i := range candidates {
			if candidates[i].Model.ID == parsed.ModelID || candidates[i].Model.UpstreamModel == parsed.ModelID {
				chosen = &candidates[i]
				break
			}
		}
	}
	if chosen == nil {
		return nil, "", "", false
	}
	return chosen, strings.TrimSpace(parsed.Action), clipRunes(strings.TrimSpace(parsed.Reason), 40), true
}

// HeuristicRoute is the deterministic zero-network fallback router used when the
// Judge LLM is unreachable or times out. It combines query intent detection,
// admin routing_note matching, and the server's built-in model rank/version scores.
func HeuristicRoute(req RouteRequest) *RouteDecision {
	wantTier, wantDomain, reason := classifyQueryHeuristic(req)

	// In dynamic mode, only switch when the desired tier differs by at least 2 levels
	// or crosses between flagship(T4-T5) and daily/lightweight(T1-T3) with clear intent.
	if req.Mode == RouteDynamic && req.CurrentModel != nil {
		for i := range req.Candidates {
			c := &req.Candidates[i]
			if c.Model.ID == req.CurrentModel.ID {
				curTier := c.Profile.Tier
				bigDowngrade := curTier >= llm.TierStrong && wantTier <= llm.TierFast && (wantDomain == llm.DomainHomework || wantDomain == llm.DomainDaily)
				bigUpgrade := curTier <= llm.TierBalanced && wantTier >= llm.TierStrong && (wantDomain == llm.DomainMedical || wantDomain == llm.DomainResearch || wantDomain == llm.DomainReasoning)
				if !bigDowngrade && !bigUpgrade {
					return &RouteDecision{
						Model: c.Model, Provider: c.Provider, Switched: false,
						Reason: "heuristic_keep", RouterKind: "heuristic",
					}
				}
				break
			}
		}
	}

	type scored struct {
		cand  RouteCandidate
		score int
	}
	items := make([]scored, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		sc := 0
		// 1. Tier proximity: penalize over-kill on casual queries and under-power on hard queries.
		diff := c.Profile.Tier - wantTier
		if diff < 0 {
			sc += diff * 3500 // strong penalty if weaker than needed
		} else {
			sc -= diff * 2200 // mild penalty if much heavier/slower than needed
		}
		// 2. Domain match in built-in profile.
		if wantDomain != "" && c.Profile.HasDomain(wantDomain) {
			sc += 3000
		}
		// 3. Admin routing_note keyword boost (admin's explicit guidance wins).
		if note := strings.ToLower(c.Model.RoutingNote); note != "" {
			if matchesDomainNote(note, wantDomain, req.UserMessage) {
				sc += 6500
			}
		}
		// 4. Prefer newer versions within a series and stronger sub-rank within the target tier.
		if c.IsLatestInSeries {
			sc += 900
		} else if c.NewerVersion != "" {
			sc -= 900
		}
		sc += c.Profile.SubRank*20 + min(c.Profile.Version.Score(), 9999)/5
		if req.DeepThink && c.Model.ThinkEffort != "" {
			sc += 1500
		}
		items = append(items, scored{cand: c, score: sc})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
	best := items[0].cand
	return &RouteDecision{
		Model:      best.Model,
		Provider:   best.Provider,
		Switched:   req.CurrentModel == nil || req.CurrentModel.ID != best.Model.ID,
		Reason:     reason,
		RouterKind: "heuristic",
	}
}

func classifyQueryHeuristic(req RouteRequest) (wantTier int, domain string, reason string) {
	q := strings.ToLower(strings.TrimSpace(req.UserMessage))
	asstID := ""
	if req.Assistant != nil {
		asstID = req.Assistant.ID
	}

	// Kid's schoolwork / basic math check first so "孩子数学题" isn't confused with advanced research math!
	if asstID == "builtin-tutor" || hasAnyStr(q,
		"孩子", "小学", "一年级", "二年级", "三年级", "四年级", "五年级", "六年级",
		"初中", "应用题", "鸡兔同笼", "口算", "竖式", "家庭作业", "辅导作业", "练习题", "背诵") {
		return llm.TierFast, llm.DomainHomework, "homework_fast"
	}
	// Medical & health check.
	if asstID == "builtin-health" || hasAnyStr(q,
		"体检", "化验", "血常规", "血压", "血糖", "血脂", "甘油三酯", "尿酸", "结节", "CT", "核磁",
		"药", "副作用", "剂量", "症状", "发烧", "咳嗽", "头晕", "胸闷", "疼痛", "医院", "确诊", "手术", "处方", "医保") {
		return llm.TierFlagship, llm.DomainMedical, "medical_flagship"
	}
	// Hard science / physics / research / anti-fraud / deep thinking.
	if req.DeepThink || asstID == "builtin-antifraud" || hasAnyStr(q,
		"物理", "量子", "相对论", "电磁", "热力学", "力学", "微积分", "线性代数", "概率论", "定理", "推导", "证明",
		"论文", "科研", "文献", "有机化学", "分子", "架构设计", "源码", "诈骗", "转账", "安全账户") {
		return llm.TierFlagship, llm.DomainResearch, "research_flagship"
	}
	// Long writing / document analysis.
	if asstID == "builtin-writer" || utf8.RuneCountInString(q) > 350 || hasAnyStr(q,
		"写一篇", "长文", "演讲稿", "发言稿", "合同", "协议", "总结这篇", "润色", "方案", "计划书") {
		return llm.TierStrong, llm.DomainWriting, "writing_strong"
	}
	// Everyday chat, cooking, translation, greetings.
	if utf8.RuneCountInString(q) <= 30 || asstID == "builtin-cook" || asstID == "builtin-translate" || hasAnyStr(q,
		"你好", "早上好", "晚上好", "吃什么", "菜谱", "怎么做", "红烧", "炒", "汤", "翻译", "天气", "笑话", "闲聊") {
		return llm.TierFast, llm.DomainDaily, "daily_fast"
	}
	return llm.TierBalanced, llm.DomainDaily, "balanced_default"
}

func matchesDomainNote(noteLower, domain, userMsg string) bool {
	switch domain {
	case llm.DomainMedical:
		return hasAnyStr(noteLower, "医", "健康", "体检", "药", "研究", "病")
	case llm.DomainResearch, llm.DomainReasoning:
		return hasAnyStr(noteLower, "研究", "物理", "科学", "数学", "推理", "复杂", "难题", "严肃")
	case llm.DomainHomework:
		return hasAnyStr(noteLower, "作业", "数学题", "辅导", "孩子", "日常", "快", "轻量")
	case llm.DomainWriting:
		return hasAnyStr(noteLower, "写", "文", "润色", "长文本", "总结")
	case llm.DomainDaily:
		return hasAnyStr(noteLower, "日常", "聊天", "闲聊", "生活", "快", "省钱")
	}
	_ = userMsg
	return false
}

func clipRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes]) + "…"
}

// ErrRouterUnavailable is returned when an unimplemented router kind (e.g. JEV before configuration) is invoked directly.
var ErrRouterUnavailable = errors.New("router kind not configured")
