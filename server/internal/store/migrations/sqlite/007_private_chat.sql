ALTER TABLE conversations ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
CREATE INDEX conversations_private_updated ON conversations(updated_at) WHERE private = 1;
ALTER TABLE files ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
CREATE INDEX files_private_created ON files(created_at) WHERE private = 1;
