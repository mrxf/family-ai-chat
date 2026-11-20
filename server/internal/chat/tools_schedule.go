package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

const maxScheduleCalls = 10

var listSchedulesTool = llm.Tool{
	Name: "list_schedules",
	Description: "Query the user's schedules and reminders within a date range or by keyword, and check for time conflicts. " +
		"Returns concrete occurrences (expanding recurring schedules) with their schedule_id.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from_date": map[string]any{
				"type":        "string",
				"description": "Start date YYYY-MM-DD in Asia/Shanghai (defaults to today).",
			},
			"to_date": map[string]any{
				"type":        "string",
				"description": "End date YYYY-MM-DD inclusive (defaults to 14 days after from_date; max 180 days span).",
			},
			"keyword": map[string]any{
				"type":        "string",
				"description": "Optional keyword filter matching title, location, or note.",
			},
			"include_completed": map[string]any{
				"type":        "boolean",
				"description": "Include completed items in the result (false by default).",
			},
		},
	},
}

var createScheduleTool = llm.Tool{
	Name: "create_schedule",
	Description: "Create a new schedule, todo item, or phone reminder (one-time, all-day, or recurring) for the user.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Concise Chinese title of the event or reminder (1-120 chars), e.g. \"吃降压药\", \"去协和医院复查\".",
			},
			"start_time": map[string]any{
				"type":        "string",
				"description": "\"YYYY-MM-DD HH:MM\" (24-hour, Asia/Shanghai) for timed events, or \"YYYY-MM-DD\" when all_day=true.",
			},
			"end_time": map[string]any{
				"type":        "string",
				"description": "Optional end time \"YYYY-MM-DD HH:MM\" when the event has a duration.",
			},
			"all_day": map[string]any{
				"type":        "boolean",
				"description": "Set true for date-only events without a specific clock time (phone reminder fires at 09:00 unless remind_before_min=-1).",
			},
			"remind_before_min": map[string]any{
				"type":        "integer",
				"description": "Minutes before start_time to ring/notify on the phone: 0 = right on time, >0 = advance minutes (e.g. 15, 30, 60, 1440), -1 = memo only without phone notification.",
			},
			"recurrence": map[string]any{
				"type":        "string",
				"enum":        []string{"none", "daily", "weekdays", "weekly", "monthly", "yearly"},
				"description": "Repeat rule: \"none\" (default), \"daily\", \"weekdays\" (Mon-Fri), \"weekly\", \"monthly\", or \"yearly\".",
			},
			"recurrence_days": map[string]any{
				"type":        "string",
				"description": "Only when recurrence=\"weekly\": comma-separated ISO weekdays \"1\"..\"7\" (1=Mon .. 7=Sun), e.g. \"1,3,5\" for Mon/Wed/Fri.",
			},
			"recurrence_until": map[string]any{
				"type":        "string",
				"description": "Optional end date \"YYYY-MM-DD\" for a recurring schedule (omit if ongoing).",
			},
			"location": map[string]any{
				"type":        "string",
				"description": "Optional place or address, e.g. \"社区医院门诊三楼\".",
			},
			"note": map[string]any{
				"type":        "string",
				"description": "Optional helpful note or preparation reminder, e.g. \"空腹抽血，带医保卡\".",
			},
		},
		"required": []string{"title", "start_time"},
	},
}

var updateScheduleTool = llm.Tool{
	Name: "update_schedule",
	Description: "Modify an existing schedule's time, title, reminder offset, recurrence, location, or note by its schedule_id.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schedule_id": map[string]any{
				"type":        "string",
				"description": "Exact schedule_id from read_skill snapshot or list_schedules.",
			},
			"title":             map[string]any{"type": "string"},
			"start_time":        map[string]any{"type": "string", "description": "\"YYYY-MM-DD HH:MM\" or \"YYYY-MM-DD\"."},
			"end_time":          map[string]any{"type": "string", "description": "\"YYYY-MM-DD HH:MM\", or \"\" to clear."},
			"all_day":           map[string]any{"type": "boolean"},
			"remind_before_min": map[string]any{"type": "integer", "description": "0 = on time, >0 = minutes before, -1 = disable notification."},
			"recurrence":        map[string]any{"type": "string", "enum": []string{"none", "daily", "weekdays", "weekly", "monthly", "yearly"}},
			"recurrence_days":   map[string]any{"type": "string"},
			"recurrence_until":  map[string]any{"type": "string", "description": "\"YYYY-MM-DD\", or \"\" to clear."},
			"location":          map[string]any{"type": "string"},
			"note":              map[string]any{"type": "string"},
		},
		"required": []string{"schedule_id"},
	},
}

