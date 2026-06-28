# PLAN-0033 - Household Overlay (people, childcare, subjects, suggestions)

* **Status:** Complete
* **Date:** 2026-06-27
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0012-home-calendar.md (effort 0012.4)
* **Branch:** feat/household-overlay
* **Related ADRs:** ADR-0042 (calendar sync architecture), ADR-0040 (read API), ADR-0041 (OpenAPI governance)

---

## Scope

Add the local-only household overlay — people directory, childcare providers, and event associations
(subjects + childcare) — never written to Google; enrich calendar responses with resolved
`subjects[]`, resolved `childcare`, and attendees reconciled to `person_id`; and add the directory,
provider, and ranked-suggestions read endpoints. Depends on PLAN-0032 (calendar mirror + read endpoint
exist). **Out of scope:** per-instance childcare override (series-level only for MVP); group
membership for `kind=group`; any interaction with the Ada caretaker notification toggle (unrelated
concept that merely also references HA `person` entities).

---

## Pre-conditions

* [ ] On branch `feat/household-overlay` cut from latest `main`, with 0012.3 merged.
* [ ] `citext` extension availability on the shared Postgres confirmed (see Open Questions for the
      `text` + `lower()` fallback if `CREATE EXTENSION` privilege is unavailable).

---

## Steps

### Step 1 — Migration `000002_household_overlay`

**Action:** Add `pkg/calendar/store/migrations/000002_household_overlay.up.sql`/`.down.sql` per
ADR-0042/ROADMAP-0012: `directory_person` (uuid PK, display_name, `kind text` + CHECK
(`person|group`), ha_person_entity_id null, `email citext` null, family null, color null, active),
`childcare_provider` (uuid PK, display_name, `person_id uuid` null FK → directory_person,
`relationship text` + CHECK null, archived bool, created_at), `event_subject` (uuid PK,
`google_event_id` FK → calendar_event, `person_id` FK → directory_person, UNIQUE(google_event_id,
person_id)), `event_childcare` (uuid PK, `google_event_id` FK, `provider_id` FK → childcare_provider,
created_at, UNIQUE(google_event_id, provider_id)). Enum-like columns are `text` + CHECK, not PG enums.
Add `queries/overlay.sql`. Regenerate sqlc.

**Verification:** testcontainers Postgres applies `000001`+`000002` cleanly; `sqlc generate` diff-clean;
inserting two `event_subject` rows with the same (event, person) violates the unique constraint; the
`kind` CHECK rejects an invalid value.

### Step 2 — Overlay write handlers in the calendar processor

**Action:** Extend `services/engine/processors/calendar/` to handle the overlay write contracts routed
in via PLAN-0031: `ruby_home.childcare.provider.upsert` (insert/update; person link optional) and
`ruby_home.childcare.provider.delete` (= set `archived=true`, retain row). Subject associations ride
inside `calendar.event.upsert.subjects[]` — after the Google id is known, reconcile `event_subject`
rows for that event (add/remove to match the payload). Wire the **cascade**: a true `event_delete`
removes the event's `event_subject`/`event_childcare` rows (the deferred cascade from PLAN-0032 Step 6).

**Verification:** `go test -tags=fast ./services/engine/...`: `provider.upsert` creates/updates a row;
`provider.delete` sets `archived=true` without deleting; `event.upsert` with `subjects[]` converges the
association rows; a true `event.delete` cascades the overlay rows. Integration confirms against SQL.

### Step 3 — Enrich the calendar read endpoint

**Action:** Fill in the `subjects[]`, `childcare`, and attendee resolution placeholders left by
PLAN-0032 Step 9 in `services/api/handlers/calendar.go` and `api/openapi/paths/calendar.yaml`: resolve
`event_subject` → `directory_person`, `event_childcare` → `childcare_provider`, and reconcile Google
attendees by email (citext) to `person_id` where matched, carrying Google RSVP status. Regenerate.

**Verification:** `make openapi-gen` clean; integration — an event carrying subjects, a childcare
provider, and an attendee whose email matches a directory person returns all three resolved with RSVP;
an unmatched attendee returns with `person_id` null.

### Step 4 — Overlay read endpoints

