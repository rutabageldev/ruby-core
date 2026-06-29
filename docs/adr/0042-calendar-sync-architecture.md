# ADR-0042 - Calendar Sync Architecture: Google as Source of Record, Local Durable Mirror

* **Status:** Accepted (amended 2026-06-28, see Amendments)
* **Date:** 2026-06-27
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

ROADMAP-0012 makes ruby-core the durable authority and read source for the family calendar while keeping the Google **Family** calendar as the human-facing system of record. The household will keep adding and editing events in the Google app on their phones; ruby-core must reflect those changes, serve all reads, layer on local-only data Google cannot hold (event subjects, childcare assignments), own reminder policy, and drive automations — and it must also write back the events ruby-core itself originates.

Several forces shape the design:

* **The account is a consumer Gmail account.** It cannot be impersonated by a Google Workspace service account, so domain-wide delegation is unavailable. Access must be via OAuth user consent with an offline refresh token.
* **Google's OAuth "Testing" publishing status expires refresh tokens after ~7 days.** A long-lived integration requires the OAuth app in **production** publishing status.
* **NATS is at-least-once** (the existing engine consumer already dedupes on event id, ADR-0025). A create that is redelivered must not double-insert into Google.
* **Two writers can race.** ruby-core writes through to Google and also polls Google for changes; without care, ruby-core re-observes its own writes and reacts to them (echo ping-pong), and a concurrent human edit can be clobbered.
* **Google's data model is specific.** Dates are a date XOR datetime+timezone trio; all-day `end` is exclusive; recurrence is a set of RRULE/EXDATE/RDATE lines; a modified or cancelled single occurrence is a separate event carrying `recurringEventId` and an original-start anchor. A faithful mirror must reproduce this rather than flatten it, or downstream consumers get subtly wrong times (especially across DST).
* **The repo already has the building blocks.** Postgres persistence with sqlc + golang-migrate (Ada, ADR-0029/0037), a single-writer principle (ADR-0023), self-contained engine processors (ADR-0007), idempotency tracking (ADR-0025), and the HA→NATS write path through the gateway (ADR-0009) with subject naming per ADR-0027.

The decision records how sync, write-through, persistence shape, recurrence, and reminders fit together — and the conscious boundaries (single calendar, series-level edits) we are accepting for the MVP.

## Alternatives Considered

**Google service account with domain-wide delegation** — The clean impersonation path, but impossible on a consumer Gmail account (delegation requires a Workspace domain). Rejected: not available for this account.

**Treat Google as a cache; make ruby-core the write-side system of record** — ruby-core would own events and push to Google one-directionally. Rejected: the family lives in the Google app; it must stay the human-facing source of record, and edits made there must win. ruby-core is the durable *mirror* and read authority, not the primary author.

**Google push notifications (`watch` channels) instead of polling** — Lower latency, no timer. Rejected *for now*: push requires a public HTTPS webhook endpoint and channel lifecycle management; ~60s polling is operationally trivial on a single node. The design keeps push as a drop-in future replacement — a push only needs to trigger the same incremental sync the timer triggers.

**Flatten dates to a single UTC timestamp pair in the mirror** — Simpler schema, but loses the all-day distinction, the original timezone, and the exclusive-end convention, and makes DST-correct recurrence expansion impossible. Rejected: the mirror must reproduce Google's native shape; derived UTC columns exist only for range queries.

**Pre-expand recurring events into stored instance rows** — Makes range reads a trivial SELECT, but stores a combinatorial explosion, drifts whenever a rule changes, and still needs tz logic to generate. Rejected: expansion is computed on demand, timezone-aware, with a max-window guard.

**Honor Google's per-event reminder overrides** — Would mirror a `reminders` column and fire what users set in the Google app. Rejected: reminder policy is a ruby-core concern (it drives HA automations with no card open); honoring Google's overrides splits ownership and creates two competing reminder sources. ruby-core owns reminders outright.

**Per-instance recurrence editing in the MVP** — Matches Google's full capability, but the write path, idempotency, and overlay cascade get materially more complex. Rejected for MVP: delete/edit are series-level; the `original_start_*` columns are laid down now as the groundwork for per-instance later.

**Multi-calendar from the start** — More general, but `google_event_id` is unique only within a calendar, so the mirror PK and every overlay foreign key would need a calendar dimension. Rejected: single-calendar is a conscious boundary; going multi later is a known, scoped migration.

## Decision

1. **Authority split.** Google **MUST** remain the human-facing system of record for standard calendar fields (summary, start/end, recurrence, location, description, calendarId, attendees). ruby-core **MUST** own, locally and **never** written to Google, the event subjects, childcare assignments, people/provider registries, reminder policy, and all derived state.

