-- feedings is the header record for all feeding types.
-- source values: breast_left, breast_right, bottle_breast, bottle_formula, mixed
-- Bottle-specific amounts live in feeding_bottle_detail (3NF: amount columns
-- only apply when source is a bottle type — partial dependency on source).
-- Breast-specific timing lives in feeding_segments.
-- duration_s is omitted from the header — derivable from segments for breast,
-- stored in feeding_bottle_detail for bottle.
CREATE TABLE IF NOT EXISTS feedings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp   TIMESTAMPTZ NOT NULL,
    source      TEXT NOT NULL,
    logged_by   TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- feeding_bottle_detail holds amounts for bottle feedings only.
-- All volumes are stored in oz (the unit used at point of entry).
-- Convert to ml only if needed by external consumers (1 oz = 29.5735 ml).
-- For mixed bottles, both breast_milk_oz and formula_oz are populated.
-- For single-source bottles, only amount_oz is populated.
-- duration_s captures how long the bottle feed took (optional; from timer).
CREATE TABLE IF NOT EXISTS feeding_bottle_detail (
    feeding_id     UUID PRIMARY KEY REFERENCES feedings(id) ON DELETE CASCADE,
    amount_oz      NUMERIC(6,2),
    breast_milk_oz NUMERIC(6,2),
    formula_oz     NUMERIC(6,2),
    duration_s     INTEGER
);

-- feeding_segments holds per-side timing for breast feeding sessions.
CREATE TABLE IF NOT EXISTS feeding_segments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feeding_id  UUID NOT NULL REFERENCES feedings(id) ON DELETE CASCADE,
    side        TEXT NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL,
    ended_at    TIMESTAMPTZ NOT NULL,
    duration_s  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS diapers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp   TIMESTAMPTZ NOT NULL,
    type        TEXT NOT NULL,
    logged_by   TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sleep_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ,
    sleep_type  TEXT NOT NULL DEFAULT 'nap',
    logged_by   TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tummy_time_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ NOT NULL,
    duration_s  INTEGER NOT NULL,
    logged_by   TEXT NOT NULL DEFAULT '',
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_feedings_timestamp   ON feedings(timestamp)        WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_bottle_feeding_id    ON feeding_bottle_detail(feeding_id);
CREATE INDEX IF NOT EXISTS idx_diapers_timestamp    ON diapers(timestamp)         WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sleep_sessions_start ON sleep_sessions(start_time) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tummy_time_start     ON tummy_time_sessions(start_time) WHERE deleted_at IS NULL;

-- Config key-value store for persistent processor state.
-- Used for: next_feeding_target, feed_interval_hours.
-- Extend with additional keys for future program config without migrations.
CREATE TABLE IF NOT EXISTS ada_config (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
