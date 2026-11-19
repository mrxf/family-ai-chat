-- v10: web search provider credentials managed by admins and per-message web sources.
-- settings('search_provider') selects the active kind ('deepseek', 'serp', or '' for disabled).
CREATE TABLE search_configs (
  kind       TEXT PRIMARY KEY,
  api_key    TEXT NOT NULL DEFAULT '',
  base_url   TEXT NOT NULL DEFAULT '',
  extra      TEXT NOT NULL DEFAULT '{}',
  updated_at BIGINT NOT NULL
);

ALTER TABLE messages ADD COLUMN sources TEXT NOT NULL DEFAULT '[]';
