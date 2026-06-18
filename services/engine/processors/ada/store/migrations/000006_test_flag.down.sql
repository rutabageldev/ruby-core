DROP INDEX IF EXISTS idx_feedings_test;
DROP INDEX IF EXISTS idx_diapers_test;
DROP INDEX IF EXISTS idx_sleep_sessions_test;
DROP INDEX IF EXISTS idx_tummy_time_sessions_test;
DROP INDEX IF EXISTS idx_growth_measurements_test;

ALTER TABLE feedings            DROP COLUMN IF EXISTS test;
ALTER TABLE diapers             DROP COLUMN IF EXISTS test;
ALTER TABLE sleep_sessions      DROP COLUMN IF EXISTS test;
ALTER TABLE tummy_time_sessions DROP COLUMN IF EXISTS test;
ALTER TABLE growth_measurements DROP COLUMN IF EXISTS test;
