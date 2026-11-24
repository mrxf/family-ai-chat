package store

import (
	"context"
	"encoding/json"
	"strings"
)

// MaxComponentVersions is the maximum number of version snapshots retained per component entity.
const MaxComponentVersions = 10

// ComponentEntity represents the canonical, globally unique live state of a versioned UI component.
type ComponentEntity struct {
	ID                       string          `json:"id"`
	UserID                   string          `json:"-"`
	Kind                     string          `json:"kind"`
	Title                    string          `json:"title"`
	CurrentVersion           int             `json:"current_version"`
	Data                     json.RawMessage `json:"data"`
	State                    json.RawMessage `json:"state,omitempty"`
	Private                  bool            `json:"private,omitempty"`
	OriginConversationID     string          `json:"origin_conversation_id,omitempty"`
	OriginMessageID          string          `json:"origin_message_id,omitempty"`
	OriginComponentID        string          `json:"origin_component_id,omitempty"`
	LastSourceType           string          `json:"last_source_type,omitempty"`
	LastSourceConversationID string          `json:"last_source_conversation_id,omitempty"`
	LastSourceMessageID      string          `json:"last_source_message_id,omitempty"`
	LastSourceTaskID         string          `json:"last_source_task_id,omitempty"`
	CreatedAt                int64           `json:"created_at"`
	UpdatedAt                int64           `json:"updated_at"`
	Deleted                  bool            `json:"deleted,omitempty"`
}

// ComponentVersion records a single immutable version snapshot of a ComponentEntity.
type ComponentVersion struct {
	ID                      string          `json:"id"`
	ComponentID             string          `json:"component_id"`
	Version                 int             `json:"version"`
	Data                    json.RawMessage `json:"data"`
	State                   json.RawMessage `json:"state,omitempty"`
	ChangeSummary           string          `json:"change_summary,omitempty"`
	SourceType              string          `json:"source_type"`
	SourceConversationID    string          `json:"source_conversation_id,omitempty"`
	SourceConversationTitle string          `json:"source_conversation_title,omitempty"`
	SourceMessageID         string          `json:"source_message_id,omitempty"`
	SourceTaskID            string          `json:"source_task_id,omitempty"`
	SourceTaskTitle         string          `json:"source_task_title,omitempty"`
	CreatedAt               int64           `json:"created_at"`
}

// ComponentMutationMeta describes provenance metadata for an entity data update.
type ComponentMutationMeta struct {
	Title                string
	ChangeSummary        string
	SourceType           string // "create" | "chat_update" | "task_update" | "manual_edit"
	SourceConversationID string
	SourceMessageID      string
	SourceTaskID         string
}

// IsVersionedComponentKind reports whether kind participates in canonical entity + version tracking.
func IsVersionedComponentKind(kind string) bool {
	return kind != "" && kind != "ask_user" && kind != "app_action"
}

const componentEntityCols = `id, user_id, kind, title, current_version, data, state, private,
	origin_conversation_id, origin_message_id, origin_component_id,
	last_source_type, last_source_conversation_id, last_source_message_id, last_source_task_id,
	created_at, updated_at, deleted_at`

func scanComponentEntity(row interface{ Scan(...any) error }) (*ComponentEntity, error) {
	var e ComponentEntity
	var rawData, rawState string
	var deletedAt int64
	if err := row.Scan(
		&e.ID, &e.UserID, &e.Kind, &e.Title, &e.CurrentVersion, &rawData, &rawState, &e.Private,
		&e.OriginConversationID, &e.OriginMessageID, &e.OriginComponentID,
		&e.LastSourceType, &e.LastSourceConversationID, &e.LastSourceMessageID, &e.LastSourceTaskID,
		&e.CreatedAt, &e.UpdatedAt, &deletedAt,
	); err != nil {
		return nil, notFound(err)
	}
	e.Data = json.RawMessage(rawData)
	if rawState != "" {
		e.State = json.RawMessage(rawState)
	}
	e.Deleted = deletedAt > 0
	return &e, nil
}

