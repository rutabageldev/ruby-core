-- Reverse 000009: TEXT ids back to UUID. Best-effort — the tables are empty, so the
-- ::uuid casts have no rows to fail on; if any non-UUID string id were present this
-- would error (by design — those ids cannot be represented as UUID).
ALTER TABLE medication_routines    DROP CONSTRAINT IF EXISTS medication_routines_medication_id_fkey;
ALTER TABLE medication_events      DROP CONSTRAINT IF EXISTS medication_events_medication_id_fkey;
ALTER TABLE medication_temp_series DROP CONSTRAINT IF EXISTS medication_temp_series_medication_id_fkey;

ALTER TABLE emergency_rows
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

ALTER TABLE medication_temp_series
    ALTER COLUMN anchor_dose_id TYPE UUID USING anchor_dose_id::uuid,
    ALTER COLUMN medication_id TYPE UUID USING medication_id::uuid,
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

ALTER TABLE medication_events
    ALTER COLUMN series_id TYPE UUID USING series_id::uuid,
    ALTER COLUMN routine_id TYPE UUID USING routine_id::uuid,
    ALTER COLUMN medication_id TYPE UUID USING medication_id::uuid,
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

ALTER TABLE medication_routines
    ALTER COLUMN medication_id TYPE UUID USING medication_id::uuid,
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

ALTER TABLE medications
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

ALTER TABLE medication_routines
    ADD CONSTRAINT medication_routines_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
ALTER TABLE medication_events
    ADD CONSTRAINT medication_events_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
ALTER TABLE medication_temp_series
    ADD CONSTRAINT medication_temp_series_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
