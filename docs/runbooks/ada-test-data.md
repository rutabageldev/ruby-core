# Runbook — Ada test-data lifecycle (snapshot / seed / clear)

Operational guide for the Ada test-data tooling (ROADMAP-0010.6, ADR-0031). All commands run on
the host (`ruby-z04-node-01`) from the repo root and require Docker access to the `postgres`
network plus a Vault token with read access to `secret/ruby-core/*` (sourced from
`deploy/prod/.env`).

`ENV` selects the target database: `dev` → `ruby_core_dev`, `staging` → `ruby_core_staging`,
`prod` → `ruby_core` (all in foundation-postgres). Every seeded row is `test = true` and
`logged_by = 'seed'`.

## Concepts

- **`test` column (ADR-0031).** Every Ada table carries `test BOOLEAN`. Test data behaves
  identically in every sensor/projection; only the clear target selects on it.
- **One shared Home Assistant.** All environments share `secret/ruby-core/ha`, and **only the prod
  engine should project to it.** The engine does not gate HA pushes by environment, so seeding/
  clearing only restarts the **prod** engine. Seeding `dev`/`staging` writes their DB but does not
  appear on the dashboard — use `ENV=prod` to validate the dashboard.

## Snapshot (pre-destructive backup)

```bash
make ada-db-snapshot ENV=prod
```

Writes `~/ada-snapshots/ada-<env>-<utc>.sql` (override dir with `ADA_SNAPSHOT_DIR`). This is the
only backup the clear relies on; full automated Postgres backups are tracked in ROADMAP-0011.

### Restore from a snapshot (DANGER — overwrites the env's Ada tables)

```bash
ENV=prod   # set creds via the same Vault path the tooling uses
cat ~/ada-snapshots/ada-prod-<utc>.sql | docker run -i --rm --network postgres \
  -e PGPASSWORD="<from vault secret/ruby-core/postgres>" postgres:16-alpine \
  psql -h <host> -p <port> -U <user> -d <dbname>
docker restart ruby-core-prod-engine   # re-project
```

## Seed

```bash
make ada-db-seed ENV=prod DOB=2025-04-01T08:00:00-05:00
```

Clear-then-seed (idempotent): removes prior `logged_by='seed'` rows, then writes a ~14-month
dataset — feeds (breast L/R, bottle breast-milk/formula, mixed), diapers (wet/dirty/mixed), sleeps
(naps + nights), tummy sessions, and an 8-point WHO-channel growth series per metric. For
`ENV=prod` it also sets the HA test helpers (`input_datetime.ada_test_dob = DOB`,
`input_boolean.ada_live_test` on, `input_boolean.ada_born` off) and restarts the prod engine so the
seed projects onto the dashboard. Override the host-reachable HA URL with `ADA_HA_URL` (default
`http://127.0.0.1:8123`).

## Clear (DESTRUCTIVE)

```bash
make ada-db-clear-test ENV=prod              # dry run — prints test counts, deletes nothing
make ada-db-clear-test ENV=prod CONFIRM=yes  # deletes only test=true rows
```

Guards: dry-run unless `CONFIRM=yes`; `ENV=prod` additionally prompts for the database name; a
snapshot is taken before any delete; every statement is `WHERE test = true`, so real data is never
touched. The prod engine is restarted afterward to recompute sensors.

## One-time junk purge (pre-existing non-test rows)

Junk created before the `test` column exists is `test = false`, so the clear target will not remove
it. Purge it explicitly:

- **Preferred:** delete by id from the dashboard (fires `ada.growth.delete` / `ada.<area>.delete`),
  which is honored as of ROADMAP-0010.4.
- **Fallback (after a snapshot):** a one-shot guarded `DELETE ... WHERE id = '<uuid>'` via a psql
  container on the `postgres` network. Take a snapshot first; this is irreversible.

## Reliability check (§4)

After `make ada-db-seed ENV=prod`, confirm projection integrity from the dashboard/HA side: the
growth chart shows the full 8-point series with `logged_by` intact, histories/today totals
populate, and counts are not duplicated (single-stack ingest, ADR-0033).

## Known issues / drift

- The engine ignores `HA_INGEST_ENABLED` (only the gateway honors it), so non-prod engines push to
  the shared HA on startup/refresh. This is why the tooling restarts only the prod engine. Tracked
  separately.
