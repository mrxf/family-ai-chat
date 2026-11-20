package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
)

func TestTemporalLabelsExpiredOngoingAndReopenedConversationDates(t *testing.T) {
	s, u, c := memoryFixture(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 19, 14, 0, 0, 0, zone).UnixMilli()

	// 1. Auto-extracted ongoing item without valid_until gets a default 21-day TTL and EventAt = msg.CreatedAt.
	msgTime := now - 5*dayMs // 2026-11-14
	msg := &store.Message{Role: "user", Content: "我在准备考驾照科目二", Status: store.StatusDone, CreatedAt: msgTime}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: c.ID, Append: []*store.Message{msg}}); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, s, c)
	if err := s.apply(ctx, snap, Extraction{
		Actions:  []Action{{Op: "add", Category: "ongoing", Content: "用户在准备考驾照科目二", Keywords: "驾照 学车 科目二"}},
		Episodes: []EpisodeDraft{{Content: "讨论科目二倒车入库练习计划", Keywords: "驾照 科目二 倒车"}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	ms, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil || len(ms) != 2 {
		t.Fatalf("expected 2 memories, got %d (%v)", len(ms), err)
	}
	var ongoingMem, epMem *store.Memory
	for _, m := range ms {
		if m.Category == "ongoing" {
			ongoingMem = m
		} else {
			epMem = m
		}
	}
	if ongoingMem == nil || ongoingMem.ValidUntil == nil || ongoingMem.EventAt != msgTime {
		t.Fatalf("ongoing missing default valid_until or event_at: %+v", ongoingMem)
	}
	wantUntil := time.Date(2026, 11, 14, 0, 0, 0, 0, zone).AddDate(0, 0, DefaultOngoingDays+1).UnixMilli() - 1
	if *ongoingMem.ValidUntil != wantUntil {
		t.Fatalf("ongoing valid_until=%d, want %d", *ongoingMem.ValidUntil, wantUntil)
	}
	coreLine := formatCoreEntry(ongoingMem)
	if !strings.Contains(coreLine, "记于 2026-11-14") || !strings.Contains(coreLine, "预计至 2026-12-05") {
		t.Fatalf("unexpected core ongoing line: %s", coreLine)
	}

	// 2. Reopening an old conversation 35 days later preserves unchanged episode date while dating new topic today.
	oldEv := now - 40*dayMs // 2026-10-10
	oldDate := time.UnixMilli(oldEv).In(zone).Format("2006-01-02")
	oldConv := &store.Conversation{UserID: u.ID}
	if err := s.Store.CreateConversation(ctx, oldConv); err != nil {
		t.Fatal(err)
	}
	seedEp := &store.Memory{
		UserID:       u.ID,
		Category:     "episode",
		Content:      oldDate + " 讨论给阳台买绿萝和浇水频率",
		Keywords:     "绿萝 浇水 植物",
		SourceConvID: &oldConv.ID,
		EventAt:      oldEv,
		CreatedAt:    oldEv,
	}
	if err := s.Store.CreateMemories(ctx, []*store.Memory{seedEp}); err != nil {
		t.Fatal(err)
	}
	newMsg := &store.Message{Role: "user", Content: "另外问一下宽带升级千兆划算吗", Status: store.StatusDone, CreatedAt: now}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: oldConv.ID, Append: []*store.Message{newMsg}}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(ctx, snapshot(t, s, oldConv), Extraction{
		Episodes: []EpisodeDraft{
			{Content: "讨论给阳台买绿萝和浇水频率", Keywords: "绿萝 浇水 植物"},
			{Content: "咨询家庭宽带升级千兆的性价比", Keywords: "宽带 千兆 路由器"},
		},
	}, nil); err != nil {
		t.Fatal(err)
	}
	updatedMems, _ := s.Store.ListMemories(ctx, u.ID)
	foundOldPreserved, foundNewDated := false, false
	todayStr := time.UnixMilli(now).In(zone).Format("2006-01-02")
	for _, m := range updatedMems {
		if m.SourceConvID != nil && *m.SourceConvID == oldConv.ID {
			if strings.Contains(m.Content, "绿萝") {
				if strings.HasPrefix(m.Content, oldDate) && m.EventAt == oldEv {
					foundOldPreserved = true
				}
			}
			if strings.Contains(m.Content, "宽带") {
				if strings.HasPrefix(m.Content, todayStr) && m.EventAt == now {
					foundNewDated = true
				}
			}
		}
	}
	if !foundOldPreserved || !foundNewDated {
		t.Fatalf("episode dates wrong after reopening old conv: preserved=%v new=%v mems=%+v", foundOldPreserved, foundNewDated, updatedMems)
	}

	// 3. Expired ongoing item formats with explicit relative age + "已过时效" and strips misleading "近期正在".
	expiredUntil := now - 10*dayMs
	staleOngoing := &store.Memory{
		UserID:     u.ID,
		Category:   "ongoing",
		Content:    "用户近期正在看学区房",
		ValidUntil: &expiredUntil,
		EventAt:    now - 35*dayMs,
		CreatedAt:  now - 35*dayMs,
	}
	formatted := formatRecallEntry(staleOngoing, now, "other-conv")
	if !strings.Contains(formatted, "1个月前") || !strings.Contains(formatted, "已过时效") || strings.Contains(formatted, "近期") || !strings.Contains(formatted, "当时正在看学区房") {
		t.Fatalf("stale ongoing formatting failed: %s", formatted)
	}
	_ = epMem
}

func TestAgeGatedAutoInjectionVsExplicitRecallTool(t *testing.T) {
	s, u, _ := memoryFixture(t)
	ctx := t.Context()
	now := store.Now()

	freshEv := now - 2*dayMs
	coolingEv := now - 18*dayMs
	distantEv := now - 42*dayMs
	traitEv := now - 120*dayMs

	mems := []*store.Memory{
		// 2-day-old episode
		{UserID: u.ID, Category: "episode", Content: "2026-11-17 聊了买新羽绒服的颜色选择", EventAt: freshEv, CreatedAt: freshEv},
		// 18-day-old episode (Cooling window: 8..30d)
		{UserID: u.ID, Category: "episode", Content: "2026-11-01 讨论挑选机械键盘红轴与茶轴的区别", EventAt: coolingEv, CreatedAt: coolingEv},
		// 42-day-old episode (Distant window: >30d)
		{UserID: u.ID, Category: "episode", Content: "2026-10-08 聊了想吃麻辣火锅但当时嗓子发炎疼痛", EventAt: distantEv, CreatedAt: distantEv},
		// 120-day-old demoted preference (Long-term trait, NOT time-bound chatter)
		{UserID: u.ID, Category: "preference", Content: "用户点外卖从来不吃香菜和折耳根", Pinned: false, EventAt: traitEv, CreatedAt: traitEv},
	}
	if err := s.Store.CreateMemories(ctx, mems); err != nil {
		t.Fatal(err)
	}

	// Case 1: Fresh 2-day-old episode IS recalled on ordinary mention with [2天前] tag.
	c1 := s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c1", UserMessages: []string{"羽绒服到了"}})
	if !strings.Contains(c1.Recall, "羽绒服") || !strings.Contains(c1.Recall, "[2天前]") {
		t.Fatalf("fresh episode should auto-inject with [2天前]: %s", c1.Recall)
	}

	// Case 2: 18-day-old episode is NOT recalled on single-word collision ("键盘"),
	//         but IS recalled when multiple specific concepts match ("机械键盘 红轴").
	c2Noise := s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c2", UserMessages: []string{"我刚把电脑键盘擦干净了"}})
	if strings.Contains(c2Noise.Recall, "茶轴") {
		t.Fatalf("18-day episode falsely triggered on casual single word '键盘': %s", c2Noise.Recall)
	}
	c2Specific := s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c2", UserMessages: []string{"机械键盘选红轴好吗"}})
	if !strings.Contains(c2Specific.Recall, "茶轴") || !strings.Contains(c2Specific.Recall, "周前") {
		t.Fatalf("18-day episode missed specific multi-bigram query: %s", c2Specific.Recall)
	}

	// Case 3: 42-day-old episode ("麻辣火锅+嗓子发炎") MUST STAY SILENT when user casually says
	//         "今晚我们去吃麻辣火锅吧" (preventing the model from asking about a 42-day-old sore throat!).
	c3Forward := s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c3", UserMessages: []string{"今晚我们去吃麻辣火锅吧"}})
	if strings.Contains(c3Forward.Recall, "嗓子发炎") {
		t.Fatalf("42-day stale episode leaked into forward-looking hotpot chat: %s", c3Forward.Recall)
	}

	// Case 4: When user explicitly uses a retrospective cue ("之前吃麻辣火锅那次"), it IS recalled with [1个月前]!
	c4Retro := s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c4", UserMessages: []string{"你还记得之前聊吃麻辣火锅那次吗"}})
	if !strings.Contains(c4Retro.Recall, "嗓子发炎") || !strings.Contains(c4Retro.Recall, "[1个月前]") {
		t.Fatalf("42-day episode missed explicit retrospective query: %s", c4Retro.Recall)
	}

	// Case 5: Active tool Recall() ALWAYS finds the 42-day-old episode even without retrospective phrasing.
	toolOut := s.Recall(ctx, u, nil, "c5", RecallRequest{Queries: []string{"麻辣火锅"}})
	if !strings.Contains(toolOut, "嗓子发炎") || !strings.Contains(toolOut, "1个月前") {
		t.Fatalf("recall_memory tool failed to retrieve 42-day episode: %s", toolOut)
	}

	// Case 6: 120-day-old demoted preference ("不吃香菜") IS still recalled in ordinary chat.
	c6Trait := s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c6", UserMessages: []string{"今晚点什么外卖好，注意香菜"}})
	if !strings.Contains(c6Trait.Recall, "折耳根") {
		t.Fatalf("long-term demoted preference should not be blocked by 30-day episode gate: %s", c6Trait.Recall)
	}
}

func TestRecallActivationTrackingDebounce(t *testing.T) {
	s, u, _ := memoryFixture(t)
	ctx := t.Context()
	ep := &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-11-18 讨论购买荣泰按摩椅"}
	if err := s.Store.CreateMemories(ctx, []*store.Memory{ep}); err != nil {
		t.Fatal(err)
	}

	// Browse mode="recent" must NOT increment RecallCount.
	_ = s.Recall(ctx, u, nil, "c1", RecallRequest{Mode: "recent"})
	got, _ := s.Store.GetMemory(ctx, ep.ID, u.ID)
	if got.RecallCount != 0 || got.LastRecalledAt != nil {
		t.Fatalf("recent browse should not count as semantic activation: %+v", got)
	}

	// First semantic hit increments RecallCount to 1.
	_ = s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c1", UserMessages: []string{"荣泰按摩椅尺寸"}})
	got, _ = s.Store.GetMemory(ctx, ep.ID, u.ID)
	if got.RecallCount != 1 || got.LastRecalledAt == nil {
		t.Fatalf("expected recall_count=1 after first hit, got %+v", got)
	}

	// Immediate second hit within 24h cooldown stays at 1.
	_ = s.InjectTurn(ctx, u, InjectRequest{ConversationID: "c1", UserMessages: []string{"荣泰按摩椅价格"}})
	got, _ = s.Store.GetMemory(ctx, ep.ID, u.ID)
	if got.RecallCount != 1 {
		t.Fatalf("expected 24h debounce to hold recall_count=1, got %d", got.RecallCount)
	}

	// Simulate 25 hours later via TouchMemoriesRecall -> increments to 2!
	future := *got.LastRecalledAt + 25*3600*1000
	if err := s.Store.TouchMemoriesRecall(ctx, u.ID, []string{ep.ID}, future, RecallTouchCooldownMs); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Store.GetMemory(ctx, ep.ID, u.ID)
	if got.RecallCount != 2 || *got.LastRecalledAt != future {
		t.Fatalf("expected recall_count=2 after cooldown elapsed, got %+v", got)
	}
}

func TestNightlySleepConsolidationFourStagesAndSafetyInvariants(t *testing.T) {
	s, u, pastConv := memoryFixture(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 19, 3, 35, 0, 0, zone).UnixMilli()

	// Seed protected memories (220 days old, 0 recalls -> MUST NEVER BE DROPPED).
	veryOld := now - 220*dayMs
	protectedHealth := &store.Memory{UserID: u.ID, Category: "health", Content: "用户对青霉素严重过敏", Pinned: true, EventAt: veryOld, CreatedAt: veryOld}
	protectedFamily := &store.Memory{UserID: u.ID, Category: "family", Content: "用户的女儿叫林晓，在上海工作", Pinned: true, EventAt: veryOld, CreatedAt: veryOld}
	protectedManual := &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-04-01 手动记下的重要纪念日备忘", Source: "manual", Pinned: false, EventAt: veryOld, CreatedAt: veryOld}

	// Stage 1 fixtures: one ongoing expired 5d ago (-> becomes episode), one ongoing still valid (+10d -> stays ongoing).
	expUntil := now - 5*dayMs
	ongoingEv := now - 25*dayMs
	expiredOngoing := &store.Memory{UserID: u.ID, Category: "ongoing", Content: "用户近期正在给小狗换低敏狗粮", Pinned: true, ValidUntil: &expUntil, EventAt: ongoingEv, CreatedAt: ongoingEv}
	activeUntil := now + 10*dayMs
	activeOngoing := &store.Memory{UserID: u.ID, Category: "ongoing", Content: "2026年11月用户在准备去三亚过冬", Pinned: true, ValidUntil: &activeUntil, EventAt: now - 3*dayMs, CreatedAt: now - 3*dayMs}

	// Stage 2 fixtures: 9 raw episodes from ~45 days ago (2026-10), >= SleepCompressMinBatch (8).
	octEv := time.Date(2026, 10, 5, 10, 0, 0, 0, zone).UnixMilli()
	var octBatch []*store.Memory
	// #1..#4: repeated massage chair research
	for i, txt := range []string{
		"2026-10-05 咨询给母亲挑按摩椅的预算在3000元左右",
		"2026-10-06 对比荣泰与奥佳华按摩椅的占地尺寸",
		"2026-10-08 询问老年人用按摩椅气囊力度是否伤骨头",
		"2026-10-10 决定趁活动入手荣泰入门款按摩椅",
	} {
		octBatch = append(octBatch, &store.Memory{
			UserID: u.ID, Category: "episode", Content: txt, Keywords: "按摩椅 妈妈 荣泰",
			SourceConvID: &pastConv.ID, EventAt: octEv + int64(i)*dayMs, CreatedAt: octEv + int64(i)*dayMs,
		})
	}
	// #5..#8: trivial one-off Q&As (0 recalls -> LLM drops them)
	for i, txt := range []string{
		"2026-10-11 询问西红柿炒鸡蛋先放蛋还是先放番茄",
		"2026-10-12 让小暖把一段英文邮件翻译成中文",
		"2026-10-13 询问家里光猫闪红灯怎么重启",
		"2026-10-14 闲聊周末天气适不适合晒被子",
	} {
		octBatch = append(octBatch, &store.Memory{
			UserID: u.ID, Category: "episode", Content: txt,
			EventAt: octEv + int64(5+i)*dayMs, CreatedAt: octEv + int64(5+i)*dayMs,
		})
	}
	// #9: high-recall episode (RecallCount=3, recalled 20d ago) -> even if LLM puts #9 in drop, safety guard rescues it!
	recalled20d := now - 20*dayMs
	highRecallEp := &store.Memory{
		UserID: u.ID, Category: "episode", Content: "2026-10-15 讨论父亲体检尿酸偏高的饮食控制方案",
		Keywords: "父亲 尿酸 痛风 体检", RecallCount: 3, LastRecalledAt: &recalled20d,
		EventAt: octEv + 10*dayMs, CreatedAt: octEv + 10*dayMs,
	}
	octBatch = append(octBatch, highRecallEp)

	// Stage 3 fixtures:
	// - On u: 195d compressed digest with 0 recalls -> auto-archived (>180d);
	//         195d compressed digest with 1 recall (recalled 40d ago) -> kept alive!
	// - On u2 (<8 raw episodes, so Stage 2 LLM skips u2):
	//         100d raw episode with 0 recalls -> auto-archived (>90d);
	//         100d raw episode with 2 recalls (recalled 30d ago) -> kept alive!
	ev100d := now - 100*dayMs
	ev195d := now - 195*dayMs
	rec30d := now - 30*dayMs
	rec40d := now - 40*dayMs
	staleDigest0 := &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-05 曾讨论五一假期周边游路线", Compressed: true, EventAt: ev195d, CreatedAt: ev195d}
	reinforcedDigest := &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-05 曾整理母亲膝关节核磁检查结论与康复动作", Compressed: true, RecallCount: 1, LastRecalledAt: &rec40d, EventAt: ev195d, CreatedAt: ev195d}

	u2 := &store.User{Role: "member", Username: "user2", DisplayName: "二叔"}
	if err := s.Store.CreateUser(ctx, u2); err != nil {
		t.Fatal(err)
	}
	staleRaw0 := &store.Memory{UserID: u2.ID, Category: "episode", Content: "2026-08-10 问过一次高铁改签手续费", EventAt: ev100d, CreatedAt: ev100d}
	reinforcedRaw := &store.Memory{UserID: u2.ID, Category: "episode", Content: "2026-08-10 讨论老伴术后复查指标变化", RecallCount: 2, LastRecalledAt: &rec30d, EventAt: ev100d, CreatedAt: ev100d}
	if err := s.Store.CreateMemories(ctx, []*store.Memory{staleRaw0, reinforcedRaw}); err != nil {
		t.Fatal(err)
	}

	// Stage 4 fixtures: Core > 1200 runes with duplicate profile items ("住在杭州" + "家在杭州市西湖区").
	dupProfile1 := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户住在杭州", Pinned: true}
	dupProfile2 := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户家在杭州市西湖区", Pinned: true}
	paddingCore := &store.Memory{UserID: u.ID, Category: "profile", Content: "用户退休前从事机械工程设计工作，" + strings.Repeat("经历丰富", 290), Pinned: true}

	allSeed := []*store.Memory{
		protectedHealth, protectedFamily, protectedManual,
		expiredOngoing, activeOngoing,
		staleDigest0, reinforcedDigest,
		dupProfile1, dupProfile2, paddingCore,
	}
	allSeed = append(allSeed, octBatch...)
	if err := s.Store.CreateMemories(ctx, allSeed); err != nil {
		t.Fatal(err)
	}

	// Also insert an already-archived memory whose updated_at is 35 days old -> Stage 1 GC must purge it physically.
	deadOld := &store.Memory{UserID: u.ID, Category: "episode", Content: "旧归档垃圾", Status: "archived"}
	if err := s.Store.CreateMemories(ctx, []*store.Memory{deadOld}); err != nil {
		t.Fatal(err)
	}
	// Backdate deadOld.updated_at via PurgeInactiveMemories cutoff test: set now + 35d? No, we already use `now`;
	// CreateMemories sets UpdatedAt = store.Now() (~2026-11-19 15:xx), so let's advance `sleepNow = store.Now() + 1000`
	// and create a superseded row via ApplySleepEpisodeCompression or test PurgeInactiveMemories at `now + 35*dayMs` in pass 2!

	var compressCalls, coreDedupCalls, embedCalls atomic.Int32
	client := llm.NewClient("", "https://test.invalid/v1", "key")
	client.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			embedCalls.Add(1)
			var in struct{ Input []string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			data := make([]map[string]any, len(in.Input))
			for i := range in.Input {
				data[i] = map[string]any{"index": i, "embedding": []float32{0.8, 0.6}}
			}
			raw, _ := json.Marshal(map[string]any{"data": data})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		sysPrompt := req.Messages[0].Content
		var reply string
		if strings.Contains(sysPrompt, "夜间记忆睡眠整理员") {
			compressCalls.Add(1)
			// Merge #1..#4 into 1 digest; attempt to drop #5..#9 (note: #9 has RecallCount=3 so safety guard must rescue #9!).
			reply = `{"digests":[{"refs":[1,2,3,4],"month":"2026-10","content":"近期正在给母亲对比按摩椅并决定入手荣泰入门款","keywords":"按摩椅 妈妈 荣泰 奥佳华"}],"drop":[5,6,7,8,9]}`
		} else if strings.Contains(sysPrompt, "常驻画像整理员") {
			coreDedupCalls.Add(1)
			// Merge #1 ("用户住在杭州") and #2 ("用户家在杭州市西湖区") in autoCore.
			userPrompt := req.Messages[1].Content
			ref1, ref2 := 0, 0
			for _, line := range strings.Split(userPrompt, "\n") {
				var n int
				if strings.Contains(line, "用户住在杭州") && !strings.Contains(line, "西湖区") {
					_, _ = fmt.Sscanf(line, "#%d", &n)
					ref1 = n
				} else if strings.Contains(line, "用户家在杭州市西湖区") {
					_, _ = fmt.Sscanf(line, "#%d", &n)
					ref2 = n
				}
			}
			reply = fmt.Sprintf(`{"update":[{"ref":%d,"content":"用户家在杭州市西湖区","keywords":"杭州 西湖区 住址"}],"drop":[%d]}`, ref1, ref2)
		} else {
			// Keyword backfill prompt during DrainEmbeddings.
			reply = `{"items":[{"ref":1,"keywords":"小狗 狗粮 低敏"}]}`
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(_ context.Context, kind string) (*store.Model, *llm.Client, error) {
		return &store.Model{ID: "m-" + kind, UpstreamModel: kind, DefaultEffort: "low"}, client, nil
	}

	w := &Worker{service: s, ctx: ctx, busy: map[string]bool{}, failures: map[string]int{}, slots: make(chan struct{}, 2)}
	sleepNow := max(now, store.Now()+1000)
	rep, err := w.RunSleep(ctx, sleepNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OngoingConverted != 1 || rep.DigestsCreated != 1 || rep.EpisodesCompressed != 8 || rep.StaleArchived != 2 || rep.CoreRewritten != 1 || rep.CoreDropped != 1 {
		t.Fatalf("unexpected sleep report: %+v", rep)
	}
	if compressCalls.Load() != 1 || coreDedupCalls.Load() != 1 || embedCalls.Load() == 0 {
		t.Fatalf("expected 1 compress, 1 core dedup, >=1 embed call; got %d, %d, %d", compressCalls.Load(), coreDedupCalls.Load(), embedCalls.Load())
	}
	u2Mems, err := s.Store.ListMemories(ctx, u2.ID)
	if err != nil || len(u2Mems) != 1 || u2Mems[0].ID != reinforcedRaw.ID {
		t.Fatalf("expected u2 to archive 100d 0-recall raw episode and keep reinforced raw episode, got %+v (%v)", u2Mems, err)
	}

	after, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*store.Memory{}
	var digest *store.Memory
	for _, m := range after {
		byID[m.ID] = m
		if m.Compressed && strings.Contains(m.Content, "荣泰入门款") {
			digest = m
		}
	}

	// Protected items MUST still be active and untouched.
	for _, prot := range []*store.Memory{protectedHealth, protectedFamily, protectedManual, activeOngoing, reinforcedDigest} {
		if byID[prot.ID] == nil {
			t.Fatalf("protected/reinforced memory %q (%s) was wrongly archived!", prot.Content, prot.Category)
		}
	}
	// High-recall episode #9 MUST have been rescued from LLM drop!
	if byID[highRecallEp.ID] == nil {
		t.Fatalf("high-recall episode (RecallCount=3) should not be dropped by LLM")
	}
	// 195d zero-recall digest MUST be archived.
	if byID[staleDigest0.ID] != nil {
		t.Fatalf("195-day zero-recall digest should have been archived")
	}
	// Expired ongoing item MUST now be an unpinned dated episode without "近期正在".
	convEp := byID[expiredOngoing.ID]
	if convEp == nil || convEp.Category != "episode" || convEp.Pinned || strings.Contains(convEp.Content, "近期") {
		t.Fatalf("expired ongoing transition wrong: %+v", convEp)
	}
	// Compressed digest MUST preserve October EventAt & CreatedAt (so is_new is false!) and strip "近期正在".
	if digest == nil {
		t.Fatalf("missing compressed massage chair digest in %+v", after)
	}
	if !strings.HasPrefix(digest.Content, "2026-10 ") || strings.Contains(digest.Content, "近期正在") {
		t.Fatalf("digest content should have 2026-10 prefix and historical phrasing: %s", digest.Content)
	}
	if digest.EventAt != octEv+3*dayMs || digest.CreatedAt != digest.EventAt {
		t.Fatalf("digest did not preserve historical October timestamp: event=%d created=%d", digest.EventAt, digest.CreatedAt)
	}
	rawJSON, _ := json.Marshal(digest)
	if strings.Contains(string(rawJSON), `"is_new":true`) {
		t.Fatalf("historical compressed digest must not be badged is_new: %s", string(rawJSON))
	}

	// Core dedup: "用户家在杭州市西湖区" retained once, duplicate superseded.
	hangzhouCount := 0
	for _, m := range after {
		if m.Category == "profile" && strings.Contains(m.Content, "杭州") {
			hangzhouCount++
			if m.Content != "用户家在杭州市西湖区" {
				t.Fatalf("unexpected merged profile text: %s", m.Content)
			}
		}
	}
	if hangzhouCount != 1 {
		t.Fatalf("expected exactly 1 merged Hangzhou profile memory, got %d", hangzhouCount)
	}

	// Running sleep again 1 hour later without new edits does ZERO LLM calls;
	// running 35 days later physically purges the archived/superseded rows!
	rep2, err := w.RunSleep(ctx, sleepNow+3600*1000)
	if err != nil {
		t.Fatal(err)
	}
	if compressCalls.Load() != 1 || coreDedupCalls.Load() != 1 || rep2.Changed() {
		t.Fatalf("idempotent re-run should do zero work: calls=(%d,%d) rep=%+v", compressCalls.Load(), coreDedupCalls.Load(), rep2)
	}

	// Advance 35 days: physical GC purges all the archived/superseded rows from pass 1,
	// and raw episodes with 0 recalls that crossed 90 days also archive cleanly.
	rep3, err := w.RunSleep(ctx, sleepNow+35*dayMs)
	if err != nil {
		t.Fatal(err)
	}
	if rep3.PurgedInactive < 10 {
		t.Fatalf("expected >=10 inactive rows physically purged after 35 days, got %d", rep3.PurgedInactive)
	}
}

func TestHighIntensityChatterCapacityCap400(t *testing.T) {
	s, u, _ := memoryFixture(t)
	ctx := t.Context()
	now := store.Now()

	// Seed protected facts + 455 fresh episodes (<5 days old):
	// 10 have RecallCount=4, 10 are Compressed=true, 1 is manual, and 434 are 0-recall raw episodes.
	var seed []*store.Memory
	seed = append(seed,
		&store.Memory{UserID: u.ID, Category: "health", Content: "用户服用缬沙坦 80mg", Pinned: true},
		&store.Memory{UserID: u.ID, Category: "episode", Content: "手动保存的重要对话记录", Source: "manual"},
	)
	var mustKeepIDs []string
	for i := 0; i < 10; i++ {
		rc := now - dayMs
		m := &store.Memory{
			UserID: u.ID, Category: "episode", Content: fmt.Sprintf("2026-11-18 高频激活记录 #%d", i),
			RecallCount: 4, LastRecalledAt: &rc, EventAt: now - 3*dayMs, CreatedAt: now - 3*dayMs,
		}
		seed = append(seed, m)
	}
	for i := 0; i < 10; i++ {
		m := &store.Memory{
			UserID: u.ID, Category: "episode", Content: fmt.Sprintf("2026-11 睡眠汇总记录 #%d", i),
			Compressed: true, EventAt: now - 4*dayMs, CreatedAt: now - 4*dayMs,
		}
		seed = append(seed, m)
	}
	for i := 0; i < 434; i++ {
		ev := now - int64(i%5+1)*dayMs - int64(i)*1000
		seed = append(seed, &store.Memory{
			UserID: u.ID, Category: "episode", Content: fmt.Sprintf("2026-11-15 高强度闲聊流水账 #%d", i),
			EventAt: ev, CreatedAt: ev,
		})
	}
	if err := s.Store.CreateMemories(ctx, seed); err != nil {
		t.Fatal(err)
	}
	for _, m := range seed[:22] {
		mustKeepIDs = append(mustKeepIDs, m.ID)
	}

	rep, err := s.SleepConsolidate(ctx, now+1000)
	if err != nil {
		t.Fatal(err)
	}
	// Total retrieval items was 1 (manual) + 10 + 10 + 434 = 455 -> 55 excess archived down to 400!
	if rep.StaleArchived != 55 {
		t.Fatalf("expected 55 excess episodes archived to enforce 400 cap, got %+v", rep)
	}
	after, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 1 pinned health + 400 retrieval = 401 total active memories.
	if len(after) != 401 {
		t.Fatalf("expected 401 total active memories (1 core + 400 recall), got %d", len(after))
	}
	activeSet := map[string]bool{}
	for _, m := range after {
		activeSet[m.ID] = true
	}
	for _, id := range mustKeepIDs {
		if !activeSet[id] {
			t.Fatalf("protected/high-retention item %s was wrongly evicted under cap pressure", id)
		}
	}
}

func TestSleepScheduleWindowsAndPausedUserSkipped(t *testing.T) {
	// 1. Schedule window logic:
	// First deploy at 20:15 CST -> wait for night window (false).
	evening := time.Date(2026, 11, 19, 20, 15, 0, 0, zone)
	if shouldRunSleep(evening, 0) {
		t.Fatal("fresh install should not trigger sleep consolidation during evening peak hours")
	}
	// First night at 03:40 CST -> run (true).
	night := time.Date(2026, 11, 20, 3, 40, 0, 0, zone)
	if !shouldRunSleep(night, 0) {
		t.Fatal("should trigger at 03:40 CST on first night")
	}
	// Same day 15:00 CST after 03:40 run -> skip (false).
	afternoon := time.Date(2026, 11, 20, 15, 0, 0, 0, zone)
	if shouldRunSleep(afternoon, night.UnixMilli()) {
		t.Fatal("should not re-run on the same day after 03:40 run")
	}
	// Next day 10:30 CST (server was offline at 03:30 AM) -> catch up (true).
	nextMorning := time.Date(2026, 11, 21, 10, 30, 0, 0, zone)
	if !shouldRunSleep(nextMorning, night.UnixMilli()) {
		t.Fatal("should catch up when server missed 03:30 AM")
	}

	// 2. Paused user (memory_enabled=false) must be completely untouched by SleepConsolidate.
	s, u, _ := memoryFixture(t)
	ctx := t.Context()
	if _, err := s.Store.UpdateUser(ctx, u.ID, store.UserPatch{Settings: json.RawMessage(`{"memory_enabled":false}`)}); err != nil {
		t.Fatal(err)
	}
	now := store.Now()
	veryOld := now - 200*dayMs
	ep := &store.Memory{UserID: u.ID, Category: "episode", Content: "2026-04-01 暂停用户的旧记录", EventAt: veryOld, CreatedAt: veryOld}
	if err := s.Store.CreateMemories(ctx, []*store.Memory{ep}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.SleepConsolidate(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UsersProcessed != 0 || rep.StaleArchived != 0 {
		t.Fatalf("paused user should be skipped: %+v", rep)
	}
	if got, err := s.Store.GetMemory(ctx, ep.ID, u.ID); err != nil || got.Status != "active" {
		t.Fatalf("paused user memory modified: %+v %v", got, err)
	}
}

func TestSleepConcurrentEditGuardAndRunawayCoreDropProtection(t *testing.T) {
	s, u, _ := memoryFixture(t)
	ctx := t.Context()
	now := store.Now()
	oct := now - 45*dayMs

	// Seed 8 old episodes (#1..#8) + 30 Core memories.
	var eps []*store.Memory
	for i := 0; i < 8; i++ {
		eps = append(eps, &store.Memory{
			UserID: u.ID, Category: "episode", Content: fmt.Sprintf("2026-10-0%d 旧话题记录 #%d", i+1, i+1),
			EventAt: oct + int64(i)*1000, CreatedAt: oct + int64(i)*1000,
		})
	}
	var cores []*store.Memory
	for i := 0; i < 28; i++ {
		cores = append(cores, &store.Memory{
			UserID: u.ID, Category: "preference", Content: fmt.Sprintf("用户喜好条目 #%d", i+1), Pinned: true,
		})
	}
	if err := s.Store.CreateMemories(ctx, append(eps, cores...)); err != nil {
		t.Fatal(err)
	}

	editedText := "用户在App里刚刚手动改过的重要记录"
	client := llm.NewClient("", "https://test.invalid/v1", "key")
	client.HTTP = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var reply string
		if strings.Contains(req.Messages[0].Content, "夜间记忆睡眠整理员") {
			// Simulate user concurrently editing eps[0] (#1) and eps[4] (#5) while LLM is in flight!
			_, err := s.Store.UpdateMemory(ctx, eps[0].ID, u.ID, nil, &editedText)
			if err != nil {
				t.Error(err)
			}
			_, err = s.Store.UpdateMemory(ctx, eps[4].ID, u.ID, nil, &editedText)
			if err != nil {
				t.Error(err)
			}
			// LLM tries to merge #1+#2 (contaminated by concurrent edit -> must skip),
			// merge #3+#4 (clean -> must succeed), drop #5 (edited -> must skip), drop #6 (clean -> must archive).
			reply = `{"digests":[{"refs":[1,2],"month":"2026-10","content":"不应写入的合并","keywords":"测试"},{"refs":[3,4],"month":"2026-10","content":"正常合并的摘要","keywords":"测试"}],"drop":[5,6]}`
		} else if strings.Contains(req.Messages[0].Content, "常驻画像整理员") {
			// Runaway model tries to drop 20 out of 28 (>40%) core items -> safety guard MUST reject!
			reply = `{"update":[],"drop":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20]}`
		} else {
			reply = `{"items":[]}`
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})}
	s.Resolve = func(_ context.Context, kind string) (*store.Model, *llm.Client, error) {
		if kind == "embedding" {
			return nil, nil, nil
		}
		return &store.Model{ID: "m", UpstreamModel: "fake"}, client, nil
	}

	rep, err := s.SleepConsolidate(ctx, now+5000)
	if err != nil {
		t.Fatal(err)
	}
	// Only digest [3,4] (1 digest, 2 sources) + drop [6] (1 source) = 1 created, 3 compressed; 0 core drops!
	if rep.DigestsCreated != 1 || rep.EpisodesCompressed != 3 || rep.CoreDropped != 0 {
		t.Fatalf("unexpected concurrent/safety report: %+v", rep)
	}
	// User-edited eps[0] and eps[4] must still hold the user's edited text!
	for _, id := range []string{eps[0].ID, eps[4].ID} {
		got, err := s.Store.GetMemory(ctx, id, u.ID)
		if err != nil || got.Content != editedText {
			t.Fatalf("concurrently edited memory %s was clobbered: %+v (%v)", id, got, err)
		}
	}
	// eps[1] (#2, paired with edited #1) must NOT have been orphaned/archived!
	if _, err := s.Store.GetMemory(ctx, eps[1].ID, u.ID); err != nil {
		t.Fatalf("episode paired with concurrently edited sibling should stay active: %v", err)
	}
	// All 28 core preferences must remain active despite runaway LLM drop response!
	after, _ := s.Store.ListMemories(ctx, u.ID)
	prefCount := 0
	for _, m := range after {
		if m.Category == "preference" {
			prefCount++
		}
	}
	if prefCount != 28 {
		t.Fatalf("runaway LLM drop was not blocked: expected 28 preferences, got %d", prefCount)
	}
}

