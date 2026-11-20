package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ScheduleStatusActive    = "active"
	ScheduleStatusCompleted = "completed"
	ScheduleStatusCancelled = "cancelled"

	RecurrenceNone     = "none"
	RecurrenceDaily    = "daily"
	RecurrenceWeekdays = "weekdays"
	RecurrenceWeekly   = "weekly"
	RecurrenceMonthly  = "monthly"
	RecurrenceYearly   = "yearly"

	maxCompletedDates = 120
)

var shanghaiLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// ShanghaiLocation returns the Asia/Shanghai timezone used for schedule dates.
func ShanghaiLocation() *time.Location { return shanghaiLoc }

type Schedule struct {
	ID              string   `json:"id"`
	UserID          string   `json:"-"`
	Title           string   `json:"title"`
	Note            string   `json:"note"`
	Location        string   `json:"location"`
	StartAt         int64    `json:"start_at"`
	EndAt           int64    `json:"end_at"`
	AllDay          bool     `json:"all_day"`
	RemindBeforeMin int      `json:"remind_before_min"` // -1 = no reminder, 0 = on time, >0 = minutes before
	Recurrence      string   `json:"recurrence"`        // none | daily | weekdays | weekly | monthly | yearly
	RecurrenceDays  string   `json:"recurrence_days"`   // comma-separated ISO weekdays 1..7 (1=Mon..7=Sun) for weekly
	RecurrenceUntil int64    `json:"recurrence_until"`  // 0 = forever
	CompletedDates  []string `json:"completed_dates"`   // YYYY-MM-DD completed occurrences for recurring schedules
	Status          string   `json:"status"`            // active | completed | cancelled
	CompletedAt     int64    `json:"completed_at"`
	Source          string   `json:"source"` // ai | manual
	SourceConvID    *string  `json:"source_conv_id,omitempty"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

func (s *Schedule) IsRecurring() bool {
	return s.Recurrence != "" && s.Recurrence != RecurrenceNone
}

type SchedulePatch struct {
	Title           *string
	Note            *string
	Location        *string
	StartAt         *int64
	EndAt           *int64
	AllDay          *bool
	RemindBeforeMin *int
	Recurrence      *string
	RecurrenceDays  *string
	RecurrenceUntil *int64
	Status          *string
}

type ScheduleOccurrence struct {
	Schedule       *Schedule `json:"schedule"`
	OccurrenceDate string    `json:"occurrence_date"` // YYYY-MM-DD
	StartAt        int64     `json:"start_at"`
	EndAt          int64     `json:"end_at"`
	FireAt         int64     `json:"fire_at"` // 0 when RemindBeforeMin < 0
	Completed      bool      `json:"completed"`
}

// ScheduleReminder is a concrete local notification trigger synced to the client device.
type ScheduleReminder struct {
	ID             string `json:"id"`
	ScheduleID     string `json:"schedule_id"`
	OccurrenceDate string `json:"occurrence_date"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	StartAt        int64  `json:"start_at"`
	FireAt         int64  `json:"fire_at"`
	AllDay         bool   `json:"all_day"`
}

func ValidRecurrence(r string) bool {
	switch r {
	case "", RecurrenceNone, RecurrenceDaily, RecurrenceWeekdays, RecurrenceWeekly, RecurrenceMonthly, RecurrenceYearly:
		return true
	}
	return false
}

// NormalizeRecurrenceDays parses weekday tokens ("1,3,5", "周一,周三", "mon,fri") into sorted "1,3,5" (1=Mon..7=Sun).
func NormalizeRecurrenceDays(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"，", ",", "、", ",", ";", ",", "；", ",", " ", ",", "/", ",",
		"星期一", "1", "星期二", "2", "星期三", "3", "星期四", "4", "星期五", "5", "星期六", "6", "星期日", "7", "星期天", "7",
		"周一", "1", "周二", "2", "周三", "3", "周四", "4", "周五", "5", "周六", "6", "周日", "7", "周天", "7",
		"礼拜一", "1", "礼拜二", "2", "礼拜三", "3", "礼拜四", "4", "礼拜五", "5", "礼拜六", "6", "礼拜日", "7", "礼拜天", "7",
	)
	norm := strings.ToLower(replacer.Replace(raw))
	seen := [8]bool{}
	for _, tok := range strings.Split(norm, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		d := 0
		switch tok {
		case "1", "mon", "monday", "一":
			d = 1
		case "2", "tue", "tues", "tuesday", "二":
			d = 2
		case "3", "wed", "wednesday", "三":
			d = 3
		case "4", "thu", "thur", "thurs", "thursday", "四":
			d = 4
		case "5", "fri", "friday", "五":
			d = 5
		case "6", "sat", "saturday", "六":
			d = 6
		case "7", "0", "sun", "sunday", "日", "天":
			d = 7
		}
		if d >= 1 && d <= 7 {
			seen[d] = true
		}
	}
	var parts []string
	for d := 1; d <= 7; d++ {
		if seen[d] {
			parts = append(parts, strconv.Itoa(d))
		}
	}
	return strings.Join(parts, ",")
}

