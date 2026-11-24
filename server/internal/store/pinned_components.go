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

// PinnedComponent represents a component pinned to the user's home board.
// When EntityID is set, Component always reflects the live canonical entity from `components`.
type PinnedComponent struct {
	ID                string           `json:"id"`
	EntityID          string           `json:"entity_id,omitempty"`
	ConversationID    string           `json:"conversation_id"`
	ConversationTitle string           `json:"conversation_title"`
	MessageID         string           `json:"message_id"`
	ComponentID       string           `json:"component_id"`
	SortOrder         int              `json:"sort_order"`
	PinnedAt          int64            `json:"pinned_at"`
	UpdatedAt         int64            `json:"updated_at,omitempty"`
	Component         MessageComponent `json:"component"`
}

// QuotedComponent stores an immutable snapshot of a component referenced when starting a chat.
type QuotedComponent struct {
	EntityID          string          `json:"entity_id,omitempty"`
	Version           int             `json:"version,omitempty"`
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
	rows, err := s.query(ctx, `SELECT
		p.id, p.entity_id, p.conversation_id, c.title, p.message_id, p.component_id, p.sort_order, p.pinned_at, m.components,
		COALESCE(e.id, ''), COALESCE(e.kind, ''), COALESCE(e.current_version, 0), COALESCE(e.data, ''), COALESCE(e.state, ''), COALESCE(e.updated_at, 0), COALESCE(e.deleted_at, 0)
		FROM pinned_components p
		JOIN messages m ON m.id = p.message_id
		JOIN conversations c ON c.id = p.conversation_id
		LEFT JOIN components e ON e.id = p.entity_id AND e.user_id = p.user_id
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
		var entID, entKind, entData, entState string
		var entVer int
		var entUpdated, entDeleted int64
		if err := rows.Scan(
			&p.ID, &p.EntityID, &p.ConversationID, &p.ConversationTitle, &p.MessageID, &p.ComponentID, &p.SortOrder, &p.PinnedAt, &rawComps,
			&entID, &entKind, &entVer, &entData, &entState, &entUpdated, &entDeleted,
		); err != nil {
			return nil, err
		}
		if entID != "" {
			if entDeleted > 0 {
				continue
			}
			p.EntityID = entID
			p.UpdatedAt = entUpdated
			p.Component = MessageComponent{
				ID:            p.ComponentID,
				EntityID:      entID,
				Kind:          entKind,
				Version:       entVer,
				LatestVersion: entVer,
				Data:          json.RawMessage(entData),
			}
			if entState != "" {
				p.Component.State = json.RawMessage(entState)
			}
			out = append(out, &p)
			continue
		}

		// Legacy fallback: resolve directly from messages.components JSON.
		var comps []MessageComponent
		if json.Unmarshal([]byte(rawComps), &comps) != nil {
			continue
		}
		found := false
		for _, comp := range comps {
			if comp.ID == p.ComponentID {
				p.Component = comp
				if p.EntityID == "" && comp.EntityID != "" {
					p.EntityID = comp.EntityID
				}
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
// Pinning an already-pinned component (or another message referencing the same entity_id) is idempotent.
func (s *Store) PinComponent(ctx context.Context, userID, messageID, componentID string) (*PinnedComponent, error) {
	// Ensure legacy components have a canonical entity before pinning so home board always stays live.
	ent, err := s.EnsureComponentEntityForMessage(ctx, messageID, userID, componentID, "")
	if err != nil && !errors.Is(err, ErrComponentNotPinnable) {
		return nil, err
	}
	if errors.Is(err, ErrComponentNotPinnable) {
		return nil, ErrComponentNotPinnable
	}

	var result *PinnedComponent
	err = s.tx(ctx, func(tx *Tx) error {
		var convID, convTitle string
		var isPrivate bool
		err := tx.queryRow(ctx, `SELECT m.conversation_id, c.title, c.private
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.id = ? AND c.user_id = ?`, messageID, userID).
			Scan(&convID, &convTitle, &isPrivate)
		if err != nil {
			return notFound(err)
		}
		if isPrivate || ent.Private {
			return ErrComponentNotPinnable
		}

		liveComp := MessageComponent{
			ID:            componentID,
			EntityID:      ent.ID,
			Kind:          ent.Kind,
			Version:       ent.CurrentVersion,
			LatestVersion: ent.CurrentVersion,
			Data:          ent.Data,
			State:         ent.State,
		}

		// Idempotent: check by entity_id first, then by (message_id, component_id).
		var existing PinnedComponent
		err = tx.queryRow(ctx, `SELECT p.id, p.entity_id, p.conversation_id, c.title, p.message_id, p.component_id, p.sort_order, p.pinned_at
			FROM pinned_components p
			JOIN conversations c ON c.id = p.conversation_id
			WHERE p.user_id = ? AND (p.entity_id = ? OR (p.message_id = ? AND p.component_id = ?))
			LIMIT 1`, userID, ent.ID, messageID, componentID).
			Scan(&existing.ID, &existing.EntityID, &existing.ConversationID, &existing.ConversationTitle,
				&existing.MessageID, &existing.ComponentID, &existing.SortOrder, &existing.PinnedAt)
		if err == nil {
			if existing.EntityID == "" {
				_, _ = tx.exec(ctx, `UPDATE pinned_components SET entity_id = ? WHERE id = ?`, ent.ID, existing.ID)
				existing.EntityID = ent.ID
			}
			existing.UpdatedAt = ent.UpdatedAt
			liveComp.ID = existing.ComponentID
			existing.Component = liveComp
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
			EntityID:          ent.ID,
			ConversationID:    convID,
			ConversationTitle: convTitle,
			MessageID:         messageID,
			ComponentID:       componentID,
			SortOrder:         0,
			PinnedAt:          Now(),
			UpdatedAt:         ent.UpdatedAt,
			Component:         liveComp,
		}
		if _, err := tx.exec(ctx, `INSERT INTO pinned_components (id, user_id, entity_id, conversation_id, message_id, component_id, sort_order, pinned_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			pin.ID, userID, pin.EntityID, pin.ConversationID, pin.MessageID, pin.ComponentID, pin.SortOrder, pin.PinnedAt); err != nil {
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

// UnpinComponent removes a component pin for userID by (messageID, componentID) or matching entity_id.
func (s *Store) UnpinComponent(ctx context.Context, userID, messageID, componentID string) error {
	// Resolve entity_id if the message holds a versioned component so unpinning from an updated message also unpins the home card.
	var entityID string
	if _, comp, err := s.GetMessageComponent(ctx, messageID, userID, componentID); err == nil && comp != nil {
		entityID = comp.EntityID
	}
	if entityID != "" {
		_, err := s.exec(ctx, `DELETE FROM pinned_components WHERE user_id = ? AND (entity_id = ? OR (message_id = ? AND component_id = ?))`,
			userID, entityID, messageID, componentID)
		return err
	}
	_, err := s.exec(ctx, `DELETE FROM pinned_components WHERE user_id = ? AND message_id = ? AND component_id = ?`,
		userID, messageID, componentID)
	return err
}

// UnpinComponentByEntity removes any home pin for entityID owned by userID.
func (s *Store) UnpinComponentByEntity(ctx context.Context, userID, entityID string) error {
	_, err := s.exec(ctx, `DELETE FROM pinned_components WHERE user_id = ? AND entity_id = ?`, userID, entityID)
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