var completeScheduleTool = llm.Tool{
	Name: "complete_schedule",
	Description: "Mark a one-time schedule or a specific date's occurrence of a recurring schedule as completed (or undo completion).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schedule_id": map[string]any{
				"type":        "string",
				"description": "Exact schedule_id of the schedule.",
			},
			"completed": map[string]any{
				"type":        "boolean",
				"description": "true (default) to mark done; false to reopen.",
			},
			"occurrence_date": map[string]any{
				"type":        "string",
				"description": "For recurring schedules, the date \"YYYY-MM-DD\" to check off (defaults to today). Tomorrow and future occurrences remain active.",
			},
		},
		"required": []string{"schedule_id"},
	},
}

var deleteScheduleTool = llm.Tool{
	Name: "delete_schedule",
	Description: "Cancel and delete a schedule or recurring reminder permanently by its schedule_id.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schedule_id": map[string]any{
				"type":        "string",
				"description": "Exact schedule_id to delete.",
			},
		},
		"required": []string{"schedule_id"},
	},
}

const scheduleSkillContent = `# 日程与提醒管理指南

你可以为用户记录、查询、调整和完成日程安排。保存后的提醒会自动同步到用户的手机本地通知准点响铃或弹窗，用户也能在小暖「我的日程」页面随时查看和勾选。

## 一、何时记录与分寸把握

1. **用户明确要求设提醒 / 记日程**（如「明天早上8点提醒我吃降压药」「帮我记一下周六下午两点理发」）：
   - 先核对下方动态附带的「近 7 天已有日程快照」（超出 7 天则先调 ` + "`list_schedules`" + `），确认不重复后直接调用 ` + "`create_schedule`" + ` 创建。
2. **用户随口提到明确的个人未来安排**（如「我下周三上午九点要去协和医院复查」「明天下午三点得去接孙子放学」）：
   - 只要日期明确且属于用户自己的待办事项，主动帮他记进日程（先看快照避免重复），并在回复里温暖地顺带说一句：「顺便帮您记进日程啦，到点前手机会提醒您；要是不用提醒跟我说一声就行。」
3. **日期或时间模糊**（如「过两天去趟银行」「下周找时间去体检」）：
   - 不要凭空编造具体哪一天或几点钟！如果日期确定但没说几点（如「周日交物业费」），可设为全天事项（` + "`all_day: true`" + `）；如果连哪一天都不确定，先正常回答问题，再温和问一句需不需要定个具体日子的提醒。
4. **不要误记的情况**：
   - 已经发生过的往事、闲聊别人的事、纯粹打听政策/常识而并非个人行程时，不要创建日程。

## 二、先查后操作与防冲突规范

- **严禁编造 ` + "`schedule_id`" + `**：修改、打卡完成或删除日程前，若下方 7 天快照里没有目标日程的 ` + "`id`" + `，必须先调用 ` + "`list_schedules`" + ` 查出真实 ` + "`id`" + `。
- **时间冲突提醒**：创建或改期时，若发现同一时段前后已有其他安排（工具返回也会提示冲突），要把冲突清楚告诉用户（例如「记好啦，不过那天上午 9:00 您还有『接孙子』，两边时间挨得很近，要不要错开一点？」）。

## 三、时间换算与提前提醒经验

- **严格对照下方「近 14 天日历对照」换算相对日期**：注意区分中文口语里的「这周X」（本周）与「下周X」（下一自然周）。
- **提前提醒（` + "`remind_before_min`" + `）默认推荐值**（用户未特别指定时）：
  - 吃药、测血压、喝水、起床、关火等即时动作 → ` + "`0`" + `（准点提醒）；
  - 去医院看病、出门办事、开会、接人、赶车等需提前准备或路途时间的事项 → ` + "`30`" + ` 或 ` + "`60`" + `（提前半小时或 1 小时提醒）；
  - 全天事项（` + "`all_day: true`" + `，如生日、缴费日） → ` + "`0`" + `（手机默认在当天上午 09:00 温馨提醒）或 ` + "`1440`" + `（提前 1 天上午 09:00 提醒）；
  - 用户明确说「记一下就行，不用响铃打扰」 → ` + "`-1`" + `（仅记录不发通知）。

## 四、周期重复与「打卡 vs 删除」区分

- **重复规则（` + "`recurrence`" + `）**：
  - ` + "`none`" + `（单次）、` + "`daily`" + `（每天）、` + "`weekdays`" + `（周一至周五工作日）、` + "`weekly`" + `（每周，` + "`recurrence_days`" + ` 填 ` + "`\"1,3,5\"`" + ` 代表周一/三/五，` + "`1`" + `=周一 … ` + "`7`" + `=周日）、` + "`monthly`" + `（每月同日）、` + "`yearly`" + `（每年同月同日）。
- **今天做完了 vs 彻底取消**：
  - 对于周期日程（如每天吃药），用户说「今天药吃过了 / 今天的做完了」时，调用 ` + "`complete_schedule`" + `（只勾选今天，明天的提醒照常生效），**千万不要误调 ` + "`delete_schedule`" + `**！
  - 只有用户说「这个提醒以后都不用了 / 删掉这个日程」时，才调用 ` + "`delete_schedule`" + `。

## 五、向家人确认的表达风格

- 操作完成后，用自然亲切的一两句话说清：**哪一天（带星期几）、几点、什么事、什么时候提醒**，不要输出内部 ` + "`schedule_id`" + ` 或 JSON 字段名。`

