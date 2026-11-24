package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	TaskScheduleDaily    = "daily"
	TaskScheduleWeekdays = "weekdays"
	TaskScheduleWeekly   = "weekly"
	TaskScheduleMonthly  = "monthly"
	TaskScheduleInterval = "interval"
	TaskScheduleOnce     = "once"

	TaskRunStatusRunning = "running"
	TaskRunStatusSuccess = "success"
	TaskRunStatusError   = "error"

	MinTaskIntervalMinutes = 15
	MaxTaskIntervalMinutes = 10080 // 7 days
	MaxScheduledTasksUser  = 40
)

// ScheduledTask represents a recurring or one-shot background AI automation task.
type ScheduledTask struct {
	ID                string  `json:"id"`
	UserID            string  `json:"-"`
	Title             string  `json:"title"`
	Prompt            string  `json:"prompt"`
	ScheduleKind      string  `json:"schedule_kind"`    // daily | weekdays | weekly | monthly | interval | once
	TimeOfDay         string  `json:"time_of_day"`      // HH:MM in Asia/Shanghai
	Weekdays          string  `json:"weekdays"`         // comma-separated ISO weekdays 1..7 for weekly
	DayOfMonth        int     `json:"day_of_month"`     // 1..31 for monthly
	IntervalMinutes   int     `json:"interval_minutes"` // >= 15 for interval
	RunAt             int64   `json:"run_at"`           // unix ms for once
	Enabled           bool    `json:"enabled"`
	ModelID           *string `json:"model_id,omitempty"`
	TargetMessageID   string  `json:"target_message_id,omitempty"`
	TargetComponentID string  `json:"target_component_id,omitempty"`
	ConversationID    *string `json:"conversation_id,omitempty"`
	Source            string  `json:"source"` // ai | manual
	NextRunAt         int64   `json:"next_run_at"`
	LastRunAt         int64   `json:"last_run_at"`
	LastStatus        string  `json:"last_status"`
	LastSummary       string  `json:"last_summary"`
	LastError         string  `json:"last_error"`
	RunCount          int     `json:"run_count"`
	CreatedAt         int64   `json:"created_at"`
	UpdatedAt         int64   `json:"updated_at"`
}

// ScheduledTaskPatch holds optional field updates for UpdateScheduledTask.
type ScheduledTaskPatch struct {
	Title             *string
	Prompt            *string
	ScheduleKind      *string
	TimeOfDay         *string
	Weekdays          *string
	DayOfMonth        *int
	IntervalMinutes   *int
	RunAt             *int64
	Enabled           *bool
	ModelID           *string // empty string clears model override
	TargetMessageID   *string
	TargetComponentID *string
}

// ScheduledTaskRun records one execution attempt of a ScheduledTask.
type ScheduledTaskRun struct {
	ID                string `json:"id"`
	TaskID            string `json:"task_id"`
	UserID            string `json:"-"`
	TriggerSource     string `json:"trigger_source"` // schedule | manual
	Status            string `json:"status"`         // running | success | error
	Summary           string `json:"summary"`
	Error             string `json:"error"`
	MessageID         string `json:"message_id,omitempty"`
	ConversationID    string `json:"conversation_id,omitempty"`
	ToolCalls         int    `json:"tool_calls"`
	UpdatedComponents int    `json:"updated_components"`
	StartedAt         int64  `json:"started_at"`
	FinishedAt        int64  `json:"finished_at"`
}

func ValidTaskScheduleKind(k string) bool {
	switch k {
	case TaskScheduleDaily, TaskScheduleWeekdays, TaskScheduleWeekly, TaskScheduleMonthly, TaskScheduleInterval, TaskScheduleOnce:
		return true
	}
	return false
}

// ParseTimeOfDay validates and normalizes "H:MM" / "HH:MM" into ("HH:MM", hour, minute, ok).
func ParseTimeOfDay(raw string) (string, int, int, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "：", ":"))
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return "", 0, 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return "", 0, 0, false
	}
	return fmt.Sprintf("%02d:%02d", h, m), h, m, true
}

