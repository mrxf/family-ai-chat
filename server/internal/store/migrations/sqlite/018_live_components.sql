CREATE TABLE components (
  id                          TEXT PRIMARY KEY,
  user_id                     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind                        TEXT NOT NULL,
  title                       TEXT NOT NULL DEFAULT '',
  current_version             INTEGER NOT NULL DEFAULT 1,
  data                        TEXT NOT NULL,
  state                       TEXT NOT NULL DEFAULT '',
  private                     INTEGER NOT NULL DEFAULT 0,
  origin_conversation_id      TEXT NOT NULL DEFAULT '',
  origin_message_id           TEXT NOT NULL DEFAULT '',
  origin_component_id         TEXT NOT NULL DEFAULT '',
  last_source_type            TEXT NOT NULL DEFAULT 'create',
  last_source_conversation_id TEXT NOT NULL DEFAULT '',
  last_source_message_id      TEXT NOT NULL DEFAULT '',
  last_source_task_id         TEXT NOT NULL DEFAULT '',
  created_at                  INTEGER NOT NULL,
  updated_at                  INTEGER NOT NULL,
  deleted_at                  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX components_user_updated
  ON components(user_id, deleted_at, updated_at DESC);

CREATE INDEX components_origin_ref
  ON components(origin_message_id, origin_component_id);

CREATE TABLE component_versions (
  id                     TEXT PRIMARY KEY,
  component_id           TEXT NOT NULL REFERENCES components(id) ON DELETE CASCADE,
  version                INTEGER NOT NULL,
  data                   TEXT NOT NULL,
  state                  TEXT NOT NULL DEFAULT '',
  change_summary         TEXT NOT NULL DEFAULT '',
  source_type            TEXT NOT NULL DEFAULT 'create',
  source_conversation_id TEXT NOT NULL DEFAULT '',
  source_message_id      TEXT NOT NULL DEFAULT '',
  source_task_id         TEXT NOT NULL DEFAULT '',
  created_at             INTEGER NOT NULL,
  UNIQUE(component_id, version)
);

CREATE INDEX component_versions_comp_ver
  ON component_versions(component_id, version DESC);

ALTER TABLE pinned_components ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';
CREATE INDEX pinned_components_entity ON pinned_components(user_id, entity_id);

ALTER TABLE scheduled_tasks ADD COLUMN target_entity_id TEXT NOT NULL DEFAULT '';