func parseWeekdaysMask(days string, fallback time.Weekday) [8]bool {
	var mask [8]bool
	norm := NormalizeRecurrenceDays(days)
	if norm != "" {
		for _, p := range strings.Split(norm, ",") {
			if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 7 {
				mask[n] = true
			}
		}
	}
	hasAny := false
	for d := 1; d <= 7; d++ {
		if mask[d] {
			hasAny = true
			break
		}
	}
	if !hasAny {
		mask[isoWeekday(fallback)] = true
	}
	return mask
}

func isoWeekday(w time.Weekday) int {
	if w == time.Sunday {
		return 7
	}
	return int(w)
}

func normalizeCompletedDates(dates []string) []string {
	if len(dates) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		d = strings.TrimSpace(d)
		if len(d) == 10 && d[4] == '-' && d[7] == '-' && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	if len(out) > maxCompletedDates {
		out = out[len(out)-maxCompletedDates:]
	}
	return out
}

func normalizeSchedule(s *Schedule) error {
	s.Title = strings.TrimSpace(s.Title)
	if n := utf8.RuneCountInString(s.Title); n < 1 || n > 120 {
		return fmt.Errorf("日程标题需要 1–120 个字")
	}
	s.Note = strings.TrimSpace(s.Note)
	if utf8.RuneCountInString(s.Note) > 500 {
		s.Note = string([]rune(s.Note)[:500])
	}
	s.Location = strings.TrimSpace(s.Location)
	if utf8.RuneCountInString(s.Location) > 120 {
		s.Location = string([]rune(s.Location)[:120])
	}
	if s.StartAt <= 0 {
		return fmt.Errorf("日程开始时间不正确")
	}
	if s.AllDay {
		t := time.UnixMilli(s.StartAt).In(shanghaiLoc)
		s.StartAt = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, shanghaiLoc).UnixMilli()
		s.EndAt = 0
	} else if s.EndAt > 0 && s.EndAt < s.StartAt {
		s.EndAt = 0
	}
	if s.RemindBeforeMin < -1 {
		s.RemindBeforeMin = -1
	} else if s.RemindBeforeMin > 10080 {
		s.RemindBeforeMin = 10080
	}
	if s.Recurrence == "" {
		s.Recurrence = RecurrenceNone
	}
	if !ValidRecurrence(s.Recurrence) {
		return fmt.Errorf("不支持的重复规则：%s", s.Recurrence)
	}
	if s.Recurrence == RecurrenceWeekly {
		s.RecurrenceDays = NormalizeRecurrenceDays(s.RecurrenceDays)
		if s.RecurrenceDays == "" {
			t := time.UnixMilli(s.StartAt).In(shanghaiLoc)
			s.RecurrenceDays = strconv.Itoa(isoWeekday(t.Weekday()))
		}
	} else {
		s.RecurrenceDays = ""
	}
	if s.Recurrence == RecurrenceNone {
		s.RecurrenceUntil = 0
	} else if s.RecurrenceUntil > 0 && s.RecurrenceUntil < s.StartAt {
		s.RecurrenceUntil = 0
	}
	s.CompletedDates = normalizeCompletedDates(s.CompletedDates)
	if s.Status == "" {
		s.Status = ScheduleStatusActive
	}
	if s.Source == "" {
		s.Source = "ai"
	}
	return nil
}

const scheduleCols = `id, user_id, title, note, location, start_at, end_at, all_day, remind_before_min, recurrence, recurrence_days, recurrence_until, completed_dates, status, completed_at, source, source_conv_id, created_at, updated_at`