const scheduleLeaseSummary = "日程管理技能（schedule）当前处于激活期，可直接调用 list_schedules / create_schedule / update_schedule / complete_schedule / delete_schedule；换算日期前留意周几，修改或删除须用真实 schedule_id，周期事项单日打卡用 complete_schedule 而非 delete_schedule；需要完整日历对照或详细规则时可再次调用 read_skill。"

func (s *Service) buildScheduleSkill() skills.Definition {
	return skills.Definition{
		Key:          "schedule",
		Title:        "日程与提醒管理",
		Description:  "为用户记录、查询、修改、完成或取消个人日程、待办事项与手机准点/周期提醒（吃药、复查、办事、接送、生日缴费等）。",
		WhenToUse:    "当用户要求定闹钟/设提醒/记日程、询问某天有什么安排、在聊天中说出自己接下来明确的日程计划、或需要改期/打卡完成/取消已有安排时使用。",
		LeaseSummary: scheduleLeaseSummary,
		ToolHints: []skills.ToolHint{
			{Name: listSchedulesTool.Name, Brief: "按日期范围或关键词查日程、检查冲突"},
			{Name: createScheduleTool.Name, Brief: "新建单次/全天或每天/每周/每月/每年提醒"},
			{Name: updateScheduleTool.Name, Brief: "修改日程时间、标题、提前提醒或重复规则"},
			{Name: completeScheduleTool.Name, Brief: "标记单次事项或某天周期事项已完成"},
			{Name: deleteScheduleTool.Name, Brief: "永久取消或删除日程"},
		},
		Tools: []llm.Tool{
			listSchedulesTool,
			createScheduleTool,
			updateScheduleTool,
			completeScheduleTool,
			deleteScheduleTool,
		},
		Content: scheduleSkillContent,
		Dynamic: func(sctx skills.Context) string {
			return s.renderScheduleSkillSnapshot(sctx)
		},
		Available: func(sctx skills.Context) bool {
			return !sctx.Private && sctx.UserID != ""
		},
	}
}

func (s *Service) renderScheduleSkillSnapshot(sctx skills.Context) string {
	now := sctx.Now.In(shanghai)
	var b strings.Builder
	b.WriteString("## 当前时间基准与近 14 天日历对照\n")
	fmt.Fprintf(&b, "- 当前精准时间：%s（%s）\n", now.Format("2006-01-02 15:04"), weekdays[now.Weekday()])

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai)
	var calParts []string
	for i := 0; i < 14; i++ {
		d := today.AddDate(0, 0, i)
		prefix := ""
		switch i {
		case 0:
			prefix = "今天 "
		case 1:
			prefix = "明天 "
		case 2:
			prefix = "后天 "
		}
		calParts = append(calParts, fmt.Sprintf("%s%s(%s)", prefix, d.Format("2006-01-02"), weekdayShort(d.Weekday())))
	}
	fmt.Fprintf(&b, "- 近 14 天日期对照：%s\n", strings.Join(calParts, "、"))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	items, err := s.Store.ListSchedules(ctx, sctx.UserID, false)
	if err != nil {
		return strings.TrimSpace(b.String())
	}
	end7 := today.AddDate(0, 0, 7).Add(24*time.Hour - time.Millisecond)
	occs := store.ExpandOccurrences(items, today.UnixMilli(), end7.UnixMilli(), shanghai)
	b.WriteString("\n## 用户未来 7 天已有日程快照（可直接引用 id，无需重复查询这 7 天）\n")
	if len(occs) == 0 {
		b.WriteString("- 未来 7 天暂无任何待办日程。")
		return b.String()
	}
	const maxSnap = 20
	for i, occ := range occs {
		if i >= maxSnap {
			fmt.Fprintf(&b, "- …其余 %d 项可调用 list_schedules 查看\n", len(occs)-maxSnap)
			break
		}
		b.WriteString("- " + formatOccurrenceForLLM(occ) + "\n")
	}
	return strings.TrimSpace(b.String())
}

