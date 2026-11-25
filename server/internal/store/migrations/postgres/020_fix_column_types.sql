-- Upgrade existing Postgres databases where earlier migrations created
-- pinned_at as INTEGER (overflows unix ms) or boolean columns as BOOLEAN
-- (incompatible with store.prepare's bool->int64 binding).
ALTER TABLE pinned_components ALTER COLUMN pinned_at TYPE BIGINT;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'scheduled_tasks'
      AND column_name = 'enabled'
      AND data_type = 'boolean'
  ) THEN
    ALTER TABLE scheduled_tasks ALTER COLUMN enabled DROP DEFAULT;
    ALTER TABLE scheduled_tasks ALTER COLUMN enabled TYPE INTEGER USING (CASE WHEN enabled THEN 1 ELSE 0 END);
    ALTER TABLE scheduled_tasks ALTER COLUMN enabled SET DEFAULT 1;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'components'
      AND column_name = 'private'
      AND data_type = 'boolean'
  ) THEN
    ALTER TABLE components ALTER COLUMN private DROP DEFAULT;
    ALTER TABLE components ALTER COLUMN private TYPE INTEGER USING (CASE WHEN private THEN 1 ELSE 0 END);
    ALTER TABLE components ALTER COLUMN private SET DEFAULT 0;
  END IF;
END $$;
