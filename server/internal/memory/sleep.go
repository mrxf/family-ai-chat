package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/store"
)

const (
	SettingLastSleepAt = "memory_last_sleep_at"

	SleepScheduleHour   = 3
	SleepScheduleMinute = 30
	sleepNightEndHour   = 6
	SleepCatchupWindow  = 28 * time.Hour

	InactivePurgeAgeDays         = 30
	OngoingGraceDays             = 3
	SleepCompressAgeDays         = 30
	SleepCompressOverflowAgeDays = 14
	SleepCompressIdleDays        = 14
	SleepCompressMinBatch        = 8
	SleepCompressMaxBatch        = 24
	SleepCompressMaxRounds       = 4

	RawEpisodeMaxIdleDays    = 90
	DigestEpisodeMaxIdleDays = 180
	MaxActiveRecallMemories  = 400

	CoreDedupMinItems = 25
	CoreDedupMinRunes = 1200

	sleepCallTimeout = 2 * time.Minute
)

// SleepReport summarizes one nightly sleep consolidation run.
type SleepReport struct {
	UsersProcessed     int
	PurgedInactive     int64
	OngoingConverted   int
	DigestsCreated     int
	EpisodesCompressed int
	StaleArchived      int
	CoreRewritten      int
	CoreDropped        int
}

func (r *SleepReport) add(o SleepReport) {
	r.UsersProcessed += o.UsersProcessed
	r.PurgedInactive += o.PurgedInactive
	r.OngoingConverted += o.OngoingConverted
	r.DigestsCreated += o.DigestsCreated
	r.EpisodesCompressed += o.EpisodesCompressed
	r.StaleArchived += o.StaleArchived
	r.CoreRewritten += o.CoreRewritten
	r.CoreDropped += o.CoreDropped
}

func (r SleepReport) Changed() bool {
	return r.PurgedInactive > 0 || r.OngoingConverted > 0 || r.DigestsCreated > 0 ||
		r.EpisodesCompressed > 0 || r.StaleArchived > 0 || r.CoreRewritten > 0 || r.CoreDropped > 0
}

const sleepEpisodePrompt = `你是「小暖」的夜间记忆睡眠整理员。下面是某位用户 30 天以前积累的一批历史对话摘要（#1…#n）。请你像人脑睡眠整理记忆一样，对这些旧记录进行「去芜存菁、同类压缩」：

输出严格的 JSON，不要任何解释：
{"digests":[{"refs":[1,3],"month":"YYYY-MM","content":"…","keywords":"…"}],"drop":[2,4]}

整理规则：
1. 果断丢弃过时琐事（放入 drop）：
   - 早已结束且没有后续参考价值的一次性琐事、常识科普问答、菜谱做法、改文案/写代码、临时小故障排查、早就过去的单次娱乐或周末小安排、偶发轻微且未再提及的小状况，把编号放进 drop。
   - 注意：如果某条记录标有「曾被回想 N 次」（N >= 1），说明用户后来提起过，优先保留或合并进 digests，不要轻易丢弃。
2. 多合一提炼压缩（放入 digests）：
   - 把围绕同一件事、同一家人/宠物困扰、同一阶段计划或同一主题的多条记录，合并浓缩为 1 条精炼的远期摘要（refs 填写被合并的所有原编号）。
   - 单条本身就有长期背景价值的重要往事（如大额购置决策、重要就医检查与用药结论、旅行经历、家庭重要事件），也可以单独提炼为 1 条保留（refs 填单个编号）。
3. 严格的历史视角与时间规范（极重要！）：
   - month 必须写原事件实际发生的年月 YYYY-MM（取对应原记录上的年月，绝不能写成今天！）。
   - content 严禁出现「最近」「近期」「正在」「这几天」「下周」等当下口吻！必须用客观历史视角陈述（例如写「曾对比选购给母亲的 3000 元价位按摩椅，倾向荣泰」「五岁柯基曾因雨后踩水反复皮肤瘙痒，讨论了吹干与药浴护理」），不超过 90 字，不要在 content 开头重复写日期前缀。
   - keywords 写 4–10 个空格分隔的口语词、俗称、同义词、上下位词。
4. ` + privateTopics + `
   涉及上述私密范畴的旧记录一律放入 drop，不要写进 digests。`

