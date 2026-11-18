package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
)

var queryCache sync.Map

// bind rewrites placeholders outside SQL string literals. Backslashes are literal
// in both SQLite strings and Postgres standard_conforming_strings; quotes double.
func bind(query string) string {
	if cached, ok := queryCache.Load(query); ok {
		return cached.(string)
	}
	var b strings.Builder
	quoted, n := false, 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' {
			b.WriteByte(c)
			if quoted && i+1 < len(query) && query[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			quoted = !quoted
		} else if c == '?' && !quoted {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(c)
		}
	}
	result := b.String()
	queryCache.Store(query, result)
	return result
}

func (s *Store) prepare(query string, args []any) (string, []any) {
	if !s.pg {
		return query, args
	}
	converted := make([]any, len(args))
	for i, arg := range args {
		if v, ok := arg.(bool); ok {
			converted[i] = int64(b2i(v))
		} else {
			converted[i] = arg
		}
	}
	return bind(query), converted
}

func (s *Store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	query, args = s.prepare(query, args)
	return s.w.ExecContext(ctx, query, args...)
}

func (s *Store) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	query, args = s.prepare(query, args)
	return s.r.QueryContext(ctx, query, args...)
}

func (s *Store) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	query, args = s.prepare(query, args)
	return s.r.QueryRowContext(ctx, query, args...)
}

// Tx applies the store dialect within a write or snapshot transaction.
type Tx struct {
	tx *sql.Tx
	s  *Store
}

func (tx *Tx) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	query, args = tx.s.prepare(query, args)
	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx *Tx) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	query, args = tx.s.prepare(query, args)
	return tx.tx.QueryContext(ctx, query, args...)
}

func (tx *Tx) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	query, args = tx.s.prepare(query, args)
	return tx.tx.QueryRowContext(ctx, query, args...)
}

func (s *Store) forUpdate() string {
	if s.pg {
		return " FOR UPDATE"
	}
	return ""
}

func (s *Store) like() string {
	if s.pg {
		return "ILIKE"
	}
	return "LIKE"
}

// Lock the table even when empty so appending and reordering cannot race.
func (tx *Tx) lockModels(ctx context.Context) error {
	if !tx.s.pg {
		return nil
	}
	_, err := tx.exec(ctx, `LOCK TABLE models IN SHARE ROW EXCLUSIVE MODE`)
	return err
}
