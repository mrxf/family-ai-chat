CREATE TABLE scheduled_tasks (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title TEXT NOT NULL,
 prompt TEXT NOT NULL,
 schedule_kind TEXT NOT NULL DEFAULT 'daily',
 time_of_day TEXT NOT NULL DEFAULT '08:00',
 weekdays TEXT NOT NULL DEFAULT '',
 day_of_month INTEGER NOT NULL DEFAULT 1,
 interval_minutes INTEGER NOT NULL DEFAULT 0,
 run_at BIGINT NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1,
 model_id TEXT,
 target_message_id TEXT NOT NULL DEFAULT '',
 target_component_id TEXT NOT NULL DEFAULT '',
 conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
 source TEXT NOT NULL DEFAULT 'ai',
 next_run_at BIGINT NOT NULL DEFAULT 0,
 last_run_at BIGINT NOT NULL DEFAULT 0,
 last_status TEXT NOT NULL DEFAULT '',
 last_summary TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '',
 run_count INTEGER NOT NULL DEFAULT 0,
 created_at BIGINT NOT NULL,
 updated_at BIGINT NOT NULL
);
CREATE INDEX scheduled_tasks_user ON scheduled_tasks(user_id, created_at DESC);
CREATE INDEX scheduled_tasks_due ON scheduled_tasks(enabled, next_run_at);

CREATE TABLE scheduled_task_runs (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 trigger_source TEXT NOT NULL DEFAULT 'schedule',
 status TEXT NOT NULL DEFAULT 'running',
 summary TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 message_id TEXT NOT NULL DEFAULT '',
 conversation_id TEXT NOT NULL DEFAULT '',
 tool_calls INTEGER NOT NULL DEFAULT 0,
 updated_components INTEGER NOT NULL DEFAULT 0,
 started_at BIGINT NOT NULL,
 finished_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX scheduled_task_runs_task ON scheduled_task_runs(task_id, started_at DESC);
