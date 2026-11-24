package store

import (
	"context"
	"encoding/json"
)

// ComponentRecord describes a live component along with its owning message and pin metadata.
type ComponentRecord struct {
	MessageID         string           `json:"message_id"`
	ComponentID       string           `json:"component_id"`
	ConversationID    string           `json:"conversation_id"`
	ConversationTitle string           `json:"conversation_title"`
	Pinned            bool             `json:"pinned"`
	PinID             string           `json:"pin_id,omitempty"`
	UpdatedAt         int64            `json:"updated_at"`
	Component         MessageComponent `json:"component"`
}

// SetComponents saves a streaming message's interactive UI components as soon as they exist.
func (s *Store) SetComponents(ctx context.Context, messageID string, components []MessageComponent) error {
	if components == nil {
		components = []MessageComponent{}
	}
	raw, _ := json.Marshal(components)
	_, err := s.exec(ctx, `UPDATE messages SET components=?, updated_at=? WHERE id=?`, string(raw), Now(), messageID)
	return err
}

// GetMessageComponent returns one component from a message owned by userID.
func (s *Store) GetMessageComponent(ctx context.Context, messageID, userID, compID string) (*Message, *MessageComponent, error) {
	m, err := s.GetMessageForUser(ctx, messageID, userID)
	if err != nil {
		return nil, nil, err
	}
	for i := range m.Components {
		if m.Components[i].ID == compID {
			cp := m.Components[i]
			return m, &cp, nil
		}
	}
	return nil, nil, ErrNotFound
}

// UpdateMessageComponentState updates the mutable user interaction state of a single
// component inside a message belonging to userID (e.g. toggling a todo checkbox,
// selecting a quiz option, or submitting an ask_user answer).
func (s *Store) UpdateMessageComponentState(ctx context.Context, messageID, userID, compID string, state json.RawMessage) (*Message, *MessageComponent, error) {
	return s.UpdateMessageComponent(ctx, messageID, userID, compID, nil, &state)
}

// UpdateMessageComponent updates the data and/or state of one component inside a message owned by userID.
// Passing nil for data or state leaves that field untouched.
func (s *Store) UpdateMessageComponent(ctx context.Context, messageID, userID, compID string, data json.RawMessage, state *json.RawMessage) (*Message, *MessageComponent, error) {
	var updatedMsg *Message
	var updatedComp *MessageComponent
	err := s.tx(ctx, func(tx *Tx) error {
		q := `SELECT ` + prefixCols(msgCols, "m.") + ` FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.id = ? AND c.user_id = ?` + s.forUpdate()
		m, err := scanMsg(tx.queryRow(ctx, q, messageID, userID))
		if err != nil {
			return err
		}
		idx := -1
		for i := range m.Components {
			if m.Components[i].ID == compID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrNotFound
		}
		if len(data) > 0 {
			m.Components[idx].Data = append(json.RawMessage(nil), data...)
		}
		if state != nil {
			m.Components[idx].State = append(json.RawMessage(nil), (*state)...)
		}
		m.UpdatedAt = Now()
		raw, err := json.Marshal(m.Components)
		if err != nil {
			return err
		}
		if _, err := tx.exec(ctx, `UPDATE messages SET components = ?, updated_at = ? WHERE id = ?`, string(raw), m.UpdatedAt, m.ID); err != nil {
			return err
		}
		cp := m.Components[idx]
		updatedComp = &cp
		updatedMsg = m
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return updatedMsg, updatedComp, nil
}

// DeleteMessageComponent removes one component from a message owned by userID
// and unpins it from the home board if pinned.
func (s *Store) DeleteMessageComponent(ctx context.Context, messageID, userID, compID string) error {
	return s.tx(ctx, func(tx *Tx) error {
		q := `SELECT ` + prefixCols(msgCols, "m.") + ` FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.id = ? AND c.user_id = ?` + s.forUpdate()
		m, err := scanMsg(tx.queryRow(ctx, q, messageID, userID))
		if err != nil {
			return err
		}
		idx := -1
		for i := range m.Components {
			if m.Components[i].ID == compID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrNotFound
		}
		next := append(m.Components[:idx], m.Components[idx+1:]...)
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if _, err := tx.exec(ctx, `UPDATE messages SET components = ?, updated_at = ? WHERE id = ?`, string(raw), Now(), m.ID); err != nil {
			return err
		}
		if _, err := tx.exec(ctx, `DELETE FROM pinned_components WHERE user_id = ? AND message_id = ? AND component_id = ?`, userID, messageID, compID); err != nil {
			return err
		}
		return nil
	})
}

// ListUserComponents returns the user's pinned home components followed by recent components
// from convID (or across non-private conversations when includeRecent is true), deduplicated.
func (s *Store) ListUserComponents(ctx context.Context, userID, convID string, includeRecent bool, limit int) ([]ComponentRecord, error) {
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	pins, err := s.ListPinnedComponents(ctx, userID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(pins))
	out := make([]ComponentRecord, 0, min(limit, len(pins)+8))
	for _, p := range pins {
		key := p.MessageID + ":" + p.ComponentID
		seen[key] = true
		out = append(out, ComponentRecord{
			MessageID:         p.MessageID,
			ComponentID:       p.ComponentID,
			ConversationID:    p.ConversationID,
			ConversationTitle: p.ConversationTitle,
			Pinned:            true,
			PinID:             p.ID,
			UpdatedAt:         p.PinnedAt,
			Component:         p.Component,
		})
	}

	if convID != "" && len(out) < limit {
		rows, err := s.query(ctx, `SELECT m.id, m.conversation_id, c.title, m.updated_at, m.components
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.conversation_id = ? AND c.user_id = ? AND m.components <> '[]' AND m.components <> ''
			ORDER BY m.id DESC LIMIT 20`, convID, userID)
		if err != nil {
			return nil, err
		}
		out, err = appendMessageComponents(rows, seen, out, limit)
		if err != nil {
			return nil, err
		}
	}

	if includeRecent && len(out) < limit {
		rows, err := s.query(ctx, `SELECT m.id, m.conversation_id, c.title, m.updated_at, m.components
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE c.user_id = ? AND c.private = 0 AND m.components <> '[]' AND m.components <> ''
			ORDER BY m.updated_at DESC, m.id DESC LIMIT 25`, userID)
		if err != nil {
			return nil, err
		}
		out, err = appendMessageComponents(rows, seen, out, limit)
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

func appendMessageComponents(rows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}, seen map[string]bool, out []ComponentRecord, limit int) ([]ComponentRecord, error) {
	defer rows.Close()
	for rows.Next() && len(out) < limit {
		var msgID, cid, cTitle, rawComps string
		var updatedAt int64
		if err := rows.Scan(&msgID, &cid, &cTitle, &updatedAt, &rawComps); err != nil {
			return nil, err
		}
		var comps []MessageComponent
		if json.Unmarshal([]byte(rawComps), &comps) != nil {
			continue
		}
		for i := len(comps) - 1; i >= 0 && len(out) < limit; i-- {
			c := comps[i]
			if c.Kind == "ask_user" {
				continue
			}
			key := msgID + ":" + c.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ComponentRecord{
				MessageID:         msgID,
				ComponentID:       c.ID,
				ConversationID:    cid,
				ConversationTitle: cTitle,
				Pinned:            false,
				UpdatedAt:         updatedAt,
				Component:         c,
			})
		}
	}
	return out, rows.Err()
}