const sleepCorePrompt = `你是「小暖」的常驻画像整理员。下面是某位用户当前的常驻记忆条目（#1…#n）。长期积累后其中可能存在语义重复或碎片化表达，请在不丢失任何有效信息的前提下做「无损合并去重」：

输出严格的 JSON，不要任何解释：
{"update":[{"ref":1,"content":"合并改写后的内容","keywords":"4-8个空格分隔检索词"}],"drop":[4,7]}

铁律：
1. 绝不删减独立事实：任何具体的疾病、过敏原、药名与剂量、手术史、每位家人的信息、明确的个人习惯都必须完整保留！看不出重复或拿不准的一律不动。
2. 同类合并：只有两条或多条属于同一 category 且说的是同一件事（例如 #1「用户住在杭州」和 #4「用户家在杭州市西湖区」，或两条重复的饮食偏好）时，把完整信息合并进靠前那条（update），被吸收冗余的那条放进 drop。
3. 改写后的 content 仍用第三人称「用户……」，不超过 60 字，严禁使用无具体年月的「最近」「近期」「正在」。
4. 没有需要合并的重复项时，输出 {"update":[],"drop":[]}。`

// shouldRunSleep decides whether the nightly sleep pass is due at now.
func shouldRunSleep(now time.Time, lastSleepMs int64) bool {
	nowCST := now.In(zone)
	todayTarget := time.Date(nowCST.Year(), nowCST.Month(), nowCST.Day(), SleepScheduleHour, SleepScheduleMinute, 0, 0, zone)
	if lastSleepMs <= 0 {
		// On a brand-new or freshly upgraded deployment, wait for the quiet 03:30-06:00
		// night window rather than firing during daytime peak usage.
		nightEnd := time.Date(nowCST.Year(), nowCST.Month(), nowCST.Day(), sleepNightEndHour, 0, 0, 0, zone)
		return !nowCST.Before(todayTarget) && nowCST.Before(nightEnd)
	}
	if !nowCST.Before(todayTarget) && lastSleepMs < todayTarget.UnixMilli() {
		return true
	}
	return now.UnixMilli()-lastSleepMs >= SleepCatchupWindow.Milliseconds()
}

// SleepConsolidate runs the full 4-stage sleep consolidation across all users
// directly (callers sharing a live Worker should invoke Worker.RunSleep so per-user
// locks coordinate with ongoing conversation extractions).
func (s *Service) SleepConsolidate(ctx context.Context, nowMs int64) (SleepReport, error) {
	if nowMs <= 0 {
		nowMs = store.Now()
	}
	var total SleepReport
	purged, err := s.Store.PurgeInactiveMemories(ctx, nowMs-InactivePurgeAgeDays*dayMs)
	if err != nil {
		return total, err
	}
	total.PurgedInactive = purged

	prevSleepMs := s.lastSleepAt(ctx)
	users, err := s.Store.ListMemoryUserIDs(ctx)
	if err != nil {
		return total, err
	}
	needEmbed := false
	for _, uid := range users {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		rep, uErr := s.SleepUser(ctx, uid, nowMs, prevSleepMs)
		total.add(rep)
		if uErr != nil {
			s.Log.Warn("memory sleep user failed", "user", uid, "err", uErr)
			continue
		}
		if rep.OngoingConverted > 0 || rep.DigestsCreated > 0 || rep.CoreRewritten > 0 {
			needEmbed = true
		}
	}
	if err := s.Store.SetSetting(ctx, SettingLastSleepAt, strconv.FormatInt(nowMs, 10)); err != nil {
		s.Log.Warn("save memory_last_sleep_at", "err", err)
	}
	if needEmbed && ctx.Err() == nil {
		s.DrainEmbeddings(ctx)
	}
	return total, nil
}

func (s *Service) lastSleepAt(ctx context.Context) int64 {
	raw, err := s.Store.GetSetting(ctx, SettingLastSleepAt)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return v
}

