ALTER TABLE conversations ADD COLUMN summary TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN summary_cursor TEXT NOT NULL DEFAULT '';