func scanSchedule(row interface{ Scan(...any) error }) (*Schedule, error) {
	var s Schedule
	var completedRaw string
	err := row.Scan(
		&s.ID, &s.UserID, &s.Title, &s.Note, &s.Location,
		&s.StartAt, &s.EndAt, &s.AllDay, &s.RemindBeforeMin,
		&s.Recurrence, &s.RecurrenceDays, &s.RecurrenceUntil,
		&completedRaw, &s.Status, &s.CompletedAt,
		&s.Source, &s.SourceConvID, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, notFound(err)
	}
	if completedRaw != "" {
		_ = json.Unmarshal([]byte(completedRaw), &s.CompletedDates)
	}
	if s.CompletedDates == nil {
		s.CompletedDates = []string{}
	}
	return &s, nil
}

func readSchedules(rows *sql.Rows, err error) ([]*Schedule, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *Store) CreateSchedule(ctx context.Context, item *Schedule) error {
	if err := normalizeSchedule(item); err != nil {
		return err
	}
	if item.ID == "" {
		item.ID = NewID()
	}
	now := Now()
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	datesJSON, _ := json.Marshal(item.CompletedDates)
	_, err := s.exec(ctx,
		`INSERT INTO schedules (`+scheduleCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.UserID, item.Title, item.Note, item.Location,
		item.StartAt, item.EndAt, item.AllDay, item.RemindBeforeMin,
		item.Recurrence, item.RecurrenceDays, item.RecurrenceUntil,
		string(datesJSON), item.Status, item.CompletedAt,
		item.Source, item.SourceConvID, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (s *Store) GetSchedule(ctx context.Context, id, userID string) (*Schedule, error) {
	return scanSchedule(s.queryRow(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE id=? AND user_id=?`, id, userID))
}

// ListSchedules returns a user's schedules ordered by start_at ascending.
// Cancelled schedules are excluded; completed one-time schedules are included when includeCompleted is true.
func (s *Store) ListSchedules(ctx context.Context, userID string, includeCompleted bool) ([]*Schedule, error) {
	if includeCompleted {
		return readSchedules(s.query(ctx,
			`SELECT `+scheduleCols+` FROM schedules WHERE user_id=? AND status IN ('active','completed') ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, start_at ASC, created_at ASC`,
			userID,
		))
	}
	return readSchedules(s.query(ctx,
		`SELECT `+scheduleCols+` FROM schedules WHERE user_id=? AND status='active' ORDER BY start_at ASC, created_at ASC`,
		userID,
	))
}

func (s *Store) UpdateSchedule(ctx context.Context, id, userID string, patch SchedulePatch) (*Schedule, error) {
	var updated *Schedule
	err := s.tx(ctx, func(tx *Tx) error {
		cur, err := scanSchedule(tx.queryRow(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE id=? AND user_id=?`+s.forUpdate(), id, userID))
		if err != nil {
			return err
		}
		if patch.Title != nil {
			cur.Title = *patch.Title
		}
		if patch.Note != nil {
			cur.Note = *patch.Note
		}
		if patch.Location != nil {
			cur.Location = *patch.Location
		}
		if patch.StartAt != nil {
			cur.StartAt = *patch.StartAt
		}
		if patch.EndAt != nil {
			cur.EndAt = *patch.EndAt
		}
		if patch.AllDay != nil {
			cur.AllDay = *patch.AllDay
		}
		if patch.RemindBeforeMin != nil {
			cur.RemindBeforeMin = *patch.RemindBeforeMin
		}
		if patch.Recurrence != nil {
			cur.Recurrence = *patch.Recurrence
		}
		if patch.RecurrenceDays != nil {
			cur.RecurrenceDays = *patch.RecurrenceDays
		}
		if patch.RecurrenceUntil != nil {
			cur.RecurrenceUntil = *patch.RecurrenceUntil
		}
		if patch.Status != nil {
			switch *patch.Status {
			case ScheduleStatusActive, ScheduleStatusCompleted, ScheduleStatusCancelled:
				cur.Status = *patch.Status
				if cur.Status == ScheduleStatusCompleted {
					cur.CompletedAt = Now()
				} else if cur.Status == ScheduleStatusActive {
					cur.CompletedAt = 0
				}
			default:
				return fmt.Errorf("不支持的状态：%s", *patch.Status)
			}
		}
		if err := normalizeSchedule(cur); err != nil {
			return err
		}
		cur.UpdatedAt = Now()
		datesJSON, _ := json.Marshal(cur.CompletedDates)
		_, err = tx.exec(ctx,
			`UPDATE schedules SET title=?, note=?, location=?, start_at=?, end_at=?, all_day=?, remind_before_min=?, recurrence=?, recurrence_days=?, recurrence_until=?, completed_dates=?, status=?, completed_at=?, updated_at=? WHERE id=? AND user_id=?`,
			cur.Title, cur.Note, cur.Location, cur.StartAt, cur.EndAt, cur.AllDay, cur.RemindBeforeMin,
			cur.Recurrence, cur.RecurrenceDays, cur.RecurrenceUntil,
			string(datesJSON), cur.Status, cur.CompletedAt, cur.UpdatedAt,
			id, userID,
		)
		updated = cur
		return err
	})
	return updated, err
}

