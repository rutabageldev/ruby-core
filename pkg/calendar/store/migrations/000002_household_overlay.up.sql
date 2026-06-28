-- Household overlay (ROADMAP-0012 Slice D): local-only registries and event
-- associations, never written to Google. Enum-like columns are text + CHECK (not
-- PG enums). email uses text + a case-insensitive unique index rather than the
-- citext extension, to avoid a CREATE EXTENSION privilege dependency.

CREATE TABLE directory_person (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name         text NOT NULL,
    kind                 text NOT NULL DEFAULT 'person' CHECK (kind IN ('person', 'group')),
    ha_person_entity_id  text,
    email                text,
    family               text,
    color                text,
    active               boolean NOT NULL DEFAULT true,
    created_at           timestamptz NOT NULL DEFAULT now()
);

-- Case-insensitive unique email for attendee reconciliation (when present).
CREATE UNIQUE INDEX directory_person_email_lower_idx
    ON directory_person (lower(email)) WHERE email IS NOT NULL;

CREATE TABLE childcare_provider (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name  text NOT NULL,
    person_id     uuid REFERENCES directory_person (id) ON DELETE SET NULL,
    relationship  text,
    archived      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX childcare_provider_active_idx ON childcare_provider (display_name) WHERE archived = false;

-- Event associations. ON DELETE CASCADE to calendar_event means a true event delete
-- cleans up its overlay rows automatically (the cascade ADR-0042/PLAN-0033 require).
CREATE TABLE event_subject (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    google_event_id  text NOT NULL REFERENCES calendar_event (google_event_id) ON DELETE CASCADE,
    person_id        uuid NOT NULL REFERENCES directory_person (id) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (google_event_id, person_id)
);

CREATE INDEX event_subject_event_idx ON event_subject (google_event_id);

CREATE TABLE event_childcare (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    google_event_id  text NOT NULL REFERENCES calendar_event (google_event_id) ON DELETE CASCADE,
    provider_id      uuid NOT NULL REFERENCES childcare_provider (id) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (google_event_id, provider_id)
);

CREATE INDEX event_childcare_event_idx ON event_childcare (google_event_id);
