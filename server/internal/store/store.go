// Package store is the persistence layer for SQLite and Postgres. SQL lives
// here so the rest of the server only sees plain Go types.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a row does not exist (or is not visible to the caller).
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on unique constraint violations.
var ErrConflict = errors.New("conflict")

// Store uses a single Postgres pool or separate SQLite writer and reader pools.
type Store struct {
	pg bool
	w  *sql.DB
	r  *sql.DB
}

func Open(dsn string) (*Store, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(10)
		if err := waitReady(db, 15*time.Second); err != nil {
			db.Close()
			return nil, fmt.Errorf("connect postgres: %w", err)
		}
		s := &Store{w: db, r: db, pg: true}
		if err := s.migrate(context.Background()); err != nil {
			s.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
		return s, nil
	}
	path := dsn
	pragmas := "_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)"
	w, err := sql.Open("sqlite", "file:"+path+"?"+pragmas+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxIdleTime(0)
	r, err := sql.Open("sqlite", "file:"+path+"?"+pragmas+"&mode=ro")
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(max(4, runtime.NumCPU()))
	s := &Store{w: w, r: r}
	if err := s.migrate(context.Background()); err != nil {
		s.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.pg {
		return s.w.Close()
	}
	return errors.Join(s.r.Close(), s.w.Close())
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, applied_at BIGINT NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := s.queryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	dir := "migrations/sqlite"
	if s.pg {
		dir = "migrations/postgres"
	}
	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		v, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		if v <= current {
			continue
		}
		body, err := migrationFS.ReadFile(filepath.ToSlash(filepath.Join(dir, name)))
		if err != nil {
			return err
		}
		err = s.tx(ctx, func(tx *Tx) error {
			if _, err := tx.exec(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err := tx.exec(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, v, Now())
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// tx runs fn in a write transaction.
func (s *Store) tx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&Tx{tx: tx, s: s}); err != nil {
		return err
	}
	return tx.Commit()
}

// Now returns the current time in unix milliseconds.
func Now() int64 { return time.Now().UnixMilli() }

// NewID returns a new time-ordered UUIDv7 string.
func NewID() string { return uuid.Must(uuid.NewV7()).String() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return (errors.As(err, &pgErr) && pgErr.Code == "23505") || (err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed"))
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// prefixCols turns "a, b" into "m.a, m.b".
func prefixCols(cols, prefix string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// waitReady retries the first connection for a while: after a reboot the
// Postgres container may come up later than the server.
func waitReady(db *sql.DB, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	delay := 250 * time.Millisecond
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := db.PingContext(ctx)
		cancel()
		if err == nil || time.Now().Add(delay).After(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, 2*time.Second)
	}
}
