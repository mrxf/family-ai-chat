package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// ---- messages ----

type Attachment struct {
	FileID string `json:"file_id"`
	Mime   string `json:"mime"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Prompt string `json:"prompt,omitempty"` // pictures drawn by the assistant: the prompt used
	Source string `json:"source,omitempty"` // a modified picture: file id of the picture it was made from
}

// WebSource records a web page searched or fetched while generating an assistant message.
type WebSource struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name,omitempty"`
	Date     string `json:"date,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
}

// MessageComponent represents an inline rich UI card (weather, todo, chart, quiz,
// metrics, feed, or ask_user/app_action prompt) embedded inside an assistant message.
//
// For versioned live components, EntityID points to the canonical row in `components`,
// Version is the snapshot version anchored to this message, Data holds that version's
// fallback snapshot, and LatestVersion / Deleted are hydrated dynamically on read.
type MessageComponent struct {
	ID            string          `json:"id"`
	EntityID      string          `json:"entity_id,omitempty"`
	Kind          string          `json:"kind"`            // "weather" | "todo" | "feed" | "chart" | "quiz" | "metrics" | "ask_user" | "app_action"
	Pos           int             `json:"pos"`             // rune offset in Message.Content where the component is interleaved
	Version       int             `json:"version,omitempty"`
	LatestVersion int             `json:"latest_version,omitempty"`
	Deleted       bool            `json:"deleted,omitempty"`
	Data          json.RawMessage `json:"data"`            // structured display payload for this message's anchored version
	State         json.RawMessage `json:"state,omitempty"` // mutable user interaction state (synced when at latest version)
}

const (
	StatusDone        = "done"
	StatusStreaming   = "streaming"
	StatusError       = "error"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"
)

type Message struct {
	ID               string             `json:"id"`
	ConversationID   string             `json:"conversation_id"`
	SenderUserID     string             `json:"sender_user_id,omitempty"`
	SenderName       string             `json:"sender_name,omitempty"`
	SenderAvatar     string             `json:"sender_avatar,omitempty"`
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	Reasoning        string             `json:"reasoning"`
	Attachments      []Attachment       `json:"attachments"`
	Sources          []WebSource        `json:"sources,omitempty"`
	Components       []MessageComponent `json:"components,omitempty"`
	QuotedComponent  *QuotedComponent   `json:"quoted_component,omitempty"`
	Status           string             `json:"status"`
	Error            string             `json:"error"`
	ModelID          *string            `json:"model_id"`
	PromptTokens     int                `json:"prompt_tokens"`
	CompletionTokens int                `json:"completion_tokens"`
	FirstTokenMs     int64              `json:"first_token_ms"`
	DurationMs       int64              `json:"duration_ms"`
	Cost             float64            `json:"-"` // USD; only written by FinishMessage, read via usage queries
	CreatedAt        int64              `json:"created_at"`
	UpdatedAt        int64              `json:"updated_at"`
}

const msgCols = `id, conversation_id, sender_user_id, role, content, reasoning, attachments, sources, components, quoted_component, status, error, model_id, prompt_tokens, completion_tokens, first_token_ms, duration_ms, created_at, updated_at`

func scanMsg(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var att, src, comps, quoted string
	if err := row.Scan(&m.ID, &m.ConversationID, &m.SenderUserID, &m.Role, &m.Content, &m.Reasoning, &att, &src, &comps, &quoted, &m.Status, &m.Error, &m.ModelID,
		&m.PromptTokens, &m.CompletionTokens, &m.FirstTokenMs, &m.DurationMs, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(att), &m.Attachments)
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	_ = json.Unmarshal([]byte(src), &m.Sources)
	if len(m.Sources) == 0 {
		m.Sources = nil
	}
	_ = json.Unmarshal([]byte(comps), &m.Components)
	if len(m.Components) == 0 {
		m.Components = nil
	}
	if quoted != "" {
		var q QuotedComponent
		if json.Unmarshal([]byte(quoted), &q) == nil && q.Kind != "" {
			m.QuotedComponent = &q
		}
	}
	return &m, nil
}

func insertMsg(ctx context.Context, tx *Tx, m *Message) error {
	now := Now()
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	att, _ := json.Marshal(m.Attachments)
	sources := m.Sources
	if sources == nil {
		sources = []WebSource{}
	}
	src, _ := json.Marshal(sources)
	components := stripTransientComponentFields(m.Components)
	comps, _ := json.Marshal(components)
	quoted := ""
	if m.QuotedComponent != nil {
		if b, err := json.Marshal(m.QuotedComponent); err == nil {
			quoted = string(b)
		}
	}
	_, err := tx.exec(ctx, `INSERT INTO messages (`+msgCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.SenderUserID, m.Role, m.Content, m.Reasoning, string(att), string(src), string(comps), quoted, m.Status, m.Error, m.ModelID,
		m.PromptTokens, m.CompletionTokens, m.FirstTokenMs, m.DurationMs, m.CreatedAt, m.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// stripTransientComponentFields clears read-only hydrated fields before persisting to messages.components.
func stripTransientComponentFields(in []MessageComponent) []MessageComponent {
	if len(in) == 0 {
		return []MessageComponent{}
	}
	out := make([]MessageComponent, len(in))
	for i, c := range in {
		c.LatestVersion = 0
		c.Deleted = false
		out[i] = c
	}
	return out
}

// Turn describes one write operation on a conversation's linear history:
// optionally truncate from a message (edit / regenerate), then append messages.
type Turn struct {
	ConversationID string
	TruncateFrom   string // delete this message and everything after it ("" = none)
	Append         []*Message
}

// ApplyTurn performs a Turn atomically and bumps the conversation's updated_at.
func (s *Store) ApplyTurn(ctx context.Context, t Turn) error {
	return s.tx(ctx, func(tx *Tx) error {
		// Serialize edits and appends to the same conversation before touching history.
		if s.pg {
			var id string
			if err := tx.queryRow(ctx, `SELECT id FROM conversations WHERE id = ? FOR UPDATE`, t.ConversationID).Scan(&id); err != nil {
				return notFound(err)
			}
		}
		if t.TruncateFrom != "" {
			if _, err := tx.exec(ctx, `DELETE FROM messages WHERE conversation_id = ? AND id >= ?`, t.ConversationID, t.TruncateFrom); err != nil {
				return err
			}
			if _, err := tx.exec(ctx, `UPDATE conversations SET summary = '', summary_cursor = '' WHERE id = ? AND summary_cursor >= ?`, t.ConversationID, t.TruncateFrom); err != nil {
				return err
			}
		}
		for _, m := range t.Append {
			m.ConversationID = t.ConversationID
			if err := insertMsg(ctx, tx, m); err != nil {
				return err
			}
		}
		_, err := tx.exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, Now(), t.ConversationID)
		return err
	})
}

func (s *Store) GetMessage(ctx context.Context, id string) (*Message, error) {
	m, err := scanMsg(s.queryRow(ctx, `SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	_ = s.HydrateMessageComponents(ctx, []*Message{m})
	return m, nil
}

// GetMessageForUser returns a message if its conversation belongs to or is shared with userID.
func (s *Store) GetMessageForUser(ctx context.Context, id, userID string) (*Message, error) {
	m, err := scanMsg(s.queryRow(ctx, `SELECT `+prefixCols(msgCols, "m.")+` FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.id = ? AND (c.user_id = ? OR EXISTS (
			SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = c.id AND cm.user_id = ?
		))`, id, userID, userID))
	if err != nil {
		return nil, err
	}
	_ = s.HydrateMessageComponents(ctx, []*Message{m})
	_ = s.HydrateMessageSenders(ctx, []*Message{m})
	return m, nil
}

// ListMessages returns up to limit messages before the cursor id, in ascending order.
func (s *Store) ListMessages(ctx context.Context, convID, before string, limit int) ([]*Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	msgs, _, err := s.ListMessagesAfter(ctx, convID, "", before, limit)
	return msgs, err
}

// ListMessagesAfter returns up to limit messages in (after, before) in ascending order,
// plus hasOlder reporting whether more messages exist in the conversation at or before the returned slice.
func (s *Store) ListMessagesAfter(ctx context.Context, convID, after, before string, limit int) ([]*Message, bool, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE conversation_id = ?`
	args := []any{convID}
	if after != "" {
		q += ` AND id > ?`
		args = append(args, after)
	}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := make([]*Message, 0, min(limit+1, 64))
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	moreInRange := len(out) > limit
	if moreInRange {
		out = out[:limit]
	}
	// reverse to ascending
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	_ = s.HydrateMessageComponents(ctx, out)
	_ = s.HydrateMessageSenders(ctx, out)
	return out, after != "" || moreInRange, nil
}

// ListFirstMessages returns the earliest up to limit messages strictly before cursor (or all when before == ""),
// in ascending chronological order.
func (s *Store) ListFirstMessages(ctx context.Context, convID, before string, limit int) ([]*Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE conversation_id = ?`
	args := []any{convID}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.HydrateMessageComponents(ctx, out)
	return out, nil
}

// ListMessagesAround returns messages around centerID within (-inf, upperBound), ordered ascending.
func (s *Store) ListMessagesAround(ctx context.Context, convID, centerID, upperBound string, beforeCount, afterCount int) ([]*Message, error) {
	if beforeCount < 0 {
		beforeCount = 0
	}
	if afterCount < 0 {
		afterCount = 0
	}
	qHead := `SELECT ` + msgCols + ` FROM messages WHERE conversation_id = ? AND id <= ?`
	argsHead := []any{convID, centerID}
	if upperBound != "" {
		qHead += ` AND id < ?`
		argsHead = append(argsHead, upperBound)
	}
	qHead += ` ORDER BY id DESC LIMIT ?`
	argsHead = append(argsHead, beforeCount+1)
	rows, err := s.query(ctx, qHead, argsHead...)
	if err != nil {
		return nil, err
	}
	var head []*Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		head = append(head, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i, j := 0, len(head)-1; i < j; i, j = i+1, j-1 {
		head[i], head[j] = head[j], head[i]
	}
	if afterCount == 0 {
		_ = s.HydrateMessageComponents(ctx, head)
		return head, nil
	}
	qTail := `SELECT ` + msgCols + ` FROM messages WHERE conversation_id = ? AND id > ?`
	argsTail := []any{convID, centerID}
	if upperBound != "" {
		qTail += ` AND id < ?`
		argsTail = append(argsTail, upperBound)
	}
	qTail += ` ORDER BY id ASC LIMIT ?`
	argsTail = append(argsTail, afterCount)
	trows, err := s.query(ctx, qTail, argsTail...)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		m, err := scanMsg(trows)
		if err != nil {
			return nil, err
		}
		head = append(head, m)
	}
	if err := trows.Err(); err != nil {
		return nil, err
	}
	_ = s.HydrateMessageComponents(ctx, head)
	return head, nil
}

// ListConversationImageRefs returns chronological image/* attachments from up to limit
// image-bearing messages before cursor, without scanning message text bodies.
func (s *Store) ListConversationImageRefs(ctx context.Context, convID, before string, limit int) ([]Attachment, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q := `SELECT attachments FROM messages WHERE conversation_id = ? AND attachments <> '[]'`
	args := []any{convID}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var raws []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		raws = append(raws, raw)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var refs []Attachment
	for i := len(raws) - 1; i >= 0; i-- {
		var atts []Attachment
		if json.Unmarshal([]byte(raws[i]), &atts) != nil {
			continue
		}
		for _, a := range atts {
			if strings.HasPrefix(a.Mime, "image/") {
				refs = append(refs, a)
			}
		}
	}
	return refs, nil
}

// GetConversationSummary returns the rolling summary and cursor of a conversation.
func (s *Store) GetConversationSummary(ctx context.Context, convID string) (string, string, error) {
	var summary, cursor string
	err := s.queryRow(ctx, `SELECT summary, summary_cursor FROM conversations WHERE id = ?`, convID).Scan(&summary, &cursor)
	return summary, cursor, notFound(err)
}

// CommitConversationSummary atomically advances summary_cursor to newCursor when expectedCursor
// still matches and newCursor still exists in the conversation (was not truncated or deleted).
func (s *Store) CommitConversationSummary(ctx context.Context, convID, expectedCursor, newCursor, summary string) (bool, error) {
	summary = strings.TrimSpace(summary)
	if convID == "" || newCursor == "" || summary == "" {
		return false, nil
	}
	committed := false
	err := s.tx(ctx, func(tx *Tx) error {
		var curCursor string
		err := tx.queryRow(ctx, `SELECT summary_cursor FROM conversations WHERE id = ?`+s.forUpdate(), convID).Scan(&curCursor)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if curCursor != expectedCursor || (curCursor != "" && newCursor <= curCursor) {
			return nil
		}
		var msgID string
		err = tx.queryRow(ctx, `SELECT id FROM messages WHERE conversation_id = ? AND id = ? AND status <> ?`, convID, newCursor, StatusStreaming).Scan(&msgID)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.exec(ctx, `UPDATE conversations SET summary = ?, summary_cursor = ? WHERE id = ?`, summary, newCursor, convID); err != nil {
			return err
		}
		committed = true
		return nil
	})
	return committed, err
}

// FinishMessage writes the final state of an assistant message.
func (s *Store) FinishMessage(ctx context.Context, m *Message) error {
	m.UpdatedAt = Now()
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	att, _ := json.Marshal(m.Attachments)
	sources := m.Sources
	if sources == nil {
		sources = []WebSource{}
	}
	src, _ := json.Marshal(sources)
	components := stripTransientComponentFields(m.Components)
	comps, _ := json.Marshal(components)
	_, err := s.exec(ctx, `UPDATE messages SET content=?, reasoning=?, attachments=?, sources=?, components=?, status=?, error=?, model_id=?, prompt_tokens=?, completion_tokens=?,
		first_token_ms=?, duration_ms=?, cost=?, updated_at=? WHERE id=?`,
		m.Content, m.Reasoning, string(att), string(src), string(comps), m.Status, m.Error, m.ModelID, m.PromptTokens, m.CompletionTokens, m.FirstTokenMs, m.DurationMs, m.Cost, m.UpdatedAt, m.ID)
	return err
}

// SetAttachments saves a streaming message's pictures as soon as they exist.
func (s *Store) SetAttachments(ctx context.Context, messageID string, atts []Attachment) error {
	att, _ := json.Marshal(atts)
	_, err := s.exec(ctx, `UPDATE messages SET attachments=?, updated_at=? WHERE id=?`, string(att), Now(), messageID)
	return err
}

// SetSources saves a streaming message's web search references as soon as they exist.
func (s *Store) SetSources(ctx context.Context, messageID string, sources []WebSource) error {
	if sources == nil {
		sources = []WebSource{}
	}
	src, _ := json.Marshal(sources)
	_, err := s.exec(ctx, `UPDATE messages SET sources=?, updated_at=? WHERE id=?`, string(src), Now(), messageID)
	return err
}

// MarkInterrupted flags messages left in "streaming" state by a previous process.
func (s *Store) MarkInterrupted(ctx context.Context) (int64, error) {
	res, err := s.exec(ctx, `UPDATE messages SET status = ?, updated_at = ? WHERE status = ?`, StatusInterrupted, Now(), StatusStreaming)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
