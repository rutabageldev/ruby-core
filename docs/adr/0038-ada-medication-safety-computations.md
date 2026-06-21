# ADR-0038 - Ada medication safety computations: server is authoritative

* **Status:** Accepted
* **Date:** 2026-06-20
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

The Ada medication surface includes an always-on **dosing safety guard** (minimum spacing between doses, rolling-24h ceiling), a **schedule state machine** (fixed-time slots that become due, then missed), **routine completion** (a course of N doses or an end date), and **as-needed temporary watches** (a transient interval that re-anchors on each dose). The dashboard computes all of this client-side in `adaMeds.ts` (`computeGuard`, `scheduledInstancesToday`, `isRoutineComplete`, `computeDue`, `seriesNextDueMs`) for instant feedback — but those functions only run while the app is open.

The whole point of moving this server-side is that **the dangerous cases happen when no one is looking**: a scheduled dose comes due and is forgotten; a temporary Tylenol watch should quietly expire overnight; a course of antibiotics should auto-complete. If the engine is not authoritative for these, the guard and the timeline are only as correct as the last time someone opened the app — unacceptable for a drug-spacing safety feature. At the same time, the server and client must never disagree (the same local/remote mirror discipline as the feed-claim), so the server math must match the client formulas exactly, to the boundary.

A normal Ada event is caregiver-initiated. These computations are different: some produce state with **no human actor** (a `missed` slot, an expired watch) and must be written by the engine itself. That is a deliberate, constrained exception to "the engine only records what caregivers do," and it needs to be bounded so the engine can never invent a dose.

## Alternatives Considered

**Leave the computations client-side; engine stores only raw given/skipped** — The guard, missed-detection, and expiry would freeze whenever the app is closed — exactly the failure this work exists to fix. Rejected.

**Engine recomputes everything on read with no persisted `missed`/expiry** — Pure-function projection is appealing, but "due → reminder" and "watch expired" are time-edge transitions that must fire once at the right moment even with no event arriving; a read-only model has nothing to trigger them and no idempotent record that they happened. Rejected in favor of persisting the system transitions.

**Let the engine also auto-log a `given` on routine completion** — Tidy-looking, but it would fabricate a dose that no caregiver administered, corrupting the dosing history and the guard. Explicitly rejected — completion is a status change, never a dose.

## Decision

1. The engine MUST be the **authority** for the derived medication state and MUST compute it with formulas identical to `adaMeds.ts`, to the boundary:
   * `earliest_safe` = `last_given.timestamp + min_interval_hours`; a dose is "unsafe" only when `now < earliest_safe` (safe exactly at the boundary).
   * `doses_in_24h` = count of `given` events in the half-open window `(now − 24h, now]`.
   * `next_due` = fixed-time slot (`fixed_times`), or `last_given + interval_hours` (interval routine), or `anchor_dose + interval_hours` (active series).
   * Only `status='given'` events feed any dosing math; `skipped` and `missed` are invisible to spacing, the 24h ceiling, and series anchoring.

2. `missed` MUST be **system-emitted by supersession** and MUST NOT stack: for an active fixed-time routine, when a slot has no `given`/`skipped` event **and a later slot of the same routine has already come due**, the engine writes exactly one actorless `missed` MedEvent for that slot. No carry-forward, no catch-up, at most one `missed` per (routine, slot, local-day). A `missed` row never becomes a `given`.

3. Routine **auto-complete** MUST set `status='completed'` when the given-dose count reaches `max_doses`, or when the `end_date` has passed, and MUST NOT write any dose event on completion (no phantom dose).

4. Temporary series **auto-expire** MUST set an active series to `status='expired'` (`ended_reason='auto_expire'`) once its anchor dose is older than the ~24h backstop (Exit 3; the dashboard owns the explicit user exits).

5. `missed` emission, routine completion, and series expiry are the **only** events/state the engine writes without a caregiver action. They run on the existing 60-second safety-net reconcile (drift without an inbound event) and/or per-target one-shot timers, and every one of them MUST be idempotent so the reconcile loop and restarts never double-write.

6. "Due" reminders fire when `now ≥ next_due` and MUST be de-duplicated (a due is reminded once, not every tick) using persisted markers in `ada_config`, mirroring the feeding alert timer.

## Consequences

### Positive

* The dosing guard, missed-detection, and expiry stay correct with the app closed — the safety property that justifies the whole effort.
* The bounded list of engine-written transitions (missed / complete / expire) makes it auditable that the engine can never fabricate a dose.
* Parity tests pin the server math to the client formulas, so the two surfaces cannot silently diverge.

### Negative

* Re-implementing `adaMeds.ts` in Go duplicates logic across two languages; the formulas and their boundary conditions must be kept in sync by discipline and parity tests, not by a shared implementation.
* Time-edge correctness (local-day boundaries, DST, the engine container's UTC clock vs. the family's local time) is genuinely fiddly and is the most likely source of a subtle divergence.

### Neutral

* Establishes the engine as a writer of system events for the Ada domain, a capability previously unused there.
* Reminder delivery and the projection/persistence contract are decided in ADR-0037; this ADR governs only the computations and the system-emitted transitions.
