// Package auth handles password hashing, opaque session tokens and login throttling.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"nuan/internal/store"
)

// ---- passwords (argon2id, PHC string format) ----

const (
	argonTime    = 2
	argonMemory  = 32 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---- sessions ----

const (
	SessionTTL    = 180 * 24 * time.Hour
	touchInterval = time.Hour
)

var ErrInvalidToken = errors.New("invalid or expired token")

type Sessions struct {
	Store *store.Store
}

func hashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

// Create issues a new token for the user.
func (s *Sessions) Create(ctx context.Context, userID, device string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	err := s.Store.CreateSession(ctx, hashToken(tok), store.Session{
		UserID: userID, Device: device,
		CreatedAt: now.UnixMilli(), LastSeenAt: now.UnixMilli(), ExpiresAt: now.Add(SessionTTL).UnixMilli(),
	})
	return tok, err
}

// Validate resolves a token to its user, extending the session at most once per hour.
func (s *Sessions) Validate(ctx context.Context, tok string) (*store.User, error) {
	if tok == "" {
		return nil, ErrInvalidToken
	}
	h := hashToken(tok)
	sess, err := s.Store.GetSession(ctx, h)
	if err != nil {
		return nil, ErrInvalidToken
	}
	now := time.Now()
	if now.UnixMilli() > sess.ExpiresAt {
		_ = s.Store.DeleteSession(ctx, h)
		return nil, ErrInvalidToken
	}
	u, err := s.Store.GetUser(ctx, sess.UserID)
	if err != nil || u.Disabled {
		return nil, ErrInvalidToken
	}
	if now.Sub(time.UnixMilli(sess.LastSeenAt)) > touchInterval {
		_ = s.Store.TouchSession(ctx, h, now.UnixMilli(), now.Add(SessionTTL).UnixMilli())
	}
	return u, nil
}

func (s *Sessions) Revoke(ctx context.Context, tok string) error {
	return s.Store.DeleteSession(ctx, hashToken(tok))
}

// ---- login throttling ----

// Limiter is a tiny in-memory failure counter keyed by IP and by username.
// After maxFails failures within window the key is blocked until the window passes.
type Limiter struct {
	mu       sync.Mutex
	fails    map[string][]time.Time
	maxFails int
	window   time.Duration
}

func NewLimiter(maxFails int, window time.Duration) *Limiter {
	return &Limiter{fails: map[string][]time.Time{}, maxFails: maxFails, window: window}
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	list := l.fails[key]
	i := 0
	for i < len(list) && now.Sub(list[i]) > l.window {
		i++
	}
	list = list[i:]
	if len(list) == 0 {
		delete(l.fails, key)
	} else {
		l.fails[key] = list
	}
	return list
}

// Blocked reports whether any of the keys is currently locked out.
func (l *Limiter) Blocked(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		if len(l.prune(k, now)) >= l.maxFails {
			return true
		}
	}
	return false
}

func (l *Limiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		l.fails[k] = append(l.prune(k, now), now)
	}
}

func (l *Limiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.fails, k)
	}
}
