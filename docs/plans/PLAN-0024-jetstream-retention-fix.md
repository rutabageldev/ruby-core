# PLAN-0024 - Bound JetStream stream retention + reconcile (JetStream-full incident)

* **Status:** In Progress
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** none (operational reliability fix)
* **Branch:** fix/jetstream-stream-retention
* **Related ADRs:** ADR-0034

---

## Scope

Fix the unbounded `HA_EVENTS` stream that filled the prod JetStream store and starved the
idempotency KV. Bound `HA_EVENTS` by age + bytes, cap every stream, make `ensureStream` reconcile
existing streams, and add a reusable admin helper. Out of scope: automated JetStream backups
(ADR-0021 already covers the bind-mount), the Prometheus storage alert (foundation repo).

## Pre-conditions

* [x] On `fix/jetstream-stream-retention` (from current `origin/main`).
* [x] Immediate mitigation already applied to the live prod stream (see Step 0).

## Steps

### Step 0 — Immediate mitigation (DONE, operational)

**Action:** `ENV=prod scripts/nats-admin.sh stream edit HA_EVENTS --max-age=48h --max-bytes=512MB --force`.
**Verification:** ✅ HA_EVENTS 894 MiB → 118 MiB; account storage 100% → 24%; `insufficient resources`
warnings stopped within ~1 min. The edit persists in JetStream across NATS restarts.

### Step 1 — Bound HA_EVENTS + cap all streams

**Action:** Add `DefaultHAEventsMaxAge` (48h) and per-stream `MaxBytes*` constants to
`pkg/config/consumer_defaults.go`; set `MaxAge`+`MaxBytes` on `HA_EVENTS` and a `MaxBytes` cap on
every other stream in `pkg/natsx/streams.go`.
**Verification:** `go build ./...`; cap sum (880 MiB) < `max_file_store` (1 GiB).

### Step 2 — Reconcile existing streams

**Action:** Change `ensureStream` to `UpdateStream` an existing stream when its mutable limits
(`MaxAge`/`MaxBytes`/`MaxMsgs`) drift from desired (pure `streamLimitsDrifted` helper); never touch
immutable fields.
**Verification:** new `TestStreamLimitsDrifted` passes; `go test -tags=fast ./pkg/natsx/...`.

### Step 3 — Reusable admin helper

**Action:** Add `scripts/nats-admin.sh` — fetch admin NKEY + PKI-issued mTLS from Vault and run the
`nats` CLI (the same auth `scripts/smoke-test.sh` uses), env-parameterized.
**Verification:** `ENV=prod scripts/nats-admin.sh stream report` connects and lists streams.

### Step 4 — ADR + build/lint/test

**Action:** Write ADR-0034. `go build ./...`; `go test -tags=fast ./...`; golangci-lint; shellcheck.
**Verification:** all green.

## Rollback

Code change is a clean revert (no schema/state migration). The live stream's reconciled limits
persist regardless of the code; reverting the code would, on the next deploy, leave the live limits
as last set (the reconcile is idempotent and only acts on drift). The Step-0 purge is irreversible
(old `state_changed` events are gone) but those have no operational value.

## Open Questions

None — ready for execution.