// ComputeNextRunAt computes the next scheduled unix ms strictly after afterMs (in loc, default Asia/Shanghai).
// Returns 0 when the task is disabled or a one-shot task has no future trigger.
func ComputeNextRunAt(t *ScheduledTask, afterMs int64, loc *time.Location) int64 {
	if t == nil || !t.Enabled {
		return 0
	}
	if loc == nil {
		loc = shanghaiLoc
	}
	if afterMs <= 0 {
		afterMs = Now()
	}
	now := time.UnixMilli(afterMs).In(loc)

	switch t.ScheduleKind {
	case TaskScheduleOnce:
		if t.RunAt > afterMs {
			return t.RunAt
		}
		return 0

	case TaskScheduleInterval:
		mins := t.IntervalMinutes
		if mins < MinTaskIntervalMinutes {
			mins = MinTaskIntervalMinutes
		}
		return afterMs + int64(mins)*60_000

	case TaskScheduleDaily, TaskScheduleWeekdays, TaskScheduleWeekly:
		_, hh, mm, ok := ParseTimeOfDay(t.TimeOfDay)
		if !ok {
			hh, mm = 8, 0
		}
		mask := parseWeekdaysMask(t.Weekdays, now.Weekday())
		baseDay := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
		for offset := 0; offset < 15; offset++ {
			cand := baseDay.AddDate(0, 0, offset)
			if cand.UnixMilli() <= afterMs {
				continue
			}
			iso := isoWeekday(cand.Weekday())
			switch t.ScheduleKind {
			case TaskScheduleDaily:
				return cand.UnixMilli()
			case TaskScheduleWeekdays:
				if iso >= 1 && iso <= 5 {
					return cand.UnixMilli()
				}
			case TaskScheduleWeekly:
				if mask[iso] {
					return cand.UnixMilli()
				}
			}
		}
		return 0

	case TaskScheduleMonthly:
		_, hh, mm, ok := ParseTimeOfDay(t.TimeOfDay)
		if !ok {
			hh, mm = 8, 0
		}
		dom := t.DayOfMonth
		if dom < 1 {
			dom = 1
		} else if dom > 31 {
			dom = 31
		}
		curMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		for offset := 0; offset < 14; offset++ {
			m := curMonth.AddDate(0, offset, 0)
			d := min(dom, daysInMonth(m.Year(), m.Month()))
			cand := time.Date(m.Year(), m.Month(), d, hh, mm, 0, 0, loc)
			if cand.UnixMilli() > afterMs {
				return cand.UnixMilli()
			}
		}
		return 0
	default:
		return 0
	}
}

// FormatTaskScheduleLabel formats a ScheduledTask's trigger rule in concise Chinese.
func FormatTaskScheduleLabel(t *ScheduledTask) string {
	if t == nil {
		return ""
	}
	switch t.ScheduleKind {
	case TaskScheduleDaily:
		return "每天 " + t.TimeOfDay
	case TaskScheduleWeekdays:
		return "工作日 " + t.TimeOfDay
	case TaskScheduleWeekly:
		lbl := FormatRecurrenceLabel(RecurrenceWeekly, t.Weekdays)
		if lbl == "" {
			lbl = "每周"
		}
		return lbl + " " + t.TimeOfDay
	case TaskScheduleMonthly:
		return fmt.Sprintf("每月 %d 日 %s", t.DayOfMonth, t.TimeOfDay)
	case TaskScheduleInterval:
		if t.IntervalMinutes%60 == 0 {
			return fmt.Sprintf("每 %d 小时", t.IntervalMinutes/60)
		}
		return fmt.Sprintf("每 %d 分钟", t.IntervalMinutes)
	case TaskScheduleOnce:
		if t.RunAt > 0 {
			return "单次 " + time.UnixMilli(t.RunAt).In(shanghaiLoc).Format("01-02 15:04")
		}
		return "单次执行"
	default:
		return t.ScheduleKind
	}
}

