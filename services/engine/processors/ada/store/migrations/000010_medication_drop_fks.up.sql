-- Drop the medication_id foreign keys. The dashboard generates all ids client-side
-- and fires medication / routine / dose / series as independent events that the
-- engine's worker pool processes concurrently (no guaranteed parent-before-child
-- order). A FK therefore rejects a routine/dose/series whose parent medication has
-- not yet been inserted — breaking the normal "add a medication, then add a routine"
-- flow. medication_id becomes a loose ref (like series_id / anchor_dose_id already
-- are); referential integrity is the dashboard's responsibility and the
-- medication.delete cascade is already app-level (soft-delete in the handler), so
-- nothing depended on the FK's ON DELETE CASCADE.
ALTER TABLE medication_routines    DROP CONSTRAINT IF EXISTS medication_routines_medication_id_fkey;
ALTER TABLE medication_events      DROP CONSTRAINT IF EXISTS medication_events_medication_id_fkey;
ALTER TABLE medication_temp_series DROP CONSTRAINT IF EXISTS medication_temp_series_medication_id_fkey;
