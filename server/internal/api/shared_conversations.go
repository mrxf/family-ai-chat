package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) listFamilyMembers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	members, err := s.Store.ListFamilyMembers(r.Context(), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": members})
}

func (s *Server) listOwnedSharedConversations(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	items, err := s.Store.ListOwnedSharedConversations(r.Context(), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) addConversationMembers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	var in struct {
		UserIDs []string `json:"user_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.UserIDs) == 0 {
		writeErr(w, 400, "bad_request", "请选择至少一位家人")
		return
	}
	conv, err := s.Store.AddConversationMembers(r.Context(), convID, u.ID, in.UserIDs)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"conversation": conv})
}

func (s *Server) removeConversationMember(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	targetID := chi.URLParam(r, "userId")
	if err := s.Store.RemoveConversationMember(r.Context(), convID, u.ID, targetID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	// If the owner is still a participant, return the updated conversation snapshot.
	conv, err := s.Store.GetConversation(r.Context(), convID, u.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "conversation": conv})
}

func (s *Server) revokeAllConversationMembers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	if err := s.Store.RevokeAllConversationMembers(r.Context(), convID, u.ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	conv, err := s.Store.GetConversation(r.Context(), convID, u.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "conversation": conv})
}

func (s *Server) deleteConversationMessage(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	msgID := chi.URLParam(r, "msgId")
	n, err := s.Store.DeleteMessagesFromConversation(r.Context(), convID, u.ID, []string{msgID})
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

func (s *Server) deleteConversationMessages(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	var in struct {
		MessageIDs []string `json:"message_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	n, err := s.Store.DeleteMessagesFromConversation(r.Context(), convID, u.ID, in.MessageIDs)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// syncConversation is a lightweight polling endpoint for active shared conversations.
// If ?since=<updated_at> matches and there is no active run, it returns {changed: false}.
func (s *Server) syncConversation(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	convID := chi.URLParam(r, "id")
	c, err := s.Store.GetConversation(r.Context(), convID, u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	active := ""
	activeUserID := ""
	activeUserName := ""
	if run := s.Chat.Hub.ActiveInConversation(c.ID); run != nil {
		active = run.MessageID
		activeUserID = run.UserID
		activeUserName = run.UserName
	}
	since, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("since")), 10, 64)
	if since > 0 && c.UpdatedAt <= since && active == "" {
		writeJSON(w, 200, map[string]any{
			"changed":          false,
			"updated_at":       c.UpdatedAt,
			"active_message_id": "",
			"active_user_id":   "",
			"active_user_name": "",
		})
		return
	}
	const page = 60
	msgs, err := s.Store.ListMessages(r.Context(), c.ID, "", page+1)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	hasMore := len(msgs) > page
	if hasMore {
		msgs = msgs[1:]
	}
	writeJSON(w, 200, map[string]any{
		"changed":           true,
		"conversation":      c,
		"messages":          msgs,
		"has_more":          hasMore,
		"updated_at":        c.UpdatedAt,
		"active_message_id": active,
		"active_user_id":    activeUserID,
		"active_user_name":  activeUserName,
	})
}
