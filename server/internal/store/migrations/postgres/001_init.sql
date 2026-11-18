-- All timestamps are unix milliseconds. IDs are UUIDv7 strings (time ordered).

CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL,
  display_name  TEXT NOT NULL,
  avatar        TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
  password_hash TEXT NOT NULL,
  settings      TEXT NOT NULL DEFAULT '{}',
  disabled      BIGINT NOT NULL DEFAULT 0,
  created_at    BIGINT NOT NULL,
  updated_at    BIGINT NOT NULL
);

CREATE UNIQUE INDEX users_username_lower ON users (lower(username));

CREATE TABLE sessions (
  token_hash   BYTEA PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device       TEXT NOT NULL DEFAULT '',
  created_at   BIGINT NOT NULL,
  last_seen_at BIGINT NOT NULL,
  expires_at   BIGINT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE providers (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  base_url   TEXT NOT NULL,
  api_key    TEXT NOT NULL,
  enabled    BIGINT NOT NULL DEFAULT 1,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE models (
  id             TEXT PRIMARY KEY,
  provider_id    TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  display_name   TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  vision         BIGINT NOT NULL DEFAULT 0,
  default_effort TEXT NOT NULL DEFAULT '',   -- reasoning_effort for normal chats ('' = don't send)
  think_effort   TEXT NOT NULL DEFAULT '',   -- reasoning_effort for "deep thinking" ('' = unsupported)
  context_window BIGINT NOT NULL DEFAULT 128000,
  max_output     BIGINT NOT NULL DEFAULT 0,
  enabled        BIGINT NOT NULL DEFAULT 1,
  sort           BIGINT NOT NULL DEFAULT 0,
  created_at     BIGINT NOT NULL,
  updated_at     BIGINT NOT NULL
);

-- Global key/value settings (default_model_id, title_model_id, ...).
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Assistants are the seed of future "character cards".
-- owner_id NULL = shared with everyone (built-in or created by admin).
CREATE TABLE assistants (
  id            TEXT PRIMARY KEY,
  owner_id      TEXT REFERENCES users(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  avatar        TEXT NOT NULL DEFAULT '',
  description   TEXT NOT NULL DEFAULT '',
  system_prompt TEXT NOT NULL DEFAULT '',
  greeting      TEXT NOT NULL DEFAULT '',
  suggestions   TEXT NOT NULL DEFAULT '[]',
  model_id      TEXT REFERENCES models(id) ON DELETE SET NULL,
  temperature   DOUBLE PRECISION,
  sort          BIGINT NOT NULL DEFAULT 0,
  created_at    BIGINT NOT NULL,
  updated_at    BIGINT NOT NULL
);

CREATE TABLE conversations (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  assistant_id TEXT REFERENCES assistants(id) ON DELETE SET NULL,
  model_id     TEXT REFERENCES models(id) ON DELETE SET NULL,
  title        TEXT NOT NULL DEFAULT '',
  pinned       BIGINT NOT NULL DEFAULT 0,
  created_at   BIGINT NOT NULL,
  updated_at   BIGINT NOT NULL
);
CREATE INDEX conversations_user_updated ON conversations(user_id, updated_at DESC);

CREATE TABLE messages (
  id                TEXT PRIMARY KEY,
  conversation_id   TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role              TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
  content           TEXT NOT NULL DEFAULT '',
  reasoning         TEXT NOT NULL DEFAULT '',
  attachments       TEXT NOT NULL DEFAULT '[]',
  status            TEXT NOT NULL DEFAULT 'done',
  error             TEXT NOT NULL DEFAULT '',
  model_id          TEXT,
  prompt_tokens     BIGINT NOT NULL DEFAULT 0,
  completion_tokens BIGINT NOT NULL DEFAULT 0,
  first_token_ms    BIGINT NOT NULL DEFAULT 0,
  duration_ms       BIGINT NOT NULL DEFAULT 0,
  created_at        BIGINT NOT NULL,
  updated_at        BIGINT NOT NULL
);
CREATE INDEX messages_conversation ON messages(conversation_id, id);

CREATE TABLE files (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mime       TEXT NOT NULL,
  size       BIGINT NOT NULL,
  sha256     TEXT NOT NULL,
  width      BIGINT NOT NULL DEFAULT 0,
  height     BIGINT NOT NULL DEFAULT 0,
  path       TEXT NOT NULL,
  created_at BIGINT NOT NULL
);
CREATE INDEX files_user ON files(user_id);
