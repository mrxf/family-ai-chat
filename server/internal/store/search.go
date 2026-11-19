package store

import (
	"context"
	"encoding/json"
)

// SearchConfig holds credentials and options for one web search provider kind.
type SearchConfig struct {
	Kind      string            `json:"kind"` // deepseek | serp | ...
	APIKey    string            `json:"-"`
	BaseURL   string            `json:"base_url"`
	Extra     map[string]string `json:"extra"`
	UpdatedAt int64             `json:"updated_at"`
}

const searchCols = `kind, api_key, base_url, extra, updated_at`

func scanSearchConfig(row interface{ Scan(...any) error }) (*SearchConfig, error) {
	var c SearchConfig
	var extra string
	if err := row.Scan(&c.Kind, &c.APIKey, &c.BaseURL, &extra, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(extra), &c.Extra)
	if c.Extra == nil {
		c.Extra = map[string]string{}
	}
	return &c, nil
}

func (s *Store) GetSearchConfig(ctx context.Context, kind string) (*SearchConfig, error) {
	return scanSearchConfig(s.queryRow(ctx, `SELECT `+searchCols+` FROM search_configs WHERE kind = ?`, kind))
}

func (s *Store) ListSearchConfigs(ctx context.Context) (map[string]*SearchConfig, error) {
	rows, err := s.query(ctx, `SELECT `+searchCols+` FROM search_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*SearchConfig{}
	for rows.Next() {
		c, err := scanSearchConfig(rows)
		if err != nil {
			return nil, err
		}
		out[c.Kind] = c
	}
	return out, rows.Err()
}

func (s *Store) UpsertSearchConfig(ctx context.Context, c *SearchConfig) error {
	c.UpdatedAt = Now()
	if c.Extra == nil {
		c.Extra = map[string]string{}
	}
	extra, _ := json.Marshal(c.Extra)
	_, err := s.exec(ctx, `INSERT INTO search_configs (`+searchCols+`) VALUES (?,?,?,?,?)
		ON CONFLICT(kind) DO UPDATE SET api_key=excluded.api_key, base_url=excluded.base_url,
		extra=excluded.extra, updated_at=excluded.updated_at`,
		c.Kind, c.APIKey, c.BaseURL, string(extra), c.UpdatedAt)
	return err
}
