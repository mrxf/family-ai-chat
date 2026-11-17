-- v2: provider presets, model prices, per-message cost.

ALTER TABLE providers ADD COLUMN kind TEXT NOT NULL DEFAULT 'custom';
ALTER TABLE providers ADD COLUMN sort INTEGER NOT NULL DEFAULT 0;

-- USD per million tokens, used to estimate cost when the upstream does not report it.
ALTER TABLE models ADD COLUMN input_price  REAL NOT NULL DEFAULT 0;
ALTER TABLE models ADD COLUMN output_price REAL NOT NULL DEFAULT 0;

-- USD spent on this reply (reported by the upstream, or estimated from prices).
ALTER TABLE messages ADD COLUMN cost REAL NOT NULL DEFAULT 0;

-- Existing installs already imported the .env provider: never import it again,
-- so an admin can delete it for good.
INSERT OR IGNORE INTO settings (key, value) SELECT 'providers_seeded', '1' WHERE EXISTS (SELECT 1 FROM providers);