// SleepUser executes the 4 sleep consolidation stages for a single user.
func (s *Service) SleepUser(ctx context.Context, userID string, nowMs, prevSleepMs int64) (SleepReport, error) {
	var rep SleepReport
	u, err := s.Store.GetUser(ctx, userID)
	if err != nil {
		return rep, err
	}
	// Users who paused memory ("已暂停记下和使用记忆，已有的内容会保留") keep their existing rows untouched.
	if !Enabled(u) {
		return rep, nil
	}
	rep.UsersProcessed = 1

	// Stage 1: Deterministic transition of expired ongoing items -> episode.
	converted, err := s.Store.TransitionExpiredOngoing(ctx, userID, nowMs-OngoingGraceDays*dayMs, nowMs, formatOngoingAsEpisode)
	if err != nil {
		return rep, err
	}
	rep.OngoingConverted = converted

	// Stage 2: LLM compression of >30d unactivated raw episodes into concise monthly digests.
	ms, err := s.Store.ListMemories(ctx, userID)
	if err != nil {
		return rep, err
	}
	activeRecallCount := 0
	for _, m := range ms {
		if m.AssistantID == nil && !isCore(m, nowMs) {
			activeRecallCount++
		}
	}
	minAgeDays := SleepCompressAgeDays
	minBatch := SleepCompressMinBatch
	if activeRecallCount > MaxActiveRecallMemories {
		minAgeDays = SleepCompressOverflowAgeDays
		minBatch = 4
	}
	var eligible []*store.Memory
	for _, m := range ms {
		if m.AssistantID != nil || m.Category != "episode" || m.Compressed || m.Source == "manual" {
			continue
		}
		if nowMs-memoryEventAt(m) < int64(minAgeDays)*dayMs {
			continue
		}
		if m.LastRecalledAt != nil && nowMs-*m.LastRecalledAt < SleepCompressIdleDays*dayMs {
			continue
		}
		eligible = append(eligible, m)
	}
	if len(eligible) >= minBatch {
		sort.SliceStable(eligible, func(i, j int) bool {
			ei, ej := memoryEventAt(eligible[i]), memoryEventAt(eligible[j])
			if ei != ej {
				return ei < ej
			}
			return eligible[i].ID < eligible[j].ID
		})
		batches := splitEpisodeBatches(eligible, minBatch, SleepCompressMaxBatch, SleepCompressMaxRounds)
		for _, batch := range batches {
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
			created, archived, cErr := s.compressEpisodeBatch(ctx, userID, batch, nowMs)
			if cErr != nil {
				s.Log.Warn("sleep episode compression skipped", "user", userID, "err", cErr)
				break
			}
			rep.DigestsCreated += created
			rep.EpisodesCompressed += archived
		}
	}

	// Reload after Stage 1 & Stage 2 mutations for accurate Stage 3 & Stage 4 evaluation.
	ms, err = s.Store.ListMemories(ctx, userID)
	if err != nil {
		return rep, err
	}

	// Stage 3: Natural inactivity forgetting (90d raw / 180d digest, boosted by recall_count) + 400-item cap.
	archiveIDs, expected := selectDecayArchives(ms, nowMs)
	if len(archiveIDs) > 0 {
		archived, aErr := s.Store.ArchiveDecayMemories(ctx, userID, expected, archiveIDs, nowMs)
		if aErr != nil {
			return rep, aErr
		}
		rep.StaleArchived = archived
	}

	// Stage 4: Lossless semantic deduplication of auto-extracted Core memories when overgrown.
	var autoCore []*store.Memory
	coreRunes := 0
	var latestCoreUpdate int64
	for _, m := range ms {
		if m.AssistantID == nil && m.Source != "manual" && isCore(m, nowMs) {
			autoCore = append(autoCore, m)
			coreRunes += utf8.RuneCountInString(m.Content)
			if m.UpdatedAt > latestCoreUpdate {
				latestCoreUpdate = m.UpdatedAt
			}
		}
	}
	if (len(autoCore) > CoreDedupMinItems || coreRunes > CoreDedupMinRunes) && latestCoreUpdate > prevSleepMs {
		rewritten, dropped, dErr := s.dedupCoreMemories(ctx, userID, autoCore, nowMs)
		if dErr != nil {
			s.Log.Warn("sleep core dedup skipped", "user", userID, "err", dErr)
		} else {
			rep.CoreRewritten = rewritten
			rep.CoreDropped = dropped
		}
	}

	return rep, nil
}

func splitEpisodeBatches(items []*store.Memory, minBatch, maxBatch, maxRounds int) [][]*store.Memory {
	var batches [][]*store.Memory
	for pos := 0; pos < len(items) && len(batches) < maxRounds; {
		rem := len(items) - pos
		if rem < minBatch {
			break
		}
		take := min(rem, maxBatch)
		// Avoid leaving a tiny tail smaller than minBatch when splitting evenly keeps both valid.
		if rem > maxBatch && rem-maxBatch < minBatch {
			take = rem / 2
		}
		batches = append(batches, items[pos:pos+take])
		pos += take
	}
	return batches
}

