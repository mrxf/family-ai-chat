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
	AutoMode    bool    `json:"auto_mode"`
	Title       string  `json:"title"`
	Pinned      bool    `json:"pinned"`
	Private     bool    `json:"private"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
	Snippet     string  `json:"snippet,omitempty"`
}

const convCols = `id, user_id, assistant_id, model_id, auto_mode, title, pinned, private, created_at, updated_at`

func scanConv(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	if err := row.Scan(&c.ID, &c.UserID, &c.AssistantID, &c.ModelID, &c.AutoMode, &c.Title, &c.Pinned, &c.Private, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

func (s *Store) CreateConversation(ctx context.Context, c *Conversation) error {
	now := Now()
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.Private {
		c.Pinned = false
	}
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO conversations (`+convCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.UserID, c.AssistantID, c.ModelID, c.AutoMode, c.Title, c.Pinned, c.Private, c.CreatedAt, c.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// GetConversation returns the conversation only if it belongs to userID.
func (s *Store) GetConversation(ctx context.Context, id, userID string) (*Conversation, error) {
	return scanConv(s.queryRow(ctx, `SELECT `+convCols+` FROM conversations WHERE id = ? AND user_id = ?`, id, userID))
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
		selectCols += `, COALESCE((SELECT m.content FROM messages m WHERE m.conversation_id = c.id AND m.content ` + s.like() + ` ? ESCAPE '\' ORDER BY m.id DESC LIMIT 1), '')`
		args = append(args, like)
	}
	where := []string{"c.user_id = ?", "c.private = 0"}
	args = append(args, q.UserID)
	if q.Before > 0 {
		where = append(where, "c.updated_at < ?")
		args = append(args, q.Before)
	}
	if search != "" {
		like := "%" + escapeLike(search) + "%"
		where = append(where, `(c.title `+s.like()+` ? ESCAPE '\' OR EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.content `+s.like()+` ? ESCAPE '\'))`)
		args = append(args, like, like)
	}
	args = append(args, q.Limit)
	rows, err := s.query(ctx, `SELECT `+selectCols+` FROM conversations c
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
		if err := rows.Scan(&c.ID, &c.UserID, &c.AssistantID, &c.ModelID, &c.AutoMode, &c.Title, &c.Pinned, &c.Private, &c.CreatedAt, &c.UpdatedAt, &body); err != nil {
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
	AutoMode    *bool
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
		sets = append(sets, "pinned = CASE WHEN private = 1 THEN 0 ELSE ? END")
		args = append(args, *p.Pinned)
	}
	if p.ModelID != nil {
		sets = append(sets, "model_id = ?")
		args = append(args, nullable(*p.ModelID))
	}
	if p.AutoMode != nil {
		sets = append(sets, "auto_mode = ?")
		args = append(args, *p.AutoMode)
	}
	if p.AssistantID != nil {
		sets = append(sets, "assistant_id = ?")
		args = append(args, nullable(*p.AssistantID))
	}
	if len(sets) > 0 {
		args = append(args, id, userID)
		if err := affected(s.exec(ctx, `UPDATE conversations SET `+strings.Join(sets, ", ")+` WHERE id = ? AND user_id = ?`, args...)); err != nil {
			return nil, err
		}
	}
	return s.GetConversation(ctx, id, userID)
}

// SetConversationTitle sets the title only if it is still empty (auto titles never overwrite user edits).
func (s *Store) SetConversationTitle(ctx context.Context, id, title string) (bool, error) {
	res, err := s.exec(ctx, `UPDATE conversations SET title = ? WHERE id = ? AND title = ''`, title, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) TouchConversation(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, Now(), id)
	return err
}

func (s *Store) DeleteConversation(ctx context.Context, id, userID string) error {
	_, err := s.DeleteConversationCleanup(ctx, id, userID)
	return err
}

// DeleteConversationCleanup deletes a conversation and, when it is a private
// conversation, also deletes any unreferenced file rows from its messages in the
// same transaction and returns their on-disk paths for immediate removal.
func (s *Store) DeleteConversationCleanup(ctx context.Context, id, userID string) ([]string, error) {
	var paths []string
	err := s.tx(ctx, func(tx *Tx) error {
		// Match extraction/turn locking so an episode cannot appear between deletes.
		var lockedID string
		var isPrivate bool
		if err := tx.queryRow(ctx, `SELECT id, private FROM conversations WHERE id=? AND user_id=?`+s.forUpdate(), id, userID).Scan(&lockedID, &isPrivate); err != nil {
			return notFound(err)
		}
		var fileIDs []string
		if isPrivate {
			rows, err := tx.query(ctx, `SELECT attachments FROM messages WHERE conversation_id = ? AND attachments <> '[]'`, id)
			if err != nil {
				return err
			}
			var rawAtts []string
			for rows.Next() {
				var raw string
				if err := rows.Scan(&raw); err != nil {
					rows.Close()
					return err
				}
				rawAtts = append(rawAtts, raw)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			seen := map[string]bool{}
			for _, raw := range rawAtts {
				var atts []Attachment
				_ = json.Unmarshal([]byte(raw), &atts)
				for _, a := range atts {
					if a.FileID != "" && !seen[a.FileID] {
						seen[a.FileID] = true
						fileIDs = append(fileIDs, a.FileID)
					}
				}
			}
		}
		if _, err := tx.exec(ctx, `DELETE FROM memories WHERE source_conv_id = ? AND user_id = ? AND category = 'episode'`, id, userID); err != nil {
			return err
		}
		if err := affected(tx.exec(ctx, `DELETE FROM conversations WHERE id = ? AND user_id = ?`, id, userID)); err != nil {
			return err
		}
		for _, fid := range fileIDs {
			var p string
			err := tx.queryRow(ctx, `SELECT path FROM files WHERE id = ? AND user_id = ? AND id NOT IN (`+s.referencedFileIDsSQL()+`)`, fid, userID).Scan(&p)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.exec(ctx, `DELETE FROM files WHERE id = ?`, fid); err != nil {
				return err
			}
			paths = append(paths, p)
		}
		return nil
	})
	return paths, err
}

// ExpiredPrivateConversations lists private conversations untouched before the given unix ms.
func (s *Store) ExpiredPrivateConversations(ctx context.Context, before int64) ([]*Conversation, error) {
	rows, err := s.query(ctx, `SELECT `+convCols+` FROM conversations WHERE private = 1 AND updated_at < ? ORDER BY updated_at ASC LIMIT 200`, before)
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
	return out, rows.Err()
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

// WebSource records a web page searched or fetched while generating an assistant message.
type WebSource struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name,omitempty"`
	Date     string `json:"date,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
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
	Sources          []WebSource  `json:"sources,omitempty"`
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

const msgCols = `id, conversation_id, role, content, reasoning, attachments, sources, status, error, model_id, prompt_tokens, completion_tokens, first_token_ms, duration_ms, created_at, updated_at`

func scanMsg(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var att, src string
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Reasoning, &att, &src, &m.Status, &m.Error, &m.ModelID,
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
	_, err := tx.exec(ctx, `INSERT INTO messages (`+msgCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Role, m.Content, m.Reasoning, string(att), string(src), m.Status, m.Error, m.ModelID,
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
	return scanMsg(s.queryRow(ctx, `SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
}

// GetMessageForUser returns a message only if its conversation belongs to userID.
func (s *Store) GetMessageForUser(ctx context.Context, id, userID string) (*Message, error) {
	return scanMsg(s.queryRow(ctx, `SELECT `+prefixCols(msgCols, "m.")+` FROM messages m
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
	sources := m.Sources
	if sources == nil {
		sources = []WebSource{}
	}
	src, _ := json.Marshal(sources)
	_, err := s.exec(ctx, `UPDATE messages SET content=?, reasoning=?, attachments=?, sources=?, status=?, error=?, model_id=?, prompt_tokens=?, completion_tokens=?,
		first_token_ms=?, duration_ms=?, cost=?, updated_at=? WHERE id=?`,
		m.Content, m.Reasoning, string(att), string(src), m.Status, m.Error, m.ModelID, m.PromptTokens, m.CompletionTokens, m.FirstTokenMs, m.DurationMs, m.Cost, m.UpdatedAt, m.ID)
	return err
}

// SetAttachments saves a streaming message's pictures as soon as they exist,
// so they survive a restart in the middle of the reply.
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

type MessageHit struct {
	MessageID         string
	ConversationID    string
	ConversationTitle string
	Role              string
	Content           string
	CreatedAt         int64
}

// SearchUserMessages finds candidate historical messages in non-private conversations
// owned by userID matching any of the given terms, excluding excludeConvID.
func (s *Store) SearchUserMessages(ctx context.Context, userID, excludeConvID string, terms []string, limit int) ([]*MessageHit, error) {
	if limit <= 0 || limit > 200 {
		limit = 80
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
		if len(clean) >= 12 {
			break
		}
	}
	if len(clean) == 0 {
		return []*MessageHit{}, nil
	}
	args := []any{userID, excludeConvID}
	clauses := make([]string, 0, len(clean))
	likeOp := s.like()
	for _, t := range clean {
		pat := "%" + escapeLike(t) + "%"
		clauses = append(clauses, `(m.content `+likeOp+` ? ESCAPE '\' OR c.title `+likeOp+` ? ESCAPE '\')`)
		args = append(args, pat, pat)
	}
	args = append(args, limit)
	q := `SELECT m.id, m.conversation_id, c.title, m.role, m.content, m.created_at
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE c.user_id = ? AND c.private = 0 AND m.conversation_id <> ?
		  AND m.status = 'done' AND m.content <> ''
		  AND (` + strings.Join(clauses, " OR ") + `)
		ORDER BY m.id DESC
		LIMIT ?`
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*MessageHit{}
	for rows.Next() {
		var h MessageHit
		if err := rows.Scan(&h.MessageID, &h.ConversationID, &h.ConversationTitle, &h.Role, &h.Content, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &h)
	}
	return out, rows.Err()
}

// RecentUnextractedConversations returns non-private conversations of userID (excluding
// excludeConvID) touched since the given unix ms that still have unextracted done messages.
func (s *Store) RecentUnextractedConversations(ctx context.Context, userID, excludeConvID string, since int64, limit int) ([]*Conversation, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	rows, err := s.query(ctx, `SELECT `+prefixCols(convCols, "c.")+` FROM conversations c
		WHERE c.user_id = ? AND c.private = 0 AND c.id <> ? AND c.updated_at >= ?
		  AND EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.id > c.memory_cursor AND m.status = 'done')
		ORDER BY c.updated_at DESC
		LIMIT ?`, userID, excludeConvID, since, limit)
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
	return out, rows.Err()
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
	Private   bool   `json:"private,omitempty"`
	Path      string `json:"-"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) CreateFile(ctx context.Context, f *File) error {
	if f.ID == "" {
		f.ID = NewID()
	}
	f.CreatedAt = Now()
	_, err := s.exec(ctx, `INSERT INTO files (id, user_id, mime, size, sha256, width, height, private, path, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.UserID, f.Mime, f.Size, f.SHA256, f.Width, f.Height, f.Private, f.Path, f.CreatedAt)
	return err
}

func (s *Store) GetFile(ctx context.Context, id, userID string) (*File, error) {
	var f File
	err := s.queryRow(ctx, `SELECT id, user_id, mime, size, sha256, width, height, private, path, created_at FROM files WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&f.ID, &f.UserID, &f.Mime, &f.Size, &f.SHA256, &f.Width, &f.Height, &f.Private, &f.Path, &f.CreatedAt)
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
	rows, err := s.query(ctx, `SELECT c.user_id, COUNT(*), COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0), COALESCE(SUM(m.cost),0)
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
	rows, err := s.query(ctx, `SELECT COALESCE(md.id, ''), COALESCE(md.display_name, ''), COUNT(*),
		COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0), COALESCE(SUM(m.cost),0)
		FROM messages m LEFT JOIN models md ON md.id = m.model_id
		WHERE m.role = 'assistant' AND m.created_at >= ?
		GROUP BY COALESCE(md.id, ''), COALESCE(md.display_name, '') ORDER BY 6 DESC, 5 DESC`, since)
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
