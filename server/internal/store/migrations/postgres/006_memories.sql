CREATE TABLE memories (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 assistant_id TEXT REFERENCES assistants(id) ON DELETE CASCADE,
 category TEXT NOT NULL,
 content TEXT NOT NULL,
 keywords TEXT NOT NULL DEFAULT '',
 embedding BYTEA,
 embedding_model TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL DEFAULT 'auto',
 source_conv_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
 status TEXT NOT NULL DEFAULT 'active',
 pinned BIGINT NOT NULL DEFAULT 1,
 superseded_by TEXT,
 valid_until BIGINT,
 created_at BIGINT NOT NULL,
 updated_at BIGINT NOT NULL
);
CREATE INDEX memories_user ON memories(user_id, status, category);
CREATE INDEX memories_conv ON memories(source_conv_id);
CREATE UNIQUE INDEX memories_episode ON memories(source_conv_id) WHERE category = 'episode' AND status = 'active';
ALTER TABLE conversations ADD COLUMN memory_cursor TEXT NOT NULL DEFAULT '';
ALTER TABLE assistants ADD COLUMN use_memory BIGINT NOT NULL DEFAULT 1;
UPDATE assistants SET use_memory = 0 WHERE id = 'builtin-translate';
