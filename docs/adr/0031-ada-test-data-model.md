# ADR-0031 - Ada test-data marking model

* **Status:** Proposed
* **Date:** 2026-06-18
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

Ada has no way to distinguish seeded/test rows from real rows. The current `make db-seed`
(`scripts/seed-db.sh` + `scripts/seed-data.sql`) tags rows with `logged_by='seed'` and the
clear path (`scripts/clear-seed-db.sh`) deletes `WHERE logged_by = 'seed'`. This overloads the
attribution field: it destroys the real caregiver name on test rows, cannot represent test
data that also carries a real name, and directly collides with the #80/§4 fix that requires
`logged_by` to be preserved end-to-end through the growth-history projection.

We need to validate the Ada experience end-to-end — including against the **production**
dashboard before the real birth — without risking real records. That requires test data that
behaves **identically** to real data in every sensor and projection (so the dashboard renders
it normally for validation) yet is **selectable** for bulk removal. Additionally, with
live HA ingest gated to prod only (ADR-0033), staging and dev are exercised purely on seeded
data, so the marker must coexist with, not overwrite, real attribution.

## Alternatives Considered

**`logged_by='seed'` sentinel (status quo)** — Overloads the attribution field, cannot carry a
real caregiver name on test data, and conflicts with the #80 requirement to preserve `logged_by`.

**Separate `*_test` tables** — Doubles the schema and forces every projection query to union real
and test tables; defeats the "behaves identically" requirement and multiplies maintenance.

**Out-of-band tagging (an `ada_config` key or external manifest of test row ids)** — Lives apart
from the data it describes, drifts out of sync, and is racy under concurrent writes.

**Dedicated `test BOOLEAN` column per table** — Chosen. Minimal, in-band, queryable, and lets
projections ignore it entirely while the clear target selects on it.

## Decision

1. Every Ada event/record table — `feedings`, `diapers`, `sleep_sessions`,
   `tummy_time_sessions`, `growth_measurements`, and any future Ada table — MUST carry a
   `test BOOLEAN NOT NULL DEFAULT false` column.
2. Ingestion MUST persist a `test` flag carried on the event payload (`{..., "test": true}`)
   on every insert path. Absence of the field MUST be treated as `false`.
3. Every projection, aggregation, and sensor push MUST treat `test=true` rows **identically**
   to real rows. No projection MUST filter on `test`. Test data is invisible *as test* to the
   dashboard.
4. Only the clear target (ROADMAP-0010.6) MAY select on `test=true`. It MUST NOT delete any
   `test=false` row, and `WHERE test = true` MUST be present in every delete statement it runs.
5. The consumer (homeassistant dashboard) MUST stamp `test: true` on every fired event whenever
   `input_boolean.ada_live_test` is on, via its central `fire()` helper.
6. The `logged_by='seed'` convention is retired. Pre-existing rows created before this column
   exists are `test=false` by default and are therefore NOT auto-clearable; they are handled by
   the one-time guarded purge in ROADMAP-0010.6.

## Consequences

### Positive

* Clean, repeatable teardown between validation runs and before go-live: clear removes only
  `test=true` data and leaves real records untouched.
* Real attribution (`logged_by`) is preserved on test rows, so the dashboard renders the actual
  caregiver name during validation.
* Test data renders identically to real data, so validation exercises the real projection code.

### Negative

* Requires a schema migration touching every Ada table.
* Requires a coordinated consumer change (event stamping) before test data can be marked from
  the dashboard.

### Neutral

* Establishes a `test` flag as a permanent part of the Ada data model, carried through every
  future Ada table and insert path.
* Pre-existing junk predating the column must be purged explicitly rather than by the standard
  clear path.
