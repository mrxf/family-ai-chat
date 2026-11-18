package store

import "context"

// LogImage records one picture drawn for a user (daily limit).
func (s *Store) LogImage(ctx context.Context, userID, modelID string, cost float64) error {
	_, err := s.exec(ctx, `INSERT INTO image_log (user_id, model_id, cost, created_at) VALUES (?,?,?,?)`, userID, modelID, cost, Now())
	return err
}

// ImagesSince counts the pictures drawn for a user since the given unix ms.
func (s *Store) ImagesSince(ctx context.Context, userID string, since int64) (int, error) {
	var n int
	err := s.queryRow(ctx, `SELECT COUNT(*) FROM image_log WHERE user_id = ? AND created_at >= ?`, userID, since).Scan(&n)
	return n, err
}

// OrphanFiles lists files created before the given unix ms that no message
// refers to any more (conversation deleted, message edited away or regenerated,
// upload never sent, admin's test pictures).
func (s *Store) OrphanFiles(ctx context.Context, before int64) ([]*File, error) {
	refs := `SELECT json_extract(j.value, '$.file_id') FROM messages m, json_each(m.attachments) j WHERE m.attachments <> '[]' AND json_extract(j.value, '$.file_id') IS NOT NULL`
	if s.pg {
		refs = `SELECT j.value ->> 'file_id' FROM messages m, jsonb_array_elements(m.attachments::jsonb) j WHERE j.value ->> 'file_id' IS NOT NULL`
	}
	rows, err := s.query(ctx, `SELECT id, user_id, path FROM files WHERE created_at < ? AND id NOT IN (`+refs+`)`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f := &File{}
		if err := rows.Scan(&f.ID, &f.UserID, &f.Path); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteFile removes a file row (the caller removes the file on disk).
func (s *Store) DeleteFile(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM files WHERE id = ?`, id)
	return err
}

// FileIDs returns the ids of all stored files.
func (s *Store) FileIDs(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.query(ctx, `SELECT id FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}
