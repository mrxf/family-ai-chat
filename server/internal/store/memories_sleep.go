package store

import (
	"context"
	"strings"
)

// SleepDigestSpec describes one compressed long-term episode digest and the
// raw episode IDs it consolidates.
type SleepDigestSpec struct {
	Digest    *Memory
	SourceIDs []string
}

// CoreDedupUpdate rewrites one core memory when merging redundant entries.
type CoreDedupUpdate struct {
	RefID    string
	Content  string
	Keywords string
}

// TouchMemoriesRecall records a retrieval activation for each matched memory ID,
// debounced by cooldownMs so repeated turns in the same day do not inflate counts.
// updated_at is intentionally left untouched to preserve optimistic embedding locks.
func (s *Store) TouchMemoriesRecall(ctx context.Context, userID string, ids []string, nowMs, cooldownMs int64) error {
	seen := map[string]bool{}
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 || userID == "" {
		return nil
	}
	cutoff := nowMs - cooldownMs
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(clean)), ",")
	args := make([]any, 0, len(clean)+3)
	args = append(args, nowMs, userID)
	for _, id := range clean {
		args = append(args, id)
	}
	args = append(args, cutoff)
	_, err := s.exec(ctx,
		`UPDATE memories SET recall_count = recall_count + 1, last_recalled_at = ?
		 WHERE user_id = ? AND status = 'active' AND id IN (`+placeholders+`)
		   AND (last_recalled_at IS NULL OR last_recalled_at <= ?)`,
		args...)
	return err
}

// ListMemoryUserIDs returns distinct user IDs that own at least one active memory.
func (s *Store) ListMemoryUserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.query(ctx, `SELECT DISTINCT user_id FROM memories WHERE status = 'active' ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PurgeInactiveMemories physically deletes superseded and archived rows whose
// updated_at is older than cutoffMs.
func (s *Store) PurgeInactiveMemories(ctx context.Context, cutoffMs int64) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM memories WHERE status <> 'active' AND updated_at < ?`, cutoffMs)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// TransitionExpiredOngoing converts non-manual ongoing memories whose valid_until
// passed before expiredBeforeMs into retrieval-layer episode memories, preserving
// their original event_at timestamp.
func (s *Store) TransitionExpiredOngoing(ctx context.Context, userID string, expiredBeforeMs, nowMs int64, rewrite func(*Memory) string) (int, error) {
	converted := 0
	err := s.tx(ctx, func(tx *Tx) error {
		var uid string
		if err := tx.queryRow(ctx, `SELECT id FROM users WHERE id = ?`+s.forUpdate(), userID).Scan(&uid); err != nil {
			return notFound(err)
		}
		rows, err := tx.query(ctx,
			`SELECT `+memoryCols+` FROM memories
			 WHERE user_id = ? AND status = 'active' AND category = 'ongoing'
			   AND source <> 'manual' AND valid_until IS NOT NULL AND valid_until < ?`+s.forUpdate(),
			userID, expiredBeforeMs)
		items, err := readMemories(rows, err)
		if err != nil {
			return err
		}
		for _, m := range items {
			nextContent := strings.TrimSpace(m.Content)
			if rewrite != nil {
				if r := strings.TrimSpace(rewrite(m)); r != "" {
					nextContent = r
				}
			}
			if nextContent == m.Content {
				if _, err := tx.exec(ctx,
					`UPDATE memories SET category = 'episode', pinned = 0, valid_until = NULL, updated_at = ? WHERE id = ?`,
					nowMs, m.ID); err != nil {
					return err
				}
			} else {
				if _, err := tx.exec(ctx,
					`UPDATE memories SET category = 'episode', content = ?, pinned = 0, valid_until = NULL, embedding = NULL, embedding_model = '', updated_at = ? WHERE id = ?`,
					nextContent, nowMs, m.ID); err != nil {
					return err
				}
			}
			converted++
		}
		return nil
	})
	return converted, err
}

