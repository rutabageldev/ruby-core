# ADR-0036 - Ada birth clean slate: host watcher snapshots then nukes

* **Status:** Accepted
* **Date:** 2026-06-19
* **Supersedes:** *(the engine-side birth wipe in ADR-0035, decision 2)*
* **Superseded by:** *(none)*

---

## Context

ADR-0035 made the engine wipe pre-birth tracking data on the first `ada.born`. That gives a clean
slate but with **no real backup** — the engine is a locked-down, read-only, distroless container
that cannot run `pg_dump`, so the wipe is irreversible with only an in-DB copy at best.

The requirement for go-live is firm: when the birth notification arrives, a **real `pg_dump` backup
must be taken before the database is nuked**, fully automatically, with **zero action at the birth
moment** (a parent receiving a newborn cannot run a launch checklist).

The backup capability already exists — `make ada-db-snapshot` (`scripts/ada-db-snapshot.sh`) runs
`pg_dump` against the Ada tables from a throwaway `postgres:16-alpine` container, and
`scripts/ada-db-clear-test.sh` already does **snapshot → delete → restart engine** in that order,
aborting the delete if the snapshot fails. The only missing piece is an automated, host-side trigger
for it, because `pg_dump` needs Docker/host access the engine deliberately lacks.

## Alternatives Considered

**Keep the wipe in the engine + an in-DB archive** — No real off-box backup; the engine can't
`pg_dump`; archive tables clutter the schema. Rejected (ADR-0035's gap is exactly this).

**Engine ↔ backup-worker request/reply over NATS** — Engine requests a snapshot from a sidecar,
waits, then wipes. Robust but introduces a new long-running service, a custom image, and a blocking
protocol on the critical birth path. Heavier than needed given the snapshot+nuke script already
exists.

**Host watcher that runs the existing snapshot-then-nuke script (chosen).** A small host service
catches the birth and runs `ada-db-clear-test.sh` (snapshot → clear → restart). The engine stops
wiping. Reuses all existing tooling; the only new artifact is the trigger.

## Decision

1. The engine's `handleBornEvent` MUST NOT wipe. It records the birth (`UpsertProfile`) and sets the
   in-memory `born` flag (which still forces `test=true` on pre-birth events per ADR-0035), then
   returns. A re-fired `ada.born` remains a no-op.
2. A host service `ada-birth-watcher` MUST, on the first `ada.born`, run
   `scripts/ada-db-clear-test.sh ENV=prod` non-interactively, which **snapshots (`pg_dump`) first**,
   then deletes the pre-birth (`test=true`) tracking rows, then restarts the engine. Because the
   pre-birth backfill + pre-birth forcing (ADR-0035) make all pre-birth data `test=true`, this clears
   the whole pre-birth slate while sparing any post-birth (`test=false`) real event.
3. The watcher MUST **validate** the result before declaring success: the snapshot file exists and is
   non-empty, every tracking table has zero `test=true` rows, and the engine container is running.
   On validation failure it retries (the operation is idempotent) and never marks the birth handled.
4. After a validated success the watcher MUST **spin down and not recur**: it writes a persistent
   sentinel and exits cleanly; on any restart it sees the sentinel and exits immediately. So no later
   `ada.born` — accidental or malicious — can ever trigger a second nuke.
5. `ada-db-clear-test.sh` MUST support a non-interactive mode (skip the typed prod confirmation) for
   the watcher; the interactive prompt stays the default for human operators.

## Consequences

### Positive

* A real `pg_dump` backup is taken before every birth nuke, automatically, with zero birth-time
  action — exactly the requirement.
* Reuses existing, already-tested tooling (`ada-db-snapshot.sh`, `ada-db-clear-test.sh`); the new
  surface is one watcher script + a systemd unit.
* Fails safe: the snapshot precedes the delete and aborts it on failure; validation gates success;
  the sentinel prevents any recurrence.

### Negative

* Adds a host service that must be installed once (`systemctl enable --now ada-birth-watcher`) — a
  one-time infrastructure step, the same pattern as the GitHub self-hosted runner already on the
  host. If it is never installed, no birth clean-slate happens (the engine no longer wipes), so the
  install is a documented go-live prerequisite.
* The clean slate now lands a moment after the birth (the watcher's processing + engine restart)
  rather than synchronously in the engine.

### Neutral

* Moves the birth teardown from the engine into host operations, alongside the snapshot it depends
  on. The engine retains only what it must own: recording the birth and the pre-birth test forcing.
