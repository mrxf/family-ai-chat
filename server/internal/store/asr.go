package store

import (
	"context"
	"encoding/json"
)

// ASRConfig holds credentials and options for one speech-to-text provider kind.
type ASRConfig struct {
	Kind      string            `json:"kind"` // tencent | openai | xfyun
	AppID     string            `json:"app_id"`
	AccessKey string            `json:"access_key"`
	SecretKey string            `json:"-"`
	BaseURL   string            `json:"base_url"`
	Model     string            `json:"model"`
	Extra     map[string]string `json:"extra"`
	UpdatedAt int64             `json:"updated_at"`
}

const asrCols = `kind, app_id, access_key, secret_key, base_url, model, extra, updated_at`

func scanASRConfig(row interface{ Scan(...any) error }) (*ASRConfig, error) {
	var c ASRConfig
	var extra string
	if err := row.Scan(&c.Kind, &c.AppID, &c.AccessKey, &c.SecretKey, &c.BaseURL, &c.Model, &extra, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(extra), &c.Extra)
	if c.Extra == nil {
		c.Extra = map[string]string{}
	}
	return &c, nil
}

func (s *Store) GetASRConfig(ctx context.Context, kind string) (*ASRConfig, error) {
	return scanASRConfig(s.r.QueryRowContext(ctx, `SELECT `+asrCols+` FROM asr_configs WHERE kind = ?`, kind))
}

func (s *Store) ListASRConfigs(ctx context.Context) (map[string]*ASRConfig, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+asrCols+` FROM asr_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ASRConfig{}
	for rows.Next() {
		c, err := scanASRConfig(rows)
		if err != nil {
			return nil, err
		}
		out[c.Kind] = c
	}
	return out, rows.Err()
}

func (s *Store) UpsertASRConfig(ctx context.Context, c *ASRConfig) error {
	c.UpdatedAt = Now()
	if c.Extra == nil {
		c.Extra = map[string]string{}
	}
	extra, _ := json.Marshal(c.Extra)
	_, err := s.w.ExecContext(ctx, `INSERT INTO asr_configs (`+asrCols+`) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(kind) DO UPDATE SET app_id=excluded.app_id, access_key=excluded.access_key, secret_key=excluded.secret_key,
		base_url=excluded.base_url, model=excluded.model, extra=excluded.extra, updated_at=excluded.updated_at`,
		c.Kind, c.AppID, c.AccessKey, c.SecretKey, c.BaseURL, c.Model, string(extra), c.UpdatedAt)
	return err
}
