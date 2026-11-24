package chat

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/store"
)

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

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		if allDay {
			lt := t.In(loc)
			return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc).UnixMilli(), true
		}
		return t.UnixMilli(), true
	}

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
		targetEnd = target.StartAt + 45*60_000
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
