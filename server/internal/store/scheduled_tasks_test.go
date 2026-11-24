package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestComputeNextRunAt(t *testing.T) {
	loc := ShanghaiLocation()
	// 2026-11-23 is Monday 08:30 CST
	base := time.Date(2026, 11, 23, 8, 30, 0, 0, loc).UnixMilli()

	// Daily at 07:00 -> tomorrow 07:00
	daily := &ScheduledTask{Enabled: true, ScheduleKind: TaskScheduleDaily, TimeOfDay: "07:00"}
	next := ComputeNextRunAt(daily, base, loc)
	want := time.Date(2026, 11, 24, 7, 0, 0, 0, loc).UnixMilli()
	if next != want {
		t.Fatalf("daily 07:00 got %v, want %v", time.UnixMilli(next).In(loc), time.UnixMilli(want).In(loc))
	}

	// Daily at 09:00 -> today 09:00
	dailyLater := &ScheduledTask{Enabled: true, ScheduleKind: TaskScheduleDaily, TimeOfDay: "09:00"}
	next = ComputeNextRunAt(dailyLater, base, loc)
	want = time.Date(2026, 11, 23, 9, 0, 0, 0, loc).UnixMilli()
	if next != want {
		t.Fatalf("daily 09:00 got %v, want %v", time.UnixMilli(next).In(loc), time.UnixMilli(want).In(loc))
	}

	// Weekdays from Friday 18:00 -> next Monday 07:00
	friEvening := time.Date(2026, 11, 27, 18, 0, 0, 0, loc).UnixMilli()
	wd := &ScheduledTask{Enabled: true, ScheduleKind: TaskScheduleWeekdays, TimeOfDay: "07:00"}
	next = ComputeNextRunAt(wd, friEvening, loc)
	want = time.Date(2026, 11, 30, 7, 0, 0, 0, loc).UnixMilli()
	if next != want {
		t.Fatalf("weekdays got %v, want %v", time.UnixMilli(next).In(loc), time.UnixMilli(want).In(loc))
	}

	// Weekly Wed & Fri at 07:00 from Mon 08:30 -> Wed 07:00
	weekly := &ScheduledTask{Enabled: true, ScheduleKind: TaskScheduleWeekly, TimeOfDay: "07:00", Weekdays: "3,5"}
	next = ComputeNextRunAt(weekly, base, loc)
	want = time.Date(2026, 11, 25, 7, 0, 0, 0, loc).UnixMilli()
	if next != want {
		t.Fatalf("weekly got %v, want %v", time.UnixMilli(next).In(loc), time.UnixMilli(want).In(loc))
	}

	// Monthly 31st in Feb clamps to 28th
	jan31Late := time.Date(2026, 1, 31, 20, 0, 0, 0, loc).UnixMilli()
	monthly := &ScheduledTask{Enabled: true, ScheduleKind: TaskScheduleMonthly, DayOfMonth: 31, TimeOfDay: "09:00"}
	next = ComputeNextRunAt(monthly, jan31Late, loc)
	want = time.Date(2026, 2, 28, 9, 0, 0, 0, loc).UnixMilli()
	if next != want {
		t.Fatalf("monthly got %v, want %v", time.UnixMilli(next).In(loc), time.UnixMilli(want).In(loc))
	}

	// Interval 60 minutes
	iv := &ScheduledTask{Enabled: true, ScheduleKind: TaskScheduleInterval, IntervalMinutes: 60}
	if got := ComputeNextRunAt(iv, base, loc); got != base+60*60_000 {
		t.Fatalf("interval got %d, want %d", got, base+60*60_000)
	}

	// Disabled task returns 0
	daily.Enabled = false
	if got := ComputeNextRunAt(daily, base, loc); got != 0 {
		t.Fatalf("disabled task expected 0, got %d", got)
	}
}