func normalizeScheduledTask(t *ScheduledTask, nowMs int64) error {
	t.Title = strings.TrimSpace(t.Title)
	if n := utf8.RuneCountInString(t.Title); n < 1 || n > 80 {
		return fmt.Errorf("任务标题需要 1–80 个字")
	}
	t.Prompt = strings.TrimSpace(t.Prompt)
	if n := utf8.RuneCountInString(t.Prompt); n < 1 || n > 2000 {
		return fmt.Errorf("任务指令需要 1–2000 个字")
	}
	t.ScheduleKind = strings.ToLower(strings.TrimSpace(t.ScheduleKind))
	if t.ScheduleKind == "" {
		t.ScheduleKind = TaskScheduleDaily
	}
	if !ValidTaskScheduleKind(t.ScheduleKind) {
		return fmt.Errorf("不支持的定时规则：%s", t.ScheduleKind)
	}
	switch t.ScheduleKind {
	case TaskScheduleDaily, TaskScheduleWeekdays, TaskScheduleWeekly, TaskScheduleMonthly:
		norm, _, _, ok := ParseTimeOfDay(t.TimeOfDay)
		if !ok {
			return fmt.Errorf("执行时间格式不正确，请使用 HH:MM（24 小时制）")
		}
		t.TimeOfDay = norm
		t.IntervalMinutes = 0
		t.RunAt = 0
		if t.ScheduleKind == TaskScheduleWeekly {
			t.Weekdays = NormalizeRecurrenceDays(t.Weekdays)
			if t.Weekdays == "" {
				t.Weekdays = strconv.Itoa(isoWeekday(time.UnixMilli(nowMs).In(shanghaiLoc).Weekday()))
			}
		} else {
			t.Weekdays = ""
		}
		if t.ScheduleKind == TaskScheduleMonthly {
			if t.DayOfMonth < 1 {
				t.DayOfMonth = 1
			} else if t.DayOfMonth > 31 {
				t.DayOfMonth = 31
			}
		} else {
			t.DayOfMonth = 1
		}
	case TaskScheduleInterval:
		if t.IntervalMinutes < MinTaskIntervalMinutes {
			return fmt.Errorf("定时任务间隔最短为 %d 分钟", MinTaskIntervalMinutes)
		}
		if t.IntervalMinutes > MaxTaskIntervalMinutes {
			t.IntervalMinutes = MaxTaskIntervalMinutes
		}
		t.TimeOfDay = ""
		t.Weekdays = ""
		t.DayOfMonth = 1
		t.RunAt = 0
	case TaskScheduleOnce:
		if t.RunAt <= 0 {
			return fmt.Errorf("单次定时任务需要指定执行时间")
		}
		t.TimeOfDay = ""
		t.Weekdays = ""
		t.DayOfMonth = 1
		t.IntervalMinutes = 0
	}
	if t.ModelID != nil && strings.TrimSpace(*t.ModelID) == "" {
		t.ModelID = nil
	}
	t.TargetMessageID = strings.TrimSpace(t.TargetMessageID)
	t.TargetComponentID = strings.TrimSpace(t.TargetComponentID)
	if t.TargetMessageID == "" || t.TargetComponentID == "" {
		t.TargetMessageID = ""
		t.TargetComponentID = ""
	}
	if t.Source == "" {
		t.Source = "ai"
	}
	return nil
}

const scheduledTaskCols = `id, user_id, title, prompt, schedule_kind, time_of_day, weekdays, day_of_month, interval_minutes, run_at, enabled, model_id, target_message_id, target_component_id, conversation_id, source, next_run_at, last_run_at, last_status, last_summary, last_error, run_count, created_at, updated_at`

