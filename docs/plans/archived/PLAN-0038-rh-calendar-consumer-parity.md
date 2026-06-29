# PLAN-0038 — rh-calendar Consumer Parity (#155)

* **Status:** Complete (2026-06-29 — §4b/§1/§3/§2 shipped; §2 this_and_following + recurrence-clearing deferred per ADR-0044)
* **Date:** 2026-06-29
* **Project:** ruby-core
* **Roadmap Item:** follow-on to [ROADMAP-0012](../roadmap/archived/ROADMAP-0012-home-calendar-api-foundation.md) (Home Calendar + API Foundation, complete)
* **Branch:** `feat/rh-calendar-consumer-parity`
* **Related ADRs:** [ADR-0044](../adr/0044-calendar-write-semantics-patch-and-per-instance.md) (calendar write semantics — patch-merge + per-instance recurring edits); refines [ADR-0042](../adr/0042-calendar-sync-architecture.md) (calendar sync)

---

## Scope

Close the ruby-core-side gaps that block the Home Assistant `rh-calendar` surface from
**fully and safely** consuming the calendar seams (#155). Delivered as **one PR** covering all
four asks, ordered data-loss-fix-first:

* **§4b** — calendar event UPDATE must preserve fields the caller omits (esp. `recurrence`).
* **§1** — expose `etag`, `recurrence`, `recurring_event_id`, `original_start` on read-API instances.
* **§3** — add a directory-person write route so HA can add/rename people.
* **§2** — per-instance recurring edit + delete (this-occurrence / this-and-following / all).
* **§4a** — confirm + document that childcare-provider upsert is insert-or-update by client id.

**Out of scope:** HA-side changes (the `homeassistant` repo owns the surface + `rest_command`s);
colour storage (HA-local, never synced); any change to the read transport (verified working);
mTLS for the API (tracked separately in #122).

---

## Pre-conditions

* [ ] Branch `feat/rh-calendar-consumer-parity` cut from `main` (done).
* [ ] OpenAPI toolchain available locally (`make openapi-bundle/gen/lint`): ogen v1.22.0,
      redocly 1.34.5, spectral 6.15.0 — already pinned (ROADMAP-0012 Slice A).
* [ ] No new Vault/secret or migration prerequisites — the `calendar_event`, `directory_person`,
      and `childcare_provider` tables already carry every column this work reads or writes.

---

## Design decisions

**§4b — patch, not replace.** `update()` currently calls Google `events.update` (full replace)
with an event built only from the payload, so an omitted `recurrence` is sent as empty and Google
strips the series. Switch the update path to Google **`events.patch`**: fields absent from the
serialized event are left unchanged, which matches HA's "omit if untouched" contract. The 412/etag
resync-and-retry-once behaviour is preserved. Known limitation (documented, not fixed here):
because `CalendarUpsertData.Recurrence` is `[]string,omitempty`, an *explicit* "make this series
non-recurring" cannot be distinguished from "untouched" — clearing recurrence is deferred to §2's
scope-aware editor or a later presence-typed payload.

**§2 — Google's native override model.** Per-instance edits map to Google instance overrides:
list the master's instances, find the occurrence by `original_start`, and **patch that instance's
event id**. Per-instance delete patches the instance to `status=cancelled` (the mirror + expansion
already subtract cancelled overrides — verified in `expand_test.go`). "This and following" is a
**series split**: set `UNTIL` on the master's RRULE at the cut point and create a new series for
the remainder. The split is the riskiest sub-part; it is gated behind its own step and can be
dropped from this PR without blocking the rest (see Open Questions).

**§3 — mirror the childcare-provider path.** The store already has `UpsertPerson` +
`UpsertPersonEmail` (`ON CONFLICT (id) DO UPDATE`). Only the contract + routing + handler are new.

---

## Steps

### Step 1 — §4b: preserve omitted fields on update (data-loss fix)

**Action:** Add `Patch(ctx, calendarID, eventID, etag string, ev *calendarv3.Event)` to
`gcal.Service` (Google `events.patch`, If-Match etag, 412 → `ErrConflict`). Change
`writethrough.go` `update()` to call `Patch` instead of `Update`. Leave `payloadToGoogle` setting
only the fields the payload carries (recurrence stays `nil` when omitted → omitted from the patch
body). Keep the resync-and-retry-once-on-412 flow.

**Verification:** New unit test in `services/engine/processors/calendar` using the mockable
`gcal.Service`: an upsert for an existing recurring event whose payload omits `recurrence` results
in a `Patch` call whose `ev.Recurrence` is empty/omitted (so Google preserves it), **not** an
`Update`. `go test -tags=fast ./services/engine/processors/calendar/...` green.

**Notes:** This is the priority — it prevents silent destruction of recurring series on edit.

### Step 2 — §1: expose recurrence/etag/identifiers on read-API instances

**Action:** Add to the `CalendarInstance` schema in `api/openapi/` (non-nullable to avoid the
known ogen `problem+json` nullable→spectral crash): `etag` (string), `recurrence` (array of
string), `recurring_event_id` (string), `original_start` (string/date-time). Run
`make openapi-bundle openapi-gen`. Populate them in `toAPIInstance` (calendar.go) from the source
`*store.CalendarEvent` row (`etag`, `recurrence`, `recurring_event_id`) and the `expand.Instance`
(`OriginalStart` for overrides, else the occurrence start).

**Verification:** `make openapi-lint` passes (spectral clean). A handler/unit test asserts a
recurring-series instance carries the RRULE in `recurrence` and a non-empty `recurring_event_id`,
and an override instance carries `original_start`. `make openapi-verify` shows no unintended
breaking diff (additive only). `go build ./... && go test -tags=fast ./services/api/...` green.

### Step 3 — §4a: confirm + document childcare upsert semantics

**Action:** No code change — `UpsertProvider` is already `INSERT … ON CONFLICT (id) DO UPDATE`.
Add a one-line "upsert = insert-or-update by client-supplied id" note to the calendar runbook
(or `services/gateway/README.md` `ruby_home_event` table) and record the confirmation to close
out §4a on #155.

**Verification:** The runbook/README states the semantics; the confirmation is captured in the PR
description for the #155 reply.

### Step 4 — §3: directory-person write route

**Action:** Add `ha.events.ruby_home.directory.person_upsert` (+ `_delete`) subject constants in
`pkg/schemas/homecal.go`; register `ruby_home.directory.person.upsert` / `.delete` in the gateway
`eventRoutes` (`services/gateway/rubyhome/publish.go`); add a `DirectoryPersonData` payload schema;
add engine `Subscriptions()` patterns + `handlePersonUpsert` / `handlePersonDelete` in
`overlay_write.go` calling the existing `UpsertPerson` (+ `UpsertPersonEmail` when an email is
present) and a soft-delete via the `active` flag for `.delete`.

**Verification:** Unit test for the new handlers (fake store) covering insert-by-id, update-by-id,
and soft-delete. A `rubyhome` publish test confirms the new `event` strings route to the new
subjects. `go test -tags=fast ./...` green.

### Step 5 — §2: per-instance recurring edit + delete

**Action (5a — gcal capability):** Add `Instances(ctx, calendarID, recurringEventID)` and
`PatchInstance(ctx, calendarID, instanceEventID, etag, ev)` to `gcal.Service` (Google
`events.instances` + `events.patch` on the instance id). **(5b — payload):** add `scope`
(`this` | `this_and_following` | `all`, default `all` for back-compat), `recurring_event_id`, and
`original_start` to `CalendarUpsertData`/`CalendarDeleteData`. **(5c — write-through):** when
`scope=this`, resolve the occurrence by `original_start` via `Instances`, then `PatchInstance`
(edit) or patch `status=cancelled` (delete); `scope=all` keeps today's series-level path. Mirror
the resulting override row via the existing `UpsertEvent`/overlay reconcile. **(5d — optional)**
`scope=this_and_following`: split the master (RRULE `UNTIL`) + create the remainder series.

**Verification:** Unit tests over the mockable `gcal.Service`: `scope=this` edit issues a
`PatchInstance` on the resolved instance id (not the master); `scope=this` delete cancels only that
instance; `scope=all` is unchanged. Expansion of the mirror after a per-instance cancel omits that
occurrence (extends existing `expand_test.go` coverage). `go test -tags=fast ./...` green.

**Notes:** Largest step. 5a–5c deliver this-occurrence + all; 5d (this-and-following split) is
separable — drop it to a follow-up if the diff gets unwieldy (Open Questions).

### Step 6 — Pre-Push + close-out

**Action:** Update the calendar runbook (new directory route, per-instance scopes, patch
semantics) and `services/gateway/README.md` route table; run `make openapi-verify`, full
`go build`, `golangci-lint run` (PATH binary v2.11.x), `go test -tags=fast -race -short ./...`;
draft the #155 reply (per-section answers); archive this plan (status → Complete) as the final
commit.

**Verification:** Pre-Push checklist clean; `--new-from-rev=origin/main` lint = 0; the PR closes
issue 155 and the reply answers §1–§4 explicitly.

---

## Rollback

Read-API + payload additions are additive (no removed fields, no breaking OpenAPI diff). No schema
migration. The §4b update→patch switch is behaviour-only — revert the commit and redeploy
(`make deploy-prod`, smoke + auto-rollback) to restore the prior path. Per-instance writes touch
Google as overrides/cancellations exactly as the HA UI requests; rollback is reverting the engine
image. No stateful/irreversible step.

---

## Resolved decisions (2026-06-29)

1. **ADR written.** [ADR-0044](../adr/0044-calendar-write-semantics-patch-and-per-instance.md)
   captures the patch-merge update rule and the per-instance override model (Accepted).
2. **`this_and_following` (Step 5d) is DEFERRED** to a fast-follow (ADR-0044 obligation 5). This PR
   ships this-occurrence + all; the consumer withholds the split option until it lands.
3. **Explicit recurrence-clearing is DEFERRED** (ADR-0044 obligation 6) — not expressible under the
   `omitempty` payload; routed through a future scope-aware path, not inferred from an empty value.
