# PLAN-0035 - Calendar Issue Cleanup

* **Status:** Complete
* **Date:** 2026-06-29
* **Project:** ruby-core
* **Roadmap Item:** [ROADMAP-0012](../roadmap/ROADMAP-0012-home-calendar.md)
* **Branch:** fix/calendar-overlay-and-idempotency
* **Related ADRs:** [ADR-0042](../adr/0042-calendar-sync-architecture.md) (amended)

---

## Scope

Close out the open calendar-implementation issues in a single PR: constrain
`childcare_provider.relationship` to a vocabulary (#134), reconcile attendees against multiple
emails per person (#133), document the OpenAPI omit-vs-null convention (#126), close the
gateway re-publish double-insert gap by deriving a stable `idempotency_key` (#138), and add a
Postgres testcontainers integration harness for the stateful processors (#127). Out of scope
(both stay open): issue #122 (Traefik→api mTLS — blocked on a foundation PKI AppRole) and
issue #137 (idempotency metrics — blocked on the engine `/metrics` gap, #31). Issue #123 is
closed separately (prod provisioned). The role-gotcha runbook ships as its own PR.

---

## Pre-conditions

* [x] ROADMAP-0012 merged + deployed (v0.26.1); overlay (Slice D) live.
* [x] Next calendar migration number is `000003` (after `000001_calendar_core`,
      `000002_household_overlay`).
* [x] `make test-integration` + the CI `integration-tests` job already exist (Docker on
      `ubuntu-latest`); the NATS testcontainers pattern at
      `pkg/natsx/consumer_integration_test.go` is the template.
* [ ] Existing prod `childcare_provider.relationship` values fit the vocabulary (else the
      Step-2 normalization `UPDATE` rewrites them to `'other'`) — confirm at apply time.

---

## Steps

### Step 1 — Commit this plan

**Action:** Author this file + index in `docs/plans/README.md`.
**Verification:** pre-commit passes; committed on the branch.

### Step 2 — Migration `000003_overlay_refinements` (#134 + #133)

**Action:** `pkg/calendar/store/migrations/000003_overlay_refinements.{up,down}.sql`.

* #134: normalize then constrain `childcare_provider.relationship` to
  `('grandparent','sibling','aunt_uncle','nanny','daycare','babysitter','friend','neighbour','other')`
  (NULL allowed). Down: drop the constraint.
* #133: `person_email` side table (`person_id` FK → `directory_person` ON DELETE CASCADE,
  `email`, unique `lower(email)` index). Down: drop the table.

**Verification:** applies via `calendarstore.MigrateUp` on the harness (Step 6); bad
`relationship` and duplicate `lower(email)` are rejected.

### Step 3 — sqlc queries + regen (#133)

**Action:** Add `UpsertPersonEmail` + `ListAllPersonEmails` in
`pkg/calendar/store/queries/overlay.sql`; regenerate sqlc. Confirm the `directory_person`
write/seed path and mirror it for alias emails (or document seeding).
**Verification:** sqlc diff clean; `go build ./...`.

### Step 4 — Read-API multi-email merge (#133)

**Action:** `services/api/handlers/calendar.go` `loadEmailIndex` merges
`directory_person.email` + `ListAllPersonEmails` (primary wins on collision).
**Verification:** integration test — an alias email resolves to the right `person_id`.

### Step 5 — Gateway content-hash `idempotency_key` (#138)

**Action:** `services/gateway/rubyhome/publish.go` injects
`sha256(summary|start|end|recurrence|logged_by)` as `idempotency_key` on
`calendar.event.upsert` when absent; pass-through when present. Unit test.
**Verification:** `go test -tags=fast ./services/gateway/...`.

### Step 6 — Postgres testcontainers harness (#127)

**Action:** Add `testcontainers-go/modules/postgres`; integration test
(`//go:build integration`) running `MigrateUp` and asserting date-XOR CHECKs, range queries,
overlay FK cascade, the new relationship CHECK, and `person_email` uniqueness + alias matching.
**Verification:** `make test-integration` passes.

### Step 7 — Docs: style-guide (#126) + ADR-0042 amendment

**Action:** `docs/api/style-guide.md` omit-vs-null convention; ADR-0042 amendment (gateway
`idempotency_key` contract, multi-email reconciliation, relationship vocabulary); README notes.
**Verification:** `make openapi-lint` + markdownlint pass.

### Step 8 — Pre-Push Checklist + archive this plan

**Action:** Review docs; archive to `docs/plans/archived/` (status Complete); full
`go test -tags=fast ./...` + `golangci-lint run ./...`.
**Verification:** pre-commit clean; lint 0 issues; fast suite green.

---

## Rollback

Revert the merge. Only persistent change is migration `000003`; its down drops the constraint
plus the `person_email` table (ships empty → no data loss). The Step-2 relationship
normalization `UPDATE` is **not** auto-reversible — capture
`SELECT id, relationship FROM childcare_provider` before apply if the table is non-empty.
Gateway/api/test changes are code-revert.

---

## Open Questions

None blocking. Apply-time confirmations: existing prod `relationship` values (Step 2); the
`directory_person` write/seed path (Step 3).

---

## Completion Notes (2026-06-29)

Implemented on `fix/calendar-overlay-and-idempotency`; `go test -tags=fast ./...` green,
`golangci-lint` 0 issues, Spectral lint green (#126), sqlc no drift, and the new
`make test-integration` harness passes against real Postgres. Commits:

* `25fc6d4` — migration 000003 (relationship CHECK + `person_email`) + sqlc queries +
  `buildEmailIndex` multi-email merge + fast test (#134, #133).
* `b901c6e` — gateway derives a stable `idempotency_key` for calendar upserts (#138).
* `a59d43e` — Postgres testcontainers integration harness (#127).
* `a3c2bfe` — style-guide omit-vs-null (#126) + ADR-0042 amendment + gateway README.

Deviations: the API-layer attendee reconciliation is exercised via the pure `buildEmailIndex`
fast test + the store-layer integration assertions (no separate `services/api` integration
harness this round). `directory_person`/`person_email` remain seed/manual data — no runtime
write event (the calendar processor's `calStore` doesn't wire person writes); `UpsertPersonEmail`
exists for seeding + the integration test. The relationship Step-2 normalization is a no-op in
prod if the table is empty — confirm before the migration runs.
