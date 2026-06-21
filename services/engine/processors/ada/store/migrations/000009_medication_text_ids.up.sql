-- Medication/Emergency ids are dashboard-provided strings (m-/rt-/ev-/s-/ec-
-- prefixed millisecond timestamps), not UUIDs. 000008 created the tables with UUID
-- ids before this was understood and has already deployed, so the column types are
-- corrected here rather than by amending 000008. The tables are empty in every
-- environment — under the UUID schema the engine rejected every string id, so no
-- dose/registry row ever persisted — so this type change touches no data.

-- Drop the three medication_id foreign keys so the referenced/ referencing columns
-- can change type, then re-add them after.
ALTER TABLE medication_routines    DROP CONSTRAINT IF EXISTS medication_routines_medication_id_fkey;
ALTER TABLE medication_events      DROP CONSTRAINT IF EXISTS medication_events_medication_id_fkey;
ALTER TABLE medication_temp_series DROP CONSTRAINT IF EXISTS medication_temp_series_medication_id_fkey;

ALTER TABLE medications
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE TEXT USING id::text;

ALTER TABLE medication_routines
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE TEXT USING id::text,
    ALTER COLUMN medication_id TYPE TEXT USING medication_id::text;

ALTER TABLE medication_events
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE TEXT USING id::text,
    ALTER COLUMN medication_id TYPE TEXT USING medication_id::text,
    ALTER COLUMN routine_id TYPE TEXT USING routine_id::text,
    ALTER COLUMN series_id TYPE TEXT USING series_id::text;

ALTER TABLE medication_temp_series
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE TEXT USING id::text,
    ALTER COLUMN medication_id TYPE TEXT USING medication_id::text,
    ALTER COLUMN anchor_dose_id TYPE TEXT USING anchor_dose_id::text;

ALTER TABLE emergency_rows
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE TEXT USING id::text;

ALTER TABLE medication_routines
    ADD CONSTRAINT medication_routines_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
ALTER TABLE medication_events
    ADD CONSTRAINT medication_events_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
ALTER TABLE medication_temp_series
    ADD CONSTRAINT medication_temp_series_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
