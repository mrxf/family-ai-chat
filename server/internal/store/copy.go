package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Foreign-key order, shared by copy and reverse-order cleanup.
const businessTables = "users sessions providers models settings assistants conversations messages components component_versions pinned_components files image_log asr_configs search_configs memories schedules user_suggestions scheduled_tasks scheduled_task_runs"

// TableCount records row counts verified before committing a copy.
type TableCount struct {
	Table       string
	Source      int64
	Destination int64
}

// CopyTo copies a stable source snapshot into one destination transaction.
// Both applications must be stopped while migrating, and stores must be distinct.
func (s *Store) CopyTo(ctx context.Context, target *Store, force bool) ([]TableCount, error) {
	if s == target {
		return nil, errors.New("source and destination must be different stores")
	}
	opts := &sql.TxOptions{ReadOnly: true}
	if s.pg {
		opts.Isolation = sql.LevelRepeatableRead
	}
	raw, err := s.r.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer raw.Rollback()
	source := &Tx{tx: raw, s: s}
	tables := strings.Fields(businessTables)
	var counts []TableCount
	err = target.tx(ctx, func(dest *Tx) error {
		// Prevent destination writers from changing the emptiness check or row counts.
		if target.pg {
			if _, err := dest.exec(ctx, "LOCK TABLE "+strings.Join(tables, ", ")+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				return err
			}
		}
		// Any row counts: a destination started once already holds the built-in
		// assistants and settings even before an admin exists.
		if !force {
			for _, table := range tables {
				var n int64
				if err := dest.queryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
					return err
				}
				if n != 0 {
					return fmt.Errorf("destination table %s is not empty; use --force to clear business tables", table)
				}
			}
		}
		if force {
			for i := len(tables) - 1; i >= 0; i-- {
				if _, err := dest.exec(ctx, "DELETE FROM "+tables[i]); err != nil {
					return err
				}
			}
		}
		for _, table := range tables {
			if err := copyTable(ctx, source, dest, table); err != nil {
				return fmt.Errorf("copy %s: %w", table, err)
			}
			c := TableCount{Table: table}
			if err := source.queryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&c.Source); err != nil {
				return err
			}
			if err := dest.queryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&c.Destination); err != nil {
				return err
			}
			if c.Source != c.Destination {
				return fmt.Errorf("%s row count mismatch: source=%d destination=%d", table, c.Source, c.Destination)
			}
			counts = append(counts, c)
		}
		if target.pg {
			_, err := dest.exec(ctx, `SELECT setval(pg_get_serial_sequence('image_log', 'id'), COALESCE(MAX(id), 1), MAX(id) IS NOT NULL) FROM image_log`)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}

func copyTable(ctx context.Context, source, dest *Tx, table string) error {
	rows, err := source.query(ctx, "SELECT * FROM "+table)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for i, col := range cols {
		cols[i] = `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
	}
	prefix := "INSERT INTO " + table + " (" + strings.Join(cols, ",") + ") VALUES "
	placeholders := "(" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	const batchSize = 500
	var args []any
	var values []string
	flush := func() error {
		if len(values) == 0 {
			return nil
		}
		_, err := dest.exec(ctx, prefix+strings.Join(values, ","), args...)
		args, values = nil, nil
		return err
	}
	for rows.Next() {
		row, ptrs := make([]any, len(cols)), make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				row[i] = bytes.Clone(b)
			}
		}
		args = append(args, row...)
		values = append(values, placeholders)
		if len(values) == batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}
