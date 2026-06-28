# PLAN-0031 - Event-Bus Generalization (gateway `ruby_home_event`)

* **Status:** Complete
* **Date:** 2026-06-27
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0012-home-calendar.md (effort 0012.2)
* **Branch:** feat/event-bus-generalization
* **Related ADRs:** ADR-0009 (gateway responsibilities), ADR-0027 (subject naming), ADR-0003 (CloudEvents)

---

## Scope

Make the gateway's HA→NATS write pipe domain-neutral by adding a new `ruby_home_event` Home Assistant
event type alongside the existing `ada_event`, deriving NATS subjects from the payload `event` string.
This carries the home-calendar write contracts (`calendar.event.upsert`, `calendar.event.delete`,
`ruby_home.childcare.provider.upsert`, `ruby_home.childcare.provider.delete`) onto the bus so Slice C
has a consumer-ready ingress. **Out of scope:** any consumer of the new subjects (engine processor is
PLAN-0032); dropping the `ada_event` subscription (deferred until HA producers migrate — cross-repo
follow-up, the `homeassistant` repo is not modified here); the HA-side producer change itself
(described, not committed).

---

## Pre-conditions

* [ ] On branch `feat/event-bus-generalization` cut from latest `main`.
* [ ] Familiarity confirmed with the existing dual-subscribe in `services/gateway/ha/client.go`
      (state_changed subID=1, ada_event adaSubID=2, `handleEvent()` switch) and the route-map publish
      pattern in `services/gateway/ada/publish.go` (`eventRoutes` map + CloudEvent wrap).

---

## Steps

### Step 1 — Define the `ruby_home` route table

**Action:** Create `services/gateway/rubyhome/publish.go` mirroring `services/gateway/ada/publish.go`:
a `eventRoutes` map from the payload `event` string to the precomputed NATS subject, covering
`calendar.event.upsert` → `ha.events.calendar.event_upsert`, `calendar.event.delete` →
`ha.events.calendar.event_delete`, `ruby_home.childcare.provider.upsert` →
`ha.events.ruby_home.childcare.provider_upsert`, `ruby_home.childcare.provider.delete` →
`ha.events.ruby_home.childcare.provider_delete`. (Confirm the exact subject tokens against ADR-0027
`BuildSubject` validity — lowercase, underscores; no dots inside a token.) Wrap each in a CloudEvent
like the ada path and publish. Unknown `event` → warn + return error (ada parity).

**Verification:** `go build ./services/gateway/...` compiles; the subject for each known event matches
the ROADMAP-0012 write-contract names.

### Step 2 — Subscribe to `ruby_home_event` in the HA client

**Action:** In `services/gateway/ha/client.go`, add `const homeEventSubID = 3` and a third
`subscribe_events` write for `EventType: "ruby_home_event"` next to the existing two; add
`case "ruby_home_event":` to the `handleEvent()` switch routing to a new `handleRubyHomeEvent()` that
extracts the payload and calls `rubyhome.Publish(...)` (mirroring `handleAdaEvent`).

**Verification:** `go build ./services/gateway/...`; unit test (Step 3) green. Gateway logs show three
subscriptions established at startup.

### Step 3 — Unit tests (`-tags=fast`)

**Action:** Add table-driven tests mirroring the ada publish tests: each known `event` string maps to
the expected subject; unknown event returns an error and publishes nothing; the published payload is a
well-formed CloudEvent (id, source, type) per ADR-0003.

**Verification:** `go test -tags=fast -race ./services/gateway/...` passes.

### Step 4 — Integration test (`-tags=integration`)

**Action:** Add a testcontainers-NATS integration test (existing pattern in `pkg/natsx`): publish a
synthetic `ruby_home_event` payload through the gateway publish path and assert the CloudEvent lands on
the correct `ha.events.*` subject in the `HA_EVENTS` stream; assert an `ada_event` still routes
unchanged (no regression).

**Verification:** `go test -tags=integration -race ./services/gateway/...` passes.

### Step 5 — Document the HA-side producer change (no commit to that repo)

**Action:** Add a short section to `services/gateway/README.md` (and reference it from the calendar
runbook) describing what the Home Assistant side must emit: fire a `ruby_home_event` with an `event`
field set to one of the route keys and the documented payload. Note that the eventual `ada_event` drop
happens only after HA producers migrate.

**Verification:** README documents each `ruby_home_event` route key and payload shape; matches the
`eventRoutes` map exactly (no drift).

### Step 6 — Pre-PR checklist

**Action:** Run the Pre-PR Checklist; update `docs/plans/README.md`; archive this plan to
`docs/plans/archived/` (status → Complete) as the final commit on the branch.

**Verification:** Pre-commit hooks pass; the route keys in the README, the `eventRoutes` map, and the
ROADMAP-0012 write contracts agree.

---

## Rollback

Additive and isolated to the gateway. Rollback = revert the commit and redeploy the gateway; the new
subscription simply stops. No stateful change, no schema, no impact on the existing `state_changed` /
`ada_event` paths (they are untouched). Clean rollback.

---

## Open Questions

* **Subject token for childcare provider events:** ADR-0027 forbids dots inside a token, so
  `provider.upsert` becomes `provider_upsert` in the subject (`ha.events.ruby_home.childcare.provider_upsert`).
  Confirm this is the desired on-bus subject, versus collapsing childcare under a flatter
  `ha.events.ruby_home.*`. (Recommendation: keep the `ruby_home.childcare.provider_upsert` structure
  for clarity; the engine processor subscribes with a `.>` pattern regardless.)

---

## Completion Notes

Delivered on branch `feat/event-bus-generalization`. Built as planned; notes:

* **Open question resolved as recommended** — kept the structured subject
  `ha.events.ruby_home.childcare.provider_upsert` (`provider.upsert` → `provider_upsert` per
  ADR-0027). A unit test asserts every routed subject is composed of valid ADR-0027 tokens.
* **Shared subject constants live in `pkg/schemas/homecal.go`** (not inline in the gateway), so the
  Slice C/D engine consumers import the same contract (ADR-0014). `HomeEventCalendar{Upsert,Delete}`
  and `HomeEventChildcareProvider{Upsert,Delete}`.
* **HA producer convention:** `ruby_home_event` wraps the caller payload under a `payload` key, same
  as `ada_event`'s `script.fire_ada_event` intermediary — documented in the gateway README.
* **`ada_event` untouched** — dual-subscribe only; the HA-side producer migration and eventual
  `ada_event` retirement are cross-repo follow-ups in the `homeassistant` repo, out of scope here.
* Integration test (`-tags=integration`, testcontainers NATS) proves both new subjects land in
  `HA_EVENTS` and that `ada_event` still routes unchanged.
