DROP INDEX IF EXISTS idx_medications_test;
DROP INDEX IF EXISTS idx_medication_routines_test;
DROP INDEX IF EXISTS idx_medication_events_test;
DROP INDEX IF EXISTS idx_medication_temp_series_test;
DROP INDEX IF EXISTS idx_emergency_rows_test;

DROP INDEX IF EXISTS idx_medication_routines_med;
DROP INDEX IF EXISTS idx_medication_events_ts;
DROP INDEX IF EXISTS idx_medication_events_med;
DROP INDEX IF EXISTS idx_medication_temp_series_med;
DROP INDEX IF EXISTS idx_emergency_rows_order;

DROP TABLE IF EXISTS emergency_rows;
DROP TABLE IF EXISTS medication_temp_series;
DROP TABLE IF EXISTS medication_events;
DROP TABLE IF EXISTS medication_routines;
DROP TABLE IF EXISTS medications;
