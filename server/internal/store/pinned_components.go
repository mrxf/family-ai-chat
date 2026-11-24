package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// ErrPinLimit is returned when a user exceeds the maximum number of pinned components.
var ErrPinLimit = errors.New("pinned component limit reached")

// ErrComponentNotPinnable is returned when a component kind or conversation cannot be pinned.
var ErrComponentNotPinnable = errors.New("component cannot be pinned")

const MaxPinnedComponents = 20

// PinnedComponent represents a message component pinned to the user's home board.
// The component payload and mutable state are always resolved live from messages.components.
type PinnedComponent struct {
	ID                string           `json:"id"`
	ConversationID    string           `json:"conversation_id"`
	ConversationTitle string           `json:"conversation_title"`
	MessageID         string           `json:"message_id"`
	ComponentID       string           `json:"component_id"`
	SortOrder         int              `json:"sort_order"`
	PinnedAt          int64            `json:"pinned_at"`
	Component         MessageComponent `json:"component"`
}

// QuotedComponent stores an immutable snapshot of a component referenced when starting a chat.
type QuotedComponent struct {
	ConversationID    string          `json:"conversation_id,omitempty"`
	ConversationTitle string          `json:"conversation_title,omitempty"`
	MessageID         string          `json:"message_id,omitempty"`
	ComponentID       string          `json:"component_id,omitempty"`
	Kind              string          `json:"kind"`
	Title             string          `json:"title"`
	Summary           string          `json:"summary,omitempty"`
	Data              json.RawMessage `json:"data,omitempty"`
	State             json.RawMessage `json:"state,omitempty"`
}

// ListPinnedComponents returns all pinned components for userID ordered by sort_order ASC, pinned_at DESC.
func (s *Store) ListPinnedComponents(ctx context.Context, userID string) ([]*PinnedComponent, error) {
	rows, err := s.query(ctx, `SELECT p.id, p.conversation_id, c.title, p.message_id, p.component_id, p.sort_order, p.pinned_at, m.components
		FROM pinned_components p
		JOIN messages m ON m.id = p.message_id
		JOIN conversations c ON c.id = p.conversation_id
		WHERE p.user_id = ? AND c.user_id = ? AND c.private = 0
		ORDER BY p.sort_order ASC, p.pinned_at DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*PinnedComponent{}
	for rows.Next() {
		var p PinnedComponent
		var rawComps string
		if err := rows.Scan(&p.ID, &p.ConversationID, &p.ConversationTitle, &p.MessageID, &p.ComponentID, &p.SortOrder, &p.PinnedAt, &rawComps); err != nil {
			return nil, err
		}
		var comps []MessageComponent
		if json.Unmarshal([]byte(rawComps), &comps) != nil {
			continue
		}
		found := false
		for _, c := range comps {
			if c.ID == p.ComponentID {
				p.Component = c
				found = true
				break
			}
		}
		if !found {
			continue
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// PinComponent pins a single component from a non-private message to the user's home board.
// Pinning an already-pinned component is idempotent and returns the existing pin with live component data.
func (s *Store) PinComponent(ctx context.Context, userID, messageID, componentID string) (*PinnedComponent, error) {
	var result *PinnedComponent
	err := s.tx(ctx, func(tx *Tx) error {
		var convID, convTitle, rawComps string
		var isPrivate bool
		err := tx.queryRow(ctx, `SELECT m.conversation_id, c.title, c.private, m.components
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.id = ? AND c.user_id = ?`, messageID, userID).
			Scan(&convID, &convTitle, &isPrivate, &rawComps)
		if err != nil {
			return notFound(err)
		}
		if isPrivate {
			return ErrComponentNotPinnable
		}
		var comps []MessageComponent
		if json.Unmarshal([]byte(rawComps), &comps) != nil {
			return ErrNotFound
		}
		var target *MessageComponent
		for i := range comps {
			if comps[i].ID == componentID {
				cp := comps[i]
				target = &cp
				break
			}
		}
		if target == nil {
			return ErrNotFound
		}
		if target.Kind == "ask_user" || target.Kind == "app_action" {
			return ErrComponentNotPinnable
		}

		// Idempotent: return existing pin if already pinned.
		var existing PinnedComponent
		err = tx.queryRow(ctx, `SELECT id, sort_order, pinned_at FROM pinned_components
			WHERE user_id = ? AND message_id = ? AND component_id = ?`, userID, messageID, componentID).
			Scan(&existing.ID, &existing.SortOrder, &existing.PinnedAt)
		if err == nil {
			existing.ConversationID = convID
			existing.ConversationTitle = convTitle
			existing.MessageID = messageID
			existing.ComponentID = componentID
			existing.Component = *target
			result = &existing
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}

		var count int
		if err := tx.queryRow(ctx, `SELECT COUNT(*) FROM pinned_components WHERE user_id = ?`, userID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxPinnedComponents {
			return ErrPinLimit
		}

		pin := &PinnedComponent{
			ID:                NewID(),
			ConversationID:    convID,
			ConversationTitle: convTitle,
			MessageID:         messageID,
			ComponentID:       componentID,
			SortOrder:         0,
			PinnedAt:          Now(),
			Component:         *target,
		}
		if _, err := tx.exec(ctx, `INSERT INTO pinned_components (id, user_id, conversation_id, message_id, component_id, sort_order, pinned_at)
			VALUES (?,?,?,?,?,?,?)`,
			pin.ID, userID, pin.ConversationID, pin.MessageID, pin.ComponentID, pin.SortOrder, pin.PinnedAt); err != nil {
			return err
		}
		result = pin
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// UnpinComponent removes a component pin for userID.
func (s *Store) UnpinComponent(ctx context.Context, userID, messageID, componentID string) error {
	_, err := s.exec(ctx, `DELETE FROM pinned_components WHERE user_id = ? AND message_id = ? AND component_id = ?`,
		userID, messageID, componentID)
	return err
}

// ReorderPinnedComponents updates sort_order on pinned components owned by userID according to pinIDs.
func (s *Store) ReorderPinnedComponents(ctx context.Context, userID string, pinIDs []string) error {
	return s.tx(ctx, func(tx *Tx) error {
		for idx, id := range pinIDs {
			if _, err := tx.exec(ctx, `UPDATE pinned_components SET sort_order = ? WHERE id = ? AND user_id = ?`,
				idx+1, id, userID); err != nil {
				return err
			}
		}
		return nil
	})
}
