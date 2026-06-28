-- name: GetSyncState :one
SELECT * FROM sync_state WHERE calendar_id = @calendar_id;

-- UpsertSyncToken records the latest nextSyncToken after a successful sync pass.
-- name: UpsertSyncToken :exec
INSERT INTO sync_state (calendar_id, sync_token, last_synced_at, updated_at)
VALUES (@calendar_id, @sync_token, now(), now())
ON CONFLICT (calendar_id) DO UPDATE SET
    sync_token = EXCLUDED.sync_token,
    last_synced_at = now(),
    updated_at = now();

-- MarkFullResync clears the sync token (e.g. on a 410 Gone) and stamps the full
-- resync time. The next pass pages through from scratch and records a fresh token.
-- name: MarkFullResync :exec
INSERT INTO sync_state (calendar_id, sync_token, last_full_resync_at, updated_at)
VALUES (@calendar_id, NULL, now(), now())
ON CONFLICT (calendar_id) DO UPDATE SET
    sync_token = NULL,
    last_full_resync_at = now(),
    updated_at = now();