func TestScheduledTaskCRUDAndComponentOperations(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	u := testUser(t, s, "task-user")

	c := &Conversation{UserID: u.ID, Title: "新闻对话"}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	msg := &Message{
		Role:    "assistant",
		Content: "今日新闻",
		Status:  StatusDone,
		Components: []MessageComponent{
			{ID: "comp_1", Kind: "feed", Pos: 0, Data: json.RawMessage(`{"title":"大模型新闻","items":[{"id":"n1","title":"旧1"}]}`)},
			{ID: "comp_2", Kind: "todo", Pos: 0, Data: json.RawMessage(`{"title":"待办","items":[{"id":"i1","text":"买菜"}]}`)},
		},
	}
	if err := s.ApplyTurn(ctx, Turn{ConversationID: c.ID, Append: []*Message{msg}}); err != nil {
		t.Fatal(err)
	}

	// Pin both and reorder
	pin1, err := s.PinComponent(ctx, u.ID, msg.ID, "comp_1")
	if err != nil {
		t.Fatal(err)
	}
	pin2, err := s.PinComponent(ctx, u.ID, msg.ID, "comp_2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderPinnedComponents(ctx, u.ID, []string{pin2.ID, pin1.ID}); err != nil {
		t.Fatal(err)
	}
	pins, err := s.ListPinnedComponents(ctx, u.ID)
	if err != nil || len(pins) != 2 || pins[0].ID != pin2.ID {
		t.Fatalf("expected pin2 first after reorder, got %+v err=%v", pins, err)
	}

	// Update component data in place
	newData := json.RawMessage(`{"title":"大模型新闻","items":[{"id":"n2","title":"新1"},{"id":"n1","title":"旧1"}]}`)
	_, updatedComp, err := s.UpdateMessageComponent(ctx, msg.ID, u.ID, "comp_1", newData, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedComp.Data) != string(newData) {
		t.Fatalf("unexpected updated data: %s", string(updatedComp.Data))
	}

	// Scheduled task CRUD & due claiming
	task := &ScheduledTask{
		UserID:            u.ID,
		Title:             "早7点大模型新闻",
		Prompt:            "查询最新大模型新闻，更新前三条并把原前三条后移",
		ScheduleKind:      TaskScheduleDaily,
		TimeOfDay:         "07:00",
		Enabled:           true,
		TargetMessageID:   msg.ID,
		TargetComponentID: "comp_1",
	}
	if err := s.CreateScheduledTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if task.NextRunAt <= Now() {
		t.Fatalf("expected future next_run_at, got %d", task.NextRunAt)
	}

	// Simulate due
	dueList, err := s.ListDueScheduledTasks(ctx, task.NextRunAt+1000, 10)
	if err != nil || len(dueList) != 1 || dueList[0].ID != task.ID {
		t.Fatalf("expected 1 due task, got %+v err=%v", dueList, err)
	}
	claimed, shouldRun, err := s.AdvanceScheduledTaskNextRun(ctx, task.ID, task.NextRunAt+1000, int64(2*time.Hour/time.Millisecond))
	if err != nil || !shouldRun || claimed.NextRunAt <= task.NextRunAt {
		t.Fatalf("advance failed: claimed=%+v shouldRun=%v err=%v", claimed, shouldRun, err)
	}

	// Record run & finish
	run := &ScheduledTaskRun{TaskID: task.ID, UserID: u.ID, TriggerSource: "schedule"}
	if err := s.CreateScheduledTaskRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.Status = TaskRunStatusSuccess
	run.Summary = "更新了 3 条大模型新闻"
	run.UpdatedComponents = 1
	if err := s.FinishScheduledTaskRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListScheduledTaskRuns(ctx, task.ID, u.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].UpdatedComponents != 1 {
		t.Fatalf("unexpected runs: %+v err=%v", runs, err)
	}

	// Delete component also unpins it
	if err := s.DeleteMessageComponent(ctx, msg.ID, u.ID, "comp_2"); err != nil {
		t.Fatal(err)
	}
	pins, _ = s.ListPinnedComponents(ctx, u.ID)
	if len(pins) != 1 || pins[0].ComponentID != "comp_1" {
		t.Fatalf("expected comp_2 unpinned on delete, got %+v", pins)
	}
}
