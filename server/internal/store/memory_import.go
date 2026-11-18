package store

import (
	"context"
	"strings"
	"unicode"
)

// MemoryKey normalizes a memory for duplicate detection: case, spacing and
// punctuation are ignored, as is the leading "用户" subject.
func MemoryKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimPrefix(b.String(), "用户")
}

// MemoryImport is one confirmed import candidate. ReplacesID names an active
// memory of the same user that the new one supersedes.
type MemoryImport struct {
	Memory     *Memory
	ReplacesID string
}

// ImportMemories inserts confirmed import candidates for one user. Items whose
// text already exists (in the batch or among active memories) are skipped; a
// replaced memory that is gone or belongs to someone else is simply left alone.
func (s *Store) ImportMemories(ctx context.Context, user string, items []MemoryImport) (saved []*Memory, skipped int, err error) {
	err = s.tx(ctx, func(tx *Tx) error {
		saved, skipped = nil, 0
		// Serialize with background extraction for the same user.
		var id string
		if err := tx.queryRow(ctx, `SELECT id FROM users WHERE id=?`+s.forUpdate(), user).Scan(&id); err != nil {
			return notFound(err)
		}
		rows, err := tx.query(ctx, `SELECT content FROM memories WHERE user_id=? AND status='active'`, user)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return err
			}
			seen[MemoryKey(c)] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		now := Now()
		for _, item := range items {
			m := item.Memory
			key := MemoryKey(m.Content)
			if key == "" || seen[key] {
				skipped++
				continue
			}
			seen[key] = true
			m.ID = NewID()
			m.UserID = user
			m.AssistantID = nil
			if item.ReplacesID != "" {
				if _, err := tx.exec(ctx, `UPDATE memories SET status='superseded',superseded_by=?,updated_at=? WHERE id=? AND user_id=? AND status='active' AND assistant_id IS NULL AND category<>'episode'`, m.ID, now, item.ReplacesID, user); err != nil {
					return err
				}
			}
			if err := insertMemory(ctx, tx, m); err != nil {
				return err
			}
			saved = append(saved, m)
		}
		return demoteMemories(ctx, tx, user)
	})
	if err != nil {
		return nil, 0, err
	}
	return saved, skipped, nil
}
