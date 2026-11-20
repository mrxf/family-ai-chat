package api

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"nuan/internal/store"
)

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	all, err := s.Store.ListSchedules(r.Context(), u.ID, true)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	loc := store.ShanghaiLocation()
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayEnd := todayStart.Add(24*time.Hour - time.Millisecond)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	tomorrowEnd := tomorrowStart.Add(24*time.Hour - time.Millisecond)
	upcomingStart := todayStart.AddDate(0, 0, 2)
	upcomingEnd := todayStart.AddDate(0, 0, 16).Add(-time.Millisecond)

	var active []*store.Schedule
	for _, item := range all {
		if item.Status == store.ScheduleStatusActive {
			active = append(active, item)
		}
	}

	todayOccs := store.ExpandOccurrences(all, todayStart.UnixMilli(), todayEnd.UnixMilli(), loc)
	tomorrowOccs := store.ExpandOccurrences(active, tomorrowStart.UnixMilli(), tomorrowEnd.UnixMilli(), loc)
	upcomingOccs := store.ExpandOccurrences(active, upcomingStart.UnixMilli(), upcomingEnd.UnixMilli(), loc)
	reminders := store.UpcomingReminders(active, now.UnixMilli(), 30, 48, loc)

	todayPending := 0
	for _, occ := range todayOccs {
		if !occ.Completed {
			todayPending++
		}
	}
	if todayOccs == nil {
		todayOccs = []store.ScheduleOccurrence{}
	}
	if tomorrowOccs == nil {
		tomorrowOccs = []store.ScheduleOccurrence{}
	}
	if upcomingOccs == nil {
		upcomingOccs = []store.ScheduleOccurrence{}
	}
	if reminders == nil {
		reminders = []store.ScheduleReminder{}
	}

	writeJSON(w, 200, map[string]any{
		"schedules":            all,
		"today_occurrences":    todayOccs,
		"tomorrow_occurrences": tomorrowOccs,
		"upcoming_occurrences": upcomingOccs,
		"upcoming_reminders":   reminders,
		"today_pending":        todayPending,
		"active_total":         len(active),
	})
}

type scheduleCreateInput struct {
	Title           string `json:"title"`
	Note            string `json:"note"`
	Location        string `json:"location"`
	StartAt         int64  `json:"start_at"`
	EndAt           int64  `json:"end_at"`
	AllDay          bool   `json:"all_day"`
	RemindBeforeMin *int   `json:"remind_before_min"`
	Recurrence      string `json:"recurrence"`
	RecurrenceDays  string `json:"recurrence_days"`
	RecurrenceUntil int64  `json:"recurrence_until"`
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in scheduleCreateInput
	if !decode(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if n := utf8.RuneCountInString(in.Title); n < 1 || n > 120 {
		writeErr(w, 400, "bad_schedule", "日程标题需要 1–120 个字")
		return
	}
	if in.StartAt <= 0 {
		writeErr(w, 400, "bad_schedule", "请选择日程时间")
		return
	}
	if !store.ValidRecurrence(in.Recurrence) {
		writeErr(w, 400, "bad_schedule", "重复规则不正确")
		return
	}
	remind := 0
	if in.RemindBeforeMin != nil {
		remind = *in.RemindBeforeMin
	}
	item := &store.Schedule{
		UserID:          currentUser(r).ID,
		Title:           in.Title,
		Note:            in.Note,
		Location:        in.Location,
		StartAt:         in.StartAt,
		EndAt:           in.EndAt,
		AllDay:          in.AllDay,
		RemindBeforeMin: remind,
		Recurrence:      in.Recurrence,
		RecurrenceDays:  in.RecurrenceDays,
		RecurrenceUntil: in.RecurrenceUntil,
		Source:          "manual",
	}
	if err := s.Store.CreateSchedule(r.Context(), item); err != nil {
		writeErr(w, 400, "bad_schedule", err.Error())
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title           *string `json:"title"`
		Note            *string `json:"note"`
		Location        *string `json:"location"`
		StartAt         *int64  `json:"start_at"`
		EndAt           *int64  `json:"end_at"`
		AllDay          *bool   `json:"all_day"`
		RemindBeforeMin *int    `json:"remind_before_min"`
		Recurrence      *string `json:"recurrence"`
		RecurrenceDays  *string `json:"recurrence_days"`
		RecurrenceUntil *int64  `json:"recurrence_until"`
		Completed       *bool   `json:"completed"`
		OccurrenceDate  string  `json:"occurrence_date"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "id")
	uid := currentUser(r).ID

	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
		if n := utf8.RuneCountInString(t); n < 1 || n > 120 {
			writeErr(w, 400, "bad_schedule", "日程标题需要 1–120 个字")
			return
		}
	}
	if in.Recurrence != nil && !store.ValidRecurrence(*in.Recurrence) {
		writeErr(w, 400, "bad_schedule", "重复规则不正确")
		return
	}

	hasFieldPatch := in.Title != nil || in.Note != nil || in.Location != nil ||
		in.StartAt != nil || in.EndAt != nil || in.AllDay != nil ||
		in.RemindBeforeMin != nil || in.Recurrence != nil ||
		in.RecurrenceDays != nil || in.RecurrenceUntil != nil

	var out *store.Schedule
	var err error
	if hasFieldPatch {
		out, err = s.Store.UpdateSchedule(r.Context(), id, uid, store.SchedulePatch{
			Title:           in.Title,
			Note:            in.Note,
			Location:        in.Location,
			StartAt:         in.StartAt,
			EndAt:           in.EndAt,
			AllDay:          in.AllDay,
			RemindBeforeMin: in.RemindBeforeMin,
			Recurrence:      in.Recurrence,
			RecurrenceDays:  in.RecurrenceDays,
			RecurrenceUntil: in.RecurrenceUntil,
		})
		if err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	if in.Completed != nil {
		out, err = s.Store.SetScheduleCompletion(r.Context(), id, uid, *in.Completed, in.OccurrenceDate)
		if err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	if out == nil {
		out, err = s.Store.GetSchedule(r.Context(), id, uid)
		if err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteSchedule(r.Context(), chi.URLParam(r, "id"), currentUser(r).ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) clearCompletedSchedules(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.ClearCompletedSchedules(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}
