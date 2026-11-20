ALTER TABLE memories ADD COLUMN event_at BIGINT NOT NULL DEFAULT 0;
UPDATE memories SET event_at = created_at WHERE event_at = 0;
ALTER TABLE memories ADD COLUMN last_recalled_at BIGINT;
ALTER TABLE memories ADD COLUMN recall_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE memories ADD COLUMN compressed BIGINT NOT NULL DEFAULT 0;
CREATE INDEX memories_inactive_gc ON memories(status, updated_at) WHERE status <> 'active';
