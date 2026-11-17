package main

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"nuan/internal/auth"
	"nuan/internal/config"
	"nuan/internal/store"
)

func TestSeedRequiresPasswordAndPreservesExistingData(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := seed(t.Context(), st, config.Config{}, log); err == nil {
		t.Fatal("first startup without an admin password succeeded")
	}
	if n, err := st.CountUsers(t.Context()); err != nil || n != 0 {
		t.Fatalf("failed seed left users: %d %v", n, err)
	}
	cfg := config.Config{AdminUsername: "admin", AdminPassword: "test-password", UpstreamURL: "http://127.0.0.1:1", UpstreamKey: "local-test-key", Models: " gpt-example, ,claude-example"}
	if err := seed(t.Context(), st, cfg, log); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByUsername(t.Context(), "admin")
	if err != nil || !u.IsAdmin() || !auth.VerifyPassword(u.PasswordHash, cfg.AdminPassword) {
		t.Fatalf("admin=%+v err=%v", u, err)
	}
	ps, err := st.ListProviders(t.Context())
	if err != nil || len(ps) != 1 {
		t.Fatalf("providers=%+v err=%v", ps, err)
	}
	ms, err := st.ListModels(t.Context(), true)
	if err != nil || len(ms) != 2 {
		t.Fatalf("models=%+v err=%v", ms, err)
	}
	def, err := st.GetSetting(t.Context(), "default_model_id")
	if err != nil || def != ms[0].ID {
		t.Fatalf("default=%s err=%v", def, err)
	}
	as, err := st.ListAssistants(t.Context(), u.ID)
	if err != nil || len(as) != len(builtinAssistants()) {
		t.Fatalf("assistants=%d err=%v", len(as), err)
	}
	// Re-running with different bootstrap settings must preserve operator edits.
	ps[0].Name = "edited provider"
	if err := st.UpsertProvider(t.Context(), ps[0]); err != nil {
		t.Fatal(err)
	}
	as[0].Name = "edited assistant"
	if err := st.UpsertAssistant(t.Context(), as[0]); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(t.Context(), "default_model_id", ms[1].ID); err != nil {
		t.Fatal(err)
	}
	cfg.AdminPassword, cfg.UpstreamKey, cfg.Models = "replacement", "replacement", "gemini-other"
	if err := seed(t.Context(), st, cfg, log); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetUser(t.Context(), u.ID)
	if err != nil || got.PasswordHash != u.PasswordHash {
		t.Fatalf("admin changed: %+v %v", got, err)
	}
	ps, err = st.ListProviders(t.Context())
	if err != nil || len(ps) != 1 || ps[0].Name != "edited provider" || ps[0].APIKey != "local-test-key" {
		t.Fatalf("provider changed: %+v %v", ps, err)
	}
	ms, err = st.ListModels(t.Context(), false)
	if err != nil || len(ms) != 2 {
		t.Fatalf("models changed: %+v %v", ms, err)
	}
	a, err := st.GetAssistant(t.Context(), as[0].ID, u.ID)
	if err != nil || a.Name != "edited assistant" {
		t.Fatalf("assistant changed: %+v %v", a, err)
	}
	if n, err := st.CountUsers(t.Context()); err != nil || n != 1 {
		t.Fatalf("users=%d %v", n, err)
	}
	if v, err := st.GetSetting(t.Context(), "default_model_id"); err != nil || v != ms[1].ID {
		t.Fatalf("default changed: %s %v", v, err)
	}
}
