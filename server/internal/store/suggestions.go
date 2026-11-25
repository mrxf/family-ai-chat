package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// HomeSuggestion is a single personalized prompt shown in the home "猜你想问" card.
type HomeSuggestion struct {
	Emoji string `json:"emoji"`
	Text  string `json:"text"`
}

// UserSuggestions stores the cached home suggestion pool and refresh watermark for one user.
type UserSuggestions struct {
	UserID      string           `json:"user_id"`
	Items       []HomeSuggestion `json:"items"`
	GeneratedAt int64            `json:"generated_at"`
	LastMsgAt   int64            `json:"last_msg_at"`
	UpdatedAt   int64            `json:"updated_at"`
}

// RecentConversationDigest summarizes a recent non-private conversation for suggestion analysis.
type RecentConversationDigest struct {
	ConversationID string
	Title          string
	UpdatedAt      int64
	FirstUserMsg   string
	LastUserMsg    string
}

// GetUserSuggestions returns the cached suggestion pool for a user, or (nil, nil) if none exists.
func (s *Store) GetUserSuggestions(ctx context.Context, userID string) (*UserSuggestions, error) {
	var raw string
	out := &UserSuggestions{UserID: userID}
	err := s.queryRow(ctx,
		`SELECT items, generated_at, last_msg_at, updated_at FROM user_suggestions WHERE user_id=?`,
		userID,
	).Scan(&raw, &out.GeneratedAt, &out.LastMsgAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out.Items)
	}
	if out.Items == nil {
		out.Items = []HomeSuggestion{}
	}
	return out, nil
}

// SaveUserSuggestions upserts the personalized suggestion pool and watermark for a user.
func (s *Store) SaveUserSuggestions(ctx context.Context, sug *UserSuggestions) error {
	items := sug.Items
	if items == nil {
		items = []HomeSuggestion{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	now := Now()
	if sug.UpdatedAt == 0 {
		sug.UpdatedAt = now
	}
	_, err = s.exec(ctx,
		`INSERT INTO user_suggestions (user_id, items, generated_at, last_msg_at, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   items=excluded.items,
		   generated_at=excluded.generated_at,
		   last_msg_at=excluded.last_msg_at,
		   updated_at=excluded.updated_at`,
		sug.UserID, string(raw), sug.GeneratedAt, sug.LastMsgAt, sug.UpdatedAt,
	)
	return err
}

// CountNewUserActivitySince counts non-private conversations and user messages created after sinceMS,
// along with the latest user message timestamp.
func (s *Store) CountNewUserActivitySince(ctx context.Context, userID string, sinceMS int64) (newConvs, newMsgs int, latestMsgAt int64, err error) {
	err = s.queryRow(ctx,
		`SELECT COUNT(DISTINCT m.conversation_id), COUNT(*), COALESCE(MAX(m.created_at), 0)
		 FROM messages m
		 JOIN conversations c ON c.id = m.conversation_id
		 WHERE c.user_id = ? AND c.private = 0 AND m.role = 'user' AND m.created_at > ?`,
		userID, sinceMS,
	).Scan(&newConvs, &newMsgs, &latestMsgAt)
	return
}

// ListRecentConversationDigests returns compact summaries of recent non-private conversations
// updated after sinceMS (newest first), including their first and last non-empty user message.
func (s *Store) ListRecentConversationDigests(ctx context.Context, userID string, sinceMS int64, limit int) ([]RecentConversationDigest, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.query(ctx,
		`SELECT id, title, updated_at
		 FROM conversations
		 WHERE user_id = ? AND private = 0 AND updated_at >= ?
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		userID, sinceMS, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var digests []RecentConversationDigest
	for rows.Next() {
		var d RecentConversationDigest
		if err := rows.Scan(&d.ConversationID, &d.Title, &d.UpdatedAt); err != nil {
			return nil, err
		}
		digests = append(digests, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range digests {
		cid := digests[i].ConversationID
		var first, last string
		_ = s.queryRow(ctx,
			`SELECT content FROM messages
			 WHERE conversation_id = ? AND role = 'user' AND content <> ''
			 ORDER BY created_at ASC LIMIT 1`,
			cid,
		).Scan(&first)
		_ = s.queryRow(ctx,
			`SELECT content FROM messages
			 WHERE conversation_id = ? AND role = 'user' AND content <> ''
			 ORDER BY created_at DESC LIMIT 1`,
			cid,
		).Scan(&last)
		digests[i].FirstUserMsg = first
		if last != first {
			digests[i].LastUserMsg = last
		}
	}
	return digests, nil
}