// CreateComponentEntity persists a new canonical component entity at v1 and records its initial version snapshot.
func (s *Store) CreateComponentEntity(ctx context.Context, ent *ComponentEntity, changeSummary string) error {
	now := Now()
	if ent.ID == "" {
		ent.ID = NewID()
	}
	if ent.CurrentVersion <= 0 {
		ent.CurrentVersion = 1
	}
	if ent.LastSourceType == "" {
		ent.LastSourceType = "create"
	}
	if ent.LastSourceConversationID == "" {
		ent.LastSourceConversationID = ent.OriginConversationID
	}
	if ent.LastSourceMessageID == "" {
		ent.LastSourceMessageID = ent.OriginMessageID
	}
	if ent.CreatedAt == 0 {
		ent.CreatedAt = now
	}
	ent.UpdatedAt = now
	if changeSummary == "" {
		changeSummary = "创建卡片"
	}
	return s.tx(ctx, func(tx *Tx) error {
		return insertComponentEntityTx(ctx, tx, ent, changeSummary)
	})
}

func insertComponentEntityTx(ctx context.Context, tx *Tx, ent *ComponentEntity, changeSummary string) error {
	if _, err := tx.exec(ctx, `INSERT INTO components (`+componentEntityCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ent.ID, ent.UserID, ent.Kind, ent.Title, ent.CurrentVersion, string(ent.Data), string(ent.State), ent.Private,
		ent.OriginConversationID, ent.OriginMessageID, ent.OriginComponentID,
		ent.LastSourceType, ent.LastSourceConversationID, ent.LastSourceMessageID, ent.LastSourceTaskID,
		ent.CreatedAt, ent.UpdatedAt, int64(0),
	); err != nil {
		return err
	}
	_, err := tx.exec(ctx, `INSERT INTO component_versions (
		id, component_id, version, data, state, change_summary,
		source_type, source_conversation_id, source_message_id, source_task_id, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		NewID(), ent.ID, ent.CurrentVersion, string(ent.Data), string(ent.State), changeSummary,
		ent.LastSourceType, ent.LastSourceConversationID, ent.LastSourceMessageID, ent.LastSourceTaskID, ent.CreatedAt,
	)
	return err
}

const componentAccessCond = `(user_id = ? OR (origin_conversation_id <> '' AND EXISTS (
	SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = components.origin_conversation_id AND cm.user_id = ?
)))`

// GetComponentEntity returns a non-deleted component entity accessible to userID
// (either owned by userID or originating in a shared conversation userID belongs to).
func (s *Store) GetComponentEntity(ctx context.Context, entityID, userID string) (*ComponentEntity, error) {
	ent, err := scanComponentEntity(s.queryRow(ctx,
		`SELECT `+componentEntityCols+` FROM components WHERE id = ? AND `+componentAccessCond+` AND deleted_at = 0`,
		entityID, userID, userID))
	if err != nil {
		return nil, err
	}
	return ent, nil
}

// ListComponentVersions returns up to MaxComponentVersions snapshots for entityID ordered newest-first.
func (s *Store) ListComponentVersions(ctx context.Context, entityID, userID string) (*ComponentEntity, []ComponentVersion, error) {
	ent, err := scanComponentEntity(s.queryRow(ctx,
		`SELECT `+componentEntityCols+` FROM components WHERE id = ? AND `+componentAccessCond,
		entityID, userID, userID))
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.query(ctx, `SELECT
		v.id, v.component_id, v.version, v.data, v.state, v.change_summary,
		v.source_type, v.source_conversation_id, COALESCE(c.title, ''),
		v.source_message_id, v.source_task_id, COALESCE(t.title, ''), v.created_at
		FROM component_versions v
		LEFT JOIN conversations c ON c.id = v.source_conversation_id AND (c.user_id = ? OR EXISTS (
			SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = c.id AND cm.user_id = ?
		))
		LEFT JOIN scheduled_tasks t ON t.id = v.source_task_id AND t.user_id = ?
		WHERE v.component_id = ?
		ORDER BY v.version DESC
		LIMIT ?`, userID, userID, userID, entityID, MaxComponentVersions)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []ComponentVersion{}
	for rows.Next() {
		var v ComponentVersion
		var rawData, rawState string
		if err := rows.Scan(
			&v.ID, &v.ComponentID, &v.Version, &rawData, &rawState, &v.ChangeSummary,
			&v.SourceType, &v.SourceConversationID, &v.SourceConversationTitle,
			&v.SourceMessageID, &v.SourceTaskID, &v.SourceTaskTitle, &v.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		v.Data = json.RawMessage(rawData)
		if rawState != "" {
			v.State = json.RawMessage(rawState)
		}
		out = append(out, v)
	}
	return ent, out, rows.Err()
}

// UpdateComponentEntity updates a canonical component entity.
//
// When data is non-empty, current_version increments by 1, a new version snapshot is inserted,
// and snapshots older than (current_version - MaxComponentVersions) are pruned in the same transaction.
// When only state is provided, components.state is updated in place without incrementing current_version.
func (s *Store) UpdateComponentEntity(ctx context.Context, entityID, userID string, data json.RawMessage, state *json.RawMessage, meta ComponentMutationMeta) (*ComponentEntity, error) {
	var updated *ComponentEntity
	err := s.tx(ctx, func(tx *Tx) error {
		cur, err := scanComponentEntity(tx.queryRow(ctx,
			`SELECT `+componentEntityCols+` FROM components WHERE id = ? AND `+componentAccessCond+` AND deleted_at = 0`+s.forUpdate(),
			entityID, userID, userID))
		if err != nil {
			return err
		}
		now := Now()
		cur.UpdatedAt = now
		if state != nil {
			cur.State = append(json.RawMessage(nil), (*state)...)
		}
		if len(data) > 0 {
			cur.Data = append(json.RawMessage(nil), data...)
			cur.CurrentVersion++
			if t := strings.TrimSpace(meta.Title); t != "" {
				cur.Title = t
			}
			srcType := strings.TrimSpace(meta.SourceType)
			if srcType == "" {
				srcType = "manual_edit"
			}
			cur.LastSourceType = srcType
			cur.LastSourceConversationID = strings.TrimSpace(meta.SourceConversationID)
			cur.LastSourceMessageID = strings.TrimSpace(meta.SourceMessageID)
			cur.LastSourceTaskID = strings.TrimSpace(meta.SourceTaskID)

			if _, err := tx.exec(ctx, `UPDATE components
				SET title = ?, current_version = ?, data = ?, state = ?,
					last_source_type = ?, last_source_conversation_id = ?, last_source_message_id = ?, last_source_task_id = ?,
					updated_at = ?
				WHERE id = ?`,
				cur.Title, cur.CurrentVersion, string(cur.Data), string(cur.State),
				cur.LastSourceType, cur.LastSourceConversationID, cur.LastSourceMessageID, cur.LastSourceTaskID,
				cur.UpdatedAt, cur.ID); err != nil {
				return err
			}
			summary := strings.TrimSpace(meta.ChangeSummary)
			if summary == "" {
				summary = "更新卡片内容"
			}
			if _, err := tx.exec(ctx, `INSERT INTO component_versions (
				id, component_id, version, data, state, change_summary,
				source_type, source_conversation_id, source_message_id, source_task_id, created_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				NewID(), cur.ID, cur.CurrentVersion, string(cur.Data), string(cur.State), summary,
				cur.LastSourceType, cur.LastSourceConversationID, cur.LastSourceMessageID, cur.LastSourceTaskID, now); err != nil {
				return err
			}
			if cutoff := cur.CurrentVersion - MaxComponentVersions; cutoff > 0 {
				if _, err := tx.exec(ctx, `DELETE FROM component_versions WHERE component_id = ? AND version <= ?`, cur.ID, cutoff); err != nil {
					return err
				}
			}
		} else if state != nil {
			if _, err := tx.exec(ctx, `UPDATE components SET state = ?, updated_at = ? WHERE id = ?`,
				string(cur.State), cur.UpdatedAt, cur.ID); err != nil {
				return err
			}
		}
		if cur.OriginConversationID != "" {
			_, _ = tx.exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, cur.UpdatedAt, cur.OriginConversationID)
		}
		updated = cur
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteComponentEntity soft-deletes a component entity and removes any home pins referencing it.
func (s *Store) DeleteComponentEntity(ctx context.Context, entityID, userID string) error {
	return s.tx(ctx, func(tx *Tx) error {
		var originMsgID, originCompID string
		err := tx.queryRow(ctx, `SELECT origin_message_id, origin_component_id FROM components WHERE id = ? AND user_id = ? AND deleted_at = 0`+s.forUpdate(), entityID, userID).
			Scan(&originMsgID, &originCompID)
		if err != nil {
			return notFound(err)
		}
		now := Now()
		if _, err := tx.exec(ctx, `UPDATE components SET deleted_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`, now, now, entityID, userID); err != nil {
			return err
		}
		if _, err := tx.exec(ctx, `DELETE FROM pinned_components WHERE user_id = ? AND (entity_id = ? OR (message_id = ? AND component_id = ?))`,
			userID, entityID, originMsgID, originCompID); err != nil {
			return err
		}
		return nil
	})
}