// SetScheduleCompletion marks or unmarks a schedule completed.
// For recurring schedules, it marks only the given occurrenceDate (YYYY-MM-DD, defaults to today in Shanghai)
// so future occurrences continue as scheduled.
func (s *Store) SetScheduleCompletion(ctx context.Context, id, userID string, completed bool, occurrenceDate string) (*Schedule, error) {
	var updated *Schedule
	err := s.tx(ctx, func(tx *Tx) error {
		cur, err := scanSchedule(tx.queryRow(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE id=? AND user_id=?`+s.forUpdate(), id, userID))
		if err != nil {
			return err
		}
		now := Now()
		if cur.IsRecurring() {
			d := strings.TrimSpace(occurrenceDate)
			if d == "" {
				d = time.UnixMilli(now).In(shanghaiLoc).Format("2006-01-02")
			}
			if len(d) != 10 || d[4] != '-' || d[7] != '-' {
				return fmt.Errorf("日期格式应为 YYYY-MM-DD")
			}
			var next []string
			for _, existing := range cur.CompletedDates {
				if existing != d {
					next = append(next, existing)
				}
			}
			if completed {
				next = append(next, d)
			}
			cur.CompletedDates = normalizeCompletedDates(next)
		} else {
			if completed {
				cur.Status = ScheduleStatusCompleted
				cur.CompletedAt = now
			} else {
				cur.Status = ScheduleStatusActive
				cur.CompletedAt = 0
			}
		}
		cur.UpdatedAt = now
		datesJSON, _ := json.Marshal(cur.CompletedDates)
		_, err = tx.exec(ctx,
			`UPDATE schedules SET completed_dates=?, status=?, completed_at=?, updated_at=? WHERE id=? AND user_id=?`,
			string(datesJSON), cur.Status, cur.CompletedAt, cur.UpdatedAt, id, userID,
		)
		updated = cur
		return err
	})
	return updated, err
}

func (s *Store) DeleteSchedule(ctx context.Context, id, userID string) error {
	return affected(s.exec(ctx, `DELETE FROM schedules WHERE id=? AND user_id=?`, id, userID))
}

func (s *Store) ClearCompletedSchedules(ctx context.Context, userID string) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM schedules WHERE user_id=? AND status='completed'`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- Recurrence & Reminder Expansion ----

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func computeFireAt(occStart int64, allDay bool, remindBeforeMin int, loc *time.Location) int64 {
	if remindBeforeMin < 0 {
		return 0
	}
	base := occStart
	if allDay {
		t := time.UnixMilli(occStart).In(loc)
		base = time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, loc).UnixMilli()
	}
	return base - int64(remindBeforeMin)*60_000
}

// ExpandOccurrences expands one-time and recurring schedules into concrete occurrences overlapping [fromMs, toMs].
func ExpandOccurrences(schedules []*Schedule, fromMs, toMs int64, loc *time.Location) []ScheduleOccurrence {
	if loc == nil {
		loc = shanghaiLoc
	}
	if toMs < fromMs {
		return nil
	}
	var out []ScheduleOccurrence
	fromTime := time.UnixMilli(fromMs).In(loc)
	toTime := time.UnixMilli(toMs).In(loc)
	startCursorDay := time.Date(fromTime.Year(), fromTime.Month(), fromTime.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	endCursorDay := time.Date(toTime.Year(), toTime.Month(), toTime.Day(), 0, 0, 0, 0, loc)

	for _, s := range schedules {
		if s == nil || s.Status == ScheduleStatusCancelled {
			continue
		}
		if !s.IsRecurring() {
			occEnd := s.EndAt
			if s.AllDay {
				t := time.UnixMilli(s.StartAt).In(loc)
				occEnd = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, int(999*time.Millisecond), loc).UnixMilli()
			} else if occEnd <= s.StartAt {
				occEnd = s.StartAt
			}
			if occEnd < fromMs || s.StartAt > toMs {
				continue
			}
			occDate := time.UnixMilli(s.StartAt).In(loc).Format("2006-01-02")
			out = append(out, ScheduleOccurrence{
				Schedule:       s,
				OccurrenceDate: occDate,
				StartAt:        s.StartAt,
				EndAt:          s.EndAt,
				FireAt:         computeFireAt(s.StartAt, s.AllDay, s.RemindBeforeMin, loc),
				Completed:      s.Status == ScheduleStatusCompleted,
			})
			continue
		}

		if s.Status != ScheduleStatusActive {
			continue
		}
		anchor := time.UnixMilli(s.StartAt).In(loc)
		anchorDay := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, loc)
		hour, minute, sec := anchor.Hour(), anchor.Minute(), anchor.Second()
		if s.AllDay {
			hour, minute, sec = 0, 0, 0
		}
		dur := int64(0)
		if !s.AllDay && s.EndAt > s.StartAt {
			dur = s.EndAt - s.StartAt
		}
		completedSet := make(map[string]bool, len(s.CompletedDates))
		for _, d := range s.CompletedDates {
			completedSet[d] = true
		}

		emitOnDay := func(y int, m time.Month, d int) {
			occTime := time.Date(y, m, d, hour, minute, sec, 0, loc)
			occStart := occTime.UnixMilli()
			if occStart < s.StartAt {
				// Allow same-day recurring match when start_at is exact, otherwise never before anchor.
				return
			}
			if s.RecurrenceUntil > 0 && occStart > s.RecurrenceUntil {
				return
			}
			occEnd := occStart + dur
			checkEnd := occEnd
			if s.AllDay {
				checkEnd = time.Date(y, m, d, 23, 59, 59, int(999*time.Millisecond), loc).UnixMilli()
			}
			if checkEnd < fromMs || occStart > toMs {
				return
			}
			var endField int64
			if dur > 0 {
				endField = occEnd
			}
			dateKey := occTime.Format("2006-01-02")
			out = append(out, ScheduleOccurrence{
				Schedule:       s,
				OccurrenceDate: dateKey,
				StartAt:        occStart,
				EndAt:          endField,
				FireAt:         computeFireAt(occStart, s.AllDay, s.RemindBeforeMin, loc),
				Completed:      completedSet[dateKey],
			})
		}

		switch s.Recurrence {
		case RecurrenceDaily, RecurrenceWeekdays, RecurrenceWeekly:
			cur := startCursorDay
			if cur.Before(anchorDay) {
				cur = anchorDay
			}
			mask := parseWeekdaysMask(s.RecurrenceDays, anchor.Weekday())
			// Safety cap against huge multi-decade ranges.
			for steps := 0; !cur.After(endCursorDay) && steps < 1500; steps++ {
				iso := isoWeekday(cur.Weekday())
				match := true
				if s.Recurrence == RecurrenceWeekdays {
					match = iso >= 1 && iso <= 5
				} else if s.Recurrence == RecurrenceWeekly {
					match = mask[iso]
				}
				if match {
					emitOnDay(cur.Year(), cur.Month(), cur.Day())
				}
				cur = cur.AddDate(0, 0, 1)
			}

		case RecurrenceMonthly:
			targetDay := anchor.Day()
			curMonth := time.Date(startCursorDay.Year(), startCursorDay.Month(), 1, 0, 0, 0, 0, loc)
			anchorMonth := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
			if curMonth.Before(anchorMonth) {
				curMonth = anchorMonth
			}
			endMonth := time.Date(endCursorDay.Year(), endCursorDay.Month(), 1, 0, 0, 0, 0, loc)
			for steps := 0; !curMonth.After(endMonth) && steps < 240; steps++ {
				d := min(targetDay, daysInMonth(curMonth.Year(), curMonth.Month()))
				emitOnDay(curMonth.Year(), curMonth.Month(), d)
				curMonth = curMonth.AddDate(0, 1, 0)
			}

		case RecurrenceYearly:
			targetMonth := anchor.Month()
			targetDay := anchor.Day()
			startYear := max(startCursorDay.Year(), anchor.Year())
			endYear := endCursorDay.Year()
			for y := startYear; y <= endYear && y-startYear < 50; y++ {
				d := min(targetDay, daysInMonth(y, targetMonth))
				emitOnDay(y, targetMonth, d)
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartAt != out[j].StartAt {
			return out[i].StartAt < out[j].StartAt
		}
		if out[i].Schedule.AllDay != out[j].Schedule.AllDay {
			return out[i].Schedule.AllDay
		}
		return out[i].Schedule.ID < out[j].Schedule.ID
	})
	return out
}

// UpcomingReminders builds up to maxAlarms concrete future local-notification triggers within horizonDays.
func UpcomingReminders(schedules []*Schedule, nowMs int64, horizonDays, maxAlarms int, loc *time.Location) []ScheduleReminder {
	if loc == nil {
		loc = shanghaiLoc
	}
	if horizonDays <= 0 {
		horizonDays = 30
	}
	if maxAlarms <= 0 {
		maxAlarms = 48
	}
	// Look ahead extra 7 days of event starts so 1-day or 7-day advance reminders inside the horizon are included.
	occs := ExpandOccurrences(schedules, nowMs, nowMs+int64(horizonDays+7)*86400_000, loc)
	horizonEnd := nowMs + int64(horizonDays)*86400_000
	var reminders []ScheduleReminder
	for _, occ := range occs {
		if occ.Completed || occ.FireAt <= nowMs || occ.FireAt > horizonEnd {
			continue
		}
		s := occ.Schedule
		if s.Status != ScheduleStatusActive || s.RemindBeforeMin < 0 {
			continue
		}
		startT := time.UnixMilli(occ.StartAt).In(loc)
		var bodyParts []string
		if s.AllDay {
			bodyParts = append(bodyParts, startT.Format("01月02日")+" 全天事项")
		} else {
			bodyParts = append(bodyParts, startT.Format("15:04"))
		}
		if s.RemindBeforeMin > 0 {
			bodyParts = append(bodyParts, FormatRemindBefore(s.RemindBeforeMin))
		}
		if s.Location != "" {
			bodyParts = append(bodyParts, s.Location)
		}
		if s.Note != "" {
			note := s.Note
			if utf8.RuneCountInString(note) > 36 {
				note = string([]rune(note)[:36]) + "…"
			}
			bodyParts = append(bodyParts, note)
		}
		reminders = append(reminders, ScheduleReminder{
			ID:             fmt.Sprintf("sched_%s_%s", s.ID, occ.OccurrenceDate),
			ScheduleID:     s.ID,
			OccurrenceDate: occ.OccurrenceDate,
			Title:          s.Title,
			Body:           strings.Join(bodyParts, " · "),
			StartAt:        occ.StartAt,
			FireAt:         occ.FireAt,
			AllDay:         s.AllDay,
		})
	}
	sort.SliceStable(reminders, func(i, j int) bool {
		if reminders[i].FireAt != reminders[j].FireAt {
			return reminders[i].FireAt < reminders[j].FireAt
		}
		return reminders[i].ID < reminders[j].ID
	})
	if len(reminders) > maxAlarms {
		reminders = reminders[:maxAlarms]
	}
	return reminders
}

// FormatRemindBefore formats remind_before_min into warm Chinese text.
func FormatRemindBefore(mins int) string {
	switch {
	case mins < 0:
		return "不响铃提醒"
	case mins == 0:
		return "准点提醒"
	case mins%1440 == 0:
		return fmt.Sprintf("提前 %d 天提醒", mins/1440)
	case mins%60 == 0:
		return fmt.Sprintf("提前 %d 小时提醒", mins/60)
	default:
		return fmt.Sprintf("提前 %d 分钟提醒", mins)
	}
}

var weekdayShortCN = [...]string{"", "周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// FormatRecurrenceLabel formats a schedule's recurrence rule into natural Chinese.
func FormatRecurrenceLabel(recurrence, days string) string {
	switch recurrence {
	case "", RecurrenceNone:
		return ""
	case RecurrenceDaily:
		return "每天"
	case RecurrenceWeekdays:
		return "每个工作日"
	case RecurrenceWeekly:
		norm := NormalizeRecurrenceDays(days)
		if norm == "" {
			return "每周"
		}
		var names []string
		for _, p := range strings.Split(norm, ",") {
			if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 7 {
				names = append(names, weekdayShortCN[n])
			}
		}
		if len(names) == 0 {
			return "每周"
		}
		return "每" + strings.Join(names, "、")
	case RecurrenceMonthly:
		return "每月"
	case RecurrenceYearly:
		return "每年"
	default:
		return recurrence
	}
}
