-- Calendar mirror of the Google Family calendar (ROADMAP-0012, ADR-0042).
-- Reproduces Google's native date shape: each of start / end / original-start is a
-- date XOR datetime+timezone trio, with all_day distinguishing. All-day end is
-- EXCLUSIVE (surfaced to consumers, not hidden). The derived *_utc columns exist
-- only for range queries and sorting — internal, one-directional, app-computed.

CREATE TABLE calendar_event (
    google_event_id          text PRIMARY KEY,
    ical_uid                 text,
    summary                  text,

    start_date               date,
    start_datetime           timestamptz,
    start_timezone           text,
    end_date                 date,
    end_datetime             timestamptz,
    end_timezone             text,
    all_day                  boolean NOT NULL,

    start_utc                timestamptz NOT NULL,
    end_utc                  timestamptz NOT NULL,

    recurrence               text[],
    recurring_event_id       text,
    original_start_date      date,
    original_start_datetime  timestamptz,
    original_start_timezone  text,

    location                 text,
    description              text,
    calendar_id              text NOT NULL,
    status                   text NOT NULL DEFAULT 'confirmed'
                                 CHECK (status IN ('confirmed', 'tentative', 'cancelled')),
    etag                     text NOT NULL,
    sequence                 integer NOT NULL DEFAULT 0,
    raw                      jsonb NOT NULL,
    synced_at                timestamptz NOT NULL DEFAULT now(),

    -- Mirror Google exactly: start and end each carry exactly one of date/datetime;
    -- original-start is optional (override children only), so 0 or 1.
    CONSTRAINT calendar_event_start_xor CHECK (num_nonnulls(start_date, start_datetime) = 1),
    CONSTRAINT calendar_event_end_xor   CHECK (num_nonnulls(end_date, end_datetime) = 1),
    CONSTRAINT calendar_event_orig_xor  CHECK (num_nonnulls(original_start_date, original_start_datetime) <= 1)
);

CREATE INDEX calendar_event_start_utc_idx ON calendar_event (start_utc);
CREATE INDEX calendar_event_end_utc_idx   ON calendar_event (end_utc);
CREATE INDEX calendar_event_recurring_idx ON calendar_event (recurring_event_id)
    WHERE recurring_event_id IS NOT NULL;

-- One row per calendar; holds the incremental sync token (ADR-0042).
CREATE TABLE sync_state (
    calendar_id          text PRIMARY KEY,
    sync_token           text,
    last_synced_at       timestamptz,
    last_full_resync_at  timestamptz,
    updated_at           timestamptz NOT NULL DEFAULT now()
);
