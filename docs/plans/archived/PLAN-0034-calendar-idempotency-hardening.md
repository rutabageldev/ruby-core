# PLAN-0034 - Calendar Idempotency Hardening

* **Status:** Complete
* **Date:** 2026-06-28
* **Project:** ruby-core
* **Roadmap Item:** [docs/roadmap/ROADMAP-0012-home-calendar.md](../roadmap/ROADMAP-0012-home-calendar.md)
* **Branch:** fix/calendar-idempotency-hardening
* **Related ADRs:** [ADR-0025](../adr/0025-idempotency-tracking-store.md), [ADR-0042](../adr/0042-calendar-sync-architecture.md)

---

## Scope

Harden the calendar write path so redeliveries cannot produce duplicate Google events or
DLQ a delete. In-scope: make the Google `Insert` side-effect idempotent via a deterministic
client-assigned event id (+ 409 convergence); make `handleDelete` idempotent via a local
mirror-presence check (+ a 410/404 backstop for the crash window); shrink the shared
`idempotency` KV TTL from 24h to 30m to stop the bloat that caused KV timeouts; add
mark-failure + TTL-mismatch observability; amend ADR-0025/0042 and the calendar README; add
a KV purge/recreate runbook. Out of scope: a new roadmap item (this is hardening under
ROADMAP-0012), per-instance (non-series) delete, claim-before-process consumer dedup,
subject-aware idempotency filtering, and the cross-repo gateway `idempotency_key` audit
(noted as a follow-up).

---

## Pre-conditions

* [x] ROADMAP-0012 slices A–D merged + deployed (v0.26.0); the write path is live.
* [x] Root cause confirmed: bloated shared `idempotency` KV (~97k entries, 24h TTL) →
      `Insert`/KV latency → AckWait expiry → concurrent redelivery → second non-idempotent
      Google `Insert`. Delete had no idempotency guard → 410 → NAK → DLQ.
* [x] `google.golang.org/api/calendar/v3` supports a client-set `Event.Id` on Insert; a
      duplicate id returns HTTP 409.
* [x] `calStore` already exposes `GetEvent` and `DeleteEvent` (mirror check is implementable
      with no new query).

---

## Steps

### Step 1 — Deterministic create id + 409 handling

**Action:** In `gcal/client.go` add `ErrDuplicate` and map `googleapi.Error{Code:409}` from
`Insert` to it (mirror the existing 412→`ErrConflict` in `Update`). Add a
`deterministicEventID(seed string)` helper (sha256 → base32hex, lowercased, truncated to 32
chars — Google charset `a-v`+`0-9`, length 5–1024). Thread `evt.ID` into `create()`; pick
the seed = `IdempotencyKey` else `evt.ID`; set `gev.Id` before `Insert`. On `ErrDuplicate`,
`Get` the existing event and return it so `handleUpsert` upserts the mirror + reconciles
associations. Remove the now-redundant `idStore.Seen`/`Mark` from `create()`. Empty-seed →
leave `gev.Id` unset + WARN.

**Verification:** `go test -tags=fast ./services/engine/processors/calendar/...` — new tests
assert: same `idempotency_key` → identical id (charset/length); redelivered create →
`inserts==1`, `gets==1`, mirror upserted once; empty-seed → Google-assigned path + no panic.

### Step 2 — Idempotent delete (mirror-check + backstop)

**Action:** In `gcal/client.go` add `ErrAlreadyGone` and map `googleapi.Error{Code:410|404}`
from `Delete` to it. In `handleDelete()`, `GetEvent` first: `pgx.ErrNoRows` or
`status=="cancelled"` → log "delete already applied, skipping" + return nil (no Google call).
If the row is present, call `gcal.Delete`; on `ErrAlreadyGone` fall through to `DeleteEvent`
to finish mirror cleanup + return nil; other errors → return (NAK).

