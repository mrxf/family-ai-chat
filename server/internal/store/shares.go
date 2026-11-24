package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
)

// DefaultShareTTLMs is 30 days in milliseconds.
const DefaultShareTTLMs int64 = 30 * 24 * 60 * 60 * 1000

var (
	ErrShareExpired = errors.New("share link has expired")
	ErrShareRevoked = errors.New("share link has been revoked")
)

// ShareSnapshotMessage is a frozen message stored inside a short-code share snapshot.
// All components have their latest data/state baked in and EntityID stripped so imported
// copies never sync back to the creator.
type ShareSnapshotMessage struct {
	Role        string             `json:"role"`
	Content     string             `json:"content"`
	Attachments []Attachment       `json:"attachments,omitempty"`
	Sources     []WebSource        `json:"sources,omitempty"`
	Components  []MessageComponent `json:"components,omitempty"`
	ModelID     *string            `json:"model_id,omitempty"`
	CreatedAt   int64              `json:"created_at"`
}

// ConversationShare is the persisted short-code snapshot record.
type ConversationShare struct {
	ID                   string  `json:"id"`
	Code                 string  `json:"code"`
	CreatorUserID        string  `json:"creator_user_id"`
	CreatorName          string  `json:"creator_name,omitempty"`
	CreatorAvatar        string  `json:"creator_avatar,omitempty"`
	SourceConversationID string  `json:"source_conversation_id,omitempty"`
	Title                string  `json:"title"`
	AssistantID          *string `json:"assistant_id,omitempty"`
	ModelID              *string `json:"model_id,omitempty"`
	MessageCount         int     `json:"message_count"`
	HasComponents        bool    `json:"has_components,omitempty"`
	ExpiresAt            int64   `json:"expires_at"`
	RevokedAt            int64   `json:"revoked_at,omitempty"`
	CreatedAt            int64   `json:"created_at"`
	snapshotJSON         string
}

const shareCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func generateShareCode() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	out := make([]byte, 8)
	for i, b := range buf {
		out[i] = shareCodeAlphabet[int(b)%len(shareCodeAlphabet)]
	}
	return string(out)
}