func (s *Service) compressEpisodeBatch(ctx context.Context, userID string, batch []*store.Memory, nowMs int64) (int, int, error) {
	cctx, cancel := context.WithTimeout(ctx, sleepCallTimeout)
	defer cancel()
	m, client, err := s.Resolve(cctx, "memory")
	if err != nil || m == nil {
		if err == nil {
			err = errNoMemoryModel
		}
		return 0, 0, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "今天日期：%s\n待整理的远期历史对话摘要（共 %d 条）：\n", time.UnixMilli(nowMs).In(zone).Format("2006-01-02"), len(batch))
	expected := make(map[string]int64, len(batch))
	for i, item := range batch {
		expected[item.ID] = item.UpdatedAt
		evMonth := time.UnixMilli(memoryEventAt(item)).In(zone).Format("2006-01")
		recallNote := ""
		if item.RecallCount > 0 {
			recallNote = fmt.Sprintf("（曾被回想 %d 次）", item.RecallCount)
		}
		fmt.Fprintf(&b, "#%d [%s]%s %s\n", i+1, evMonth, recallNote, strings.TrimSpace(item.Content))
	}

	text, _, err := client.Complete(cctx, llm.Request{
		Model:           m.UpstreamModel,
		ReasoningEffort: m.DefaultEffort,
		Messages: []llm.Message{
			{Role: "system", Content: sleepEpisodePrompt},
			{Role: "user", Content: b.String()},
		},
	})
	if err != nil {
		return 0, 0, err
	}

	specs, dropIDs, err := parseSleepEpisodeResponse(text, batch)
	if err != nil {
		return 0, 0, err
	}
	return s.Store.ApplySleepEpisodeCompression(ctx, userID, expected, specs, dropIDs, nowMs)
}

var errNoMemoryModel = fmt.Errorf("no memory model available")

func parseSleepEpisodeResponse(text string, batch []*store.Memory) ([]store.SleepDigestSpec, []string, error) {
	first, last := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if first < 0 || last <= first {
		return nil, nil, ErrParse
	}
	var raw struct {
		Digests []struct {
			Refs     []int  `json:"refs"`
			Month    string `json:"month"`
			Content  string `json:"content"`
			Keywords string `json:"keywords"`
		} `json:"digests"`
		Drop []int `json:"drop"`
	}
	if err := json.Unmarshal([]byte(text[first:last+1]), &raw); err != nil || (raw.Digests == nil && raw.Drop == nil) {
		return nil, nil, ErrParse
	}

	claimed := map[int]bool{}
	var specs []store.SleepDigestSpec
	for _, d := range raw.Digests {
		body := strings.TrimSpace(episodeDate.ReplaceAllString(strings.TrimSpace(d.Content), ""))
		body = clip(sanitizeStaleTemporalWords(body), 120)
		if body == "" {
			continue
		}
		var srcIDs []string
		var maxEv int64
		var maxRecall int64
		var maxLastRecall *int64
		var bestConv *string
		validMonths := map[string]bool{}
		for _, ref := range d.Refs {
			if ref < 1 || ref > len(batch) || claimed[ref] {
				continue
			}
			claimed[ref] = true
			src := batch[ref-1]
			srcIDs = append(srcIDs, src.ID)
			ev := memoryEventAt(src)
			validMonths[time.UnixMilli(ev).In(zone).Format("2006-01")] = true
			if ev >= maxEv {
				maxEv = ev
				if src.SourceConvID != nil && *src.SourceConvID != "" {
					cid := *src.SourceConvID
					bestConv = &cid
				}
			} else if bestConv == nil && src.SourceConvID != nil && *src.SourceConvID != "" {
				cid := *src.SourceConvID
				bestConv = &cid
			}
			if src.RecallCount > maxRecall {
				maxRecall = src.RecallCount
			}
			if src.LastRecalledAt != nil && (maxLastRecall == nil || *src.LastRecalledAt > *maxLastRecall) {
				v := *src.LastRecalledAt
				maxLastRecall = &v
			}
		}
		if len(srcIDs) == 0 {
			continue
		}
		month := strings.TrimSpace(d.Month)
		if !validMonths[month] {
			month = time.UnixMilli(maxEv).In(zone).Format("2006-01")
		}
		kw := clip(strings.TrimSpace(d.Keywords), 100)
		if kw == "" {
			// Inherit source keywords if the model omitted them so lexical recall stays strong.
			var kws []string
			seenKW := map[string]bool{}
			for _, sid := range srcIDs {
				for _, src := range batch {
					if src.ID == sid {
						for _, w := range strings.Fields(src.Keywords) {
							if !seenKW[w] {
								seenKW[w] = true
								kws = append(kws, w)
							}
						}
					}
				}
			}
			kw = clip(strings.Join(kws, " "), 100)
		}
		specs = append(specs, store.SleepDigestSpec{
			Digest: &store.Memory{
				Category:       "episode",
				Content:        month + " " + body,
				Keywords:       kw,
				SourceConvID:   bestConv,
				EventAt:        maxEv,
				CreatedAt:      maxEv,
				RecallCount:    maxRecall,
				LastRecalledAt: maxLastRecall,
				Compressed:     true,
			},
			SourceIDs: srcIDs,
		})
	}

	var dropIDs []string
	for _, ref := range raw.Drop {
		if ref < 1 || ref > len(batch) || claimed[ref] {
			continue
		}
		src := batch[ref-1]
		// Protect memories that have repeatedly proven useful across past chats.
		if src.RecallCount >= 2 {
			continue
		}
		claimed[ref] = true
		dropIDs = append(dropIDs, src.ID)
	}
	return specs, dropIDs, nil
}