**Verification:** new tests assert delete skips Google when mirror absent/cancelled
(`deletes==0`), and the 410 backstop path returns nil + calls `DeleteEvent` when the row is
present but Google is missing.

### Step 3 — Shrink idempotency TTL + dedup hardening

**Action:** `DefaultIdempotencyTTL` 24h → 30m with a doc comment recording the
redelivery-window rationale (`MaxDeliver*AckWait + Σ backoff ≈ 165s`). In
`pkg/idempotency/store.go`, keep mark-after-success but stop silently swallowing the KV
`Mark` failure: add a counter/metric alongside the existing WARN. Wire a startup read of
`kv.Status()` that logs the configured TTL and WARNs if it != `DefaultIdempotencyTTL`.

**Verification:** unit assertion `DefaultIdempotencyTTL <= 30m` and
`> MaxDeliver*AckWait + Σ backoff`. `go build ./...` clean; startup log shows the TTL.

### Step 4 — Docs (ADR-0025, ADR-0042, README, runbook)

**Action:** Amend ADR-0025 (TTL sizing rationale; "sinks MUST be idempotent" for
write-through; note the `MaxBytes`-on-`discard=new` trap). Amend ADR-0042 §5 (create via
deterministic id + 409→Get convergence) and §8 (delete = ensure-absent via mirror check;
410/404 is the satisfied postcondition for the crash window). Update the calendar README
write-consumer bullets. Add a runbook section for the KV purge/recreate.

**Verification:** pre-commit passes; ADR status/links consistent; README matches code.

### Step 5 — Operational apply (live, off-peak — user-run)

**Action:** After deploy of the new engine: `nats kv del idempotency` (admin), let startup
recreate it at 30m. Live-verify create (one event), redelivered create (still one, 409
converge in logs), delete + replay (no DLQ, "skipping" log, read returns 0).

**Verification:** startup log shows 30m TTL + low key count; live test events behave as
above; bucket stays small over 24h.

---

## Rollback

Code is revert-and-redeploy: revert the merge commit and redeploy the prior engine image;
no schema migration is involved. The KV TTL change is applied operationally (bucket
recreate) — to roll back, delete + recreate the bucket at the old TTL (or let the reverted
binary's `DefaultIdempotencyTTL` recreate it). Deterministic event ids are forward-only:
events created with a deterministic id remain valid Google events; reverting the binary
simply returns to Google-assigned ids for new creates. No data rollback required.

---

## Open Questions

* Does the gateway populate `idempotency_key` on every calendar upsert? If not, the
  `CloudEvent.id` fallback is load-bearing — verify it is stable across redeliveries.
  (Follow-up; does not block this plan since the fallback covers it.)

---

## Completion Notes (2026-06-28)

Steps 1–4 are implemented, unit-tested (`go test -tags=fast ./...` green), and linted
clean. Commits on `fix/calendar-idempotency-hardening`:

* `6defb1b` — code: deterministic create id + 409 convergence; ensure-absent delete +
  410/404 backstop; `idempotency` TTL 24h→30m; `CreateOrBindKVBucket` TTL-mismatch WARN;
  tests.
* `e3ae61f` — docs: ADR-0025/0042 amendments, calendar README, idempotency-kv runbook.

**Deviations from the plan as written:**

* The `calendar_idempotency` KV bucket is **not** deprecated — `reminders.go` still uses it
  for reminder-firing dedup. Only the create path stopped using it.
* No mark-failure **metric** was added: there is no Prometheus/OTEL counter infrastructure
  in the repo yet (known OTEL gap). Observability is the existing structured WARN plus the
  new startup TTL-mismatch WARN; a real metric + KV-size alert is deferred to the OTEL
  effort (tracked as drift).

**Step 5 (live operational apply) is post-deploy and user-run** — deploy the engine, purge

* recreate the `idempotency` bucket per `docs/runbooks/idempotency-kv.md`, and run the live
create/redeliver/delete checks.
