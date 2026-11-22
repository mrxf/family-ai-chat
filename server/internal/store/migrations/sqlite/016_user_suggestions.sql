CREATE TABLE IF NOT EXISTS user_suggestions (
  user_id      TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  items        TEXT NOT NULL DEFAULT '[]',
  generated_at BIGINT NOT NULL DEFAULT 0,
  last_msg_at  BIGINT NOT NULL DEFAULT 0,
  updated_at   BIGINT NOT NULL DEFAULT 0
);
