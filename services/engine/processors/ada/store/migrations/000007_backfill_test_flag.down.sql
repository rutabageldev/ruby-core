-- No-op: the 000007 backfill is irreversible — the original test=false values were
-- not retained, so they cannot be restored. The test column itself is dropped by the
-- 000006 down migration.
SELECT 1;
