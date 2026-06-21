-- Restore the medication_id foreign keys (reverse of 000010).
ALTER TABLE medication_routines
    ADD CONSTRAINT medication_routines_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
ALTER TABLE medication_events
    ADD CONSTRAINT medication_events_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
ALTER TABLE medication_temp_series
    ADD CONSTRAINT medication_temp_series_medication_id_fkey
    FOREIGN KEY (medication_id) REFERENCES medications(id) ON DELETE CASCADE;
