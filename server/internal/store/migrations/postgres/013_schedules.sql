CREATE TABLE schedules (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 location TEXT NOT NULL DEFAULT '',
 start_at BIGINT NOT NULL,
 end_at BIGINT NOT NULL DEFAULT 0,
 all_day BIGINT NOT NULL DEFAULT 0,
 remind_before_min BIGINT NOT NULL DEFAULT 0,
 recurrence TEXT NOT NULL DEFAULT 'none',
 recurrence_days TEXT NOT NULL DEFAULT '',
 recurrence_until BIGINT NOT NULL DEFAULT 0,
 completed_dates TEXT NOT NULL DEFAULT '[]',
 status TEXT NOT NULL DEFAULT 'active',
 completed_at BIGINT NOT NULL DEFAULT 0,
 source TEXT NOT NULL DEFAULT 'ai',
 source_conv_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
 created_at BIGINT NOT NULL,
 updated_at BIGINT NOT NULL
);
CREATE INDEX schedules_user_status ON schedules(user_id, status, start_at);
CREATE INDEX schedules_conv ON schedules(source_conv_id);
