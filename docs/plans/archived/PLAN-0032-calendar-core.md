# PLAN-0032 - Calendar Core (mirror, sync, write consumer, read endpoint)

* **Status:** Complete
* **Date:** 2026-06-27
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0012-home-calendar.md (effort 0012.3)
* **Branch:** feat/calendar-core
* **Related ADRs:** ADR-0042 (calendar sync architecture), ADR-0040 (read API), ADR-0023 (single writer), ADR-0025 (idempotency), ADR-0029/0037 (Postgres persistence pattern), ADR-0027 (subjects)

---

## Scope

Build the calendar mirror, bidirectional Google sync, the single engine-side write consumer, reminder
policy, and the `GET /v1/calendar/events` read endpoint. Depends on PLAN-0030 (read API platform) and
PLAN-0031 (the `ha.events.calendar.*` ingress). **Out of scope:** the household overlay tables and
their resolution in responses (PLAN-0033 — this slice returns native calendar fields and leaves
`subjects[]`/`childcare` empty/absent); per-instance recurrence edit and single-instance delete
(deferred; the `original_start_*` columns are laid down now); multi-calendar.

---

## Pre-conditions

* [ ] On branch `feat/calendar-core` cut from latest `main`, with 0012.1 and 0012.2 merged.
* [ ] Google OAuth complete per `docs/runbooks/google-calendar-oauth.md`: `secret/ruby-core/google`
      holds `client_id`, `client_secret`, `refresh_token`, `calendar_id`; OAuth app in **production**
      publishing status. (`client_id`/`client_secret`/`calendar_id` suffice to start; the
      `refresh_token` is minted with the Step 1 helper.)
* [ ] Engine Vault read policy will be extended to `secret/data/ruby-core/google` (Step 7).

---

## Steps

### Step 1 — `cmd/google-auth` refresh-token helper

**Action:** Build `cmd/google-auth/main.go`: a loopback OAuth flow (`golang.org/x/oauth2` +
`/google`) taking `--client-id`/`--client-secret`, scope `calendar.events`, `AccessType=offline`,
`prompt=consent`; prints the `refresh_token`.

**Verification:** Operator runs it once, signs in as the household account, and it prints a
`refresh_token`; that token is stored in Vault per the runbook.

### Step 2 — Shared calendar store package `pkg/calendar/store`

