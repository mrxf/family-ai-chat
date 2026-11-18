// Package storetest opens isolated SQLite or Postgres stores for tests.
package storetest

import (
	"testing"

	"nuan/internal/store"
	"nuan/internal/store/storetest/testdb"
)

// Open uses NUAN_TEST_PG_URL when set, otherwise a temporary SQLite file.
func Open(t testing.TB) *store.Store {
	t.Helper()
	st, err := store.Open(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	return st
}
