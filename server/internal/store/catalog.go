package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type Provider struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // preset: openai, openrouter, gemini, ... or custom
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"-"`
	Enabled   bool   `json:"enabled"`
	Sort      int    `json:"sort"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Model kinds.
const (
	KindChat  = "chat"
	KindImage = "image"
)

type Model struct {
	ID            string  `json:"id"`
	ProviderID    string  `json:"provider_id"`
	Kind          string  `json:"kind"` // KindChat | KindImage
	UpstreamModel string  `json:"upstream_model"`
	DisplayName   string  `json:"display_name"`
	Description   string  `json:"description"`
	Vision        bool    `json:"vision"`
	Tools         bool    `json:"tools"` // may call tools (chat models)
	DefaultEffort string  `json:"default_effort"`
	ThinkEffort   string  `json:"think_effort"`
	ContextWindow int     `json:"context_window"`
	MaxOutput     int     `json:"max_output"`
	InputPrice    float64 `json:"input_price"`  // USD per million prompt tokens (0 = unknown)
	OutputPrice   float64 `json:"output_price"` // USD per million completion tokens
	Enabled       bool    `json:"enabled"`
	Sort          int     `json:"sort"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

// EstimateCost returns the USD cost of a reply from the configured prices.
func (m *Model) EstimateCost(promptTokens, completionTokens int) float64 {
	return (float64(promptTokens)*m.InputPrice + float64(completionTokens)*m.OutputPrice) / 1e6
}

// ---- providers ----

const providerCols = `id, kind, name, base_url, api_key, enabled, sort, created_at, updated_at`

func scanProvider(row interface{ Scan(...any) error }) (*Provider, error) {
	var p Provider
	if err := row.Scan(&p.ID, &p.Kind, &p.Name, &p.BaseURL, &p.APIKey, &p.Enabled, &p.Sort, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

func (s *Store) UpsertProvider(ctx context.Context, p *Provider) error {
	now := Now()
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	if p.Kind == "" {
		p.Kind = "custom"
	}
	p.UpdatedAt = now
	_, err := s.w.ExecContext(ctx, `INSERT INTO providers (`+providerCols+`) VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, name=excluded.name, base_url=excluded.base_url, api_key=excluded.api_key,
		enabled=excluded.enabled, sort=excluded.sort, updated_at=excluded.updated_at`,
		p.ID, p.Kind, p.Name, p.BaseURL, p.APIKey, p.Enabled, p.Sort, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) GetProvider(ctx context.Context, id string) (*Provider, error) {
	return scanProvider(s.r.QueryRowContext(ctx, `SELECT `+providerCols+` FROM providers WHERE id = ?`, id))
}

func (s *Store) ListProviders(ctx context.Context) ([]*Provider, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+providerCols+` FROM providers ORDER BY sort, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	return affected(s.w.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id))
}

// ---- models ----

const modelCols = `id, provider_id, kind, upstream_model, display_name, description, vision, tools, default_effort, think_effort, context_window, max_output, input_price, output_price, enabled, sort, created_at, updated_at`

