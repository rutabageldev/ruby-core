-- growth_measurements stores all logged growth data for Ada.
-- Fields are nullable — a weight-only entry has NULL for length_in and head_circumference_in.
-- Percentiles are stored alongside raw values for fast display; they are recomputable from
-- the WHO LMS tables if the source data is updated.
-- This table is retained indefinitely (no 24h window, no deletion on data clear).
CREATE TABLE IF NOT EXISTS growth_measurements (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    measured_at           TIMESTAMPTZ NOT NULL,
    weight_oz             NUMERIC(7,2),
    length_in             NUMERIC(5,2),
    head_circumference_in NUMERIC(5,2),
    source                TEXT NOT NULL DEFAULT 'home',
    weight_pct            NUMERIC(5,2),
    length_pct            NUMERIC(5,2),
    head_pct              NUMERIC(5,2),
    logged_by             TEXT NOT NULL DEFAULT '',
    deleted_at            TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_growth_measurements_measured_at
    ON growth_measurements(measured_at)
    WHERE deleted_at IS NULL;
