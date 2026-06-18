# PLAN-0023 - Ada test-data lifecycle: seed + clear (DESTRUCTIVE — gated)

* **Status:** Complete
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0010-ada-hardening-test-data-lifecycle.md (effort 0010.6)
* **Branch:** feat/ada-write-and-test-lifecycle
* **Related ADRs:** ADR-0031

---

## Scope

Env-parameterized `make` targets to snapshot, seed, and clear Ada test data, built on the `test`
column (0010.5). Seed writes a representative, fully `test`-flagged ~14-month dataset and aligns
the HA test-mode helpers; clear removes only test data behind hard guards. **This effort contains
the only destructive operations in ROADMAP-0010.** Out of scope: automated/scheduled Postgres
backups (ROADMAP-0011) — only an on-demand pre-destructive snapshot is provided here.

## Pre-conditions

* [x] On `feat/ada-write-and-test-lifecycle`; efforts 0010.4–0010.5 committed.
* [x] `test` column live (0010.5). Per-env Vault PG paths: prod `secret/ruby-core/postgres`,
      staging `secret/ruby-core/staging/postgres`, dev `secret/ruby-core/dev/postgres`.

## Steps

### Step 1 — Shared lib

**Action:** `scripts/ada-db-lib.sh` resolves `ENV ∈ {dev,staging,prod}` to its Vault PG path,
fetches PG creds (read token from `deploy/prod/.env`), and exposes `run_psql` / `run_pg_dump` that
exec a one-off `postgres:16-alpine` on the `postgres` network. Refuses unknown ENV.
**Verification:** `bash -n` parses; `shellcheck` clean.

### Step 2 — Snapshot (pre-destructive backup)

**Action:** `scripts/ada-db-snapshot.sh` → `pg_dump` of the Ada tables to
`${ADA_SNAPSHOT_DIR:-$HOME/ada-snapshots}/ada-<env>-<utc-ts>.sql`. Prints the path + restore hint.
**Verification:** produces a non-empty `.sql`; restore command documented in the runbook.

### Step 3 — Seed

**Action:** `scripts/seed-ada-test-data.sql` (parameterized by `:dob`) clears prior seed rows
(`logged_by='seed'`) then inserts a `test=true`, `logged_by='seed'` dataset spanning ~14 months:
feeds (breast L/R, bottle breast-milk/formula, mixed) every 3h with segments/bottle detail,
diapers (wet/dirty/mixed), sleeps (naps + nights), tummy sessions, and an 8-point WHO-channel
growth series per metric. `scripts/ada-db-seed.sh ENV=<env> DOB=<rfc3339>` runs it, then sets the
HA test helpers (`input_datetime.ada_test_dob=DOB`, `input_boolean.ada_live_test=on`,
`input_boolean.ada_born=off`) via HA REST, restarts the env's engine so it projects the seed, and
asserts the expected row counts (reliability check, §4).
**Verification:** on a scratch DB, migrate-up then seed → per-type counts > 0 and growth spans
~14 months; re-running yields identical counts (idempotent clear-then-seed).

### Step 4 — Clear (DESTRUCTIVE, guarded)

**Action:** `scripts/ada-db-clear-test.sh ENV=<env>` deletes only `test=true` rows across all five
tables (children cascade). Guards: (a) **dry-run/count-first by default** — prints per-table test
counts and deletes nothing without `CONFIRM=yes`; (b) **prod requires an extra typed confirmation**
echoing host/db; (c) **snapshot runs first** (Step 2); (d) every statement carries `WHERE test =
true`. Restarts the engine after deletion so sensors recompute.
**Verification:** seed then clear with no CONFIRM → deletes nothing, prints counts; `CONFIRM=yes`
→ only test rows gone, a manually-inserted `test=false` row survives, snapshot file present.

### Step 5 — Makefile + runbook; retire old targets

**Action:** Add `ada-db-snapshot` / `ada-db-seed` / `ada-db-clear-test` (require `ENV=`); the clear
target documents `CONFIRM=yes`. Remove the old unguarded `db-seed` / `db-seed-clear`
(`scripts/seed-db.sh`, `clear-seed-db.sh`, `seed-data.sql`) which targeted prod via `logged_by`.
Add `docs/runbooks/ada-test-data.md` (snapshot/seed/clear/restore/junk-purge).
**Verification:** `make help` lists the new targets; old targets gone; shellcheck clean.

## Rollback

The clear and the one-time junk purge are **irreversible except via the Step-2 snapshot** — this
is a stateful, no-clean-code-rollback phase, which is why snapshot-first is mandatory and the clear
is gated. The seed itself is reversible (`ada-db-clear-test`, or its internal `logged_by='seed'`
delete). Reverting the commit removes the tooling but not already-seeded rows (clear them first).

## Open Questions

None — ready for execution.