// ApplySleepEpisodeCompression commits compressed episode digests and archives
// their source fragments and dropped trivial episodes atomically. Any source row
// edited or deleted since expectedUpdatedAt was captured is skipped safely.
func (s *Store) ApplySleepEpisodeCompression(ctx context.Context, userID string, expectedUpdatedAt map[string]int64, specs []SleepDigestSpec, dropIDs []string, nowMs int64) (created int, archived int, err error) {
	if len(specs) == 0 && len(dropIDs) == 0 {
		return 0, 0, nil
	}
	err = s.tx(ctx, func(tx *Tx) error {
		var uid string
		if err := tx.queryRow(ctx, `SELECT id FROM users WHERE id = ?`+s.forUpdate(), userID).Scan(&uid); err != nil {
			return notFound(err)
		}
		archivedSet := map[string]bool{}
		isUnchangedActiveEpisode := func(id string) (bool, error) {
			exp, ok := expectedUpdatedAt[id]
			if !ok {
				return false, nil
			}
			var updated int64
			var status, source, category string
			qErr := tx.queryRow(ctx, `SELECT updated_at, status, source, category FROM memories WHERE id = ? AND user_id = ?`+s.forUpdate(), id, userID).
				Scan(&updated, &status, &source, &category)
			if qErr != nil {
				return false, nil // deleted concurrently
			}
			return status == "active" && source != "manual" && category == "episode" && updated == exp, nil
		}

		for _, spec := range specs {
			if spec.Digest == nil || len(spec.SourceIDs) == 0 || strings.TrimSpace(spec.Digest.Content) == "" {
				continue
			}
			allValid := true
			for _, sid := range spec.SourceIDs {
				if archivedSet[sid] {
					allValid = false
					break
				}
				ok, vErr := isUnchangedActiveEpisode(sid)
				if vErr != nil {
					return vErr
				}
				if !ok {
					allValid = false
					break
				}
			}
			if !allValid {
				continue
			}
			d := spec.Digest
			if d.ID == "" {
				d.ID = NewID()
			}
			d.UserID = userID
			d.Category = "episode"
			d.Pinned = false
			d.Compressed = true
			d.Status = "active"
			if d.Source == "" {
				d.Source = "auto"
			}
			if d.EventAt <= 0 {
				d.EventAt = nowMs
			}
			if d.CreatedAt <= 0 {
				d.CreatedAt = d.EventAt
			}
			if strings.TrimSpace(d.Keywords) != "" {
				d.KeywordsChecked = true
			}
			if _, err := tx.exec(ctx,
				`INSERT INTO memories (`+memoryCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				d.ID, d.UserID, d.AssistantID, d.Category, d.Content, d.Keywords, d.KeywordsChecked,
				EncodeEmbedding(d.Embedding), d.EmbeddingModel, d.Source, d.SourceConvID, d.Status, d.Pinned,
				d.SupersededBy, d.ValidUntil, d.EventAt, d.LastRecalledAt, d.RecallCount, d.Compressed,
				d.CreatedAt, nowMs); err != nil {
				return err
			}
			created++
			for _, sid := range spec.SourceIDs {
				if _, err := tx.exec(ctx,
					`UPDATE memories SET status = 'archived', superseded_by = ?, embedding = NULL, embedding_model = '', updated_at = ?
					 WHERE id = ? AND user_id = ? AND status = 'active' AND source <> 'manual'`,
					d.ID, nowMs, sid, userID); err != nil {
					return err
				}
				archivedSet[sid] = true
				archived++
			}
		}

		for _, id := range dropIDs {
			if archivedSet[id] {
				continue
			}
			ok, vErr := isUnchangedActiveEpisode(id)
			if vErr != nil {
				return vErr
			}
			if !ok {
				continue
			}
			res, err := tx.exec(ctx,
				`UPDATE memories SET status = 'archived', embedding = NULL, embedding_model = '', updated_at = ?
				 WHERE id = ? AND user_id = ? AND status = 'active' AND source <> 'manual' AND category = 'episode'`,
				nowMs, id, userID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				archivedSet[id] = true
				archived++
			}
		}
		return nil
	})
	return created, archived, err
}

