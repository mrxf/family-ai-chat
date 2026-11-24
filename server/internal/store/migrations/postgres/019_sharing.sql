ALTER TABLE messages ADD COLUMN sender_user_id TEXT NOT NULL DEFAULT '';

CREATE TABLE conversation_members (
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role            TEXT NOT NULL DEFAULT 'member',
  added_by        TEXT NOT NULL DEFAULT '',
  created_at      BIGINT NOT NULL,
  PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX conversation_members_user
  ON conversation_members(user_id, created_at DESC);

CREATE TABLE conversation_shares (
  id                     TEXT PRIMARY KEY,
  code                   TEXT NOT NULL UNIQUE,
  creator_user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_conversation_id TEXT NOT NULL DEFAULT '',
  title                  TEXT NOT NULL DEFAULT '',
  assistant_id           TEXT,
  model_id               TEXT,
  snapshot_json          TEXT NOT NULL,
  message_count          INTEGER NOT NULL DEFAULT 0,
  expires_at             BIGINT NOT NULL DEFAULT 0,
  revoked_at             BIGINT NOT NULL DEFAULT 0,
  created_at             BIGINT NOT NULL
);

CREATE INDEX conversation_shares_creator
  ON conversation_shares(creator_user_id, created_at DESC);

CREATE TABLE file_grants (
  file_id    TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (file_id, user_id)
);

CREATE INDEX file_grants_user
  ON file_grants(user_id);