// UpcomingScheduleBrief returns a concise one-line summary of today's and tomorrow's
// uncompleted schedules for the per-turn user message note (empty if none or private).
func (s *Service) UpcomingScheduleBrief(ctx context.Context, userID string, now time.Time) string {
	if userID == "" || s.Store == nil {
		return ""
	}
	items, err := s.Store.ListSchedules(ctx, userID, false)
	if err != nil || len(items) == 0 {
		return ""
	}
	now = now.In(shanghai)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai)
	tomorrowEnd := todayStart.AddDate(0, 0, 2).Add(-time.Millisecond)
	occs := store.ExpandOccurrences(items, todayStart.UnixMilli(), tomorrowEnd.UnixMilli(), shanghai)
	if len(occs) == 0 {
		return ""
	}
	todayStr := todayStart.Format("2006-01-02")
	tomorrowStr := todayStart.AddDate(0, 0, 1).Format("2006-01-02")

	var parts []string
	for _, occ := range occs {
		if occ.Completed {
			continue
		}
		sc := occ.Schedule
		dayLabel := occ.OccurrenceDate
		if occ.OccurrenceDate == todayStr {
			dayLabel = "今天"
		} else if occ.OccurrenceDate == tomorrowStr {
			dayLabel = "明天"
		}
		timeStr := "全天"
		if !sc.AllDay {
			timeStr = time.UnixMilli(occ.StartAt).In(shanghai).Format("15:04")
		}
		entry := fmt.Sprintf("%s %s %s", dayLabel, timeStr, sc.Title)
		if sc.Location != "" {
			entry += "（" + clip(sc.Location, 16) + "）"
		}
		if sc.Note != "" {
			entry += "[" + clip(sc.Note, 20) + "]"
		}
		parts = append(parts, entry)
		if len(parts) >= 6 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "用户今明两天的待办日程：" + strings.Join(parts, "；") + "（仅在用户问起安排，或当前话题如饮食空腹/出行/时间冲突确实相关时自然提醒；无关闲聊不要生硬复述；如需增删改查更多日程请先调用 read_skill 加载 schedule）。"
}

// ---- Schedule Tool Executors ----

func (s *Service) execScheduleTool(ctx context.Context, t *toolTurn, name, rawArgs string) (string, bool) {
	switch name {
	case listSchedulesTool.Name:
		return s.execListSchedules(ctx, t, rawArgs), true
	case createScheduleTool.Name:
		return s.execCreateSchedule(ctx, t, rawArgs), true
	case updateScheduleTool.Name:
		return s.execUpdateSchedule(ctx, t, rawArgs), true
	case completeScheduleTool.Name:
		return s.execCompleteSchedule(ctx, t, rawArgs), true
	case deleteScheduleTool.Name:
		return s.execDeleteSchedule(ctx, t, rawArgs), true
	default:
		return "", false
	}
}

func (s *Service) checkScheduleGuard(t *toolTurn) string {
	if t.private {
		return "当前处于隐私对话模式，为保护隐私不会读取或修改用户的个人日程。请温和说明退出隐私对话后即可帮他设置或查看日程。"
	}
	if t.scheduleCalls >= maxScheduleCalls {
		return "本轮对话操作日程的次数已达上限，请直接根据已有结果向用户回复。"
	}
	t.scheduleCalls++
	return ""
}

