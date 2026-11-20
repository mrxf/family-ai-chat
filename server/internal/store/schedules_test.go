package store_test

import (
	"errors"
	"testing"
	"time"

	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

func TestSchedulesCRUDIsolationAndRecurrence(t *testing.T) {
	s := storetest.Open(t)
	ctx := t.Context()
	loc := store.ShanghaiLocation()

	u1 := &store.User{Role: "member", Username: "u1", DisplayName: "妈妈"}
	u2 := &store.User{Role: "member", Username: "u2", DisplayName: "爸爸"}
	for _, u := range []*store.User{u1, u2} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	conv := &store.Conversation{UserID: u1.ID}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}

	// 1. One-time schedule with 30m advance reminder
	t1 := time.Date(2026, 11, 21, 9, 30, 0, 0, loc).UnixMilli()
	sc1 := &store.Schedule{
		UserID:          u1.ID,
		Title:           "去协和医院复查",
		Note:            "空腹抽血",
		Location:        "门诊三楼",
		StartAt:         t1,
		RemindBeforeMin: 30,
		Recurrence:      store.RecurrenceNone,
		Source:          "ai",
		SourceConvID:    &conv.ID,
	}
	if err := s.CreateSchedule(ctx, sc1); err != nil {
		t.Fatal(err)
	}

	// User isolation check
	if _, err := s.GetSchedule(ctx, sc1.ID, u2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for other user, got %v", err)
	}

	// 2. Recurring weekly schedule ("周一,周三,周五")
	t2 := time.Date(2026, 11, 16, 8, 0, 0, 0, loc).UnixMilli() // 2026-11-16 is Monday
	sc2 := &store.Schedule{
		UserID:          u1.ID,
		Title:           "吃降压药",
		StartAt:         t2,
		RemindBeforeMin: 0,
		Recurrence:      store.RecurrenceWeekly,
		RecurrenceDays:  "周一、周三、周五",
	}
	if err := s.CreateSchedule(ctx, sc2); err != nil {
		t.Fatal(err)
	}
	if sc2.RecurrenceDays != "1,3,5" {
		t.Fatalf("expected normalized days 1,3,5, got %q", sc2.RecurrenceDays)
	}

	// 3. Monthly month-end clamping (Jan 31 -> Feb 28)
	t3 := time.Date(2026, 1, 31, 10, 0, 0, 0, loc).UnixMilli()
	sc3 := &store.Schedule{
		UserID:          u1.ID,
		Title:           "交话费",
		StartAt:         t3,
		RemindBeforeMin: -1,
		Recurrence:      store.RecurrenceMonthly,
	}
	if err := s.CreateSchedule(ctx, sc3); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListSchedules(ctx, u1.ID, true)
	if err != nil || len(all) != 3 {
		t.Fatalf("expected 3 schedules, got %d (%v)", len(all), err)
	}

	// Mark only one occurrence of recurring schedule completed
	if _, err := s.SetScheduleCompletion(ctx, sc2.ID, u1.ID, true, "2026-11-18"); err != nil {
		t.Fatal(err)
	}
	all, _ = s.ListSchedules(ctx, u1.ID, true)

	// Expand week 2026-11-16 .. 2026-11-22
	weekStart := time.Date(2026, 11, 16, 0, 0, 0, 0, loc).UnixMilli()
	weekEnd := time.Date(2026, 11, 22, 23, 59, 59, 0, loc).UnixMilli()
	occs := store.ExpandOccurrences(all, weekStart, weekEnd, loc)
	// Expected in that week: Mon 16 (sc2), Wed 18 (sc2 completed), Fri 20 (sc2), Sat 21 (sc1)
	if len(occs) != 4 {
		t.Fatalf("expected 4 occurrences in week, got %d", len(occs))
	}
	if !occs[1].Completed || occs[1].OccurrenceDate != "2026-11-18" {
		t.Fatalf("expected Wed 2026-11-18 occurrence completed, got %+v", occs[1])
	}
	if occs[2].Completed || occs[2].OccurrenceDate != "2026-11-20" {
		t.Fatalf("expected Fri 2026-11-20 occurrence still active, got %+v", occs[2])
	}

	// Check Feb 2026 monthly clamping
	febStart := time.Date(2026, 2, 1, 0, 0, 0, 0, loc).UnixMilli()
	febEnd := time.Date(2026, 2, 28, 23, 59, 59, 0, loc).UnixMilli()
	febOccs := store.ExpandOccurrences([]*store.Schedule{sc3}, febStart, febEnd, loc)
	if len(febOccs) != 1 || febOccs[0].OccurrenceDate != "2026-02-28" {
		t.Fatalf("expected monthly clamping to 2026-02-28, got %+v", febOccs)
	}

	// UpcomingReminders check from 2026-11-18 07:00 + 7d (= 2026-11-25 07:00):
	// Wed 11-18 completed -> skipped; Fri 11-20 (sc2), Sat 11-21 (sc1), Mon 11-23 (sc2) included!
	nowMs := time.Date(2026, 11, 18, 7, 0, 0, 0, loc).UnixMilli()
	rems := store.UpcomingReminders(all, nowMs, 7, 10, loc)
	if len(rems) != 3 {
		t.Fatalf("expected 3 upcoming reminders (Fri sc2 + Sat sc1 + Mon sc2, skipping completed Wed and -1 monthly), got %d: %+v", len(rems), rems)
	}
	if rems[0].OccurrenceDate != "2026-11-20" || rems[1].OccurrenceDate != "2026-11-21" || rems[2].OccurrenceDate != "2026-11-23" {
		t.Fatalf("unexpected reminder dates: %+v", rems)
	}
	wantSatFire := time.Date(2026, 11, 21, 9, 0, 0, 0, loc).UnixMilli()
	if rems[1].FireAt != wantSatFire {
		t.Fatalf("expected 30m early fire_at %d, got %d", wantSatFire, rems[1].FireAt)
	}

	// Deleting conversation nulls source_conv_id without deleting schedule
	if err := s.DeleteConversation(ctx, conv.ID, u1.ID); err != nil {
		t.Fatal(err)
	}
	got1, err := s.GetSchedule(ctx, sc1.ID, u1.ID)
	if err != nil || got1.SourceConvID != nil {
		t.Fatalf("expected schedule preserved with nil source_conv_id, got %+v, err=%v", got1, err)
	}

	// Complete & clear one-time schedule
	if _, err := s.SetScheduleCompletion(ctx, sc1.ID, u1.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.ClearCompletedSchedules(ctx, u1.ID)
	if err != nil || cleared != 1 {
		t.Fatalf("expected 1 cleared completed schedule, got %d (%v)", cleared, err)
	}
}
