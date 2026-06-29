# ADR-0044 - Calendar write semantics: patch-merge updates and per-instance recurring edits

* **Status:** Accepted
* **Date:** 2026-06-29
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

ADR-0042 established the calendar sync architecture: Home Assistant fires `ruby_home_event`
write events, the engine's calendar processor writes through to Google Calendar (the
system of record), and a local Postgres mirror is converged from Google's returned event.
The MVP write path that ADR-0042 shipped was deliberately series-level only and used Google
`events.update`.

Two gaps surfaced once the HA `rh-calendar` surface (`homeassistant` repo, PLAN-0034) began
consuming these seams in earnest (#155):

1. **Lossy updates.** `events.update` is a *full replace*. The engine builds the Google event
   from the write payload alone, and the HA surface — by design — **omits** fields the user
   did not touch (notably `recurrence`). The result: editing any field of a recurring event
   silently strips its RRULE at Google, destroying the series. This is a correctness bug, not
   a missing feature.

2. **No per-occurrence editing.** A shared family calendar needs "this event only", "this and
   following", and "all events" when editing or deleting a recurring series. The MVP only
   offered series-level writes, so the HA surface withholds the scope sheets entirely rather
   than ship a half-working UI.

Google Calendar already models single-occurrence changes natively (instance *overrides* —
an event whose `recurringEventId` points at the master and whose `originalStartTime` pins the
occurrence), and the ruby-core mirror + expansion already understand overrides and cancelled
occurrences. The decision is how the engine's write path should map HA's edit scopes onto
Google, and how to stop the data loss.

## Alternatives Considered

**Keep `events.update` and have HA always send the full event (including untouched recurrence).**
Pushes correctness onto every client and every future caller; one client that forgets a field
destroys data. The merge belongs on the write-through, not the consumer.

**Read-modify-write merge in the engine (Get the current event, overlay payload fields, Update).**
Works, but adds a guaranteed extra Google round-trip per edit and re-implements field-level merge
that Google's `events.patch` already does correctly — including for nested fields.

**Model "this event only" as a brand-new standalone event (not a Google override).** Detaches the
occurrence from its series: it would not move when the master moves, would not be subtracted from
the expansion, and breaks "this and following". Rejected — it fights Google's data model and the
mirror's expansion logic.

**Carry the edit scope implicitly (infer "single occurrence" when `original_start` is present).**
Ambiguous and unsafe: a present `original_start` cannot distinguish "edit just this one" from
"edit the whole series starting at this instance". An explicit `scope` is required.

## Decision

1. **Updates MUST use Google `events.patch`, not `events.update`.** The engine builds the Google
   event from only the fields the write payload carries; fields absent from the serialized event
   MUST be left unchanged by Google. The existing If-Match etag optimistic-concurrency and the
   resync-and-retry-once-on-412 behaviour (ADR-0042) are preserved.

2. **Per-instance edits MUST use Google's native override model.** A write whose scope is the
   single occurrence MUST resolve the occurrence via `events.instances` on the master (matched by
   `original_start`) and patch that instance's event id — never the master, and never a detached
   standalone event.

3. **Per-instance delete MUST cancel the occurrence, not the series.** It MUST set the resolved
   instance's `status` to `cancelled` (an override tombstone), which the mirror and expansion
   already subtract. Series-level delete remains the existing hard delete of the master.

4. **Edit/delete scope MUST be explicit.** The write payload carries a `scope` of `this`,
   `this_and_following`, or `all`. `all` (the default, for back-compat with the MVP) keeps the
   series-level path. `this` uses obligations 2–3.

5. **`this_and_following` (series split) is deferred.** It requires setting `UNTIL` on the
   master's RRULE at the cut point and creating a new series for the remainder; it is the
   highest-risk sub-case and is intentionally out of the first delivery. Until it ships, a
   `this_and_following` write SHOULD be rejected/withheld by the consumer rather than silently
   downgraded to `all` or `this`.

6. **Explicit recurrence-clearing is deferred.** Because the payload omits untouched fields,
   "make this series non-recurring" cannot be distinguished from "recurrence untouched" under the
   current `omitempty` recurrence field. Clearing recurrence MUST be routed through a future
   scope-aware path (or a presence-typed payload), not inferred from an empty value.

## Consequences

### Positive

* Editing a recurring event no longer destroys its RRULE — the data-loss bug is closed at the
  write-through, independent of client behaviour.
* HA can offer real "this / this-and-following*/ all" scope sheets (*minus the deferred split).
* Per-instance changes ride Google's native override model, so they round-trip cleanly through the
  existing mirror + expansion with no new local state.
* No extra Google round-trip for the common edit (patch is a single call).

### Negative

* `events.patch` cannot express "clear this field" by omission, so explicit field-clearing
  (recurrence especially) is now a known, deferred gap rather than a supported operation.
* The write path gains scope-dependent branches (series vs instance), increasing its surface area
  and test matrix.
* `this_and_following` remaining unimplemented means the consumer must withhold that one option;
  the model is not yet at full parity with a native calendar client.

### Neutral

* This formalizes Google Calendar's override/instances model as the canonical representation of
  per-occurrence changes in ruby-core; the mirror remains a converged projection of it.
* The `scope` field becomes part of the durable `ruby_home_event` calendar write contract.
