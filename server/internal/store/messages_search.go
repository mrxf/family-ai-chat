package store

import (
	"context"
	"strings"
)

// ConversationEpisodes returns active episode memory contents recorded for convID.
func (s *Store) ConversationEpisodes(ctx context.Context, convID, userID string) ([]string, error) {
	rows, err := s.query(ctx, `SELECT content FROM memories
		WHERE source_conv_id = ? AND user_id = ? AND category = 'episode' AND status = 'active'
		ORDER BY created_at ASC, id ASC LIMIT 5`, convID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// SearchConversationMessages searches messages strictly before upperBound within a single
// conversation (including private conversations when called for that active conversation).
func (s *Store) SearchConversationMessages(ctx context.Context, convID, upperBound string, terms []string, limit int) ([]*Message, error) {
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
		if len(clean) >= 16 {
			break
		}
	}
	if len(clean) == 0 {
		return []*Message{}, nil
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE conversation_id = ?`
	args := []any{convID}
	if upperBound != "" {
		q += ` AND id < ?`
		args = append(args, upperBound)
	}
	q += ` AND status <> ? AND status <> ? AND content <> ''`
	args = append(args, StatusError, StatusStreaming)
	likeOp := s.like()
	clauses := make([]string, 0, len(clean))
	for _, t := range clean {
		clauses = append(clauses, `content `+likeOp+` ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(t)+"%")
	}
	q += ` AND (` + strings.Join(clauses, " OR ") + `) ORDER BY id DESC LIMIT ?`
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
	return out, rows.Err()
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
// owned by userID matching any of the given terms, excluding active uncompacted turns
// of excludeConvID while still searching already-compacted early messages of excludeConvID.
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
		WHERE c.user_id = ? AND c.private = 0
		  AND (m.conversation_id <> ? OR (c.summary_cursor <> '' AND m.id <= c.summary_cursor))
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