func (s *Service) execListSchedules(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkScheduleGuard(t); msg != "" {
		return msg
	}
	var args struct {
		FromDate         string `json:"from_date"`
		ToDate           string `json:"to_date"`
		Date             string `json:"date"`
		Keyword          string `json:"keyword"`
		IncludeCompleted bool   `json:"include_completed"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)

	now := time.Now().In(shanghai)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai)
	fromDay := today
	if f := firstNonEmpty(args.FromDate, args.Date); f != "" {
		if parsed, ok := parseFlexibleScheduleTime(f, true, now); ok {
			pt := time.UnixMilli(parsed).In(shanghai)
			fromDay = time.Date(pt.Year(), pt.Month(), pt.Day(), 0, 0, 0, 0, shanghai)
		}
	}
	toDay := fromDay.AddDate(0, 0, 14)
	if args.Date != "" && args.ToDate == "" {
		toDay = fromDay
	} else if args.ToDate != "" {
		if parsed, ok := parseFlexibleScheduleTime(args.ToDate, true, now); ok {
			pt := time.UnixMilli(parsed).In(shanghai)
			toDay = time.Date(pt.Year(), pt.Month(), pt.Day(), 0, 0, 0, 0, shanghai)
		}
	}
	if toDay.Before(fromDay) {
		toDay = fromDay
	}
	if toDay.Sub(fromDay) > 180*24*time.Hour {
		toDay = fromDay.AddDate(0, 0, 180)
	}
	toEnd := time.Date(toDay.Year(), toDay.Month(), toDay.Day(), 23, 59, 59, int(999*time.Millisecond), shanghai)

	rangeLabel := fromDay.Format("01-02") + " 至 " + toDay.Format("01-02")
	if fromDay.Equal(toDay) {
		rangeLabel = fromDay.Format("01月02日")
	}
	stepIdx := t.run.startStep("schedule", "查看日程："+rangeLabel, "")
	t.run.setActivity("正在查看日程安排…")
	t.postActivity = "正在结合日程安排回复…"

	items, err := s.Store.ListSchedules(ctx, t.user.ID, args.IncludeCompleted)
	if err != nil {
		t.run.finishStep(stepIdx, "error", "查看日程失败", "")
		return "查询日程出错，请稍后再试。"
	}
	occs := store.ExpandOccurrences(items, fromDay.UnixMilli(), toEnd.UnixMilli(), shanghai)
	kw := strings.ToLower(strings.TrimSpace(args.Keyword))
	var filtered []store.ScheduleOccurrence
	for _, occ := range occs {
		if !args.IncludeCompleted && occ.Completed {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(occ.Schedule.Title + " " + occ.Schedule.Location + " " + occ.Schedule.Note)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		filtered = append(filtered, occ)
	}
	t.run.finishStep(stepIdx, "done", "已查看日程："+rangeLabel, fmt.Sprintf("找到 %d 项安排", len(filtered)))

	if len(filtered) == 0 {
		return fmt.Sprintf("在 %s 至 %s 期间没有匹配的待办日程。", fromDay.Format("2006-01-02"), toDay.Format("2006-01-02"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "日程查询结果（%s 至 %s，共 %d 项）：\n", fromDay.Format("2006-01-02"), toDay.Format("2006-01-02"), len(filtered))
	for i, occ := range filtered {
		if i >= 40 {
			fmt.Fprintf(&b, "…其余 %d 项已省略\n", len(filtered)-40)
			break
		}
		b.WriteString("- " + formatOccurrenceForLLM(occ) + "\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) execCreateSchedule(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkScheduleGuard(t); msg != "" {
		return msg
	}
	var args struct {
		Title           string `json:"title"`
		StartTime       string `json:"start_time"`
		EndTime         string `json:"end_time"`
		AllDay          bool   `json:"all_day"`
		RemindBeforeMin *int   `json:"remind_before_min"`
		Recurrence      string `json:"recurrence"`
		RecurrenceDays  string `json:"recurrence_days"`
		RecurrenceUntil string `json:"recurrence_until"`
		Location        string `json:"location"`
		Note            string `json:"note"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "参数解析失败：请检查 JSON 格式（需要 title 和 start_time）。"
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		return "参数错误：需要提供日程标题 title。"
	}
	now := time.Now().In(shanghai)
	startStr := strings.TrimSpace(args.StartTime)
	allDay := args.AllDay || isDateOnlyString(startStr)
	startMs, ok := parseFlexibleScheduleTime(startStr, allDay, now)
	if !ok {
		return fmt.Sprintf("时间格式无法解析：%q。请使用 \"YYYY-MM-DD HH:MM\"（24小时制）或全天格式 \"YYYY-MM-DD\"。", args.StartTime)
	}
	var endMs int64
	if strings.TrimSpace(args.EndTime) != "" && !allDay {
		if em, eok := parseFlexibleScheduleTime(args.EndTime, false, now); eok && em > startMs {
			endMs = em
		}
	}
	var untilMs int64
	if strings.TrimSpace(args.RecurrenceUntil) != "" {
		if um, uok := parseFlexibleScheduleTime(args.RecurrenceUntil, true, now); uok {
			ut := time.UnixMilli(um).In(shanghai)
			untilMs = time.Date(ut.Year(), ut.Month(), ut.Day(), 23, 59, 59, 0, shanghai).UnixMilli()
		}
	}
	remindMin := 0
	if args.RemindBeforeMin != nil {
		remindMin = *args.RemindBeforeMin
	}

	rec := strings.ToLower(strings.TrimSpace(args.Recurrence))
	if rec == "" {
		rec = store.RecurrenceNone
	}

	existing, _ := s.Store.ListSchedules(ctx, t.user.ID, false)
	// Idempotency guard against duplicate tool calls in the same or consecutive turns.
	for _, ex := range existing {
		if strings.EqualFold(ex.Title, title) && ex.StartAt == startMs && ex.AllDay == allDay && ex.Recurrence == rec {
			summary := formatScheduleSummaryShort(ex)
			t.run.addDoneStep("schedule", "日程已存在："+clip(ex.Title, 20), summary)
			return fmt.Sprintf("该日程已存在（无需重复创建）：%s（schedule_id=%s）。可直接向用户确认。", formatScheduleDetailForLLM(ex), ex.ID)
		}
	}

	var convIDPtr *string
	if !t.private && t.convID != "" {
		cid := t.convID
		convIDPtr = &cid
	}
	item := &store.Schedule{
		UserID:          t.user.ID,
		Title:           title,
		Note:            args.Note,
		Location:        args.Location,
		StartAt:         startMs,
		EndAt:           endMs,
		AllDay:          allDay,
		RemindBeforeMin: remindMin,
		Recurrence:      rec,
		RecurrenceDays:  args.RecurrenceDays,
		RecurrenceUntil: untilMs,
		Source:          "ai",
		SourceConvID:    convIDPtr,
	}

	stepIdx := t.run.startStep("schedule", "添加日程："+clip(title, 20), "")
	t.run.setActivity("正在保存日程提醒…")
	t.postActivity = "正在确认日程安排…"

	if err := s.Store.CreateSchedule(ctx, item); err != nil {
		t.run.finishStep(stepIdx, "error", "添加日程失败", err.Error())
		return "创建日程失败：" + err.Error()
	}
	summary := formatScheduleSummaryShort(item)
	t.run.finishStep(stepIdx, "done", "已添加日程："+clip(item.Title, 20), summary)

	conflictNote := detectNearbyConflicts(existing, item)
	out := fmt.Sprintf("日程创建成功：%s（schedule_id=%s）。客户端会自动同步手机本地提醒。", formatScheduleDetailForLLM(item), item.ID)
	if conflictNote != "" {
		out += "\n" + conflictNote
	}
	return out
}

