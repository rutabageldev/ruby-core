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
  engine projects to it** — non-prod engines run with `HA_INGEST_ENABLED=false` and skip all HA
  pushes (ADR-0033). So seeding/clearing only restarts the **prod** engine; seeding `dev`/`staging`
  writes their DB but does not appear on the dashboard — use `ENV=prod` to validate the dashboard.

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
(naps + nights), tummy sessions, an 8-point WHO-channel growth series per metric, two medications
(a scheduled vitamin + a PRN analgesic with safety limits) with a week of recent doses, and the
five emergency-card rows (three contacts + the two live fields). For
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

## Birth clean slate (automatic, ADR-0035 + ADR-0036)

Pre-birth, the engine **forces `test=true` on every Ada tracking event** (regardless of the
dashboard's `live_test` toggle), and the `000007` migration backfilled all pre-existing rows to
`test=true` — so the entire pre-birth tracking dataset is `test=true`. The exceptions are the
medication **registry + routines** and the **emergency-card rows** (ADR-0037): these are standing
config, written with the event's own `test` flag only (no pre-birth forcing), so real entries made
before birth (a pediatrician contact, a vitamin-D routine) are `test=false` and **survive the birth
clean-slate**, exactly like caretakers. Medication **dose events** are tracking and are wiped.

On the first `ada.born`, the **`ada-birth-watcher`** (a host service) automatically runs the
existing snapshot-then-nuke: **`pg_dump` the database first** (real backup, `make ada-db-snapshot`),
then clear the `test=true` rows, then restart the engine. It **validates** the result (snapshot
file present; zero `test=true` rows remain; engine running) before declaring success, retrying if
not, and then **spins itself down** (a sentinel at `~/.ada-birth-handled`) so no later `ada.born`
can ever trigger a second nuke. **Config is preserved** (caretakers, bedtime/boundary, tummy target,
alert threshold). The engine itself no longer wipes — it only records the birth profile.

**No birth-time action is required.** The only prerequisite is a one-time install of the watcher
(see `deploy/prod/ada-birth-watcher.service`):

```bash
sudo cp /opt/ruby-core/deploy/prod/ada-birth-watcher.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ada-birth-watcher
journalctl -u ada-birth-watcher -f      # watch it
```

Because the engine no longer wipes, **the watcher must be installed before go-live** — without it,
no birth clean-slate happens. The snapshot it takes is the recovery path (the clear is otherwise
irreversible).

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

- `sensor.ada_med_events` carries a growing array (7-day dose history + active watches) in its
  attributes, like the `*_history` sensors. It should be **recorder-excluded on the HA side**
  (HA `configuration.yaml`, not ruby-core) so it does not bloat the recorder database — ruby-core
  only projects it.
- The engine's worker pool processes events concurrently, so a *same-entity* create-then-modify
  within the same sub-second window can no-op silently (`medication.event.update/delete`,
  `series.end` matching zero rows if they beat the insert). Rare; candidate follow-up is
  NAK-on-zero-rows. The create-then-child case (med → routine/dose) is already handled by dropping
  the `medication_id` FK (migration 000010).
- The engine now honors `HA_INGEST_ENABLED`, so non-prod engines no longer push to the shared HA
  (ADR-0033). The tooling restarts only the prod engine because only prod projects to the dashboard.
