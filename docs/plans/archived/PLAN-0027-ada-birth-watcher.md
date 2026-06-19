# PLAN-0027 - Ada birth-watcher: snapshot-then-nuke at birth (ADR-0036)

* **Status:** Complete
* **Date:** 2026-06-19
* **Project:** ruby-core
* **Roadmap Item:** none (go-live readiness)
* **Branch:** feat/ada-birth-watcher
* **Related ADRs:** ADR-0036, ADR-0035

---

## Scope

Move the birth clean-slate out of the engine and into a host watcher that, on the first `ada.born`,
runs the existing `pg_dump` snapshot then the nuke, validates it, and spins itself down. Out of
scope: a general scheduled-backup posture (ROADMAP-0011).

## Pre-conditions

* [x] On `feat/ada-birth-watcher` (current `origin/main`, v0.19.0).
* [x] Pre-birth data is all `test=true` in prod (backfill + forcing verified live).
* [x] `ada-db-snapshot.sh` / `ada-db-clear-test.sh` exist (snapshot → clear → restart).

## Steps

### Step 1 — Engine: stop wiping

**Action:** In `handleBornEvent`, keep the first-birth gate + `UpsertProfile` + `born.Store(true)`,
remove `clearTracking` and the `DeleteAll*` queries (regenerate sqlc). The engine just records the
birth; pre-birth `test=true` forcing (ADR-0035) stays.
**Verification:** `go build ./...`; `go test -tags=fast ./...`; the born handler no longer deletes.

### Step 2 — Non-interactive clear

**Action:** Add `ASSUME_YES=1` to `ada-db-clear-test.sh` to skip the typed prod confirmation (for
the watcher). Interactive prompt stays the default.
**Verification:** `shellcheck`; `ASSUME_YES=1 CONFIRM=yes ENV=dev` path does not block on `read`.

### Step 3 — Birth watcher

**Action:** `scripts/ada-birth-watch.sh` — durable JetStream consumer of `ha.events.ada.born` (via
`nats-admin.sh`). On first birth (sentinel absent): run `ada-db-clear-test.sh ENV=prod` non-
interactively (snapshot → clear → restart), then **validate** (snapshot file present + non-empty;
`SELECT count(*) WHERE test=true = 0` for every tracking table; engine container running). On
success: write the sentinel, log, exit 0 (spin down). On failure: retry (idempotent); never write
the sentinel. On startup with the sentinel present: exit 0 immediately.
**Verification:** `shellcheck`; `bash -n`; dry-run of the validate function against a scratch DB.

### Step 4 — systemd unit + docs

**Action:** `deploy/prod/ada-birth-watcher.service` (`Restart=on-failure`, runs the watcher as the
host user). Document install (`systemctl enable --now`) and the sentinel in
`docs/runbooks/ada-test-data.md` as a go-live prerequisite.
**Verification:** unit parses (`systemd-analyze verify` if available); runbook updated.

### Step 5 — Build, lint, tests

**Action:** `go build ./...`; `go test -tags=fast ./...`; golangci-lint; shellcheck.
**Verification:** all green.

## Rollback

Revert the commit. Note: reverting restores the engine-side wipe (ADR-0035) only if that code is
also restored — this PR removes it, so until the watcher is installed there is no birth clean-slate.
The watcher's actions (snapshot, delete) reuse the existing irreversible operations; the snapshot is
the recovery path.

## Open Questions

None — approach approved (host watcher; snapshot-then-nuke; validate; spin down).
