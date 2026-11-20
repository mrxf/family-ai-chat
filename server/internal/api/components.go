package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) updateMessageComponent(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	msgID := chi.URLParam(r, "id")
	compID := chi.URLParam(r, "compId")

	var in struct {
		State json.RawMessage `json:"state"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.State) == 0 || len(in.State) > 16<<10 {
		writeErr(w, 400, "bad_state", "组件状态数据格式不正确或过大")
		return
	}
	var sanity map[string]any
	if json.Unmarshal(in.State, &sanity) != nil || sanity == nil {
		writeErr(w, 400, "bad_state", "组件状态必须是 JSON 对象")
		return
	}

	msg, comp, err := s.Store.UpdateMessageComponentState(r.Context(), msgID, u.ID, compID, in.State)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if run := s.Chat.Hub.Get(msgID); run != nil && run.UserID == u.ID {
		run.UpdateComponentState(compID, in.State)
	}
	writeJSON(w, 200, map[string]any{
		"component": comp,
		"message":   msg,
	})
}
