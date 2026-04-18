# PLAN-0012 - Ada: Live Sleep Timer and Periodic Sensor Refresh

* **Status:** Approved
* **Date:** 2026-04-18
* **Project:** ruby-core (+ homeassistant frontend, tracked separately)
* **Roadmap Item:** none (standalone improvement)
* **Branch:** feat/ada-sleep-timer-sensor-refresh
* **Related ADRs:** ADR-0029 (Ada processor)

---

## Scope

Fixes two gaps in the Ada baby tracking system:

1. **Sleep elapsed time always shows 0m.** The quick-actions button ("Sleeping · Xm") and the
   end-sleep modal ("Asleep for Xm") both read `sensor.ada_sleep_session_min`, which ruby-core
   never pushes. This causes all elapsed-time displays in ada-quick-actions to show 0m regardless
   of actual sleep duration, including when the sleep start was backdated ("started 15 min ago").
   The status strip (`ada-status-strip.ts`) is a separate component and currently computes elapsed
   time client-side — it will be updated in a companion homeassistant issue to read this sensor
   instead (rutabageldev/homeassistant#TBD).

2. **Daily aggregate sensors are only updated on event.** After midnight, sensors like
   `sensor.ada_today_feeding_count` retain yesterday's values until a new event is logged. There is
   also no safety-net refresh if HA restores sensors to unknown after a restart.

**Architecture decision:** All elapsed-time computation happens in ruby-core against the DB.
HA is a passive state broker. Clients are renderers — no client-side timers or inference.
This ensures all connected devices (phones, tablets, dashboards) flip simultaneously when
ruby-core pushes an update.

**Out of scope:** Changes to feeding, diaper, or tummy time sensor behavior. DB schema changes.
New NATS subjects. The homeassistant frontend change is tracked as a separate GitHub issue.

---

## Pre-conditions

* [ ] Branch `feat/ada-sleep-timer-sensor-refresh` created from `main`
* [ ] Engine is running and healthy in dev (`make dev-up`)
* [ ] `sensor.ada_sleep_state` and `sensor.ada_last_sleep_change` are visible and updating in HA
      developer tools after a manual sleep-start event (confirm current baseline works)

---

## Steps

### Step 1 — Add `sensorSleepSessionMin` constant

**Action:** Add `sensorSleepSessionMin = "sensor.ada_sleep_session_min"` to the constants block
in `services/engine/processors/ada/processor.go` alongside the existing sleep sensor constants.

**Verification:** `go build ./...` passes cleanly.

---

### Step 2 — Decompose `restoreSensors()` into focused push helpers

**Action:** Refactor `restoreSensors()` in `processor.go` into three composable methods:

* `pushLastEventSensors(ctx)` — last feeding (time, source, next target), last diaper (time, type)
* `pushDailyAggregates(ctx)` — all `today_*` sensors: feeding count/oz, diaper counts, sleep hours
  * nap count, tummy min + sessions
* `pushActiveSleepState(ctx)` — queries `GetActiveSleepSession` or `GetLastSleepEnd`; pushes
  `sensor.ada_sleep_state`, `sensor.ada_last_sleep_change`, and `sensor.ada_sleep_session_min`
  (elapsed minutes if sleeping, `"0"` if awake)

Replace the body of `restoreSensors()` with sequential calls to all three. Behaviour is
identical to today — this is pure refactor, no logic changes.

**Verification:** `go test ./services/engine/processors/ada/...` passes. Deploy to dev, restart
engine, confirm HA developer tools shows all sensors restored correctly within a few seconds of
engine startup.

---

### Step 3 — Push `sensor.ada_sleep_session_min` on sleep events

**Action:** Update the three sleep sensor push helpers to include `sensorSleepSessionMin`:

* `pushSleepStartedSensors(ctx, startTime)`: add
  `{sensorSleepSessionMin, strconv.Itoa(int(time.Since(startTime).Minutes()))}` to the push
  batch. This correctly reflects a backdated start (e.g. "started 15 min ago" → pushes 15).

* `pushSleepEndedSensors(ctx, endTime)`: add `{sensorSleepSessionMin, "0"}` to reset the sensor
  when a session ends. This covers both explicit end events and logged-past sessions
  (`handleSleepLogged` calls `pushSleepEndedSensors`).

**Verification:** In dev, start a sleep event with "started 15 min ago" (set `_sleepAgoMin = 15`
in the frontend). Confirm HA developer tools shows `sensor.ada_sleep_session_min = 15` within
a few seconds of confirming. Confirm the quick-actions sleep button label updates to
"Sleeping · 15m". End the sleep session; confirm the sensor resets to `0`.

---

### Step 4 — Add background ticker goroutine

**Action:** Add a `stopCh chan struct{}` field to the `Processor` struct and a
`lastRefreshDate time.Time` field (for midnight rollover tracking).

In `Initialize()`, after `restoreSensors()`, start a goroutine:

```
ticker := time.NewTicker(60 * time.Second)
for {
    select {
    case <-ticker.C:
        p.onTick(context.Background())
    case <-p.stopCh:
        ticker.Stop()
        return
    }
}
```

In `Shutdown()`, close `p.stopCh`.

Implement `onTick(ctx)`:

1. **Sleep timer (every tick):** Call `pushActiveSleepState(ctx)`. This queries the DB,
   computes current elapsed minutes, and pushes `sensor.ada_sleep_session_min`. When not
   sleeping, the push is a no-op on the state (state is already "awake" / "0") but keeps
   HA's `last_changed` timestamp current.
   * To avoid redundant pushes when awake, track a `lastSleepState bool` field and only push
     awake-state if transitioning or if safety-net interval has elapsed.

2. **Midnight rollover:** Compare `time.Now().Local()` date against `p.lastRefreshDate`.
   If the date has changed, call `pushDailyAggregates(ctx)` and update `lastRefreshDate`.

3. **4-hour safety net:** Track `lastFullRefresh time.Time`. If it has been ≥4 hours since the
   last full refresh, call `restoreSensors(ctx)` and update `lastFullRefresh`. This recovers
   from any HA state loss (e.g. HA restart between events) without waiting for the next event.

**Verification:**

* With an active sleep session, confirm `sensor.ada_sleep_session_min` increments in HA
  developer tools every ~60 seconds.
* Simulate midnight rollover by temporarily setting the comparison to trigger on the current
  minute (or by checking logs); confirm `pushDailyAggregates` is called and `today_*` sensors
  update.
* Confirm engine shuts down cleanly (`make dev-down`) with no goroutine leak in logs.

---

### Step 5 — Update processor tests

**Action:** Add/update unit tests in `processor_test.go` to cover:

* `pushSleepStartedSensors` with a backdated start time → verify `sensor.ada_sleep_session_min`
  is pushed with the correct non-zero elapsed minutes
* `pushSleepEndedSensors` → verify `sensor.ada_sleep_session_min` is pushed as `"0"`
* `pushActiveSleepState` with an active session → verify elapsed minutes are computed from
  session start
* `onTick` midnight rollover logic → mock clock or inject `lastRefreshDate` as yesterday,
  confirm `pushDailyAggregates` is called

**Verification:** `go test -tags=fast ./services/engine/processors/ada/... -v` passes with all
new test cases green.

---

### Step 6 — Commit

**Action:** Commit all changes on `feat/ada-sleep-timer-sensor-refresh` with a conventional
commit message summarising both the sleep timer fix and the periodic refresh addition.

**Verification:** Pre-commit hooks pass cleanly. `git log --oneline -3` shows the new commit.

---

## Rollback

Revert the commit and redeploy the engine. No DB schema changes, no new NATS subjects, no
migrations — rollback is a standard revert + redeploy.

After rollback, `sensor.ada_sleep_session_min` will become unavailable in HA (no more pushes).
HA will show the sensor as `unknown`. The ada-quick-actions button will fall back to `"0m"` (its
current broken state). The homeassistant frontend fix (tracked separately) should be reverted in
tandem if it has been deployed.

---

## Open Questions

_None — resolved in pre-planning discussion on 2026-04-18._
