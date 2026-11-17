-- v4: one row per picture drawn for a user, for the daily limit. Kept apart from
-- messages and files so deleting a conversation does not reset the count.
CREATE TABLE image_log (
  id         INTEGER PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  model_id   TEXT NOT NULL,
  cost       REAL NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX image_log_user ON image_log(user_id, created_at);