2. **Auth.** Access **MUST** use OAuth user-consent with an offline refresh token (scope `https://www.googleapis.com/auth/calendar.events`), stored in Vault at `secret/ruby-core/google` (`client_id`, `client_secret`, `refresh_token`, `calendar_id`). The OAuth app **MUST** be in **production** publishing status. The Family calendar **MUST** be addressed by `calendarId`.

3. **Single writer, single poller.** Exactly one component — the calendar processor in the engine — **MUST** write to Google and poll Google (ADR-0023). The poller **MUST** run only where enabled (`CALENDAR_SYNC_ENABLED`, off by default outside production) so the single shared calendar is never double-synced across environments.

4. **Incremental sync.** The poller **MUST** use Google's sync-token incremental polling (~60s), persisting `nextSyncToken` in a `SYNC_STATE` row keyed by `calendar_id`. A **410** on sync **MUST** trigger a full resync with a fresh token (clear the stored token, page through, record the new token) and **MUST** be logged — never a silent stop. The poller **MUST** be structured so a future Google `watch`/push can replace the timer by triggering the same incremental sync.

5. **Write-through + echo reconciliation.** A local write **MUST** call Google, take the returned id/etag, and upsert the mirror in the same operation. Creates **MUST** be idempotent **at Google**: the Google event id **MUST** be derived deterministically from the payload's `idempotency_key` (falling back to the CloudEvent id), so a redelivered create hits the same id and Google returns **409**; the **409** **MUST** be handled by fetching the existing event and converging the mirror, never by a second insert. (An application-layer dedup store cannot close the at-least-once redelivery/`AckWait`-expiry window — ADR-0025 — so correctness lives at the sink, not the KV.) Updates **MUST** send the stored `etag` as `If-Match`; a Google **412** **MUST** be handled by resyncing and retrying, not by clobbering. The poller **MUST** reconcile re-observed self-writes by `etag` so a write does not trigger reprocessing (no ping-pong).

6. **Mirror shape.** The `CALENDAR_EVENT` table **MUST** reproduce Google's native shape: `google_event_id` as primary key (single-calendar boundary); start/end/original-start each a **date XOR datetime+timezone** trio with `all_day` distinguishing; all-day `end` stored and surfaced as **exclusive**; `recurrence` as a `text[]` of RRULE/EXDATE/RDATE lines; `recurring_event_id` and the `original_start_*` trio on override/cancelled children; `status` as `text` + CHECK (`confirmed|tentative|cancelled`); `etag`, `sequence`, full `raw` jsonb. Derived `start_utc`/`end_utc` columns **MUST** exist for range queries and sorting only — internal, one-directional, never an output transform. Enum-like columns **MUST** be `text` + CHECK constraints, not Postgres native enums.

7. **Recurrence.** A modified or cancelled single occurrence **MUST** be stored as a separate event carrying `recurring_event_id` + the `original_start_*` trio. Cancelled children **MUST** be retained (`status = cancelled`) and subtracted during expansion like implicit EXDATEs. Expansion **MUST** be on-demand and **timezone-aware** (a weekly 9am slot stays 9am local across DST), with a **max-window guard** so a single request cannot expand an unbounded range.

8. **Delete & cascade.** MVP delete **MUST** be series-level (per-instance delete deferred). Delete **MUST** be idempotent via **ensure-absent** semantics: the local mirror is the source of truth for "already deleted" — if the mirror row is absent or a cancelled tombstone, the delete already completed and **MUST** be skipped before any Google call (so the redelivery never reprocesses). A Google **410/404** **MUST** be treated as the satisfied "event absent" postcondition (finish the mirror cleanup), not as a failure — but only as a crash-window backstop beneath the mirror check, never as the primary mechanism. A true event delete/cancel **MUST** cascade the overlay rows; cancelled events **MUST** otherwise be retained and filtered from reads and from suggestion counting (no destructive cascade on cancel).

9. **Reminders.** ruby-core **MUST** own reminder policy outright and **MUST NOT** read or honor Google's per-event reminder overrides (no `reminders` mirror column). The engine **MUST** compute reminders from event start times over the mirror and surface them via `sensor.ruby_home_calendar_status` (next event + active-reminder flag, always-on) and a NATS `calendar.reminder.due`.

10. **Persistence reuse.** The calendar store **MUST** follow the established Postgres pattern (sqlc + golang-migrate, shared foundation Postgres, a dedicated `schema_migrations_calendar` tracking table; ADR-0029/0037). Migrations **MUST** be owned and run by the engine; `services/api` reads only (ADR-0040).

## Consequences

### Positive

