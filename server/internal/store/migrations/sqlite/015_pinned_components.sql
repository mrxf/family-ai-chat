CREATE TABLE pinned_components (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  component_id    TEXT NOT NULL,
  sort_order      INTEGER NOT NULL DEFAULT 0,
  pinned_at       INTEGER NOT NULL,
  UNIQUE(user_id, message_id, component_id)
);

CREATE INDEX pinned_components_user_order
  ON pinned_components(user_id, sort_order ASC, pinned_at DESC);

ALTER TABLE messages ADD COLUMN quoted_component TEXT NOT NULL DEFAULT '';
