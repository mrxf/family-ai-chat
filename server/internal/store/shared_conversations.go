package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ErrPrivateNotShareable is returned when attempting to share a private conversation.
var ErrPrivateNotShareable = errors.New("private conversation cannot be shared")

// ErrForbidden is returned when a user lacks permission for an action on a shared conversation.
var ErrForbidden = errors.New("forbidden")

// FamilyMember is the public profile of an active household user shown in share pickers.
type FamilyMember struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
}

// ConversationMember describes one participant in a shared conversation.
type ConversationMember struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar,omitempty"`
	Role        string `json:"role"` // "owner" | "member"
	AddedAt     int64  `json:"added_at,omitempty"`
}

// ListFamilyMembers returns all non-disabled users in this household instance,
// optionally excluding excludeUserID (typically the caller).
func (s *Store) ListFamilyMembers(ctx context.Context, excludeUserID string) ([]FamilyMember, error) {
	q := `SELECT id, username, display_name, avatar FROM users WHERE disabled = 0`
	var args []any
	if excludeUserID != "" {
		q += ` AND id <> ?`
		args = append(args, excludeUserID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FamilyMember{}
	for rows.Next() {
		var m FamilyMember
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.Avatar); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CanAccessConversation reports whether userID is the owner or a shared member of convID.
func (s *Store) CanAccessConversation(ctx context.Context, convID, userID string) (bool, error) {
	var ok int
	err := s.queryRow(ctx, `SELECT 1 FROM conversations c
		WHERE c.id = ? AND (c.user_id = ? OR EXISTS (
			SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = c.id AND cm.user_id = ?
		))`, convID, userID, userID).Scan(&ok)
	if err != nil {
		if notFound(err) == ErrNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsConversationShared reports whether convID currently has at least one shared member.
func (s *Store) IsConversationShared(ctx context.Context, convID string) (bool, error) {
	var n int
	err := s.queryRow(ctx, `SELECT COUNT(*) FROM conversation_members WHERE conversation_id = ?`, convID).Scan(&n)
	return n > 0, err
}

// ListConversationMembers returns the owner followed by all shared members for convID.
func (s *Store) ListConversationMembers(ctx context.Context, convID string) ([]ConversationMember, error) {
	var owner ConversationMember
	err := s.queryRow(ctx, `SELECT u.id, u.username, u.display_name, u.avatar, c.created_at
		FROM conversations c JOIN users u ON u.id = c.user_id WHERE c.id = ?`, convID).
		Scan(&owner.UserID, &owner.Username, &owner.DisplayName, &owner.Avatar, &owner.AddedAt)
	if err != nil {
		return nil, notFound(err)
	}
	owner.Role = "owner"

	rows, err := s.query(ctx, `SELECT u.id, u.username, u.display_name, u.avatar, cm.created_at
		FROM conversation_members cm JOIN users u ON u.id = cm.user_id
		WHERE cm.conversation_id = ? ORDER BY cm.created_at ASC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConversationMember{owner}
	for rows.Next() {
		var m ConversationMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.DisplayName, &m.Avatar, &m.AddedAt); err != nil {
			return nil, err
		}
		m.Role = "member"
		out = append(out, m)
	}
	return out, rows.Err()
}

// HydrateConversationsSharing enriches a slice of conversations with sharing metadata
// relative to viewerUserID (IsShared, IsOwner, OwnerID, OwnerName, MemberCount, Members).
func (s *Store) HydrateConversationsSharing(ctx context.Context, convs []*Conversation, viewerUserID string) error {
	if len(convs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(convs))
	byID := make(map[string]*Conversation, len(convs))
	ownerIDs := make(map[string]struct{}, len(convs))
	for _, c := range convs {
		if c == nil {
			continue
		}
		c.OwnerID = c.UserID
		c.IsOwner = c.UserID == viewerUserID
		c.IsShared = false
		c.MemberCount = 0
		c.Members = nil
		ids = append(ids, c.ID)
		byID[c.ID] = c
		ownerIDs[c.UserID] = struct{}{}
	}
	if len(ids) == 0 {
		return nil
	}

	// Fetch owner profiles.
	ownerMap := map[string]FamilyMember{}
	if len(ownerIDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ownerIDs)), ",")
		args := make([]any, 0, len(ownerIDs))
		for uid := range ownerIDs {
			args = append(args, uid)
		}
		rows, err := s.query(ctx, `SELECT id, username, display_name, avatar FROM users WHERE id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var u FamilyMember
			if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Avatar); err != nil {
				rows.Close()
				return err
			}
			ownerMap[u.ID] = u
		}
		rows.Close()
	}

	// Fetch all shared members for these conversations.
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.query(ctx, `SELECT cm.conversation_id, u.id, u.username, u.display_name, u.avatar, cm.created_at
		FROM conversation_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.conversation_id IN (`+ph+`)
		ORDER BY cm.created_at ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	membersByConv := map[string][]ConversationMember{}
	for rows.Next() {
		var convID string
		var m ConversationMember
		if err := rows.Scan(&convID, &m.UserID, &m.Username, &m.DisplayName, &m.Avatar, &m.AddedAt); err != nil {
			return err
		}
		m.Role = "member"
		membersByConv[convID] = append(membersByConv[convID], m)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, c := range convs {
		if c == nil {
			continue
		}
		owner := ownerMap[c.UserID]
		c.OwnerName = owner.DisplayName
		ms := membersByConv[c.ID]
		if len(ms) > 0 {
			c.IsShared = true
			full := make([]ConversationMember, 0, 1+len(ms))
			full = append(full, ConversationMember{
				UserID:      owner.ID,
				Username:    owner.Username,
				DisplayName: owner.DisplayName,
				Avatar:      owner.Avatar,
				Role:        "owner",
				AddedAt:     c.CreatedAt,
			})
			full = append(full, ms...)
			c.Members = full
			c.MemberCount = len(full)
		}
	}
	return nil
}

// AddConversationMembers adds one or more household users to convID. Only the owner can add members.
// Also grants new members read access to any file attachments already in the conversation.
func (s *Store) AddConversationMembers(ctx context.Context, convID, ownerID string, memberIDs []string) (*Conversation, error) {
	err := s.tx(ctx, func(tx *Tx) error {
		var dbOwner string
		var isPrivate bool
		if err := tx.queryRow(ctx, `SELECT user_id, private FROM conversations WHERE id = ?`+s.forUpdate(), convID).Scan(&dbOwner, &isPrivate); err != nil {
			return notFound(err)
		}
		if dbOwner != ownerID {
			return ErrForbidden
		}
		if isPrivate {
			return ErrPrivateNotShareable
		}

		// Filter valid active non-owner user IDs.
		cleanIDs := make([]string, 0, len(memberIDs))
		seen := map[string]bool{ownerID: true}
		for _, id := range memberIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			var disabled bool
			if err := tx.queryRow(ctx, `SELECT disabled FROM users WHERE id = ?`, id).Scan(&disabled); err != nil {
				continue
			}
			if disabled {
				continue
			}
			seen[id] = true
			cleanIDs = append(cleanIDs, id)
		}
		if len(cleanIDs) == 0 {
			return nil
		}

		now := Now()
		for _, uid := range cleanIDs {
			var exists int
			if err := tx.queryRow(ctx, `SELECT 1 FROM conversation_members WHERE conversation_id = ? AND user_id = ?`, convID, uid).Scan(&exists); err == nil {
				continue
			}
			if _, err := tx.exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id, role, added_by, created_at) VALUES (?,?,?,?,?)`,
				convID, uid, "member", ownerID, now); err != nil {
				return err
			}
		}

		// Grant read access to existing attachment files in this conversation.
		fileIDs, err := collectConversationFileIDsTx(ctx, tx, convID)
		if err != nil {
			return err
		}
		for _, fid := range fileIDs {
			for _, uid := range cleanIDs {
				if err := grantFileTx(ctx, tx, fid, uid, now); err != nil {
					return err
				}
			}
		}

		_, err = tx.exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now, convID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetConversation(ctx, convID, ownerID)
}

// RemoveConversationMember removes targetUserID from convID.
// The owner may remove any member; a member may only remove themselves (leave).
func (s *Store) RemoveConversationMember(ctx context.Context, convID, actorID, targetUserID string) error {
	return s.tx(ctx, func(tx *Tx) error {
		var dbOwner string
		if err := tx.queryRow(ctx, `SELECT user_id FROM conversations WHERE id = ?`+s.forUpdate(), convID).Scan(&dbOwner); err != nil {
			return notFound(err)
		}
		if targetUserID == dbOwner {
			return ErrForbidden
		}
		if actorID != dbOwner && actorID != targetUserID {
			return ErrForbidden
		}
		if err := affected(tx.exec(ctx, `DELETE FROM conversation_members WHERE conversation_id = ? AND user_id = ?`, convID, targetUserID)); err != nil {
			return err
		}
		_, err := tx.exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, Now(), convID)
		return err
	})
}

// RevokeAllConversationMembers removes every shared member from convID, turning it back into a private-to-owner conversation.
func (s *Store) RevokeAllConversationMembers(ctx context.Context, convID, ownerID string) error {
	return s.tx(ctx, func(tx *Tx) error {
		var dbOwner string
		if err := tx.queryRow(ctx, `SELECT user_id FROM conversations WHERE id = ?`+s.forUpdate(), convID).Scan(&dbOwner); err != nil {
			return notFound(err)
		}
		if dbOwner != ownerID {
			return ErrForbidden
		}
		if _, err := tx.exec(ctx, `DELETE FROM conversation_members WHERE conversation_id = ?`, convID); err != nil {
			return err
		}
		_, err := tx.exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, Now(), convID)
		return err
	})
}