// CreateConversationShare freezes all (or selected) messages from convID into an immutable snapshot.
// Components are hydrated to their latest version at share time and detached from live entities.
func (s *Store) CreateConversationShare(ctx context.Context, userID, convID string, messageIDs []string, ttlMs int64) (*ConversationShare, error) {
	conv, err := s.GetConversation(ctx, convID, userID)
	if err != nil {
		return nil, err
	}
	if conv.Private {
		return nil, ErrPrivateNotShareable
	}
	allMsgs, err := s.ListMessages(ctx, convID, "", 500)
	if err != nil {
		return nil, err
	}

	selected := allMsgs
	if len(messageIDs) > 0 {
		allow := make(map[string]bool, len(messageIDs))
		for _, id := range messageIDs {
			allow[id] = true
		}
		filtered := make([]*Message, 0, len(messageIDs))
		for _, m := range allMsgs {
			if allow[m.ID] {
				filtered = append(filtered, m)
			}
		}
		selected = filtered
	}

	snapMsgs := make([]ShareSnapshotMessage, 0, len(selected))
	hasComps := false
	for _, m := range selected {
		if m.Status == StatusStreaming {
			continue
		}
		if strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 && len(m.Components) == 0 {
			continue
		}
		var frozenComps []MessageComponent
		for _, c := range m.Components {
			if c.Deleted {
				continue
			}
			// Always bake in the canonical entity's LATEST data & state at share time,
			// then detach EntityID so the imported copy never syncs back.
			latestData := c.Data
			latestState := c.State
			if c.EntityID != "" {
				if ent, eErr := s.GetComponentEntity(ctx, c.EntityID, userID); eErr == nil && ent != nil && !ent.Deleted {
					if len(ent.Data) > 0 {
						latestData = ent.Data
					}
					if len(ent.State) > 0 {
						latestState = ent.State
					}
				}
			}
			cp := MessageComponent{
				ID:    c.ID,
				Kind:  c.Kind,
				Pos:   c.Pos,
				Data:  append(json.RawMessage(nil), latestData...),
				State: append(json.RawMessage(nil), latestState...),
			}
			frozenComps = append(frozenComps, cp)
			hasComps = true
		}
		snapMsgs = append(snapMsgs, ShareSnapshotMessage{
			Role:        m.Role,
			Content:     m.Content,
			Attachments: m.Attachments,
			Sources:     m.Sources,
			Components:  frozenComps,
			ModelID:     m.ModelID,
			CreatedAt:   m.CreatedAt,
		})
	}
	if len(snapMsgs) == 0 {
		return nil, ErrNotFound
	}

	rawSnap, err := json.Marshal(snapMsgs)
	if err != nil {
		return nil, err
	}

	now := Now()
	if ttlMs <= 0 {
		ttlMs = DefaultShareTTLMs
	}
	title := strings.TrimSpace(conv.Title)
	if title == "" {
		title = "分享的对话"
	}

	var share *ConversationShare
	for attempt := 0; attempt < 5; attempt++ {
		code := generateShareCode()
		rec := &ConversationShare{
			ID:                   NewID(),
			Code:                 code,
			CreatorUserID:        userID,
			SourceConversationID: conv.ID,
			Title:                title,
			AssistantID:          conv.AssistantID,
			ModelID:              conv.ModelID,
			MessageCount:         len(snapMsgs),
			HasComponents:        hasComps,
			ExpiresAt:            now + ttlMs,
			CreatedAt:            now,
			snapshotJSON:         string(rawSnap),
		}
		_, err := s.exec(ctx, `INSERT INTO conversation_shares (
			id, code, creator_user_id, source_conversation_id, title, assistant_id, model_id,
			snapshot_json, message_count, expires_at, revoked_at, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			rec.ID, rec.Code, rec.CreatorUserID, rec.SourceConversationID, rec.Title,
			rec.AssistantID, rec.ModelID, rec.snapshotJSON, rec.MessageCount, rec.ExpiresAt, int64(0), rec.CreatedAt)
		if isUnique(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		share = rec
		break
	}
	if share == nil {
		return nil, ErrConflict
	}
	if u, err := s.GetUser(ctx, userID); err == nil {
		share.CreatorName = u.DisplayName
		share.CreatorAvatar = u.Avatar
	}
	return share, nil
}

// GetShareByCode returns preview metadata for a valid (non-revoked, non-expired) share code.
func (s *Store) GetShareByCode(ctx context.Context, code string) (*ConversationShare, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrNotFound
	}
	var rec ConversationShare
	err := s.queryRow(ctx, `SELECT s.id, s.code, s.creator_user_id, u.display_name, u.avatar,
		s.source_conversation_id, s.title, s.assistant_id, s.model_id,
		s.snapshot_json, s.message_count, s.expires_at, s.revoked_at, s.created_at
		FROM conversation_shares s
		JOIN users u ON u.id = s.creator_user_id
		WHERE s.code = ?`, code).Scan(
		&rec.ID, &rec.Code, &rec.CreatorUserID, &rec.CreatorName, &rec.CreatorAvatar,
		&rec.SourceConversationID, &rec.Title, &rec.AssistantID, &rec.ModelID,
		&rec.snapshotJSON, &rec.MessageCount, &rec.ExpiresAt, &rec.RevokedAt, &rec.CreatedAt,
	)
	if err != nil {
		return nil, notFound(err)
	}
	if rec.RevokedAt > 0 {
		return nil, ErrShareRevoked
	}
	if rec.ExpiresAt > 0 && Now() > rec.ExpiresAt {
		return nil, ErrShareExpired
	}
	var msgs []ShareSnapshotMessage
	if json.Unmarshal([]byte(rec.snapshotJSON), &msgs) == nil {
		for _, m := range msgs {
			if len(m.Components) > 0 {
				rec.HasComponents = true
				break
			}
		}
	}
	return &rec, nil
}

// ImportConversationShare clones the share snapshot into a standalone personal conversation owned by userID.
func (s *Store) ImportConversationShare(ctx context.Context, userID, code string) (*Conversation, error) {
	share, err := s.GetShareByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	var snapMsgs []ShareSnapshotMessage
	if err := json.Unmarshal([]byte(share.snapshotJSON), &snapMsgs); err != nil {
		return nil, err
	}

	now := Now()
	conv := &Conversation{
		ID:          NewID(),
		UserID:      userID,
		AssistantID: share.AssistantID,
		ModelID:     share.ModelID,
		Title:       share.Title,
		Pinned:      false,
		Private:     false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.exec(ctx, `INSERT INTO conversations (`+convCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			conv.ID, conv.UserID, conv.AssistantID, conv.ModelID, conv.AutoMode, conv.Title, conv.Pinned, conv.Private, conv.CreatedAt, conv.UpdatedAt); err != nil {
			return err
		}
		for i, sm := range snapMsgs {
			sender := ""
			if sm.Role == "user" {
				sender = userID
			}
			m := &Message{
				ID:             NewID(),
				ConversationID: conv.ID,
				SenderUserID:   sender,
				Role:           sm.Role,
				Content:        sm.Content,
				Attachments:    sm.Attachments,
				Sources:        sm.Sources,
				Components:     sm.Components,
				Status:         StatusDone,
				ModelID:        sm.ModelID,
				CreatedAt:      now + int64(i),
				UpdatedAt:      now + int64(i),
			}
			if err := insertMsg(ctx, tx, m); err != nil {
				return err
			}
			for _, a := range sm.Attachments {
				if a.FileID != "" {
					if err := grantFileTx(ctx, tx, a.FileID, userID, now); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetConversation(ctx, conv.ID, userID)
}

// ListUserShares lists active and recently created short-code shares authored by userID.
func (s *Store) ListUserShares(ctx context.Context, userID string) ([]*ConversationShare, error) {
	rows, err := s.query(ctx, `SELECT id, code, creator_user_id, source_conversation_id, title,
		assistant_id, model_id, message_count, expires_at, revoked_at, created_at
		FROM conversation_shares
		WHERE creator_user_id = ? AND revoked_at = 0
		ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ConversationShare{}
	for rows.Next() {
		var rec ConversationShare
		if err := rows.Scan(&rec.ID, &rec.Code, &rec.CreatorUserID, &rec.SourceConversationID, &rec.Title,
			&rec.AssistantID, &rec.ModelID, &rec.MessageCount, &rec.ExpiresAt, &rec.RevokedAt, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &rec)
	}
	return out, rows.Err()
}

// RevokeConversationShare marks a short-code share as revoked so it can no longer be imported.
func (s *Store) RevokeConversationShare(ctx context.Context, shareID, userID string) error {
	return affected(s.exec(ctx, `UPDATE conversation_shares SET revoked_at = ? WHERE id = ? AND creator_user_id = ? AND revoked_at = 0`,
		Now(), shareID, userID))
}
