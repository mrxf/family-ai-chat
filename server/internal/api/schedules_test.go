package api

import (
	"testing"
	"time"

	"nuan/internal/store"
)

func TestSchedulesAPIAndRecurringCompletion(t *testing.T) {
	f := newAPIFixture(t)
	loc := store.ShanghaiLocation()
	now := time.Now().In(loc)
	todayNine := time.Date(now.Year(), now.Month(), now.Day(), 23, 30, 0, 0, loc).UnixMilli()
	tomorrowEight := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, loc).AddDate(0, 0, 1).UnixMilli()

	var oneTime store.Schedule
	f.json("POST", "/api/schedules", map[string]any{
		"title":             "  睡前量血压  ",
		"start_at":          todayNine,
		"remind_before_min": 10,
		"note":              "左臂测量",
	}, 201, &oneTime)
	if oneTime.Title != "睡前量血压" || oneTime.Source != "manual" || oneTime.RemindBeforeMin != 10 {
		t.Fatalf("unexpected created schedule: %+v", oneTime)
	}

	var daily store.Schedule
	f.json("POST", "/api/schedules", map[string]any{
		"title":             "早起吃降压药",
		"start_at":          tomorrowEight,
		"remind_before_min": 0,
		"recurrence":        "daily",
	}, 201, &daily)

	var list struct {
		Schedules           []store.Schedule           `json:"schedules"`
		TodayOccurrences    []store.ScheduleOccurrence `json:"today_occurrences"`
		TomorrowOccurrences []store.ScheduleOccurrence `json:"tomorrow_occurrences"`
		UpcomingReminders   []store.ScheduleReminder   `json:"upcoming_reminders"`
		TodayPending        int                        `json:"today_pending"`
		ActiveTotal         int                        `json:"active_total"`
	}
	f.json("GET", "/api/schedules", nil, 200, &list)
	if list.ActiveTotal != 2 || len(list.TomorrowOccurrences) != 1 {
		t.Fatalf("unexpected list response: %+v", list)
	}

	// Complete tomorrow's recurring occurrence only
	tomDate := time.UnixMilli(tomorrowEight).In(loc).Format("2006-01-02")
	f.json("PATCH", "/api/schedules/"+daily.ID, map[string]any{
		"completed":       true,
		"occurrence_date": tomDate,
	}, 200, &daily)
	if len(daily.CompletedDates) != 1 || daily.CompletedDates[0] != tomDate || daily.Status != "active" {
		t.Fatalf("expected recurring schedule to stay active with completed_date=%s, got %+v", tomDate, daily)
	}

	// Complete one-time schedule & clear completed
	f.json("PATCH", "/api/schedules/"+oneTime.ID, map[string]any{"completed": true}, 200, &oneTime)
	if oneTime.Status != "completed" {
		t.Fatalf("expected completed status, got %+v", oneTime)
	}

	var cleared struct {
		Deleted int64 `json:"deleted"`
	}
	f.json("DELETE", "/api/schedules", nil, 200, &cleared)
	if cleared.Deleted != 1 {
		t.Fatalf("expected 1 completed schedule cleared, got %d", cleared.Deleted)
	}

	// Delete recurring schedule
	f.json("DELETE", "/api/schedules/"+daily.ID, nil, 204, nil)
	f.json("GET", "/api/schedules", nil, 200, &list)
	if list.ActiveTotal != 0 || len(list.Schedules) != 0 {
		t.Fatalf("expected all schedules removed, got %+v", list)
	}
}