**Action:** Add `api/openapi/paths/directory.yaml` and `paths/childcare.yaml` (documented + examples)
and handlers: `GET /v1/directory/people`, `GET /v1/childcare/providers` (non-archived roster), and
`GET /v1/childcare/providers/suggestions`. Implement suggestions in `pkg/calendar` (shared logic):
for each non-archived provider, expand its associated series' **past** occurrences over a recency
window via `pkg/calendar/expand`, count per-occurrence, recency-weight, and rank — **no stored
counter**. Reference the new fragments from the root spec.

**Verification:** `make openapi-gen`/`openapi-lint`/`openapi-diff` pass; `go test -tags=fast` proves a
provider on a weekly series ranks above one on a single past event, and that an archived provider is
excluded; integration drives all three endpoints via the generated client.

### Step 5 — Seed/test-data coverage (optional, if dashboards need it)

**Action:** If the household UI needs sample data, extend the seed tooling pattern (analogous to the
Ada seed) to populate `directory_person`/`childcare_provider` with `test`-flagged rows. (Calendar
events come from Google, not seeds.)

**Verification:** seed populates the overlay tables; clear removes `test=true` rows. (Skip if not
needed for the MVP UI.)

### Step 6 — Backup, README, Pre-PR checklist

**Action:** Add the overlay tables to the foundation Postgres backup set. Update the calendar processor
README and `docs/plans/README.md`. Run the Pre-PR Checklist; archive this plan to
`docs/plans/archived/` as the final commit.

**Verification:** backup includes the four overlay tables; pre-commit hooks pass; READMEs reflect the
final endpoint set.

---

## Rollback

`000002` is additive (new tables); the overlay is local-only and never touches Google. Rollback =
revert the engine + api images; the new endpoints disappear and the write handlers stop. A
down-migration drops the overlay tables — snapshot first if any real associations exist, since the
data is authored only in ruby-core and not recoverable from Google. Calendar core (PLAN-0032) is
unaffected by rolling back this slice.

---

## Open Questions

* **`citext` privilege:** if `CREATE EXTENSION citext` is not permitted on the shared Postgres, fall
  back to `email text` + a `UNIQUE` index on `lower(email)` and case-fold in queries. Confirm which
  before Step 1.
* **Recency window + weighting for suggestions:** what lookback window and decay shape rank providers
  (e.g. 90 days, linear vs exponential recency weight)? Default to a documented fixed window +
  simple recency weight in Step 4 unless specified.

---

## Completion Notes

Delivered on branch `feat/household-overlay` (commits: overlay core + deploy activation). Open
questions resolved and deviations:

* **citext → text + lower() index.** Avoided the `CREATE EXTENSION citext` privilege dependency
  entirely: `email text` with a partial unique index on `lower(email)`, and case-folded lookups.
* **Suggestions window/weight.** `pkg/calendar.DefaultSuggestionWindow` = 90 days, linear recency
  weight (1.0 at now → 0 at the window edge); per-occurrence counting via expansion, nothing stored.
* **Cascade via FK.** Overlay rows `ON DELETE CASCADE` to `calendar_event`, so a true event delete
  cleans up associations automatically — no app-side cascade code.
* **Association resolution is series-level**, keyed on the master event id (recurring/override
  instances resolve to the master); per-instance overrides are out of scope (MVP).
* **Attendee reconciliation matches the primary email only** (Google `attendees[].email`), parsed
  from the stored `raw` payload. Aliases / secondary emails are not matched — a known limitation.
* **`relationship` is unconstrained free text** (no CHECK) — the brief said the values will grow and
  none are defined yet; add a CHECK once a vocabulary is settled.
* **api un-gated for deploy.** Removed the `profiles: [api]` gate added in #130; the Slice D release
  deploys api. Host-side provisioning is a hard prerequisite — see
  `docs/runbooks/api-deploy-provisioning.md`.
* **No directory-person write event.** The brief defines no person write contract, so people are
  managed out-of-band / seeded; the read endpoint + `UpsertPerson` (for seeding) are provided.
* **No Postgres integration test** (drift #127) — overlay logic is unit-tested with fakes; real SQL
  (the CASCADE, the unique indexes, the joins) runs only in the live engine/api.
