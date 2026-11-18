DROP INDEX IF EXISTS memories_episode;
CREATE INDEX memories_episode_conv ON memories(user_id, source_conv_id) WHERE category = 'episode' AND status = 'active';
ALTER TABLE memories ADD COLUMN keywords_checked BIGINT NOT NULL DEFAULT 0;
UPDATE memories SET keywords_checked = 1 WHERE keywords <> '';
