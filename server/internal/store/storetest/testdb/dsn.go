// Package testdb allocates isolated database DSNs without importing store.
package testdb

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DSN returns an isolated database selected by NUAN_TEST_PG_URL.
func DSN(t testing.TB) string {
	t.Helper()
	if raw := os.Getenv("NUAN_TEST_PG_URL"); raw != "" {
		return PostgresDSN(t, raw)
	}
	return filepath.Join(t.TempDir(), "test.sqlite")
}

// PostgresDSN creates a private schema; callers must close their pools before cleanup.
func PostgresDSN(t testing.TB, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "nuan_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, raw)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