func (s *Service) execUpdateSchedule(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkScheduleGuard(t); msg != "" {
		return msg
	}
	var args struct {
		ScheduleID      string  `json:"schedule_id"`
		ID              string  `json:"id"`
		Title           *string `json:"title"`
		StartTime       *string `json:"start_time"`
		EndTime         *string `json:"end_time"`
		AllDay          *bool   `json:"all_day"`
		RemindBeforeMin *int    `json:"remind_before_min"`
		Recurrence      *string `json:"recurrence"`
		RecurrenceDays  *string `json:"recurrence_days"`
		RecurrenceUntil *string `json:"recurrence_until"`
		Location        *string `json:"location"`
		Note            *string `json:"note"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "参数解析失败：请检查 JSON 格式。"
	}
	id := firstNonEmpty(args.ScheduleID, args.ID)
	if id == "" {
		return "参数错误：需要提供真实的 schedule_id。"
	}
	cur, err := s.Store.GetSchedule(ctx, id, t.user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Sprintf("未找到 schedule_id=%q 的日程。请先查看 read_skill 快照或调用 list_schedules 获取真实 id。", id)
		}
		return "读取原日程失败。"
	}

	now := time.Now().In(shanghai)
	patch := store.SchedulePatch{
		Title:           args.Title,
		AllDay:          args.AllDay,
		RemindBeforeMin: args.RemindBeforeMin,
		Recurrence:      args.Recurrence,
		RecurrenceDays:  args.RecurrenceDays,
		Location:        args.Location,
		Note:            args.Note,
	}
	effectiveAllDay := cur.AllDay
	if args.AllDay != nil {
		effectiveAllDay = *args.AllDay
	}
	if args.StartTime != nil && strings.TrimSpace(*args.StartTime) != "" {
		st := strings.TrimSpace(*args.StartTime)
		if args.AllDay == nil && isDateOnlyString(st) {
			ad := true
			patch.AllDay = &ad
			effectiveAllDay = true
		}
		ms, ok := parseFlexibleScheduleTime(st, effectiveAllDay, now)
		if !ok {
			return fmt.Sprintf("start_time 格式无法解析：%q。请用 \"YYYY-MM-DD HH:MM\" 或 \"YYYY-MM-DD\"。", st)
		}
		patch.StartAt = &ms
	}
	if args.EndTime != nil {
		et := strings.TrimSpace(*args.EndTime)
		if et == "" {
			zero := int64(0)
			patch.EndAt = &zero
		} else if ms, ok := parseFlexibleScheduleTime(et, false, now); ok {
			patch.EndAt = &ms
		}
	}
	if args.RecurrenceUntil != nil {
		ru := strings.TrimSpace(*args.RecurrenceUntil)
		if ru == "" {
			zero := int64(0)
			patch.RecurrenceUntil = &zero
		} else if ms, ok := parseFlexibleScheduleTime(ru, true, now); ok {
			ut := time.UnixMilli(ms).In(shanghai)
			endDay := time.Date(ut.Year(), ut.Month(), ut.Day(), 23, 59, 59, 0, shanghai).UnixMilli()
			patch.RecurrenceUntil = &endDay
		}
	}

	stepIdx := t.run.startStep("schedule", "修改日程："+clip(cur.Title, 20), "")
	t.run.setActivity("正在更新日程…")
	t.postActivity = "正在确认更新后的日程…"

	updated, err := s.Store.UpdateSchedule(ctx, id, t.user.ID, patch)
	if err != nil {
		t.run.finishStep(stepIdx, "error", "修改日程失败", err.Error())
		return "修改日程失败：" + err.Error()
	}
	summary := formatScheduleSummaryShort(updated)
	t.run.finishStep(stepIdx, "done", "已修改日程："+clip(updated.Title, 20), summary)

	existing, _ := s.Store.ListSchedules(ctx, t.user.ID, false)
	conflictNote := detectNearbyConflicts(existing, updated)
	out := fmt.Sprintf("日程已更新：%s（schedule_id=%s）。", formatScheduleDetailForLLM(updated), updated.ID)
	if conflictNote != "" {
		out += "\n" + conflictNote
	}
	return out
}

func (s *Service) execCompleteSchedule(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkScheduleGuard(t); msg != "" {
		return msg
	}
	var args struct {
		ScheduleID     string `json:"schedule_id"`
		ID             string `json:"id"`
		Completed      *bool  `json:"completed"`
		OccurrenceDate string `json:"occurrence_date"`
		Date           string `json:"date"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	id := firstNonEmpty(args.ScheduleID, args.ID)
	if id == "" {
		return "参数错误：需要提供 schedule_id。"
	}
	done := true
	if args.Completed != nil {
		done = *args.Completed
	}
	occDate := firstNonEmpty(args.OccurrenceDate, args.Date)
	if occDate != "" {
		if ms, ok := parseFlexibleScheduleTime(occDate, true, time.Now().In(shanghai)); ok {
			occDate = time.UnixMilli(ms).In(shanghai).Format("2006-01-02")
		}
	}

	stepIdx := t.run.startStep("schedule", "更新日程完成状态", "")
	updated, err := s.Store.SetScheduleCompletion(ctx, id, t.user.ID, done, occDate)
	if err != nil {
		t.run.finishStep(stepIdx, "error", "更新日程状态失败", "")
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Sprintf("未找到 schedule_id=%q 的日程，请先用 list_schedules 确认真实 id。", id)
		}
		return "更新日程完成状态失败：" + err.Error()
	}
	verb := "已完成"
	if !done {
		verb = "已重新标记为待办"
	}
	t.run.finishStep(stepIdx, "done", verb+"："+clip(updated.Title, 20), "")
	if updated.IsRecurring() {
		targetDate := occDate
		if targetDate == "" {
			targetDate = time.Now().In(shanghai).Format("2006-01-02")
		}
		return fmt.Sprintf("周期日程「%s」在 %s 的事项%s，后续周期提醒保持正常生效。", updated.Title, targetDate, verb)
	}
	return fmt.Sprintf("日程「%s」%s。", updated.Title, verb)
}