// ListOwnedSharedConversations returns all conversations owned by ownerID that currently have at least one shared member.
func (s *Store) ListOwnedSharedConversations(ctx context.Context, ownerID string) ([]*Conversation, error) {
	rows, err := s.query(ctx, `SELECT `+prefixCols(convCols, "c.")+` FROM conversations c
		WHERE c.user_id = ? AND c.private = 0
		  AND EXISTS (SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = c.id)
		ORDER BY c.updated_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Conversation{}
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.HydrateConversationsSharing(ctx, out, ownerID); err != nil {
		return nil, err
	}
	return out, nil
}

// GrantFilesToConversationMembers grants every member (and owner) of convID access to fileIDs.
func (s *Store) GrantFilesToConversationMembers(ctx context.Context, convID string, fileIDs []string) error {
	if convID == "" || len(fileIDs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *Tx) error {
		var ownerID string
		if err := tx.queryRow(ctx, `SELECT user_id FROM conversations WHERE id = ?`, convID).Scan(&ownerID); err != nil {
			return nil
		}
		rows, err := tx.query(ctx, `SELECT user_id FROM conversation_members WHERE conversation_id = ?`, convID)
		if err != nil {
			return err
		}
		users := []string{ownerID}
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err == nil {
				users = append(users, uid)
			}
		}
		rows.Close()
		if len(users) <= 1 {
			return nil
		}
		now := Now()
		for _, fid := range fileIDs {
			if fid == "" {
				continue
			}
			for _, uid := range users {
				if err := grantFileTx(ctx, tx, fid, uid, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func collectConversationFileIDsTx(ctx context.Context, tx *Tx, convID string) ([]string, error) {
	rows, err := tx.query(ctx, `SELECT attachments FROM messages WHERE conversation_id = ? AND attachments <> '[]'`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var atts []Attachment
		_ = json.Unmarshal([]byte(raw), &atts)
		for _, a := range atts {
			if a.FileID != "" && !seen[a.FileID] {
				seen[a.FileID] = true
				out = append(out, a.FileID)
			}
		}
	}
	return out, rows.Err()
}

func grantFileTx(ctx context.Context, tx *Tx, fileID, userID string, now int64) error {
	if fileID == "" || userID == "" {
		return nil
	}
	var exists int
	if err := tx.queryRow(ctx, `SELECT 1 FROM file_grants WHERE file_id = ? AND user_id = ?`, fileID, userID).Scan(&exists); err == nil {
		return nil
	}
	_, err := tx.exec(ctx, `INSERT INTO file_grants (file_id, user_id, created_at) VALUES (?,?,?)`, fileID, userID, now)
	if isUnique(err) {
		return nil
	}
	return err
}

// HydrateMessageSenders populates SenderName and SenderAvatar for messages that have a SenderUserID
// (or falls back to the conversation owner when SenderUserID is empty on legacy user messages).
func (s *Store) HydrateMessageSenders(ctx context.Context, msgs []*Message) error {
	if len(msgs) == 0 {
		return nil
	}
	uidSet := map[string]struct{}{}
	convOwnerNeeded := map[string]struct{}{}
	for _, m := range msgs {
		if m == nil || m.Role != "user" {
			continue
		}
		if m.SenderUserID != "" {
			uidSet[m.SenderUserID] = struct{}{}
		} else if m.ConversationID != "" {
			convOwnerNeeded[m.ConversationID] = struct{}{}
		}
	}

	convOwners := map[string]string{}
	if len(convOwnerNeeded) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(convOwnerNeeded)), ",")
		args := make([]any, 0, len(convOwnerNeeded))
		for cid := range convOwnerNeeded {
			args = append(args, cid)
		}
		rows, err := s.query(ctx, `SELECT id, user_id FROM conversations WHERE id IN (`+ph+`)`, args...)
		if err == nil {
			for rows.Next() {
				var cid, uid string
				if rows.Scan(&cid, &uid) == nil {
					convOwners[cid] = uid
					uidSet[uid] = struct{}{}
				}
			}
			rows.Close()
		}
	}

	if len(uidSet) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(uidSet)), ",")
	args := make([]any, 0, len(uidSet))
	for uid := range uidSet {
		args = append(args, uid)
	}
	rows, err := s.query(ctx, `SELECT id, display_name, avatar FROM users WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	profiles := map[string]struct{ name, avatar string }{}
	for rows.Next() {
		var id, name, avatar string
		if err := rows.Scan(&id, &name, &avatar); err != nil {
			return err
		}
		profiles[id] = struct{ name, avatar string }{name: name, avatar: avatar}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range msgs {
		if m == nil || m.Role != "user" {
			continue
		}
		uid := m.SenderUserID
		if uid == "" {
			uid = convOwners[m.ConversationID]
			m.SenderUserID = uid
		}
		if p, ok := profiles[uid]; ok {
			m.SenderName = p.name
			m.SenderAvatar = p.avatar
		}
	}
	return nil
}

// DeleteMessagesFromConversation deletes one or more messages from convID.
// The conversation owner may delete any message; a shared member may delete their own user messages
// (and an assistant message immediately following their own user message).
func (s *Store) DeleteMessagesFromConversation(ctx context.Context, convID, actorID string, msgIDs []string) (int, error) {
	if len(msgIDs) == 0 {
		return 0, nil
	}
	deleted := 0
	err := s.tx(ctx, func(tx *Tx) error {
		var ownerID string
		if err := tx.queryRow(ctx, `SELECT user_id FROM conversations WHERE id = ?`+s.forUpdate(), convID).Scan(&ownerID); err != nil {
			return notFound(err)
		}
		isOwner := ownerID == actorID
		if !isOwner {
			var member int
			if err := tx.queryRow(ctx, `SELECT 1 FROM conversation_members WHERE conversation_id = ? AND user_id = ?`, convID, actorID).Scan(&member); err != nil {
				return ErrNotFound
			}
		}

		// Load ordered message IDs + role + sender for permission validation.
		rows, err := tx.query(ctx, `SELECT id, role, sender_user_id FROM messages WHERE conversation_id = ? ORDER BY id ASC`, convID)
		if err != nil {
			return err
		}
		type brief struct {
			id, role, sender string
		}
		var ordered []brief
		for rows.Next() {
			var b brief
			if err := rows.Scan(&b.id, &b.role, &b.sender); err != nil {
				rows.Close()
				return err
			}
			ordered = append(ordered, b)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		want := make(map[string]bool, len(msgIDs))
		for _, id := range msgIDs {
			want[id] = true
		}

		allowed := make([]string, 0, len(msgIDs))
		for i, b := range ordered {
			if !want[b.id] {
				continue
			}
			if isOwner {
				allowed = append(allowed, b.id)
				continue
			}
			sender := b.sender
			if sender == "" && b.role == "user" {
				sender = ownerID
			}
			if b.role == "user" && sender == actorID {
				allowed = append(allowed, b.id)
				continue
			}
			if b.role == "assistant" && i > 0 {
				prev := ordered[i-1]
				prevSender := prev.sender
				if prevSender == "" && prev.role == "user" {
					prevSender = ownerID
				}
				if prev.role == "user" && prevSender == actorID {
					allowed = append(allowed, b.id)
					continue
				}
			}
			return ErrForbidden
		}
		if len(allowed) == 0 {
			return ErrNotFound
		}

		ph := strings.TrimSuffix(strings.Repeat("?,", len(allowed)), ",")
		args := make([]any, 0, len(allowed)+1)
		args = append(args, convID)
		for _, id := range allowed {
			args = append(args, id)
		}
		res, err := tx.exec(ctx, `DELETE FROM messages WHERE conversation_id = ? AND id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		deleted = int(n)
		// Clear compaction summary so subsequent turns rebuild accurately after deletions.
		if _, err := tx.exec(ctx, `UPDATE conversations SET summary = '', summary_cursor = '', updated_at = ? WHERE id = ?`, Now(), convID); err != nil {
			return err
		}
		return nil
	})
	return deleted, err
}
