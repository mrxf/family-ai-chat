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

