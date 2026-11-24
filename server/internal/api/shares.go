package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"nuan/internal/store"
)

func (s *Server) createConversationShare(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	var in struct {
		MessageIDs []string `json:"message_ids"`
		TTLDays    int      `json:"ttl_days"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	var ttlMs int64
	if in.TTLDays > 0 && in.TTLDays <= 365 {
		ttlMs = int64(in.TTLDays) * 24 * 60 * 60 * 1000
	} else {
		ttlMs = store.DefaultShareTTLMs
	}
	share, err := s.Store.CreateConversationShare(r.Context(), u.ID, convID, in.MessageIDs, ttlMs)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, share)
}

func (s *Server) listUserShares(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	items, err := s.Store.ListUserShares(r.Context(), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) revokeConversationShare(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := s.Store.RevokeConversationShare(r.Context(), chi.URLParam(r, "id"), u.ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) previewConversationShare(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	share, err := s.Store.GetShareByCode(r.Context(), code)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, share)
}

func (s *Server) importConversationShare(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := chi.URLParam(r, "code")
	conv, err := s.Store.ImportConversationShare(r.Context(), u.ID, code)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"conversation": conv})
}
