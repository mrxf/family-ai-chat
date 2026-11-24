package store

import "context"

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
