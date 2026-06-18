-- Add a test-data marker to every Ada event/record table (ADR-0031).
-- test=true rows behave identically in every projection but are selectable for
-- bulk removal by the clear target (ROADMAP-0010.6). Existing rows default to
-- false (real data). Child tables (feeding_segments, feeding_bottle_detail) need
-- no column — they cascade from the parent feeding when it is cleared.
ALTER TABLE feedings            ADD COLUMN IF NOT EXISTS test BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE diapers             ADD COLUMN IF NOT EXISTS test BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE sleep_sessions      ADD COLUMN IF NOT EXISTS test BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE tummy_time_sessions ADD COLUMN IF NOT EXISTS test BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE growth_measurements ADD COLUMN IF NOT EXISTS test BOOLEAN NOT NULL DEFAULT false;

-- Partial indexes to make the clear target's WHERE test = true scans cheap.
CREATE INDEX IF NOT EXISTS idx_feedings_test            ON feedings(test)            WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_diapers_test             ON diapers(test)             WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_sleep_sessions_test      ON sleep_sessions(test)      WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_tummy_time_sessions_test ON tummy_time_sessions(test) WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_growth_measurements_test ON growth_measurements(test) WHERE test = true;
