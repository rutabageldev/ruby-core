-- Drop caretakers table (replaced by people + person_channels).
-- Data is intentionally not migrated — caretaker flags will be
-- re-enabled manually after sync.
DROP TABLE IF EXISTS caretakers;

-- people is the authoritative record of a person in the Ada system.
-- All people are created via HA user sync (ha_user_id is always set).
-- Phone numbers and device channels are the only things added outside sync.
-- This table is excluded from pre-birth data clear operations.
CREATE TABLE IF NOT EXISTS people (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ha_user_id      TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    username        TEXT NOT NULL,
    is_caretaker    BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_people_active_caretakers
    ON people(is_caretaker)
    WHERE deleted_at IS NULL AND is_caretaker = true;

-- person_channels stores notification channels per person.
-- type: 'ha_push' | 'sms'
-- address: mobile_app_* service name for ha_push; E.164 phone number for sms
-- label: optional human-readable label ("Mike's iPhone", "Mobile")
-- is_active: allows disabling a channel without deleting it
CREATE TABLE IF NOT EXISTS person_channels (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id   UUID NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    type        TEXT NOT NULL CHECK (type IN ('ha_push', 'sms')),
    address     TEXT NOT NULL,
    label       TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (person_id, address)
);

CREATE INDEX IF NOT EXISTS idx_person_channels_person
    ON person_channels(person_id);