func scanModel(row interface{ Scan(...any) error }) (*Model, error) {
	var m Model
	if err := row.Scan(&m.ID, &m.ProviderID, &m.Kind, &m.UpstreamModel, &m.DisplayName, &m.Description, &m.Vision, &m.Tools, &m.DefaultEffort, &m.ThinkEffort,
		&m.ContextWindow, &m.MaxOutput, &m.InputPrice, &m.OutputPrice, &m.Enabled, &m.Sort, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &m, nil
}

func (s *Store) UpsertModel(ctx context.Context, m *Model) error {
	return upsertModel(ctx, s.w, m)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertModel(ctx context.Context, db execer, m *Model) error {
	now := Now()
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.Kind == "" {
		m.Kind = KindChat
	}
	_, err := db.ExecContext(ctx, `INSERT INTO models (`+modelCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET provider_id=excluded.provider_id, kind=excluded.kind, upstream_model=excluded.upstream_model,
		display_name=excluded.display_name, description=excluded.description, vision=excluded.vision, tools=excluded.tools, default_effort=excluded.default_effort, think_effort=excluded.think_effort,
		context_window=excluded.context_window, max_output=excluded.max_output, input_price=excluded.input_price, output_price=excluded.output_price,
		enabled=excluded.enabled, sort=excluded.sort, updated_at=excluded.updated_at`,
		m.ID, m.ProviderID, m.Kind, m.UpstreamModel, m.DisplayName, m.Description, m.Vision, m.Tools, m.DefaultEffort, m.ThinkEffort,
		m.ContextWindow, m.MaxOutput, m.InputPrice, m.OutputPrice, m.Enabled, m.Sort, m.CreatedAt, m.UpdatedAt)
	return err
}

// CreateModels inserts several models in one transaction, appended after the existing ones.
func (s *Store) CreateModels(ctx context.Context, ms []*Model) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var next int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), -1) + 1 FROM models`).Scan(&next); err != nil {
			return err
		}
		for i, m := range ms {
			m.Sort = next + i
			if err := upsertModel(ctx, tx, m); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetModelOrder rewrites sort so models appear in the given order; unlisted models keep
// their relative order after the listed ones.
func (s *Store) SetModelOrder(ctx context.Context, ids []string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := Now()
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE models SET sort = ?, updated_at = ? WHERE id = ?`, i, now, id); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE models SET sort = sort + ? WHERE id NOT IN (SELECT value FROM json_each(?))`, len(ids), jsonStrings(ids))
		return err
	})
}

func jsonStrings(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ModelCounts returns, per provider id, the number of models and enabled models.
func (s *Store) ModelCounts(ctx context.Context) (map[string][2]int, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT provider_id, COUNT(*), COALESCE(SUM(enabled), 0) FROM models GROUP BY provider_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]int{}
	for rows.Next() {
		var id string
		var n, en int
		if err := rows.Scan(&id, &n, &en); err != nil {
			return nil, err
		}
		out[id] = [2]int{n, en}
	}
	return out, rows.Err()
}

func (s *Store) GetModel(ctx context.Context, id string) (*Model, error) {
	return scanModel(s.r.QueryRowContext(ctx, `SELECT `+modelCols+` FROM models WHERE id = ?`, id))
}

// ListModels returns all models (every kind). usableChat returns only enabled
// chat models whose provider is enabled too, i.e. what the family can pick.
func (s *Store) ListModels(ctx context.Context, usableChat bool) ([]*Model, error) {
	q := `SELECT ` + modelCols + ` FROM models ORDER BY sort, created_at`
	if usableChat {
		q = `SELECT ` + prefixCols(modelCols, "m.") + ` FROM models m JOIN providers p ON p.id = m.provider_id
			WHERE m.enabled = 1 AND p.enabled = 1 AND m.kind = 'chat' ORDER BY m.sort, m.created_at`
	}
	rows, err := s.r.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteModel(ctx context.Context, id string) error {
	return affected(s.w.ExecContext(ctx, `DELETE FROM models WHERE id = ?`, id))
}

// ---- settings ----

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.r.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---- assistants ----

type Assistant struct {
	ID           string   `json:"id"`
	OwnerID      *string  `json:"owner_id"`
	Name         string   `json:"name"`
	Avatar       string   `json:"avatar"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Greeting     string   `json:"greeting"`
	Suggestions  []string `json:"suggestions"`
	ModelID      *string  `json:"model_id"`
	Temperature  *float64 `json:"temperature"`
	Sort         int      `json:"sort"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

const assistantCols = `id, owner_id, name, avatar, description, system_prompt, greeting, suggestions, model_id, temperature, sort, created_at, updated_at`

func scanAssistant(row interface{ Scan(...any) error }) (*Assistant, error) {
	var a Assistant
	var sugg string
	if err := row.Scan(&a.ID, &a.OwnerID, &a.Name, &a.Avatar, &a.Description, &a.SystemPrompt, &a.Greeting, &sugg,
		&a.ModelID, &a.Temperature, &a.Sort, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(sugg), &a.Suggestions)
	if a.Suggestions == nil {
		a.Suggestions = []string{}
	}
	return &a, nil
}

func (s *Store) UpsertAssistant(ctx context.Context, a *Assistant) error {
	now := Now()
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Suggestions == nil {
		a.Suggestions = []string{}
	}
	sugg, _ := json.Marshal(a.Suggestions)
	_, err := s.w.ExecContext(ctx, `INSERT INTO assistants (`+assistantCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, avatar=excluded.avatar, description=excluded.description,
		system_prompt=excluded.system_prompt, greeting=excluded.greeting, suggestions=excluded.suggestions,
		model_id=excluded.model_id, temperature=excluded.temperature, sort=excluded.sort, updated_at=excluded.updated_at`,
		a.ID, a.OwnerID, a.Name, a.Avatar, a.Description, a.SystemPrompt, a.Greeting, string(sugg),
		a.ModelID, a.Temperature, a.Sort, a.CreatedAt, a.UpdatedAt)
	return err
}

// GetAssistant returns an assistant visible to userID (shared or owned).
func (s *Store) GetAssistant(ctx context.Context, id, userID string) (*Assistant, error) {
	return scanAssistant(s.r.QueryRowContext(ctx, `SELECT `+assistantCols+` FROM assistants
		WHERE id = ? AND (owner_id IS NULL OR owner_id = ?)`, id, userID))
}

func (s *Store) ListAssistants(ctx context.Context, userID string) ([]*Assistant, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+assistantCols+` FROM assistants
		WHERE owner_id IS NULL OR owner_id = ? ORDER BY sort, created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Assistant
	for rows.Next() {
		a, err := scanAssistant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CountAssistants(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM assistants`).Scan(&n)
	return n, err
}

// DeleteAssistant deletes an assistant. If ownerID is non-empty only that owner's assistant is deleted.
func (s *Store) DeleteAssistant(ctx context.Context, id, ownerID string) error {
	if ownerID == "" {
		return affected(s.w.ExecContext(ctx, `DELETE FROM assistants WHERE id = ?`, id))
	}
	return affected(s.w.ExecContext(ctx, `DELETE FROM assistants WHERE id = ? AND owner_id = ?`, id, ownerID))
}