func scanScheduledTask(row interface{ Scan(...any) error }) (*ScheduledTask, error) {
	var t ScheduledTask
	err := row.Scan(
		&t.ID, &t.UserID, &t.Title, &t.Prompt, &t.ScheduleKind,
		&t.TimeOfDay, &t.Weekdays, &t.DayOfMonth, &t.IntervalMinutes, &t.RunAt,
		&t.Enabled, &t.ModelID, &t.TargetMessageID, &t.TargetComponentID, &t.ConversationID,
		&t.Source, &t.NextRunAt, &t.LastRunAt, &t.LastStatus, &t.LastSummary, &t.LastError,
		&t.RunCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func readScheduledTasks(rows *sql.Rows, err error) ([]*ScheduledTask, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ScheduledTask{}
	for rows.Next() {
		t, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateScheduledTask(ctx context.Context, item *ScheduledTask) error {
	now := Now()
	if err := normalizeScheduledTask(item, now); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *Tx) error {
		var count int
		if err := tx.queryRow(ctx, `SELECT COUNT(*) FROM scheduled_tasks WHERE user_id = ?`, item.UserID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxScheduledTasksUser {
			return fmt.Errorf("最多可创建 %d 个定时任务，请先清理不需要的任务", MaxScheduledTasksUser)
		}
		if item.ID == "" {
			item.ID = NewID()
		}
		if item.CreatedAt == 0 {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		item.NextRunAt = ComputeNextRunAt(item, now, shanghaiLoc)
		_, err := tx.exec(ctx,
			`INSERT INTO scheduled_tasks (`+scheduledTaskCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			item.ID, item.UserID, item.Title, item.Prompt, item.ScheduleKind,
			item.TimeOfDay, item.Weekdays, item.DayOfMonth, item.IntervalMinutes, item.RunAt,
			item.Enabled, item.ModelID, item.TargetMessageID, item.TargetComponentID, item.ConversationID,
			item.Source, item.NextRunAt, item.LastRunAt, item.LastStatus, item.LastSummary, item.LastError,
			item.RunCount, item.CreatedAt, item.UpdatedAt,
		)
		return err
	})
}

func (s *Store) GetScheduledTask(ctx context.Context, id, userID string) (*ScheduledTask, error) {
	return scanScheduledTask(s.queryRow(ctx,
		`SELECT `+scheduledTaskCols+` FROM scheduled_tasks WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) ListScheduledTasks(ctx context.Context, userID string) ([]*ScheduledTask, error) {
	return readScheduledTasks(s.query(ctx,
		`SELECT `+scheduledTaskCols+` FROM scheduled_tasks WHERE user_id = ? ORDER BY enabled DESC, CASE WHEN next_run_at > 0 THEN next_run_at ELSE 9223372036854775807 END ASC, created_at DESC`,
		userID,
	))
}

func (s *Store) UpdateScheduledTask(ctx context.Context, id, userID string, patch ScheduledTaskPatch) (*ScheduledTask, error) {
	var updated *ScheduledTask
	err := s.tx(ctx, func(tx *Tx) error {
		cur, err := scanScheduledTask(tx.queryRow(ctx,
			`SELECT `+scheduledTaskCols+` FROM scheduled_tasks WHERE id = ? AND user_id = ?`+s.forUpdate(), id, userID))
		if err != nil {
			return err
		}
		scheduleChanged := false
		if patch.Title != nil {
			cur.Title = *patch.Title
		}
		if patch.Prompt != nil {
			cur.Prompt = *patch.Prompt
		}
		if patch.ScheduleKind != nil {
			cur.ScheduleKind = *patch.ScheduleKind
			scheduleChanged = true
		}
		if patch.TimeOfDay != nil {
			cur.TimeOfDay = *patch.TimeOfDay
			scheduleChanged = true
		}
		if patch.Weekdays != nil {
			cur.Weekdays = *patch.Weekdays
			scheduleChanged = true
		}
		if patch.DayOfMonth != nil {
			cur.DayOfMonth = *patch.DayOfMonth
			scheduleChanged = true
		}
		if patch.IntervalMinutes != nil {
			cur.IntervalMinutes = *patch.IntervalMinutes
			scheduleChanged = true
		}
		if patch.RunAt != nil {
			cur.RunAt = *patch.RunAt
			scheduleChanged = true
		}
		if patch.Enabled != nil {
			if cur.Enabled != *patch.Enabled {
				scheduleChanged = true
			}
			cur.Enabled = *patch.Enabled
		}
		if patch.ModelID != nil {
			v := strings.TrimSpace(*patch.ModelID)
			if v == "" {
				cur.ModelID = nil
			} else {
				cur.ModelID = &v
			}
		}
		if patch.TargetMessageID != nil {
			cur.TargetMessageID = *patch.TargetMessageID
		}
		if patch.TargetComponentID != nil {
			cur.TargetComponentID = *patch.TargetComponentID
		}
		now := Now()
		if err := normalizeScheduledTask(cur, now); err != nil {
			return err
		}
		if scheduleChanged || (cur.Enabled && cur.NextRunAt == 0) {
			cur.NextRunAt = ComputeNextRunAt(cur, now, shanghaiLoc)
		} else if !cur.Enabled {
			cur.NextRunAt = 0
		}
		cur.UpdatedAt = now
		_, err = tx.exec(ctx,
			`UPDATE scheduled_tasks SET title=?, prompt=?, schedule_kind=?, time_of_day=?, weekdays=?, day_of_month=?, interval_minutes=?, run_at=?, enabled=?, model_id=?, target_message_id=?, target_component_id=?, next_run_at=?, updated_at=? WHERE id=? AND user_id=?`,
			cur.Title, cur.Prompt, cur.ScheduleKind, cur.TimeOfDay, cur.Weekdays, cur.DayOfMonth,
			cur.IntervalMinutes, cur.RunAt, cur.Enabled, cur.ModelID, cur.TargetMessageID, cur.TargetComponentID,
			cur.NextRunAt, cur.UpdatedAt, id, userID,
		)
		updated = cur
		return err
	})
	return updated, err
}

func (s *Store) DeleteScheduledTask(ctx context.Context, id, userID string) error {
	return affected(s.exec(ctx, `DELETE FROM scheduled_tasks WHERE id = ? AND user_id = ?`, id, userID))
}

// ListDueScheduledTasks returns enabled tasks whose next_run_at is due at or before nowMs.
func (s *Store) ListDueScheduledTasks(ctx context.Context, nowMs int64, limit int) ([]*ScheduledTask, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return readScheduledTasks(s.query(ctx,
		`SELECT `+scheduledTaskCols+` FROM scheduled_tasks WHERE enabled = ? AND next_run_at > 0 AND next_run_at <= ? ORDER BY next_run_at ASC LIMIT ?`,
		true, nowMs, limit,
	))
}

// AdvanceScheduledTaskNextRun atomically advances a due task's next_run_at (disabling one-shot tasks)
// and marks it running (or skipped when past catchupGraceMs).
// Returns (claimed, shouldExecute, error).
func (s *Store) AdvanceScheduledTaskNextRun(ctx context.Context, taskID string, nowMs, catchupGraceMs int64) (*ScheduledTask, bool, error) {
	var out *ScheduledTask
	shouldRun := false
	err := s.tx(ctx, func(tx *Tx) error {
		cur, err := scanScheduledTask(tx.queryRow(ctx,
			`SELECT `+scheduledTaskCols+` FROM scheduled_tasks WHERE id = ?`+s.forUpdate(), taskID))
		if err != nil {
			return err
		}
		if !cur.Enabled || cur.NextRunAt <= 0 || cur.NextRunAt > nowMs {
			return nil
		}
		dueAt := cur.NextRunAt
		if cur.ScheduleKind == TaskScheduleOnce {
			cur.Enabled = false
			cur.NextRunAt = 0
		} else {
			cur.NextRunAt = ComputeNextRunAt(cur, nowMs, shanghaiLoc)
		}
		if catchupGraceMs <= 0 || nowMs-dueAt <= catchupGraceMs {
			shouldRun = true
			cur.LastStatus = TaskRunStatusRunning
		}
		cur.UpdatedAt = nowMs
		if _, err := tx.exec(ctx,
			`UPDATE scheduled_tasks SET enabled=?, next_run_at=?, last_status=?, updated_at=? WHERE id=?`,
			cur.Enabled, cur.NextRunAt, cur.LastStatus, cur.UpdatedAt, cur.ID,
		); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, shouldRun, err
}

// MarkScheduledTaskRunning marks a task as currently running (used for manual triggers).
func (s *Store) MarkScheduledTaskRunning(ctx context.Context, id, userID string) error {
	_, err := s.exec(ctx, `UPDATE scheduled_tasks SET last_status = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		TaskRunStatusRunning, Now(), id, userID)
	return err
}

// BindScheduledTaskConversation persists the dedicated archive conversation for a task.
func (s *Store) BindScheduledTaskConversation(ctx context.Context, taskID, userID, convID string) error {
	_, err := s.exec(ctx, `UPDATE scheduled_tasks SET conversation_id = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		convID, Now(), taskID, userID)
	return err
}

// BindScheduledTaskTarget sets the target component for a task if it doesn't already have one (or when force=true).
func (s *Store) BindScheduledTaskTarget(ctx context.Context, taskID, userID, messageID, compID string, force bool) error {
	if messageID == "" || compID == "" {
		return nil
	}
	if force {
		_, err := s.exec(ctx, `UPDATE scheduled_tasks SET target_message_id = ?, target_component_id = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
			messageID, compID, Now(), taskID, userID)
		return err
	}
	_, err := s.exec(ctx, `UPDATE scheduled_tasks SET target_message_id = ?, target_component_id = ?, updated_at = ? WHERE id = ? AND user_id = ? AND target_message_id = ''`,
		messageID, compID, Now(), taskID, userID)
	return err
}

const taskRunCols = `id, task_id, user_id, trigger_source, status, summary, error, message_id, conversation_id, tool_calls, updated_components, started_at, finished_at`

func (s *Store) CreateScheduledTaskRun(ctx context.Context, run *ScheduledTaskRun) error {
	if run.ID == "" {
		run.ID = NewID()
	}
	if run.StartedAt == 0 {
		run.StartedAt = Now()
	}
	if run.Status == "" {
		run.Status = TaskRunStatusRunning
	}
	if run.TriggerSource == "" {
		run.TriggerSource = "schedule"
	}
	_, err := s.exec(ctx,
		`INSERT INTO scheduled_task_runs (`+taskRunCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.TaskID, run.UserID, run.TriggerSource, run.Status,
		run.Summary, run.Error, run.MessageID, run.ConversationID,
		run.ToolCalls, run.UpdatedComponents, run.StartedAt, run.FinishedAt,
	)
	return err
}

// FinishScheduledTaskRun finalizes a run record and updates the parent task's last_run_* summary fields.
func (s *Store) FinishScheduledTaskRun(ctx context.Context, run *ScheduledTaskRun) error {
	if run.FinishedAt == 0 {
		run.FinishedAt = Now()
	}
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.exec(ctx,
			`UPDATE scheduled_task_runs SET status=?, summary=?, error=?, message_id=?, conversation_id=?, tool_calls=?, updated_components=?, finished_at=? WHERE id=?`,
			run.Status, run.Summary, run.Error, run.MessageID, run.ConversationID,
			run.ToolCalls, run.UpdatedComponents, run.FinishedAt, run.ID,
		); err != nil {
			return err
		}
		_, err := tx.exec(ctx,
			`UPDATE scheduled_tasks SET last_run_at=?, last_status=?, last_summary=?, last_error=?, run_count=run_count+1, updated_at=? WHERE id=? AND user_id=?`,
			run.FinishedAt, run.Status, run.Summary, run.Error, run.FinishedAt, run.TaskID, run.UserID,
		)
		return err
	})
}

// ListScheduledTaskRuns returns the latest execution records for a task owned by userID.
func (s *Store) ListScheduledTaskRuns(ctx context.Context, taskID, userID string, limit int) ([]*ScheduledTaskRun, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.query(ctx,
		`SELECT `+taskRunCols+` FROM scheduled_task_runs WHERE task_id = ? AND user_id = ? ORDER BY started_at DESC LIMIT ?`,
		taskID, userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ScheduledTaskRun{}
	for rows.Next() {
		var r ScheduledTaskRun
		if err := rows.Scan(
			&r.ID, &r.TaskID, &r.UserID, &r.TriggerSource, &r.Status,
			&r.Summary, &r.Error, &r.MessageID, &r.ConversationID,
			&r.ToolCalls, &r.UpdatedComponents, &r.StartedAt, &r.FinishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// MarkInterruptedTaskRuns cleans up runs left in "running" state after a server restart.
func (s *Store) MarkInterruptedTaskRuns(ctx context.Context) (int64, error) {
	now := Now()
	res, err := s.exec(ctx,
		`UPDATE scheduled_task_runs SET status = ?, error = ?, finished_at = ? WHERE status = ?`,
		TaskRunStatusError, "服务重启导致本次执行中断", now, TaskRunStatusRunning,
	)
	if err != nil {
		return 0, err
	}
	_, _ = s.exec(ctx,
		`UPDATE scheduled_tasks SET last_status = ?, last_error = ?, updated_at = ? WHERE last_status = ?`,
		TaskRunStatusError, "服务重启导致上次执行中断", now, TaskRunStatusRunning,
	)
	return res.RowsAffected()
}