func (s *Service) execDeleteSchedule(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkScheduleGuard(t); msg != "" {
		return msg
	}
	var args struct {
		ScheduleID string `json:"schedule_id"`
		ID         string `json:"id"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	id := firstNonEmpty(args.ScheduleID, args.ID)
	if id == "" {
		return "参数错误：需要提供 schedule_id。"
	}
	cur, err := s.Store.GetSchedule(ctx, id, t.user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Sprintf("未找到 schedule_id=%q 的日程（可能已被删除）。请先确认真实 schedule_id。", id)
		}
		return "查询日程失败。"
	}
	stepIdx := t.run.startStep("schedule", "取消日程："+clip(cur.Title, 20), "")
	if err := s.Store.DeleteSchedule(ctx, id, t.user.ID); err != nil {
		t.run.finishStep(stepIdx, "error", "取消日程失败", "")
		return "删除日程失败：" + err.Error()
	}
	t.run.finishStep(stepIdx, "done", "已取消日程："+clip(cur.Title, 20), "")
	return fmt.Sprintf("已永久删除日程「%s」（原时间：%s）。手机上的对应提醒也会同步取消。", cur.Title, formatScheduleSummaryShort(cur))
}

// ---- Time Parsing & Conflict Formatting ----

func isDateOnlyString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "今天" || s == "明天" || s == "后天" || s == "大后天" {
		return true
	}
	return !strings.ContainsAny(s, ":：") && utf8.RuneCountInString(s) <= 12
}

// parseFlexibleScheduleTime parses common Chinese/ISO date & datetime strings in Asia/Shanghai.
func parseFlexibleScheduleTime(raw string, allDay bool, now time.Time) (int64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	loc := shanghai
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		if allDay {
			lt := t.In(loc)
			return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc).UnixMilli(), true
		}
		return t.UnixMilli(), true
	}

	// Relative prefixes ("今天", "明天", "后天", "大后天")
	relOffsets := []struct {
		prefix string
		days   int
	}{
		{"大后天", 3},
		{"今天", 0},
		{"今日", 0},
		{"明天", 1},
		{"明日", 1},
		{"后天", 2},
	}
	for _, r := range relOffsets {
		if strings.HasPrefix(s, r.prefix) {
			d := today.AddDate(0, 0, r.days)
			rest := strings.TrimSpace(strings.TrimPrefix(s, r.prefix))
			if rest == "" || allDay {
				return d.UnixMilli(), true
			}
			s = d.Format("2006-01-02") + " " + rest
			break
		}
	}

	// Normalize Chinese separators
	norm := strings.NewReplacer(
		"年", "-", "月", "-", "日", " ", "号", " ",
		"/", "-", "：", ":", "T", " ",
	).Replace(s)
	norm = strings.Join(strings.Fields(norm), " ")

	layouts := []string{
		"2006-1-2 15:04:05",
		"2006-1-2 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-1-2",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, norm, loc); err == nil {
			if allDay {
				return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).UnixMilli(), true
			}
			return t.UnixMilli(), true
		}
	}
	// Unix milliseconds string fallback
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 1_000_000_000_000 {
		return ms, true
	}
	return 0, false
}

func detectNearbyConflicts(existing []*store.Schedule, target *store.Schedule) string {
	if target == nil || target.AllDay {
		return ""
	}
	tStart := time.UnixMilli(target.StartAt).In(shanghai)
	dayStart := time.Date(tStart.Year(), tStart.Month(), tStart.Day(), 0, 0, 0, 0, shanghai).UnixMilli()
	dayEnd := time.Date(tStart.Year(), tStart.Month(), tStart.Day(), 23, 59, 59, 0, shanghai).UnixMilli()

	var others []*store.Schedule
	for _, ex := range existing {
		if ex.ID != target.ID && ex.Status == store.ScheduleStatusActive && !ex.AllDay {
			others = append(others, ex)
		}
	}
	if len(others) == 0 {
		return ""
	}
	occs := store.ExpandOccurrences(others, dayStart, dayEnd, shanghai)
	targetEnd := target.EndAt
	if targetEnd <= target.StartAt {
		targetEnd = target.StartAt + 45*60_000 // treat point event as ~45m window for proximity check
	}
	var hits []string
	for _, occ := range occs {
		if occ.Completed {
			continue
		}
		occEnd := occ.EndAt
		if occEnd <= occ.StartAt {
			occEnd = occ.StartAt + 45*60_000
		}
		if occ.StartAt < targetEnd && occEnd > target.StartAt {
			hits = append(hits, fmt.Sprintf("「%s %s」", time.UnixMilli(occ.StartAt).In(shanghai).Format("15:04"), occ.Schedule.Title))
		}
	}
	if len(hits) == 0 {
		return ""
	}
	return "⚠️ 时间相近提醒：同一时段前后已有安排 " + strings.Join(hits, "、") + "，请顺带提醒用户留意时间是否冲突。"
}

func weekdayShort(w time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[w]
}

func formatScheduleSummaryShort(sc *store.Schedule) string {
	t := time.UnixMilli(sc.StartAt).In(shanghai)
	timePart := t.Format("2006-01-02 15:04")
	if sc.AllDay {
		timePart = t.Format("2006-01-02") + " 全天"
	}
	if rec := store.FormatRecurrenceLabel(sc.Recurrence, sc.RecurrenceDays); rec != "" {
		if sc.AllDay {
			return rec + " · 全天"
		}
		return rec + " " + t.Format("15:04")
	}
	return timePart
}

func formatScheduleDetailForLLM(sc *store.Schedule) string {
	t := time.UnixMilli(sc.StartAt).In(shanghai)
	var parts []string
	if sc.AllDay {
		parts = append(parts, fmt.Sprintf("%s（%s）全天", t.Format("2006-01-02"), weekdayShort(t.Weekday())))
	} else if sc.EndAt > sc.StartAt {
		et := time.UnixMilli(sc.EndAt).In(shanghai)
		parts = append(parts, fmt.Sprintf("%s（%s）%s-%s", t.Format("2006-01-02"), weekdayShort(t.Weekday()), t.Format("15:04"), et.Format("15:04")))
	} else {
		parts = append(parts, fmt.Sprintf("%s（%s）%s", t.Format("2006-01-02"), weekdayShort(t.Weekday()), t.Format("15:04")))
	}
	if rec := store.FormatRecurrenceLabel(sc.Recurrence, sc.RecurrenceDays); rec != "" {
		parts = append(parts, "重复："+rec)
	}
	parts = append(parts, "标题："+sc.Title)
	parts = append(parts, store.FormatRemindBefore(sc.RemindBeforeMin))
	if sc.Location != "" {
		parts = append(parts, "地点："+sc.Location)
	}
	if sc.Note != "" {
		parts = append(parts, "备注："+sc.Note)
	}
	return strings.Join(parts, " | ")
}

func formatOccurrenceForLLM(occ store.ScheduleOccurrence) string {
	sc := occ.Schedule
	t := time.UnixMilli(occ.StartAt).In(shanghai)
	timePart := fmt.Sprintf("%s(%s) %s", occ.OccurrenceDate, weekdayShort(t.Weekday()), t.Format("15:04"))
	if sc.AllDay {
		timePart = fmt.Sprintf("%s(%s) 全天", occ.OccurrenceDate, weekdayShort(t.Weekday()))
	} else if occ.EndAt > occ.StartAt {
		timePart += "-" + time.UnixMilli(occ.EndAt).In(shanghai).Format("15:04")
	}
	var extras []string
	if rec := store.FormatRecurrenceLabel(sc.Recurrence, sc.RecurrenceDays); rec != "" {
		extras = append(extras, rec)
	}
	extras = append(extras, store.FormatRemindBefore(sc.RemindBeforeMin))
	if sc.Location != "" {
		extras = append(extras, "地点:"+sc.Location)
	}
	if sc.Note != "" {
		extras = append(extras, "备注:"+sc.Note)
	}
	if occ.Completed {
		extras = append(extras, "已完成")
	}
	return fmt.Sprintf("[%s] %s（%s） | schedule_id=%s", timePart, sc.Title, strings.Join(extras, "，"), sc.ID)
}
