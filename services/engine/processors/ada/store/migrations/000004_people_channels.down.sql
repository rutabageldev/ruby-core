DROP TABLE IF EXISTS person_channels;
DROP TABLE IF EXISTS people;

-- Recreate caretakers so migration 000003 state is consistent after rollback.
-- Table will be empty; re-sync and re-enable caretaker flags after rollback.
CREATE TABLE IF NOT EXISTS caretakers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ha_user_id      TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    username        TEXT NOT NULL,
    is_caretaker    BOOLEAN NOT NULL DEFAULT false,
    notify_service  TEXT,
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_caretakers_active
    ON caretakers(is_caretaker)
    WHERE deleted_at IS NULL AND is_caretaker = true;
