package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"nuan/internal/store"
)

func TestMigrateDB(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "source.sqlite"), filepath.Join(dir, "target.sqlite")
	source, err := store.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.CreateUser(t.Context(), &store.User{Username: "migrate", DisplayName: "迁移用户", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := migrateDB([]string{"--from", from, "--to", to}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "users: source=1 destination=1") {
		t.Fatalf("output: %s", out.String())
	}
	if err := migrateDB([]string{"--from", from, "--to", to}, &out); err == nil {
		t.Fatal("expected nonempty destination error")
	}
	if err := migrateDB([]string{"--from", from, "--to", to, "--force"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--from", from}, {"--from", from, "--to", from}} {
		if err := migrateDB(args, &out); err == nil {
			t.Fatalf("expected invalid args: %v", args)
		}
	}
}
