package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type User struct {
	ID           string          `json:"id"`
	Username     string          `json:"username"`
	DisplayName  string          `json:"display_name"`
	Avatar       string          `json:"avatar"`
	Role         string          `json:"role"`
	PasswordHash string          `json:"-"`
	Settings     json.RawMessage `json:"settings"`
	Disabled     bool            `json:"disabled"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

const userCols = `id, username, display_name, avatar, role, password_hash, settings, disabled, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var settings string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.Role, &u.PasswordHash, &settings, &u.Disabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	u.Settings = json.RawMessage(settings)
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	now := Now()
	if u.ID == "" {
		u.ID = NewID()
	}
	if len(u.Settings) == 0 {
		u.Settings = json.RawMessage(`{}`)
	}
	u.CreatedAt, u.UpdatedAt = now, now
	_, err := s.w.ExecContext(ctx, `INSERT INTO users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.DisplayName, u.Avatar, u.Role, u.PasswordHash, string(u.Settings), u.Disabled, u.CreatedAt, u.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	return scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// UserPatch holds optional fields; nil means unchanged.
type UserPatch struct {
	DisplayName  *string
	Avatar       *string
	Role         *string
	PasswordHash *string
	Settings     json.RawMessage
	Disabled     *bool
}

func (s *Store) UpdateUser(ctx context.Context, id string, p UserPatch) (*User, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		u, err := scanUser(tx.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if p.DisplayName != nil {
			u.DisplayName = *p.DisplayName
		}
		if p.Avatar != nil {
			u.Avatar = *p.Avatar
		}
		if p.Role != nil {
			u.Role = *p.Role
		}
		if p.PasswordHash != nil {
			u.PasswordHash = *p.PasswordHash
		}
		if p.Settings != nil {
			u.Settings = p.Settings
		}
		if p.Disabled != nil {
			u.Disabled = *p.Disabled
		}
		_, err = tx.ExecContext(ctx, `UPDATE users SET display_name=?, avatar=?, role=?, password_hash=?, settings=?, disabled=?, updated_at=? WHERE id=?`,
			u.DisplayName, u.Avatar, u.Role, u.PasswordHash, string(u.Settings), u.Disabled, Now(), id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return affected(s.w.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id))
}

// ---- sessions ----

type Session struct {
	UserID     string
	Device     string
	CreatedAt  int64
	LastSeenAt int64
	ExpiresAt  int64
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, sess Session) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, device, created_at, last_seen_at, expires_at) VALUES (?,?,?,?,?,?)`,
		tokenHash, sess.UserID, sess.Device, sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt)
	return err
}

func (s *Store) GetSession(ctx context.Context, tokenHash []byte) (*Session, error) {
	var sess Session
	err := s.r.QueryRowContext(ctx, `SELECT user_id, device, created_at, last_seen_at, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&sess.UserID, &sess.Device, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, tokenHash []byte, lastSeen, expires int64) error {
	_, err := s.w.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?`, lastSeen, expires, tokenHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, Now())
	return err
}
