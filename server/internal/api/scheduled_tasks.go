package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"nuan/internal/chat"
	"nuan/internal/store"
)

func (s *Server) listScheduledTasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListScheduledTasks(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	active := 0
	for _, it := range items {
		if it.Enabled {
			active++
		}
	}
	writeJSON(w, 200, map[string]any{
		"items":        items,
		"active_count": active,
		"total_count":  len(items),
	})
}

func (s *Server) createScheduledTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title             string  `json:"title"`
		Prompt            string  `json:"prompt"`
		ScheduleKind      string  `json:"schedule_kind"`
		TimeOfDay         string  `json:"time_of_day"`
		Weekdays          string  `json:"weekdays"`
		DayOfMonth        int     `json:"day_of_month"`
		IntervalMinutes   int     `json:"interval_minutes"`
		RunAt             int64   `json:"run_at"`
		Enabled           *bool   `json:"enabled"`
		ModelID           *string `json:"model_id"`
		TargetMessageID   string  `json:"target_message_id"`
		TargetComponentID string  `json:"target_component_id"`
		RunImmediately    bool    `json:"run_immediately"`
	}
	if !decode(w, r, &in) {
		return
	}
	uid := currentUser(r).ID
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.ModelID != nil && strings.TrimSpace(*in.ModelID) != "" {
		if _, err := s.Store.GetModel(r.Context(), strings.TrimSpace(*in.ModelID)); err != nil {
			writeErr(w, 400, "bad_model", "指定的模型不存在")
			return
		}
	}
	tMsg := strings.TrimSpace(in.TargetMessageID)
	tComp := strings.TrimSpace(in.TargetComponentID)
	if tMsg != "" && tComp != "" {
		if _, _, err := s.Store.GetMessageComponent(r.Context(), tMsg, uid, tComp); err != nil {
			writeErr(w, 400, "bad_target", "绑定的目标卡片不存在")
			return
		}
	} else {
		tMsg, tComp = "", ""
	}
	item := &store.ScheduledTask{
		UserID:            uid,
		Title:             in.Title,
		Prompt:            in.Prompt,
		ScheduleKind:      in.ScheduleKind,
		TimeOfDay:         in.TimeOfDay,
		Weekdays:          in.Weekdays,
		DayOfMonth:        in.DayOfMonth,
		IntervalMinutes:   in.IntervalMinutes,
		RunAt:             in.RunAt,
		Enabled:           enabled,
		ModelID:           in.ModelID,
		TargetMessageID:   tMsg,
		TargetComponentID: tComp,
		Source:            "manual",
	}
	if err := s.Store.CreateScheduledTask(r.Context(), item); err != nil {
		writeErr(w, 400, "bad_task", err.Error())
		return
	}
	if in.RunImmediately && s.Chat != nil && s.Chat.Automation != nil {
		_ = s.Chat.Automation.TriggerNow(uid, item.ID)
	}
	writeJSON(w, 201, item)
}

func (s *Server) updateScheduledTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title             *string `json:"title"`
		Prompt            *string `json:"prompt"`
		ScheduleKind      *string `json:"schedule_kind"`
		TimeOfDay         *string `json:"time_of_day"`
		Weekdays          *string `json:"weekdays"`
		DayOfMonth        *int    `json:"day_of_month"`
		IntervalMinutes   *int    `json:"interval_minutes"`
		RunAt             *int64  `json:"run_at"`
		Enabled           *bool   `json:"enabled"`
		ModelID           *string `json:"model_id"`
		TargetMessageID   *string `json:"target_message_id"`
		TargetComponentID *string `json:"target_component_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	uid := currentUser(r).ID
	id := chi.URLParam(r, "id")
	if in.ModelID != nil && strings.TrimSpace(*in.ModelID) != "" {
		if _, err := s.Store.GetModel(r.Context(), strings.TrimSpace(*in.ModelID)); err != nil {
			writeErr(w, 400, "bad_model", "指定的模型不存在")
			return
		}
	}
	if in.TargetMessageID != nil && in.TargetComponentID != nil &&
		strings.TrimSpace(*in.TargetMessageID) != "" && strings.TrimSpace(*in.TargetComponentID) != "" {
		if _, _, err := s.Store.GetMessageComponent(r.Context(), strings.TrimSpace(*in.TargetMessageID), uid, strings.TrimSpace(*in.TargetComponentID)); err != nil {
			writeErr(w, 400, "bad_target", "绑定的目标卡片不存在")
			return
		}
	}
	updated, err := s.Store.UpdateScheduledTask(r.Context(), id, uid, store.ScheduledTaskPatch{
		Title:             in.Title,
		Prompt:            in.Prompt,
		ScheduleKind:      in.ScheduleKind,
		TimeOfDay:         in.TimeOfDay,
		Weekdays:          in.Weekdays,
		DayOfMonth:        in.DayOfMonth,
		IntervalMinutes:   in.IntervalMinutes,
		RunAt:             in.RunAt,
		Enabled:           in.Enabled,
		ModelID:           in.ModelID,
		TargetMessageID:   in.TargetMessageID,
		TargetComponentID: in.TargetComponentID,
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeStoreErr(w, r, err)
			return
		}
		writeErr(w, 400, "bad_task", err.Error())
		return
	}
	writeJSON(w, 200, updated)
}

func (s *Server) deleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteScheduledTask(r.Context(), chi.URLParam(r, "id"), currentUser(r).ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) runScheduledTaskNow(w http.ResponseWriter, r *http.Request) {
	if s.Chat == nil || s.Chat.Automation == nil {
		writeErr(w, 503, "unavailable", "后台定时任务执行器未启用")
		return
	}
	err := s.Chat.Automation.TriggerNow(currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, chat.ErrTaskAlreadyRunning) {
			writeErr(w, 409, "already_running", "这个定时任务正在执行中，请稍候")
			return
		}
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true})
}

func (s *Server) listScheduledTaskRuns(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	id := chi.URLParam(r, "id")
	if _, err := s.Store.GetScheduledTask(r.Context(), id, uid); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	runs, err := s.Store.ListScheduledTaskRuns(r.Context(), id, uid, 20)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": runs})
}
