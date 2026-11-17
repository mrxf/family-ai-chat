-- v5: speech-to-text (ASR) provider credentials managed by admins.
-- settings('asr_provider') selects the active kind ('tencent', 'openai', 'xfyun', or '' for disabled).
CREATE TABLE asr_configs (
  kind       TEXT PRIMARY KEY,
  app_id     TEXT NOT NULL DEFAULT '',
  access_key TEXT NOT NULL DEFAULT '',
  secret_key TEXT NOT NULL DEFAULT '',
  base_url   TEXT NOT NULL DEFAULT '',
  model      TEXT NOT NULL DEFAULT '',
  extra      TEXT NOT NULL DEFAULT '{}',
  updated_at INTEGER NOT NULL
);
