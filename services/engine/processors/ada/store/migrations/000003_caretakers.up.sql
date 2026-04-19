-- caretakers tracks HA user accounts and their notification preferences.
-- Populated and kept in sync via the ada.sync_users event.
-- is_caretaker defaults to false — users must be explicitly enabled.
-- notify_service is auto-discovered during sync (mobile_app_* matching)
-- and may be null if no Companion app is linked.
-- This table is excluded from pre-birth data clear operations.
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
