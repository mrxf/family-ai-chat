-- v9: Auto model routing notes and per-conversation auto mode flag.

ALTER TABLE models ADD COLUMN routing_note TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN auto_mode BIGINT NOT NULL DEFAULT 0;
