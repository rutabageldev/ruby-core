# Make ruby-core the authority for the family calendar, on a reusable HTTP read plane

* **Status:** Planned
* **Date:** 2026-06-27
* **Project:** ruby-core
* **Related ADRs:** ADR-0040 (read-API service + defense-in-depth auth), ADR-0041 (OpenAPI lifecycle & codegen governance), ADR-0042 (calendar sync architecture) — all to be drafted in the docs decomposition
* **Linked Plan:** docs/plans/PLAN-0030-api-foundation.md, PLAN-0031-event-bus-generalization.md, PLAN-0032-calendar-core.md, PLAN-0033-household-overlay.md

---

**Goal:** Make ruby-core the durable system of record and read authority for the family calendar — bidirectionally synced with the human-facing Google **Family** calendar, holding the local household overlay (subjects, childcare) and reminder policy Google cannot — served over ruby-core's first synchronous, spec-first HTTP read API that every future domain inherits.

---

## Authority split (the invariant this roadmap establishes)

* **Google owns:** standard calendar data — summary, start/end, recurrence, location, description, calendarId, attendees. Google stays the human-facing system of record.
* **ruby-core owns (local, never written to Google):** event *subjects* (the "FOR" lane), *childcare* assignments, the people/provider registries, reminder policy, and all derived/automation state.
* **Read path is REST; write path is the existing HA event bus → NATS → a single engine-side consumer.** The REST surface is deliberately read-only. The NATS write-event payloads are first-class, documented contracts equal to the OpenAPI surface.

---

## Efforts

Each effort is one branch/PR (single-concern). **0012.1 and 0012.2 are independent and land in parallel.** 0012.3 depends on both. 0012.4 depends on 0012.3. The cross-repo Home Assistant producer migration (the eventual `ada_event` drop in 0012.2) is HA-side work and is **described, not executed** here — the `homeassistant` repo is out of scope for commits.

### 0012.1 — API foundation (`feat/api-foundation`)

Stand up `services/api`: ruby-core's first synchronous HTTP read plane, spec-first with **ogen** generating the typed server, client, and validation from a hand-authored OpenAPI spec (per-domain fragments bundled into one served spec). RFC 9457 Problem Details error model defined once. URL-path versioning (`/v1/`). Defense-in-depth auth: Vault-issued bearer modeled as an OpenAPI security scheme, layered under Traefik edge auth + mTLS (ADR-0040). Scalar docs at `/docs`, raw spec at `/openapi.yaml`. CI gates: codegen fail-on-diff, Spectral lint (description + example required on every operation/parameter/response), oasdiff breaking-change block. Generated Python client for the HA read-proxy. **Out of scope:** any calendar/overlay endpoints with real data (they arrive in 0012.3/0012.4 against this platform); Schemathesis fuzzing and mock servers (deferred); cursor pagination *implementation* (convention documented in the style guide, only the date-range filter is built).

### 0012.2 — Event-bus generalization (`feat/event-bus-generalization`)

Make the gateway write pipe domain-neutral. The gateway **dual-subscribes** `ada_event` + a new `ruby_home_event` HA event type during cutover, deriving NATS subjects from the payload `event` string (mirroring the existing `services/gateway/ada/publish.go` route-map pattern). This carries the new `calendar.*` and `ruby_home.childcare.*` write contracts onto the bus without disturbing Ada. **Out of scope:** dropping `ada_event` (deferred until HA producers migrate — cross-repo follow-up); any consumer of the new subjects (that is 0012.3).

### 0012.3 — Calendar core (`feat/calendar-core`)

The mirror, sync, and write consumer. OAuth user-consent offline refresh token (not a service account) addressing the Family calendar by `calendarId`; refresh token in Vault; OAuth app in **production** publishing status. Incremental sync-token polling (~60s) with a `SYNC_STATE` token; 410 → full resync. A `CALENDAR_EVENT` mirror that reproduces Google's native date shape (date XOR datetime+tz trios; exclusive all-day end surfaced to consumers; derived UTC columns for range queries; recurrence as RRULE/EXDATE/RDATE lines; cancelled/override children retained). A single engine-side write consumer on `ha.events.calendar.event_upsert` / `event_delete` with idempotency-key dedupe on create, etag `If-Match` on update (412 → resync+retry), series-level delete, write-through to Google + mirror in one operation, and etag echo reconciliation. Timezone-aware on-demand recurrence expansion. Reminder policy owned by ruby-core (Google reminder overrides not honored), surfaced via `sensor.ruby_home_calendar_status` and NATS `calendar.reminder.due`. Read endpoint `GET /v1/calendar/events?start=&end=` with a max-window guard. **Out of scope:** per-instance recurrence edit and single-instance delete (deferred — the `original_start_*` columns are the groundwork); multi-calendar (single-calendar is a conscious boundary); the household overlay resolution in responses (lands in 0012.4).

### 0012.4 — Household overlay (`feat/household-overlay`)

