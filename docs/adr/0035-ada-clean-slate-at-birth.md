# ADR-0035 - Ada clean slate at birth: pre-birth test marking and auto-clear on ada.born

* **Status:** Accepted
* **Date:** 2026-06-18
* **Supersedes:** *(extends ADR-0031 test-data model)*
* **Superseded by:** *(none)*

---

## Context

Before Ada is born, every Ada event in the system is, by definition, test data — the dashboard is
being exercised for validation, not recording a real baby. ADR-0031 added a `test BOOLEAN` marker
and made the engine persist a `test` flag carried on the event payload (the dashboard stamps
`test:true` while `input_boolean.ada_live_test` is on).

In practice this left two gaps that matter for go-live:

1. **Pre-birth events are not reliably flagged.** The flag depends entirely on the dashboard
   stamping it. A prod inspection on 2026-06-18 found 371 of 374 Ada events flagged `test=false`
   (real) — only the most recent few, after stamping was switched on, were `test=true`. Two distinct
   classes of unflagged data exist: events fired without the stamp / with `live_test` off, **and**
   data created out-of-band via the HA API (e.g. a seeded WHO growth channel and verification events)
   that bypassed the dashboard's stamping helper entirely. A `test=true`-only clear would leave all
   of that behind — and the seeded growth series would bleed into Ada's real chart.
2. **`ada.born` does nothing but save the birth profile.** There is no automatic teardown, so the
   accumulated pre-birth data would carry into the real record unless an operator remembers to run
   `make ada-db-clear-test` at exactly the right moment.

The goal: when the real `ada.born` fires, Ada's history starts from a guaranteed clean slate, with
no dependence on the dashboard toggle or operator timing.

## Alternatives Considered

**Rely on the dashboard stamping + an operator-run clear at birth** — Status quo. Fragile: depends
on `live_test` staying on and on the operator running the clear at the right instant; the 371
mis-flagged rows show the dashboard path is not sufficient on its own.

**Auto-clear only `test=true` on first birth** — Relies on every pre-birth row being correctly
flagged at the one-shot birth moment. Even with the backfill and pre-birth forcing below, this stays
dependent on the flag being perfect, and would silently miss any out-of-band class of unflagged data.

**Force `test=true` pre-birth + backfill existing rows + wipe ALL tracking data on first birth
(chosen).** The pre-birth forcing and backfill keep the `test` flag accurate (so the operator-run
clear works during testing); the birth clear itself deletes *all* tracking data rather than keying
on the flag, which is robust regardless of how any pre-birth row was created. Config/settings are
preserved. Safe because nothing real can predate birth and the clear is gated to the first birth.

## Decision

1. While Ada is **not yet born** (no `ada_profile` row), the engine MUST flag every Ada event
   `test=true` regardless of the payload flag (`test = eventTest(evt) || !born`), and existing
   pre-birth rows MUST be backfilled to `test=true` (migration `000007`). This keeps the `test` flag
   accurate so the operator-run `make ada-db-clear-test` is comprehensive during testing.
2. On the **first** `ada.born` only — detected by the `ada_profile` row being absent before this
   event — the engine MUST delete **all** rows from the Ada tracking tables (feedings + cascaded
   children, diapers, sleep, tummy, growth), not only `test=true` rows, then refresh sensors, save
   the profile, and mark itself born. Deleting everything (rather than keying on the flag) catches
   both dashboard-stamped test data and out-of-band/API-seeded data that never carried a flag, and is
   safe because nothing real can predate birth.
3. The birth clear MUST preserve config/settings tables (`ada_config`, persons/caretakers,
   channels) — those are configuration the family may have set during validation, not tracking data.
4. A re-fired `ada.born` (profile already present) MUST be a no-op and MUST NOT clear anything.
5. After birth, the engine MUST honor the payload flag as before (post-birth real events record
   `test=false`); the dashboard turns `live_test` off and fires `ada.born` exactly once at birth.

## Consequences

### Positive

* Deterministic clean slate the instant the real birth is recorded — no dependence on the dashboard
  toggle or operator timing.
* Pre-birth data is guaranteed selectable/clearable; the `test` flag finally means what it should.

### Negative

* The first-birth clear is destructive and **irreversible** (the engine cannot snapshot). This is
  acceptable because the data being cleared is, by definition, test data — but an operator who wants
  to keep a copy of the pre-birth test dataset MUST run `make ada-db-snapshot ENV=prod` before the
  birth. The `000007` backfill is likewise irreversible (the original `test=false` values are lost).
* Adds an in-memory `born` flag and a `GetProfile` check at startup to the engine.

### Neutral

* The operator-run `make ada-db-clear-test` remains for clearing test data during ongoing testing;
  the birth clear is the automatic counterpart for the one-time go-live transition.
* Extends ADR-0031 (test-data model); the birth event becomes a lifecycle boundary in the data model.