// EnsureComponentEntityForMessage lazily backfills a canonical ComponentEntity for a legacy
// message component that has not yet been assigned an EntityID.
func (s *Store) EnsureComponentEntityForMessage(ctx context.Context, messageID, userID, compID, fallbackTitle string) (*ComponentEntity, error) {
	var result *ComponentEntity
	err := s.tx(ctx, func(tx *Tx) error {
		q := `SELECT ` + prefixCols(msgCols, "m.") + `, c.private, c.user_id FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.id = ? AND (c.user_id = ? OR EXISTS (
				SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = c.id AND cm.user_id = ?
			))` + s.forUpdate()
		var m Message
		var att, src, comps, quoted string
		var isPrivate bool
		var convOwnerID string
		if err := tx.queryRow(ctx, q, messageID, userID, userID).Scan(
			&m.ID, &m.ConversationID, &m.SenderUserID, &m.Role, &m.Content, &m.Reasoning, &att, &src, &comps, &quoted,
			&m.Status, &m.Error, &m.ModelID, &m.PromptTokens, &m.CompletionTokens, &m.FirstTokenMs, &m.DurationMs,
			&m.CreatedAt, &m.UpdatedAt, &isPrivate, &convOwnerID,
		); err != nil {
			return notFound(err)
		}
		_ = json.Unmarshal([]byte(comps), &m.Components)
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
		mc := &m.Components[idx]
		if !IsVersionedComponentKind(mc.Kind) {
			return ErrComponentNotPinnable
		}
		if mc.EntityID != "" {
			ent, err := scanComponentEntity(tx.queryRow(ctx,
				`SELECT `+componentEntityCols+` FROM components WHERE id = ? AND `+componentAccessCond,
				mc.EntityID, userID, userID))
			if err == nil {
				result = ent
				return nil
			}
			if err != ErrNotFound {
				return err
			}
		}
		// Check if another call already created an entity for this origin ref.
		existing, err := scanComponentEntity(tx.queryRow(ctx,
			`SELECT `+componentEntityCols+` FROM components WHERE origin_message_id = ? AND origin_component_id = ? AND user_id = ?`,
			messageID, compID, convOwnerID))
		if err == nil {
			mc.EntityID = existing.ID
			if mc.Version <= 0 {
				mc.Version = 1
			}
			raw, mErr := json.Marshal(stripTransientComponentFields(m.Components))
			if mErr != nil {
				return mErr
			}
			if _, err := tx.exec(ctx, `UPDATE messages SET components = ? WHERE id = ?`, string(raw), m.ID); err != nil {
				return err
			}
			_, _ = tx.exec(ctx, `UPDATE pinned_components SET entity_id = ? WHERE user_id = ? AND message_id = ? AND component_id = ? AND entity_id = ''`,
				existing.ID, userID, messageID, compID)
			_, _ = tx.exec(ctx, `UPDATE scheduled_tasks SET target_entity_id = ? WHERE user_id = ? AND target_message_id = ? AND target_component_id = ? AND target_entity_id = ''`,
				existing.ID, userID, messageID, compID)
			result = existing
			return nil
		}
		if err != ErrNotFound {
			return err
		}

		now := m.CreatedAt
		if now <= 0 {
			now = Now()
		}
		ent := &ComponentEntity{
			ID:                       NewID(),
			UserID:                   convOwnerID,
			Kind:                     mc.Kind,
			Title:                    strings.TrimSpace(fallbackTitle),
			CurrentVersion:           1,
			Data:                     append(json.RawMessage(nil), mc.Data...),
			State:                    append(json.RawMessage(nil), mc.State...),
			Private:                  isPrivate,
			OriginConversationID:     m.ConversationID,
			OriginMessageID:          m.ID,
			OriginComponentID:        mc.ID,
			LastSourceType:           "create",
			LastSourceConversationID: m.ConversationID,
			LastSourceMessageID:      m.ID,
			CreatedAt:                now,
			UpdatedAt:                Now(),
		}
		if err := insertComponentEntityTx(ctx, tx, ent, "创建卡片"); err != nil {
			return err
		}
		mc.EntityID = ent.ID
		mc.Version = 1
		raw, mErr := json.Marshal(stripTransientComponentFields(m.Components))
		if mErr != nil {
			return mErr
		}
		if _, err := tx.exec(ctx, `UPDATE messages SET components = ? WHERE id = ?`, string(raw), m.ID); err != nil {
			return err
		}
		_, _ = tx.exec(ctx, `UPDATE pinned_components SET entity_id = ? WHERE user_id = ? AND message_id = ? AND component_id = ? AND entity_id = ''`,
			ent.ID, userID, messageID, compID)
		_, _ = tx.exec(ctx, `UPDATE scheduled_tasks SET target_entity_id = ? WHERE user_id = ? AND target_message_id = ? AND target_component_id = ? AND target_entity_id = ''`,
			ent.ID, userID, messageID, compID)
		result = ent
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// HydrateMessageComponents populates LatestVersion, Deleted, and (when at the latest version)
// live State for every versioned MessageComponent across msgs in a single batched query.
func (s *Store) HydrateMessageComponents(ctx context.Context, msgs []*Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idSet := map[string]struct{}{}
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, c := range m.Components {
			if c.EntityID != "" {
				idSet[c.EntityID] = struct{}{}
			}
		}
	}
	if len(idSet) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(idSet))
	args := make([]any, 0, len(idSet))
	for id := range idSet {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := s.query(ctx,
		`SELECT id, current_version, state, deleted_at FROM components WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	type entityBrief struct {
		version int
		state   json.RawMessage
		deleted bool
	}
	briefs := make(map[string]entityBrief, len(idSet))
	for rows.Next() {
		var id, rawState string
		var ver int
		var deletedAt int64
		if err := rows.Scan(&id, &ver, &rawState, &deletedAt); err != nil {
			return err
		}
		var st json.RawMessage
		if rawState != "" {
			st = json.RawMessage(rawState)
		}
		briefs[id] = entityBrief{version: ver, state: st, deleted: deletedAt > 0}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for i := range m.Components {
			c := &m.Components[i]
			if c.EntityID == "" {
				continue
			}
			b, ok := briefs[c.EntityID]
			if !ok {
				continue
			}
			if c.Version <= 0 {
				c.Version = 1
			}
			c.LatestVersion = b.version
			c.Deleted = b.deleted
			if c.Version == b.version && len(b.state) > 0 {
				c.State = append(json.RawMessage(nil), b.state...)
			}
		}
	}
	return nil
}

// ListUserComponentEntities lists active non-private component entities for userID ordered by updated_at DESC.
func (s *Store) ListUserComponentEntities(ctx context.Context, userID string, limit int) ([]*ComponentEntity, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	rows, err := s.query(ctx, `SELECT `+componentEntityCols+` FROM components
		WHERE user_id = ? AND private = 0 AND deleted_at = 0
		ORDER BY updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ComponentEntity{}
	for rows.Next() {
		e, err := scanComponentEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