Local-only registries and event associations, never written to Google: `DIRECTORY_PERSON`, `CHILDCARE_PROVIDER`, `EVENT_SUBJECT`, `EVENT_CHILDCARE`. Write events `ruby_home.childcare.provider.upsert` / `.delete` (delete = archive, preserving frequency history) and subject associations riding inside `calendar.event.upsert.subjects[]`. Calendar responses gain resolved `subjects[]`, resolved `childcare`, and attendees reconciled by email to `person_id`. Read endpoints `GET /v1/directory/people`, `GET /v1/childcare/providers`, and `GET /v1/childcare/providers/suggestions` (providers ranked by per-occurrence recency-weighted usage computed from associations + expansion — no stored counter). **Out of scope:** per-instance childcare override (this Tuesday it's Grandma — series-level only for MVP); group membership for `kind = group`; any read/modification of the unrelated Ada caretaker notification toggle.

---

## Done When

* `services/api` is deployed behind Traefik, green in Uptime Kuma on `/health`, serving `/v1/` with the bundled OpenAPI spec at `/openapi.yaml` and Scalar docs at `/docs`; CI blocks any merge where the spec, generated Go server/client, generated Python client, or sqlc output is out of sync with source, where Spectral finds an operation/parameter/response missing a description or example, or where oasdiff detects an unapproved breaking change.
* Creating or editing an event in the Google **Family** calendar appears in `GET /v1/calendar/events?start=&end=` within ~60s, with recurring series expanded timezone-correctly across DST, EXDATEs and cancelled occurrences subtracted, and overrides applied — and a `calendar.event.upsert` published to NATS creates/updates the event in Google *and* the local mirror in one operation, without a redelivery double-inserting or a concurrent edit clobbering via etag.
* Calendar instances returned by the API carry resolved `subjects[]`, resolved `childcare`, and attendees reconciled to `person_id` with Google RSVP status; `GET /v1/childcare/providers/suggestions` returns providers ranked by recent per-occurrence usage.
* The engine computes and fires reminders from event start times over the mirror — surfaced on `sensor.ruby_home_calendar_status` and NATS `calendar.reminder.due` — with no Google reminder override honored and no calendar card open.
* The new durable tables (`CALENDAR_EVENT`, `SYNC_STATE`, overlay) are covered by a documented backup + restore procedure and a force-resync runbook.

---

## Acceptance Criteria

* [ ] **API platform:** `make openapi-gen` followed by `git diff --exit-code` passes on a clean tree; introducing an undocumented operation fails Spectral in CI; a removed/renamed field fails oasdiff in CI; `curl -sf https://<api-host>/health` returns 200 and `/v1/...` without a valid bearer returns an RFC 9457 `application/problem+json` 401.
* [ ] **Read-only enforcement:** the `api` service connects with a `SELECT`-only Postgres role; an attempted write through its pool fails at the database, not just in code.
* [ ] **Bus generalization:** publishing an HA `ruby_home_event` with `event: "calendar.event.upsert"` lands a CloudEvent on `ha.events.calendar.event_upsert` in `HA_EVENTS`, while `ada_event` continues to route unchanged (verified by an integration test against testcontainers NATS).
* [ ] **Sync + write-through:** with `CALENDAR_SYNC_ENABLED=true` against a scratch calendar, an event created in Google appears in `CALENDAR_EVENT` within two poll cycles; publishing `calendar.event.upsert` with no `google_event_id` twice (same `idempotency_key`) creates exactly one Google event and one mirror row; an update with a stale `etag` returns/handles Google 412 by resyncing and retrying rather than clobbering; a sync-token 410 triggers a logged full resync (not a silent stop).
* [ ] **Recurrence correctness:** `go test -tags=fast ./pkg/calendar/...` proves a weekly 9am event expands to 9am local on each occurrence across a spring-forward DST boundary, that EXDATE and `status=cancelled` children are subtracted, and that a modified-occurrence override replaces the base instance; a range request exceeding the max window returns an RFC 9457 400.
* [ ] **Overlay + suggestions:** an event with `subjects[]` and a `childcare` provider returns both resolved in `GET /v1/calendar/events`; `provider.delete` sets `archived = true` (row retained); `GET /v1/childcare/providers/suggestions` ranks a provider attached to a weekly series above one attached to a single past event, computed from associations + expansion with no stored usage column.
* [ ] **Reminders:** with no app/card open, an event whose start crosses the configured reminder lead time sets the active-reminder flag on `sensor.ruby_home_calendar_status` and publishes `calendar.reminder.due` on NATS; no Google per-event reminder override changes ruby-core behavior.
* [ ] **Operability:** the OAuth bootstrap runbook (`docs/runbooks/google-calendar-oauth.md`) and a force-resync procedure exist; the new Postgres tables are included in the foundation Postgres backup set with a documented restore.

---

## Related Work & Standards Notes

* **ADRs to write (in the docs decomposition):** ADR-0040 generalizes the ADR-0020 edge-auth posture to `services/api` and adds the in-app bearer as defense-in-depth (ADR-0020 stays in force for the gateway). ADR-0041 establishes OpenAPI/codegen governance, relating to ADR-0014 (schema governance). ADR-0042 records the calendar sync architecture (single writer per ADR-0023; self-contained processor per ADR-0007; idempotency per ADR-0025; subject naming per ADR-0027).
* **Depends on / builds from:** the Ada persistence pattern (sqlc + golang-migrate, shared external Postgres, per-processor migration table — ADR-0029/0037), the engine processor framework (ADR-0007), the gateway responsibilities split (ADR-0009), and the HA_EVENTS stream retention bounds (ADR-0034).
* **Standards this advances:** establishes the spec-first, self-documenting HTTP API the global standards require (OpenAPI source of truth, generation enforced in CI) — the read plane future domains (Ada, finance) inherit.
* **Production-readiness blockers to track:** backup + restore coverage for the new stateful tables; a read-only Postgres role and a `secret/ruby-core/postgres_readonly` Vault path provisioned before `api` deploys; OAuth app set to **production** publishing status (testing-mode refresh tokens expire ~7 days) with alerting on Google auth failure; OTEL tracing for the new `api` service (today only structured logging exists).
