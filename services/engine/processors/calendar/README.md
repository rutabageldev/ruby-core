# calendar processor

The engine-side owner of the family calendar (ROADMAP-0012, ADR-0042). Google Calendar
is the system of record; ruby-core holds the durable mirror (`pkg/calendar/store`) and the
local overlay, and serves all reads via `services/api`.

## Responsibilities

- **Write consumer** — the single ingress for calendar writes, on `ha.events.calendar.event_upsert`
  and `ha.events.calendar.event_delete` (routed in by the gateway, Slice B). Write-through:
  - create → dedupe on `idempotency_key`, Google Insert, mirror upsert (one op);
  - update → Google Update with `If-Match` etag; on a 412, resync the event and retry once
    (never clobber a concurrent edit);
  - delete → series-level Google delete + mirror delete; overlay rows cascade via the FK.
- **Household overlay writes** (`overlay_write.go`, Slice D) — on `ha.events.ruby_home.childcare.*`,
  upsert/archive childcare providers; and on `calendar.event.upsert`, reconcile the event's
  `event_subject` / `event_childcare` associations from the payload's `subjects[]` / `childcare`.
  Local-only; never written to Google.
- **Sync poller** (`poller.go`) — incremental sync-token polling (~60s) into the mirror,
  persisting `nextSyncToken` in `sync_state`. A 410 (expired token) triggers one full resync.
  Echo reconciliation skips re-observed self-writes by etag. A future Google watch/push can
  replace the timer on the same `syncOnce` path.
- **Reminders** (`reminders.go`) — ruby-core owns reminder policy and ignores Google's
  per-event reminder overrides (ADR-0042). A ~60s ticker expands the upcoming window over the
  mirror, fires each due occurrence once on NATS `calendar.reminder.due`
  (`ha.events.calendar.reminder_due`, deduped by event id + occurrence start), and refreshes
  the always-on `sensor.ruby_home_calendar_status` (state `reminder`/`upcoming`/`idle` + next
  event + `active_reminder` flag) so HA automations work with no card open. The HA push is a
  no-op where HA is not configured (non-prod). Lead time: `CALENDAR_REMINDER_LEAD` (default 10m).

## Gating

All Google access is behind `CALENDAR_SYNC_ENABLED`. Only the environment that owns the
single shared calendar (prod) sets it `true`; elsewhere the processor initializes (so the
mirror migrations run and the read API works) but does not connect to Google and ignores
write events — the ADR-0033 analog for the shared external resource.

## Configuration

| Var | Purpose |
|---|---|
| `CALENDAR_SYNC_ENABLED` | `true` to connect Google + run the poller + reminders (prod only). |
| `VAULT_GOOGLE_PATH` | Vault path for OAuth creds (default `secret/data/ruby-core/google`). |
| `CALENDAR_REMINDER_LEAD` | How far before start a reminder fires (Go duration; default `10m`). |

Credentials (`client_id`, `client_secret`, `refresh_token`, `calendar_id`) are minted once
via `cmd/google-auth` and stored in Vault — see `docs/runbooks/google-calendar-oauth.md`.
The engine's Vault token policy must grant read on the google path.

## Layout

- `gcal/` — Google Calendar v3 behind a mockable `Service` interface + the Vault-backed
  token source.
- `mapping.go` — Google event ↔ mirror row / payload conversions.
- `pkg/calendar/expand` — timezone-aware recurrence expansion (shared with the read API).
- `pkg/calendar/expand_range.go` — `ExpandRange`, the shared range→instances helper used by
  both the read endpoint and reminders.
- `hapush.go` — minimal self-contained HA REST client for the status sensor.

## Tests

`go test -tags=fast ./services/engine/processors/calendar/...` covers write-through
(create/dedup, update/412-resync, delete) and the poller (sync/echo/410) with a fake Google
client and a fake store — no live OAuth or DB container required.