// selectDecayArchives picks retrieval memories eligible for natural forgetting
// (90d raw / 180d digest with recall_count reinforcement) plus any excess over
// MaxActiveRecallMemories ranked by retentionScore.
func selectDecayArchives(ms []*store.Memory, nowMs int64) ([]string, map[string]int64) {
	expected := map[string]int64{}
	marked := map[string]bool{}
	var archiveIDs []string

	// Pass 1: Inactivity-based natural forgetting on episodes.
	for _, m := range ms {
		if m.AssistantID != nil || m.Source == "manual" || isCore(m, nowMs) || m.Category != "episode" {
			continue
		}
		anchor := memoryEventAt(m)
		if m.LastRecalledAt != nil && *m.LastRecalledAt > anchor {
			anchor = *m.LastRecalledAt
		}
		var maxIdleDays int64
		if m.Compressed {
			maxIdleDays = DigestEpisodeMaxIdleDays + min(m.RecallCount, 4)*90
		} else {
			maxIdleDays = RawEpisodeMaxIdleDays + min(m.RecallCount, 3)*60
		}
		if nowMs-anchor > maxIdleDays*dayMs {
			marked[m.ID] = true
			expected[m.ID] = m.UpdatedAt
			archiveIDs = append(archiveIDs, m.ID)
		}
	}

	// Pass 2: Enforce per-user active retrieval cap (MaxActiveRecallMemories).
	remainingRecall := 0
	var evictable []*store.Memory
	for _, m := range ms {
		if m.AssistantID != nil || isCore(m, nowMs) || marked[m.ID] {
			continue
		}
		remainingRecall++
		if !m.Pinned && m.Source != "manual" && m.Category != "health" && m.Category != "family" && m.Category != "profile" {
			evictable = append(evictable, m)
		}
	}
	if remainingRecall > MaxActiveRecallMemories && len(evictable) > 0 {
		excess := min(remainingRecall-MaxActiveRecallMemories, len(evictable))
		sort.SliceStable(evictable, func(i, j int) bool {
			si, sj := retentionScore(evictable[i], nowMs), retentionScore(evictable[j], nowMs)
			if si != sj {
				return si < sj // lowest retention score archived first
			}
			return memoryEventAt(evictable[i]) < memoryEventAt(evictable[j])
		})
		for _, m := range evictable[:excess] {
			marked[m.ID] = true
			expected[m.ID] = m.UpdatedAt
			archiveIDs = append(archiveIDs, m.ID)
		}
	}
	return archiveIDs, expected
}

