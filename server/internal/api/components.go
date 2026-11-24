package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"nuan/internal/chat"
	"nuan/internal/store"
)

func (s *Server) updateMessageComponent(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	msgID := chi.URLParam(r, "id")
	compID := chi.URLParam(r, "compId")

	var in struct {
		Data          json.RawMessage `json:"data"`
		State         json.RawMessage `json:"state"`
		ChangeSummary string          `json:"change_summary"`
	}
	if !decode(w, r, &in) {
		return
	}
	hasData := len(in.Data) > 0 && string(in.Data) != "null"
	hasState := len(in.State) > 0 && string(in.State) != "null"
	if !hasData && !hasState {
		writeErr(w, 400, "bad_request", "需要提供要更新的组件内容或状态")
		return
	}

	var normData json.RawMessage
	if hasData {
		if len(in.Data) > 64<<10 {
			writeErr(w, 400, "bad_data", "组件内容过大")
			return
		}
		_, curComp, err := s.Store.GetMessageComponent(r.Context(), msgID, u.ID, compID)
		if err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
		normData, err = chat.NormalizeComponentData(curComp.Kind, in.Data)
		if err != nil {
			writeErr(w, 400, "bad_data", err.Error())
			return
		}
	}

	var statePtr *json.RawMessage
	if hasState {
		if len(in.State) > 16<<10 {
			writeErr(w, 400, "bad_state", "组件状态数据格式不正确或过大")
			return
		}
		var sanity map[string]any
		if json.Unmarshal(in.State, &sanity) != nil || sanity == nil {
			writeErr(w, 400, "bad_state", "组件状态必须是 JSON 对象")
			return
		}
		st := in.State
		statePtr = &st
	}

	summary := strings.TrimSpace(in.ChangeSummary)
	if summary == "" && hasData {
		summary = "手动编辑卡片内容"
	}
	msg, comp, err := s.Store.UpdateMessageComponentWithMeta(r.Context(), msgID, u.ID, compID, normData, statePtr, store.ComponentMutationMeta{
		ChangeSummary: summary,
		SourceType:    "manual_edit",
	})
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if run := s.Chat.Hub.Get(msgID); run != nil && run.UserID == u.ID {
		run.UpdateComponent(compID, comp.Data, comp.State)
	}
	writeJSON(w, 200, map[string]any{
		"component": comp,
		"message":   msg,
	})
}

func (s *Server) deleteMessageComponent(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	msgID := chi.URLParam(r, "id")
	compID := chi.URLParam(r, "compId")
	if err := s.Store.DeleteMessageComponent(r.Context(), msgID, u.ID, compID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) getComponentEntity(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	entityID := chi.URLParam(r, "entityId")
	ent, err := s.Store.GetComponentEntity(r.Context(), entityID, u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, ent)
}

func (s *Server) listComponentVersions(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	entityID := chi.URLParam(r, "entityId")
	ent, versions, err := s.Store.ListComponentVersions(r.Context(), entityID, u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"entity":   ent,
		"versions": versions,
	})
}

func (s *Server) updateComponentEntity(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	entityID := chi.URLParam(r, "entityId")

	var in struct {
		Data          json.RawMessage `json:"data"`
		State         json.RawMessage `json:"state"`
		ChangeSummary string          `json:"change_summary"`
	}
	if !decode(w, r, &in) {
		return
	}
	hasData := len(in.Data) > 0 && string(in.Data) != "null"
	hasState := len(in.State) > 0 && string(in.State) != "null"
	if !hasData && !hasState {
		writeErr(w, 400, "bad_request", "需要提供要更新的组件内容或状态")
		return
	}

	cur, err := s.Store.GetComponentEntity(r.Context(), entityID, u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}

	var normData json.RawMessage
	if hasData {
		if len(in.Data) > 64<<10 {
			writeErr(w, 400, "bad_data", "组件内容过大")
			return
		}
		normData, err = chat.NormalizeComponentData(cur.Kind, in.Data)
		if err != nil {
			writeErr(w, 400, "bad_data", err.Error())
			return
		}
	}

	var statePtr *json.RawMessage
	if hasState {
		if len(in.State) > 16<<10 {
			writeErr(w, 400, "bad_state", "组件状态数据格式不正确或过大")
			return
		}
		var sanity map[string]any
		if json.Unmarshal(in.State, &sanity) != nil || sanity == nil {
			writeErr(w, 400, "bad_state", "组件状态必须是 JSON 对象")
			return
		}
		st := in.State
		statePtr = &st
	}

	summary := strings.TrimSpace(in.ChangeSummary)
	if summary == "" && hasData {
		summary = "手动编辑卡片内容"
	}
	updated, err := s.Store.UpdateComponentEntity(r.Context(), entityID, u.ID, normData, statePtr, store.ComponentMutationMeta{
		ChangeSummary: summary,
		SourceType:    "manual_edit",
	})
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, updated)
}

func (s *Server) deleteComponentEntity(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	entityID := chi.URLParam(r, "entityId")
	if err := s.Store.DeleteComponentEntity(r.Context(), entityID, u.ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}
