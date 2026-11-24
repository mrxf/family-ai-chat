package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"nuan/internal/store"
)

func (s *Server) listPinnedComponents(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListPinnedComponents(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) pinComponent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MessageID   string `json:"message_id"`
		ComponentID string `json:"component_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.MessageID = strings.TrimSpace(in.MessageID)
	in.ComponentID = strings.TrimSpace(in.ComponentID)
	if in.MessageID == "" || in.ComponentID == "" {
		writeErr(w, 400, "bad_request", "缺少消息或组件标识")
		return
	}
	pin, err := s.Store.PinComponent(r.Context(), currentUser(r).ID, in.MessageID, in.ComponentID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPinLimit):
			writeErr(w, 400, "pin_limit", "首页最多固定 20 张卡片，请先取消一些再固定")
		case errors.Is(err, store.ErrComponentNotPinnable):
			writeErr(w, 400, "not_pinnable", "这张卡片不支持固定到首页")
		default:
			s.writeStoreErr(w, r, err)
		}
		return
	}
	writeJSON(w, 200, pin)
}

func (s *Server) unpinComponent(w http.ResponseWriter, r *http.Request) {
	msgID := chi.URLParam(r, "messageId")
	compID := chi.URLParam(r, "compId")
	if err := s.Store.UnpinComponent(r.Context(), currentUser(r).ID, msgID, compID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) reorderPinnedComponents(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Store.ReorderPinnedComponents(r.Context(), currentUser(r).ID, in.IDs); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	items, err := s.Store.ListPinnedComponents(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