func retentionScore(m *store.Memory, nowMs int64) float64 {
	const dayFloat = 24 * 3600 * 1000.0
	anchor := float64(memoryEventAt(m))
	if m.LastRecalledAt != nil && float64(*m.LastRecalledAt) > anchor {
		anchor = float64(*m.LastRecalledAt)
	}
	idleDays := math.Max(0, (float64(nowMs)-anchor)/dayFloat)
	score := math.Exp(-idleDays / 60.0)
	score += 0.35 * math.Log1p(float64(m.RecallCount))
	if m.Compressed {
		score += 0.25
	}
	if m.Category == "preference" {
		score += 0.45
	}
	return score
}

func (s *Service) dedupCoreMemories(ctx context.Context, userID string, autoCore []*store.Memory, nowMs int64) (int, int, error) {
	cctx, cancel := context.WithTimeout(ctx, sleepCallTimeout)
	defer cancel()
	m, client, err := s.Resolve(cctx, "memory")
	if err != nil || m == nil {
		if err == nil {
			err = errNoMemoryModel
		}
		return 0, 0, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "今天日期：%s\n常驻记忆列表：\n", time.UnixMilli(nowMs).In(zone).Format("2006-01-02"))
	for i, item := range autoCore {
		fmt.Fprintf(&b, "#%d [%s] %s\n", i+1, item.Category, strings.TrimSpace(item.Content))
	}

	text, _, err := client.Complete(cctx, llm.Request{
		Model:           m.UpstreamModel,
		ReasoningEffort: m.DefaultEffort,
		Messages: []llm.Message{
			{Role: "system", Content: sleepCorePrompt},
			{Role: "user", Content: b.String()},
		},
	})
	if err != nil {
		return 0, 0, err
	}

	first, last := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if first < 0 || last <= first {
		return 0, 0, ErrParse
	}
	var out struct {
		Update []struct {
			Ref      int    `json:"ref"`
			Content  string `json:"content"`
			Keywords string `json:"keywords"`
		} `json:"update"`
		Drop []int `json:"drop"`
	}
	if err := json.Unmarshal([]byte(text[first:last+1]), &out); err != nil {
		return 0, 0, ErrParse
	}
	// Safety valve: reject runaway output that tries to drop > 40% of core facts.
	if len(out.Drop) > len(autoCore)*2/5 {
		s.Log.Warn("sleep core dedup rejected excessive drops", "user", userID, "drop", len(out.Drop), "total", len(autoCore))
		return 0, 0, nil
	}

	expected := map[string]int64{}
	var dropIDs []string
	dropRefs := map[int]bool{}
	for _, ref := range out.Drop {
		if ref >= 1 && ref <= len(autoCore) && !dropRefs[ref] {
			dropRefs[ref] = true
			target := autoCore[ref-1]
			expected[target.ID] = target.UpdatedAt
			dropIDs = append(dropIDs, target.ID)
		}
	}
	var updates []store.CoreDedupUpdate
	seenUpdate := map[int]bool{}
	for _, u := range out.Update {
		if u.Ref < 1 || u.Ref > len(autoCore) || dropRefs[u.Ref] || seenUpdate[u.Ref] {
			continue
		}
		content := clip(strings.TrimSpace(u.Content), 80)
		if content == "" {
			continue
		}
		seenUpdate[u.Ref] = true
		target := autoCore[u.Ref-1]
		expected[target.ID] = target.UpdatedAt
		kw := clip(strings.TrimSpace(u.Keywords), 100)
		if kw == "" {
			kw = target.Keywords
		}
		updates = append(updates, store.CoreDedupUpdate{
			RefID:    target.ID,
			Content:  content,
			Keywords: kw,
		})
	}
	if len(updates) == 0 && len(dropIDs) == 0 {
		return 0, 0, nil
	}
	// Ensure we don't drop items when no update absorbed them unless exact MemoryKey duplicate exists.
	if len(updates) == 0 {
		activeKeys := map[string]bool{}
		for i, item := range autoCore {
			if !slices.Contains(out.Drop, i+1) {
				activeKeys[store.MemoryKey(item.Content)] = true
			}
		}
		filteredDrops := dropIDs[:0]
		for _, id := range dropIDs {
			for _, item := range autoCore {
				if item.ID == id && activeKeys[store.MemoryKey(item.Content)] {
					filteredDrops = append(filteredDrops, id)
				}
			}
		}
		dropIDs = filteredDrops
	}
	return s.Store.ApplySleepCoreDedup(ctx, userID, expected, updates, dropIDs, nowMs)
}