// ArchiveDecayMemories archives stale or over-capacity retrieval memories while
// enforcing a hard database-level invariant: health, family, profile, pinned core
// items, and manual memories are never archived.
func (s *Store) ArchiveDecayMemories(ctx context.Context, userID string, expectedUpdatedAt map[string]int64, ids []string, nowMs int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	archived := 0
	err := s.tx(ctx, func(tx *Tx) error {
		var uid string
		if err := tx.queryRow(ctx, `SELECT id FROM users WHERE id = ?`+s.forUpdate(), userID).Scan(&uid); err != nil {
			return notFound(err)
		}
		for _, id := range ids {
			exp, ok := expectedUpdatedAt[id]
			if !ok {
				continue
			}
			res, err := tx.exec(ctx,
				`UPDATE memories SET status = 'archived', embedding = NULL, embedding_model = '', updated_at = ?
				 WHERE id = ? AND user_id = ? AND status = 'active' AND pinned = 0
				   AND source <> 'manual' AND category NOT IN ('health', 'family', 'profile')
				   AND updated_at = ?`,
				nowMs, id, userID, exp)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				archived++
			}
		}
		return nil
	})
	return archived, err
}

// ApplySleepCoreDedup merges redundant core memories atomically. If any referenced
// item was edited or removed while the model ran, the merge is aborted cleanly.
func (s *Store) ApplySleepCoreDedup(ctx context.Context, userID string, expectedUpdatedAt map[string]int64, updates []CoreDedupUpdate, dropIDs []string, nowMs int64) (rewritten int, dropped int, err error) {
	if len(updates) == 0 && len(dropIDs) == 0 {
		return 0, 0, nil
	}
	err = s.tx(ctx, func(tx *Tx) error {
		var uid string
		if err := tx.queryRow(ctx, `SELECT id FROM users WHERE id = ?`+s.forUpdate(), userID).Scan(&uid); err != nil {
			return notFound(err)
		}
		existingByID := map[string]*Memory{}
		for id, exp := range expectedUpdatedAt {
			m, qErr := scanMemory(tx.queryRow(ctx, `SELECT `+memoryCols+` FROM memories WHERE id = ? AND user_id = ?`+s.forUpdate(), id, userID))
			if qErr != nil || m.Status != "active" || m.Source == "manual" || m.UpdatedAt != exp {
				// Concurrent edit or deletion: abort the batch without partial mutation.
				return nil
			}
			existingByID[id] = m
		}
		dropSet := map[string]bool{}
		for _, id := range dropIDs {
			dropSet[id] = true
		}
		for _, u := range updates {
			if dropSet[u.RefID] {
				continue
			}
			old, ok := existingByID[u.RefID]
			if !ok {
				continue
			}
			content := strings.TrimSpace(u.Content)
			if content == "" || content == old.Content {
				continue
			}
			kw := strings.TrimSpace(u.Keywords)
			checked := kw != ""
			next := &Memory{
				ID:              NewID(),
				UserID:          userID,
				AssistantID:     old.AssistantID,
				Category:        old.Category,
				Content:         content,
				Keywords:        kw,
				KeywordsChecked: checked,
				Source:          old.Source,
				SourceConvID:    old.SourceConvID,
				Status:          "active",
				Pinned:          old.Pinned,
				ValidUntil:      old.ValidUntil,
				EventAt:         old.EventAt,
				LastRecalledAt:  old.LastRecalledAt,
				RecallCount:     old.RecallCount,
				CreatedAt:       old.CreatedAt,
			}
			if _, err := tx.exec(ctx,
				`UPDATE memories SET status = 'superseded', superseded_by = ?, embedding = NULL, embedding_model = '', updated_at = ?
				 WHERE id = ? AND user_id = ? AND status = 'active' AND source <> 'manual'`,
				next.ID, nowMs, old.ID, userID); err != nil {
				return err
			}
			if _, err := tx.exec(ctx,
				`INSERT INTO memories (`+memoryCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				next.ID, next.UserID, next.AssistantID, next.Category, next.Content, next.Keywords, next.KeywordsChecked,
				nil, "", next.Source, next.SourceConvID, next.Status, next.Pinned,
				nil, next.ValidUntil, next.EventAt, next.LastRecalledAt, next.RecallCount, next.Compressed,
				next.CreatedAt, nowMs); err != nil {
				return err
			}
			rewritten++
		}
		for _, id := range dropIDs {
			if _, ok := existingByID[id]; !ok {
				continue
			}
			res, err := tx.exec(ctx,
				`UPDATE memories SET status = 'superseded', embedding = NULL, embedding_model = '', updated_at = ?
				 WHERE id = ? AND user_id = ? AND status = 'active' AND source <> 'manual'`,
				nowMs, id, userID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				dropped++
			}
		}
		return demoteMemories(ctx, tx, userID)
	})
	return rewritten, dropped, err
}
