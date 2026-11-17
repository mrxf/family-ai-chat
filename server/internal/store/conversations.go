package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"unicode"
)

type Conversation struct {
	ID          string  `json:"id"`
	UserID      string  `json:"-"`
	AssistantID *string `json:"assistant_id"`
	ModelID     *string `json:"model_id"`
	Title       string  `json:"title"`
	Pinned      bool    `json:"pinned"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
	Snippet     string  `json:"snippet,omitempty"`
}

const convCols = `id, user_id, assistant_id, model_id, title, pinned, created_at, updated_at`

func scanConv(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	if err := row.Scan(&c.ID, &c.UserID, &c.AssistantID, &c.ModelID, &c.Title, &c.Pinned, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

func (s *Store) CreateConversation(ctx context.Context, c *Conversation) error {
	now := Now()
	if c.ID == "" {
		c.ID = NewID()
	}
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.w.ExecContext(ctx, `INSERT INTO conversations (`+convCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		c.ID, c.UserID, c.AssistantID, c.ModelID, c.Title, c.Pinned, c.CreatedAt, c.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// GetConversation returns the conversation only if it belongs to userID.
func (s *Store) GetConversation(ctx context.Context, id, userID string) (*Conversation, error) {
	return scanConv(s.r.QueryRowContext(ctx, `SELECT `+convCols+` FROM conversations WHERE id = ? AND user_id = ?`, id, userID))
}

type ConversationQuery struct {
	UserID string
	Before int64 // updated_at cursor (exclusive); 0 = from newest
	Limit  int
	Search string
}

// ListConversations returns conversations ordered by updated_at desc.
// Pinned conversations are returned by the client-side grouping; we only sort by time
// so cursors stay simple.
func (s *Store) ListConversations(ctx context.Context, q ConversationQuery) ([]*Conversation, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	search := strings.TrimSpace(q.Search)
	selectCols := prefixCols(convCols, "c.")
	args := []any{}
	if search != "" {
		like := "%" + escapeLike(search) + "%"
		selectCols += `, COALESCE((SELECT m.content FROM messages m WHERE m.conversation_id = c.id AND m.content LIKE ? ESCAPE '\' ORDER BY m.id DESC LIMIT 1), '')`
		args = append(args, like)
	}
	where := []string{"c.user_id = ?"}
	args = append(args, q.UserID)
	if q.Before > 0 {
		where = append(where, "c.updated_at < ?")
		args = append(args, q.Before)
	}
	if search != "" {
		like := "%" + escapeLike(search) + "%"
		where = append(where, `(c.title LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.content LIKE ? ESCAPE '\'))`)
		args = append(args, like, like)
	}
	args = append(args, q.Limit)
	rows, err := s.r.QueryContext(ctx, `SELECT `+selectCols+` FROM conversations c
		WHERE `+strings.Join(where, " AND ")+` ORDER BY c.updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Conversation{}
	for rows.Next() {
		if search == "" {
			c, err := scanConv(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
			continue
		}
		var c Conversation
		var body string
		if err := rows.Scan(&c.ID, &c.UserID, &c.AssistantID, &c.ModelID, &c.Title, &c.Pinned, &c.CreatedAt, &c.UpdatedAt, &body); err != nil {
			return nil, notFound(err)
		}
		c.Snippet = makeSnippet(body, search)
		out = append(out, &c)
	}
	return out, rows.Err()
}

// makeSnippet extracts a compact single-line excerpt around the first match of needle.
func makeSnippet(content, needle string) string {
	clean := strings.Join(strings.Fields(content), " ")
	q := strings.Join(strings.Fields(needle), " ")
	if clean == "" || q == "" {
		return ""
	}
	runes := []rune(clean)
	qRunes := []rune(q)
	const (
		contextBefore = 18
		maxWindow     = 72
	)
	if len(runes) <= maxWindow {
		return clean
	}
	idx := indexFoldRunes(runes, qRunes)
	if idx < 0 {
		idx = 0
	}
	start := max(0, idx-contextBefore)
	end := start + maxWindow
	if end > len(runes) {
		end = len(runes)
		start = max(0, end-maxWindow)
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	b.WriteString(string(runes[start:end]))
	if end < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}

func indexFoldRunes(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		match := true
		for j, nr := range needle {
			if unicode.ToLower(haystack[i+j]) != unicode.ToLower(nr) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

type ConversationPatch struct {
	Title       *string
	Pinned      *bool
	ModelID     *string // "" clears
	AssistantID *string // "" clears
}

func (s *Store) UpdateConversation(ctx context.Context, id, userID string, p ConversationPatch) (*Conversation, error) {
	sets := []string{}
	args := []any{}
	if p.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *p.Title)
	}
	if p.Pinned != nil {
		sets = append(sets, "pinned = ?")
		args = append(args, *p.Pinned)
	}
	if p.ModelID != nil {
		sets = append(sets, "model_id = ?")
		args = append(args, nullable(*p.ModelID))
	}
	if p.AssistantID != nil {
		sets = append(sets, "assistant_id = ?")
		args = append(args, nullable(*p.AssistantID))
	}
	if len(sets) > 0 {
		args = append(args, id, userID)
		if err := affected(s.w.ExecContext(ctx, `UPDATE conversations SET `+strings.Join(sets, ", ")+` WHERE id = ? AND user_id = ?`, args...)); err != nil {
			return nil, err
		}
	}
	return s.GetConversation(ctx, id, userID)
}

// SetConversationTitle sets the title only if it is still empty (auto titles never overwrite user edits).
func (s *Store) SetConversationTitle(ctx context.Context, id, title string) (bool, error) {
	res, err := s.w.ExecContext(ctx, `UPDATE conversations SET title = ? WHERE id = ? AND title = ''`, title, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) TouchConversation(ctx context.Context, id string) error {
	_, err := s.w.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, Now(), id)
	return err
}

func (s *Store) DeleteConversation(ctx context.Context, id, userID string) error {
	return affected(s.w.ExecContext(ctx, `DELETE FROM conversations WHERE id = ? AND user_id = ?`, id, userID))
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---- messages ----

type Attachment struct {
	FileID string `json:"file_id"`
	Mime   string `json:"mime"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Prompt string `json:"prompt,omitempty"` // pictures drawn by the assistant: the prompt used
	Source string `json:"source,omitempty"` // a modified picture: file id of the picture it was made from
}

const (
	StatusDone        = "done"
	StatusStreaming   = "streaming"
	StatusError       = "error"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"
)

type Message struct {
	ID               string       `json:"id"`
	ConversationID   string       `json:"conversation_id"`
	Role             string       `json:"role"`
	Content          string       `json:"content"`
	Reasoning        string       `json:"reasoning"`
	Attachments      []Attachment `json:"attachments"`
	Status           string       `json:"status"`
	Error            string       `json:"error"`
	ModelID          *string      `json:"model_id"`
	PromptTokens     int          `json:"prompt_tokens"`
	CompletionTokens int          `json:"completion_tokens"`
	FirstTokenMs     int64        `json:"first_token_ms"`
	DurationMs       int64        `json:"duration_ms"`
	Cost             float64      `json:"-"` // USD; only written by FinishMessage, read via usage queries
	CreatedAt        int64        `json:"created_at"`
	UpdatedAt        int64        `json:"updated_at"`
}

const msgCols = `id, conversation_id, role, content, reasoning, attachments, status, error, model_id, prompt_tokens, completion_tokens, first_token_ms, duration_ms, created_at, updated_at`

func scanMsg(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var att string
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Reasoning, &att, &m.Status, &m.Error, &m.ModelID,
		&m.PromptTokens, &m.CompletionTokens, &m.FirstTokenMs, &m.DurationMs, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(att), &m.Attachments)
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	return &m, nil
}

func insertMsg(ctx context.Context, tx *sql.Tx, m *Message) error {
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
	_, err := tx.ExecContext(ctx, `INSERT INTO messages (`+msgCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Role, m.Content, m.Reasoning, string(att), m.Status, m.Error, m.ModelID,
		m.PromptTokens, m.CompletionTokens, m.FirstTokenMs, m.DurationMs, m.CreatedAt, m.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
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
	return s.tx(ctx, func(tx *sql.Tx) error {
		if t.TruncateFrom != "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ? AND id >= ?`, t.ConversationID, t.TruncateFrom); err != nil {
				return err
			}
		}
		for _, m := range t.Append {
			m.ConversationID = t.ConversationID
			if err := insertMsg(ctx, tx, m); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, Now(), t.ConversationID)
		return err
	})
}

func (s *Store) GetMessage(ctx context.Context, id string) (*Message, error) {
	return scanMsg(s.r.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
}

// GetMessageForUser returns a message only if its conversation belongs to userID.
func (s *Store) GetMessageForUser(ctx context.Context, id, userID string) (*Message, error) {
	return scanMsg(s.r.QueryRowContext(ctx, `SELECT `+prefixCols(msgCols, "m.")+` FROM messages m
		JOIN conversations c ON c.id = m.conversation_id WHERE m.id = ? AND c.user_id = ?`, id, userID))
}

// ListMessages returns up to limit messages before the cursor id, in ascending order.
func (s *Store) ListMessages(ctx context.Context, convID, before string, limit int) ([]*Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE conversation_id = ?`
	args := []any{convID}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, q, args...)
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
	// reverse to ascending
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// FinishMessage writes the final state of an assistant message.
func (s *Store) FinishMessage(ctx context.Context, m *Message) error {
	m.UpdatedAt = Now()
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	att, _ := json.Marshal(m.Attachments)
	_, err := s.w.ExecContext(ctx, `UPDATE messages SET content=?, reasoning=?, attachments=?, status=?, error=?, model_id=?, prompt_tokens=?, completion_tokens=?,
		first_token_ms=?, duration_ms=?, cost=?, updated_at=? WHERE id=?`,
		m.Content, m.Reasoning, string(att), m.Status, m.Error, m.ModelID, m.PromptTokens, m.CompletionTokens, m.FirstTokenMs, m.DurationMs, m.Cost, m.UpdatedAt, m.ID)
	return err
}

// SetAttachments saves a streaming message's pictures as soon as they exist,
// so they survive a restart in the middle of the reply.
func (s *Store) SetAttachments(ctx context.Context, messageID string, atts []Attachment) error {
	att, _ := json.Marshal(atts)
	_, err := s.w.ExecContext(ctx, `UPDATE messages SET attachments=?, updated_at=? WHERE id=?`, string(att), Now(), messageID)
	return err
}

// MarkInterrupted flags messages left in "streaming" state by a previous process.
func (s *Store) MarkInterrupted(ctx context.Context) (int64, error) {
	res, err := s.w.ExecContext(ctx, `UPDATE messages SET status = ?, updated_at = ? WHERE status = ?`, StatusInterrupted, Now(), StatusStreaming)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- files ----

type File struct {
	ID        string `json:"id"`
	UserID    string `json:"-"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Path      string `json:"-"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) CreateFile(ctx context.Context, f *File) error {
	if f.ID == "" {
		f.ID = NewID()
	}
	f.CreatedAt = Now()
	_, err := s.w.ExecContext(ctx, `INSERT INTO files (id, user_id, mime, size, sha256, width, height, path, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		f.ID, f.UserID, f.Mime, f.Size, f.SHA256, f.Width, f.Height, f.Path, f.CreatedAt)
	return err
}

func (s *Store) GetFile(ctx context.Context, id, userID string) (*File, error) {
	var f File
	err := s.r.QueryRowContext(ctx, `SELECT id, user_id, mime, size, sha256, width, height, path, created_at FROM files WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&f.ID, &f.UserID, &f.Mime, &f.Size, &f.SHA256, &f.Width, &f.Height, &f.Path, &f.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &f, nil
}

// ---- usage stats ----

type UsageRow struct {
	UserID           string  `json:"user_id"`
	Messages         int     `json:"messages"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// ModelUsageRow aggregates usage per model. ModelID is empty for replies whose model was deleted.
type ModelUsageRow struct {
	ModelID          string  `json:"model_id"`
	DisplayName      string  `json:"display_name"`
	Messages         int     `json:"messages"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// UsageSince aggregates assistant message usage per user since the given unix ms.
func (s *Store) UsageSince(ctx context.Context, since int64) ([]UsageRow, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT c.user_id, COUNT(*), COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0), COALESCE(SUM(m.cost),0)
		FROM messages m JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'assistant' AND m.created_at >= ? GROUP BY c.user_id ORDER BY 5 DESC, 4 DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageRow{}
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.UserID, &r.Messages, &r.PromptTokens, &r.CompletionTokens, &r.Cost); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ModelUsageSince aggregates assistant message usage per model since the given unix ms.
// Conversations are not joined: deleted users' messages are gone with their conversations anyway.
func (s *Store) ModelUsageSince(ctx context.Context, since int64) ([]ModelUsageRow, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT COALESCE(md.id, ''), COALESCE(md.display_name, ''), COUNT(*),
		COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0), COALESCE(SUM(m.cost),0)
		FROM messages m LEFT JOIN models md ON md.id = m.model_id
		WHERE m.role = 'assistant' AND m.created_at >= ?
		GROUP BY COALESCE(md.id, '') ORDER BY 6 DESC, 5 DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelUsageRow{}
	for rows.Next() {
		var r ModelUsageRow
		if err := rows.Scan(&r.ModelID, &r.DisplayName, &r.Messages, &r.PromptTokens, &r.CompletionTokens, &r.Cost); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
