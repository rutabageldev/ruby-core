-- Medications + Emergency domains (ROADMAP-0011, ADR-0037).
-- Five tables: the medication registry (identity + safety only), dosing routines
-- (dose + cadence + end rule), dose events (given/skipped/missed), as-needed
-- temporary series (a clock-free watch that reads the dose history), and the
-- emergency card rows. Field names mirror adaMeds.ts / adaEmergency.ts so the
-- future dashboard read-seam repoint is a pure binding swap.
--
-- ids are TEXT, not UUID: the dashboard is the id authority and generates string
-- ids (m-/rt-/ev-/s-/ec- prefixed millisecond timestamps), so the engine stores
-- them verbatim. test/deleted_at/created_at follow the established Ada conventions
-- (ADR-0031 test-data; soft-delete). All tables carry a `WHERE test=true` partial
-- index for cheap clear/birth scans. series_id (on events) and anchor_dose_id (on
-- series) are loose refs, not FKs: the dose and its series arrive as independent
-- events and reference each other, so app-level integrity avoids a circular FK.

-- medications: identity + optional safety limits ONLY. No dose, no schedule —
-- a contextual infant dose lives on the routine or the dose event, never here.
CREATE TABLE IF NOT EXISTS medications (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL,
    route              TEXT NOT NULL,                       -- oral|drops|topical|suppository
    measure_unit       TEXT NOT NULL,                       -- mL|mg|drops|supp
    min_interval_hours NUMERIC(8,3),                        -- optional: spacing minimum
    max_per_24h        INTEGER,                             -- optional: rolling-24h ceiling
    active             BOOLEAN NOT NULL DEFAULT true,
    logged_by          TEXT NOT NULL DEFAULT '',
    deleted_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    test               BOOLEAN NOT NULL DEFAULT false
);

-- medication_routines: the standing dose + cadence + end rule. The dashboard
-- sends `end` as a nested {type, value?}; persisted here as end_type + end_value
-- (value stringified — number for max_doses, date string for end_date).
CREATE TABLE IF NOT EXISTS medication_routines (
    id             TEXT PRIMARY KEY,
    medication_id  TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
    dose_amount    NUMERIC(8,3) NOT NULL,
    schedule_type  TEXT NOT NULL,                           -- fixed_times|interval
    fixed_times    TEXT[] NOT NULL DEFAULT '{}',            -- ["08:00","13:00"] for fixed_times
    interval_hours NUMERIC(8,3),                            -- interval only
    end_type       TEXT NOT NULL DEFAULT 'none',            -- none|max_doses|end_date
    end_value      TEXT,                                    -- stringified value for max_doses/end_date
    status         TEXT NOT NULL DEFAULT 'active',          -- active|completed
    logged_by      TEXT NOT NULL DEFAULT '',
    deleted_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    test           BOOLEAN NOT NULL DEFAULT false
);

-- medication_events: a dose is a first-class event. given/skipped are
-- caregiver-logged; missed is system-emitted (actorless) per ADR-0038.
-- dose_amount/dose_unit are a SNAPSHOT of what was actually given.
CREATE TABLE IF NOT EXISTS medication_events (
    id                     TEXT PRIMARY KEY,
    medication_id          TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
    status                 TEXT NOT NULL,                   -- given|skipped|missed
    timestamp              TIMESTAMPTZ NOT NULL,
    routine_id             TEXT,                            -- the routine this resolves, if scheduled
    slot_time              TEXT,                            -- "HH:MM" fixed slot this resolves
    dose_amount            NUMERIC(8,3),                    -- given only (snapshot)
    dose_unit              TEXT,                            -- given only (snapshot)
    source                 TEXT,                            -- scheduled|prn (given only)
    within_window_override BOOLEAN NOT NULL DEFAULT false,
    series_id              TEXT,                            -- loose ref to medication_temp_series
    started_watch          BOOLEAN NOT NULL DEFAULT false,  -- this dose opened a new watch
    notes                  TEXT,
    logged_by              TEXT NOT NULL DEFAULT '',         -- actor; '' for system missed
    deleted_at             TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    test                   BOOLEAN NOT NULL DEFAULT false
);

-- medication_temp_series: an as-needed watch. Holds no clock — next-due reads
-- the anchor dose. anchor_dose_id is a loose ref to a given medication_event.
CREATE TABLE IF NOT EXISTS medication_temp_series (
    id             TEXT PRIMARY KEY,
    medication_id  TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
    interval_hours NUMERIC(8,3) NOT NULL,
    anchor_dose_id TEXT,                                    -- references a given dose, not a clock
    status         TEXT NOT NULL DEFAULT 'active',          -- active|resolved|disregarded|expired
    ended_reason   TEXT,                                    -- planned|dismissed|auto_expire
    logged_by      TEXT NOT NULL DEFAULT '',
    deleted_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    test           BOOLEAN NOT NULL DEFAULT false
);

-- emergency_rows: the ordered emergency card. Live-field rows resolve their
-- value client-side off existing sensors; we persist rows + order only.
CREATE TABLE IF NOT EXISTS emergency_rows (
    id          TEXT PRIMARY KEY,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    type        TEXT NOT NULL,                              -- contact|live_field
    label       TEXT NOT NULL DEFAULT '',
    name        TEXT,                                       -- contact rows
    phone       TEXT,
    address     TEXT,
    field_key   TEXT,                                       -- live_field rows (e.g. current_weight|age)
    logged_by   TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    test        BOOLEAN NOT NULL DEFAULT false
);

-- Read indexes (active rows only).
CREATE INDEX IF NOT EXISTS idx_medication_routines_med   ON medication_routines(medication_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_medication_events_ts       ON medication_events(timestamp)        WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_medication_events_med      ON medication_events(medication_id)    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_medication_temp_series_med ON medication_temp_series(medication_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_emergency_rows_order       ON emergency_rows(sort_order)          WHERE deleted_at IS NULL;

-- Partial test indexes for cheap `WHERE test=true` clear/birth scans (ADR-0031).
CREATE INDEX IF NOT EXISTS idx_medications_test            ON medications(test)            WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_medication_routines_test    ON medication_routines(test)    WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_medication_events_test      ON medication_events(test)      WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_medication_temp_series_test ON medication_temp_series(test) WHERE test = true;
CREATE INDEX IF NOT EXISTS idx_emergency_rows_test         ON emergency_rows(test)         WHERE test = true;