* Google stays the system the family already uses; ruby-core adds durability, local overlay, reminders, and automation without changing their workflow.
* The mirror is forward-compatible: native-shape columns plus full `raw` jsonb mean unmodeled Google fields are retained and queryable later.
* At-least-once delivery and concurrent human edits are both handled explicitly (idempotency key; etag `If-Match`/412; etag echo reconciliation) rather than hoped away.
* DST-correct, on-demand expansion avoids a stored-instance explosion and avoids the drift that pre-expansion suffers when a rule changes.
* Reusing the Ada persistence and engine-processor patterns keeps the new code idiomatic and the operational surface familiar.
* The polling design is a clean seam: swapping in Google push later changes the trigger, not the sync logic.

### Negative

* The OAuth-in-production requirement is a real operational footgun: a token that lapses (app demoted to testing, consent revoked) silently stops sync until re-consented — it MUST be alerted on, and re-consent is a manual runbook step.
* Bidirectional sync with etag reconciliation is genuinely subtle; an incorrect echo/etag comparison can loop or clobber, so it demands strong unit coverage and is the highest-risk part of the build.
* New durable state (`CALENDAR_EVENT`, `SYNC_STATE`, overlay) is a production-readiness obligation: backup, restore, and a force-resync runbook are required before it is considered production-ready.
* Single-calendar and series-level-only edits are real functional limits the household will feel (no per-instance "just this Tuesday" edit yet).
* ruby-core owning reminders means a reminder set in the Google app will not fire — a deliberate behavior change that must be communicated to the family.

### Neutral

* Establishes the engine as the single calendar writer/poller and `services/api` as the read surface — a fixed division of responsibility.
* `google_event_id` as PK and overlay FKs bake in the single-calendar assumption; going multi-calendar later is a known, scoped migration (add the calendar dimension to the PK and every overlay FK).
* The full `raw` jsonb column trades storage for forward compatibility.

---

## Amendments

### 2026-06-28 — Sink-level idempotency for create + delete (PLAN-0034)

Live validation showed the original Decision §5/§8 wording — "creates MUST dedupe on an
`idempotency_key`" and a blind series-level Google delete — was insufficient under
at-least-once delivery. A redelivered create double-inserted into Google (the dedup KV
mark timed out during a slow `Insert` and an `AckWait` expiry let a second worker run);
a redelivered delete hit a Google 410 and DLQ'd.

§5 and §8 above are amended to move idempotency to the sink, per ADR-0025's strengthened
requirement that write-through side-effects be idempotent themselves:

* **Create** uses a deterministic, client-assigned Google event id derived from the
  `idempotency_key` (fallback CloudEvent id); a duplicate insert returns 409 → Get +
  converge the mirror.
* **Delete** checks the local mirror first (absent / cancelled → already applied → skip
  Google); a 410/404 is a crash-window backstop, not the primary guarantee.

Implementation: `services/engine/processors/calendar/{writethrough.go,gcal/client.go,
mapping.go}`. The `calendar_idempotency` KV bucket is retained for reminder-firing dedup
(`reminders.go`); only the create path stopped using it.

Caveat: Google retains the ids of deleted events, so reusing an `idempotency_key` after a
delete yields a 409 for a genuinely-new create. This is acceptable provided
`idempotency_key` is unique per logical create — the producer contract.

### 2026-06-29 — Producer key contract, multi-email reconciliation, relationship vocabulary (PLAN-0035)

* **Gateway `idempotency_key` contract (#138).** §5's idempotency holds only if the
  `idempotency_key` (or the CloudEvent id fallback) is **stable across re-publishes** of the
  same logical create. The gateway previously stamped a *random* CloudEvent id per publish and
  set no key, so an HA reconnect replay / double-fire derived a different Google id and
  double-inserted. The producer contract is now: the HA producer **SHOULD** supply a
  unique-per-action `idempotency_key`; when absent, the gateway derives one from the stable
  content fields (`summary|start|end|recurrence|logged_by`) so re-publishes converge
  (`services/gateway/rubyhome/publish.go`). Two genuinely-identical creates collapse to one —
  an accepted, usually-desirable dedup of an accidental double-submit.
* **Multi-email attendee reconciliation (#133).** Attendees reconcile to directory people by
  email; a `person_email` side table now holds alias / secondary addresses so reconciliation
  matches any of a person's emails (active people only; a primary wins on collision), not just
  `directory_person.email`.
* **Childcare relationship vocabulary (#134).** `childcare_provider.relationship` is now
  `text + CHECK` over a starter vocabulary (`grandparent, sibling, aunt_uncle, nanny, daycare,
  babysitter, friend, neighbour, other`; NULL allowed) per the overlay enum-as-CHECK
  convention — extend with another `ALTER` migration as the set grows.