**Action:** Create `pkg/calendar/store/` mirroring `services/engine/processors/ada/store/`:
`sqlc.yaml` (copy ada's verbatim — postgresql, pgx/v5, emit_*_struct_pointers), `migrate.go`
(`//go:embed migrations/*.sql` → `pkgstore.MigrateUp(ctx, fs, "migrations", dsn, "schema_migrations_calendar")`),
`migrations/000001_calendar_core.up.sql`/`.down.sql`, and `queries/{calendar,sync_state}.sql`.
The `000001` schema per ADR-0042: `calendar_event` (`google_event_id` PK; start/end/original_start
date-XOR-datetime+tz trios with CHECKs; `all_day`; derived `start_utc`/`end_utc` indexed;
`recurrence text[]`; `recurring_event_id`; `status text` + CHECK; `etag`; `sequence`; `raw jsonb`;
`synced_at`) and `sync_state` (`calendar_id` PK, `sync_token`, `last_synced_at`,
`last_full_resync_at`). Generate code.

**Verification:** `sqlc generate` (from `pkg/calendar/store`) produces `*.sql.go` with no diff on
re-run; `go build ./pkg/calendar/...` compiles; a testcontainers Postgres applies `000001` cleanly and
the date-XOR CHECK rejects a row with both `start_date` and `start_datetime` set.

### Step 3 — Timezone-aware expansion `pkg/calendar/expand`

**Action:** Create `pkg/calendar/expand/` using `github.com/teambition/rrule-go`: given a master event
(start in its IANA tz + `recurrence` lines) and a window, build an `rrule.Set` (RRULE+EXDATE+RDATE),
generate occurrences in-zone (DST-correct), subtract EXDATEs and `status=cancelled` children (matched
by `recurring_event_id` + `original_start_*`), and apply modified-occurrence overrides. Single
(non-recurring) events pass through directly. Enforce a max-window guard (return a typed error beyond
N days).

**Verification:** `go test -tags=fast ./pkg/calendar/expand/...` covers: weekly 9am stays 9am local
across a spring-forward boundary; EXDATE removes an instance; a cancelled child is subtracted; an
override replaces the base instance; a window beyond the guard returns the window-exceeded error.

### Step 4 — Google client wrapper behind an interface

**Action:** Create `services/engine/processors/calendar/google/` with a `CalendarService` interface
(List with sync token + ShowDeleted, Insert, Update with If-Match, Delete) and a real implementation
over `google.golang.org/api/calendar/v3`; `token.go` builds an auto-refreshing
`oauth2.ReuseTokenSource` from the Vault refresh token. Add `boot.FetchGoogleConfig` to `pkg/boot`
mirroring `FetchPostgresConfig`, reading `secret/data/ruby-core/google`.

**Verification:** `go build ./...`; a fake `CalendarService` is usable in tests; `FetchGoogleConfig`
unit test parses the four fields.

### Step 5 — Sync poller

**Action:** In `services/engine/processors/calendar/poller.go`, implement the ~60s incremental loop:
`List(calendarId).SyncToken(tok).ShowDeleted(true)`, upsert each event into the mirror (mapping
Google's native date shape into the trios + derived UTC), persist `nextSyncToken` to `sync_state`.
**410** → clear token, full page-through resync, set `last_full_resync_at`, log it. Reconcile
re-observed self-writes by `etag` (no reprocess). Launch the goroutine from the processor's
`Initialize`, cancel in `Shutdown`. Gate the whole poller on `CALENDAR_SYNC_ENABLED` (default false).

**Verification:** `go test -tags=fast` against the fake `CalendarService`: a returned event upserts a
mirror row; a 410 transitions to a full resync and records `last_full_resync_at`; an event whose etag
matches the mirror is a no-op. Integration (`-tags=integration`, testcontainers Postgres) confirms the
upsert path against real SQL.

### Step 6 — Write consumer (engine processor)

**Action:** Create `services/engine/processors/calendar/processor.go` implementing `StatefulProcessor`
(`RequiresStorage()=true`), `Subscriptions()` → `ha.events.calendar.event_upsert`,
`ha.events.calendar.event_delete`. In `writethrough.go`: on upsert, dedupe create on the payload
`idempotency_key` (reuse `pkg/idempotency` HybridStore on a `calendar` KV bucket), call Google
Insert/Update (Update sends stored `etag` as `If-Match`; **412** → resync + retry once), then upsert
the mirror with Google's returned id/etag in the same operation. On delete: a series-level Google
delete plus the mirror status update (cancel); cascade is wired in PLAN-0033 (overlay tables don't
exist yet).
Register `host.Register(calendar.New(logger))` in `services/engine/main.go` and add
`calendarstore.MigrateUp(ctx, pgCfg.DSN())` right after the existing `adastore.MigrateUp(...)` in the
`host.RequiresStorage()` block.

**Verification:** `go test -tags=fast ./services/engine/...`: an upsert with no `google_event_id`
calls Insert once and writes one mirror row; the same `idempotency_key` redelivered does not call
Insert again; an update with a stale etag triggers the 412→resync→retry path. Integration: publish
`event_upsert` to testcontainers NATS → mirror row appears (Google faked).

### Step 7 — Vault policy + engine config

**Action:** Extend the engine's Vault read grant to `secret/data/ruby-core/google` (policy file in
`/opt/foundation/vault/`, applied host-side). Add `CALENDAR_SYNC_ENABLED` to the engine config and to
`deploy/{dev,staging,prod}` env (`true` only in prod `.env`). Update `.env.example` files and the host
`deploy/prod/.env` / `deploy/staging/.env` per the version-bump feedback.

**Verification:** Prod engine starts with the policy applied and reads `secret/ruby-core/google`
without permission error; non-prod engines start with the poller disabled (log line confirms).

### Step 8 — Reminders

**Action:** In `services/engine/processors/calendar/reminders.go`, a ticker expands a near-future
window over the mirror, computes due reminders from start times (ruby-core policy; **ignore** Google
reminder overrides), pushes `sensor.ruby_home_calendar_status` (next event + active-reminder flag) via
the engine HA client, and publishes a CloudEvent on NATS `calendar.reminder.due` (subject
`ha.events.calendar.reminder_due`, lands in HA_EVENTS). HA push gated to prod (ADR-0033 analog).

**Verification:** `go test -tags=fast`: an event crossing the reminder lead time produces exactly one
due reminder and one published `reminder_due`; no Google reminder override changes the result.

### Step 9 — Read endpoint `GET /v1/calendar/events?start=&end=`

**Action:** Add `api/openapi/paths/calendar.yaml` (the endpoint with `start`/`end` params, the
instance response schema mirroring native fields + placeholders for `subjects[]`/`childcare`/attendees
filled in PLAN-0033, all documented with examples), regenerate. Implement `services/api/handlers/calendar.go`:
read from `pkg/calendar/store`, expand via `pkg/calendar/expand`, sort, apply the max-window guard
(→ RFC 9457 400). Reference `calendar.yaml` from the root spec.

**Verification:** `make openapi-gen` clean diff; `make openapi-lint`/`openapi-diff` pass; integration
(`-tags=integration`, testcontainers Postgres seeded with a recurring event) — the generated Go client
gets back expanded, sorted instances for the window; an over-wide range returns a 400 Problem.

### Step 10 — Backup, runbook, README, Pre-PR checklist

**Action:** Add the new tables to the foundation Postgres backup set and document restore + the
force-resync procedure (extend `docs/runbooks/google-calendar-oauth.md`, already drafted). Add
`services/engine/processors/calendar/README.md`. Update `docs/plans/README.md`. Run the Pre-PR
Checklist; archive this plan to `docs/plans/archived/` as the final commit.

**Verification:** Backup job includes `calendar_event`/`sync_state`; pre-commit hooks pass; READMEs
reflect reality.

---

## Rollback

This slice introduces durable state and external writes — rollback is **not** fully clean:

* **Schema:** `000001` is additive (new tables); a down-migration drops them. Rolling back the engine
  image leaves the tables in place (harmless; they are only read/written by this processor). Do not
  down-migrate if any real events have synced — snapshot first.
* **Google writes:** events ruby-core created in Google persist after rollback (Google is the source of
  record); this is correct, not a leak.
* **Sync:** disabling `CALENDAR_SYNC_ENABLED` and redeploying the engine stops all polling/writing
  immediately — the primary kill switch.
Document the snapshot-before-down-migration step explicitly; treat full rollback as engine-image
revert + `CALENDAR_SYNC_ENABLED=false`, leaving tables intact.

---

## Open Questions

* **Reminder lead-time policy:** what lead time(s) drive `calendar.reminder.due` and the active flag
  (single fixed lead, or per-event)? Defaulting to a single configurable lead in Step 8 unless
  specified.
* **`logged_by` attribution:** the write contracts inject `logged_by` for per-edit attribution on the
  shared calendar — confirm whether it is persisted on `calendar_event` (audit column) or only emitted
  to the audit stream. (Recommendation: audit stream only this slice; add a column if needed later.)

---

## Completion Notes

Delivered on branch `feat/calendar-core` (3 commits: foundation, core, reminders). Notes and
decisions:

* **Open questions resolved as recommended.** Reminders use a single configurable lead
  (`CALENDAR_REMINDER_LEAD`, default 10m). `logged_by` is emitted to logs/audit only this slice,
  not persisted on `calendar_event`.
* **Reminders built** (`reminders.go` + `hapush.go`): NATS `calendar.reminder.due` (deduped per
  occurrence) + `sensor.ruby_home_calendar_status`. The calendar got its own minimal HA REST client
  rather than reusing ada's (ADR-0007 self-containment) — a shared `pkg/haclient` is a drift candidate.
* **ogen + Spectral cannot model `nullable` schemas** (nimma crash), so the read endpoint's
  `childcare` is an optional omitted-when-absent field rather than explicitly `null`. Minor contract
  nuance; revisit if a tri-state is ever needed.
* **Overlay fields deferred to Slice D.** The read response returns `subjects: []` and omits
  `childcare`/attendees; the shape is established so Slice D fills them non-breaking.
* **All-day derived UTC** anchors at midnight UTC (internal range key only); the native date trio is
  preserved for output.
* **sqlc CI gate added** (regenerate + fail-on-diff) for the calendar store, closing drift #117 for
  calendar. Ada's sqlc remains manual (out of concern for this branch).
* **No Postgres integration test** — the repo has no PG-testcontainer pattern, so the processor is
  unit-tested with a fake store + fake Google client; real SQL is exercised only by the running engine.
  A PG-testcontainer harness is a possible follow-up.
* **Not verified end-to-end against live Google** — requires the engine deployed/run with this code,
  `CALENDAR_SYNC_ENABLED=true`, and the consent screen in production status. Credentials are confirmed
  present in Vault and readable by the engine token.
