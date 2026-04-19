-- ada_profile stores Ada's birth information.
-- This table is explicitly excluded from any future data clear operations —
-- it is never truncated, soft-deleted in bulk, or affected by pre-birth
-- data cleanup.
--
-- The singleton constraint guarantees at most one row. ON CONFLICT (singleton)
-- DO NOTHING in the insert query makes the write idempotent — repeated
-- ada.born events (e.g. accidental double-tap) are silently ignored.
CREATE TABLE IF NOT EXISTS ada_profile (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    birth_at    TIMESTAMPTZ NOT NULL,
    singleton   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ada_profile_singleton UNIQUE (singleton)
);
