# PLAN-0013 - Ada: Diaper and Sleep History Sensors

* **Status:** Complete
* **Date:** 2026-04-18
* **Project:** ruby-core
* **Roadmap Item:** none (standalone improvement)
* **Branch:** feat/ada-event-history-sensors
* **Related ADRs:** ADR-0029 (Ada processor)

---

## Scope

Adds two new HA sensors — `sensor.ada_diaper_history` and `sensor.ada_sleep_history` —
following the same push pattern as the existing `sensor.ada_feeding_history`. Each sensor
carries an `entries[]` attribute array suitable for rendering a 24-hour event timeline
modal in the HA dashboard.

Diaper entries expose: event ID, timestamp, and type (wet / dirty / mixed).

Sleep entries expose: session ID, start time, end time (omitted if active), sleep type
(nap / night), and duration in seconds (omitted if active). Active sessions appear in the
history so the dashboard can indicate an ongoing sleep without special-casing.

**Out of scope:** Any HA frontend changes. DB schema changes. Changes to existing sensors,
feeding history, or tummy time behavior.

---

## Pre-conditions

* [ ] Branch `feat/ada-event-history-sensors` created from `main`
* [ ] `sqlc` available on PATH or via `go tool` (verify with `sqlc version`)

---

## Steps

### Step 1 — Add SQL queries for diaper and sleep history

**Action:** Add two new named queries to the existing query source files.

`services/engine/processors/ada/store/queries/diapers.sql` — append:

```sql
-- name: GetLast24hDiapers :many
-- Returns all diaper events in the last 24 hours ordered newest-first.
SELECT id, timestamp, type
FROM diapers
WHERE deleted_at IS NULL
  AND timestamp >= NOW() - INTERVAL '24 hours'
ORDER BY timestamp DESC;
```

`services/engine/processors/ada/store/queries/sleep.sql` — append:

```sql
-- name: GetLast24hSleepSessions :many
-- Returns sleep sessions that started in the last 24 hours, newest-first.
-- duration_s is NULL for active sessions (end_time IS NULL).
SELECT
    id,
    start_time,
    end_time,
    sleep_type,
    CASE
        WHEN end_time IS NOT NULL
        THEN EXTRACT(EPOCH FROM (end_time - start_time))::int
        ELSE NULL
    END AS duration_s
FROM sleep_sessions
WHERE deleted_at IS NULL
  AND start_time >= NOW() - INTERVAL '24 hours'
ORDER BY start_time DESC;
```

**Verification:** Files saved and syntactically valid (no obvious typos).

---

### Step 2 — Regenerate sqlc

**Action:** Run `sqlc generate` from `/opt/ruby-core/services/engine/processors/ada/store/`
(or the repo root if `sqlc.yaml` lives there). This produces updated
`diapers.sql.go` and `sleep.sql.go` with `GetLast24hDiapers` and
`GetLast24hSleepSessions` query functions and their row types.

**Verification:** `go build ./...` passes with no errors or undefined references.

---

### Step 3 — Add sensor constants

**Action:** Add two constants to the constants block in `processor.go`:

```go
sensorDiaperHistory = "sensor.ada_diaper_history"
sensorSleepHistory  = "sensor.ada_sleep_history"
```

**Verification:** Constants are adjacent to `sensorSleepSessionMin` and the existing
history constant. `go build ./...` passes.

---

### Step 4 — Implement diaper history push

**Action:** Add to `processor.go`:

```go
// DiaperHistoryEntry is one element of the JSON array pushed as attributes on
// sensor.ada_diaper_history.
type DiaperHistoryEntry struct {
    ID        string `json:"id"`
    Timestamp string `json:"timestamp"`
    Type      string `json:"type"`
}

// pushDiaperHistory queries the last 24h of diaper events and pushes them as
// attributes on sensor.ada_diaper_history. Sensor state is the entry count.
func (p *Processor) pushDiaperHistory(ctx context.Context) {
    rows, err := p.q.GetLast24hDiapers(ctx)
    if err != nil {
        p.log.Warn("ada: query diaper history", slog.String("error", err.Error()))
        return
    }
    entries := make([]DiaperHistoryEntry, 0, len(rows))
    for _, r := range rows {
        entries = append(entries, DiaperHistoryEntry{
            ID:        uuid.UUID(r.ID.Bytes).String(),
            Timestamp: r.Timestamp.Time.UTC().Format(time.RFC3339),
            Type:      r.Type,
        })
    }
    attributes := map[string]any{
        "entries":      entries,
        "last_updated": time.Now().UTC().Format(time.RFC3339),
    }
    if err := p.ha.PushState(ctx, sensorDiaperHistory, strconv.Itoa(len(entries)), attributes); err != nil {
        p.log.Warn("ada: push diaper history", slog.String("error", err.Error()))
    }
}
```

Wire into the push pipeline:

* `pushDiaperSensors`: add `p.pushDiaperHistory(ctx)` at the end
* `pushDailyAggregates`: add `p.pushDiaperHistory(ctx)` after the diaper aggregate pushes

**Verification:** `go build ./...` passes. No unused imports.

---

### Step 5 — Implement sleep history push

**Action:** Add to `processor.go`:

```go
// SleepHistoryEntry is one element of the JSON array pushed as attributes on
// sensor.ada_sleep_history. EndTime and DurationS are omitted for active sessions.
type SleepHistoryEntry struct {
    ID        string  `json:"id"`
    StartTime string  `json:"start_time"`
    EndTime   *string `json:"end_time,omitempty"`
    SleepType string  `json:"sleep_type"`
    DurationS *int    `json:"duration_s,omitempty"`
}

// pushSleepHistory queries the last 24h of sleep sessions and pushes them as
// attributes on sensor.ada_sleep_history. Active sessions are included with
// end_time and duration_s omitted so the dashboard can surface ongoing sleeps.
// Sensor state is the total session count (including any active session).
func (p *Processor) pushSleepHistory(ctx context.Context) {
    rows, err := p.q.GetLast24hSleepSessions(ctx)
    if err != nil {
        p.log.Warn("ada: query sleep history", slog.String("error", err.Error()))
        return
    }
    entries := make([]SleepHistoryEntry, 0, len(rows))
    for _, r := range rows {
        e := SleepHistoryEntry{
            ID:        uuid.UUID(r.ID.Bytes).String(),
            StartTime: r.StartTime.Time.UTC().Format(time.RFC3339),
            SleepType: r.SleepType,
        }
        if r.EndTime.Valid {
            s := r.EndTime.Time.UTC().Format(time.RFC3339)
            e.EndTime = &s
        }
        if r.DurationS.Valid {
            d := int(r.DurationS.Int32)
            e.DurationS = &d
        }
        entries = append(entries, e)
    }
    attributes := map[string]any{
        "entries":      entries,
        "last_updated": time.Now().UTC().Format(time.RFC3339),
    }
    if err := p.ha.PushState(ctx, sensorSleepHistory, strconv.Itoa(len(entries)), attributes); err != nil {
        p.log.Warn("ada: push sleep history", slog.String("error", err.Error()))
    }
}
```

Wire into the push pipeline:

* `pushSleepStartedSensors`: add `p.pushSleepHistory(ctx)` at the end
* `pushSleepEndedSensors`: add `p.pushSleepHistory(ctx)` at the end
* `pushDailyAggregates`: add `p.pushSleepHistory(ctx)` after sleep aggregate pushes

**Notes:** `GetLast24hSleepSessions` returns `end_time` as `pgtype.Timestamptz` (nullable)
and `duration_s` as `pgtype.Int4` (nullable). Check the generated sqlc row type and adjust
the nil-checks (`r.EndTime.Valid`, `r.DurationS.Valid`) to match actual generated field names.

**Verification:** `go build ./...` passes.

---

### Step 6 — Add unit tests

**Action:** Add tests in `processor_test.go` for the two pure builder functions that can
be extracted or tested in isolation. Following the established pattern of testing pure
functions only, add:

* `buildDiaperHistory` (extracted from `pushDiaperHistory` if needed for testability,
  following the `buildFeedingHistory` pattern)
* A simple smoke test verifying a nil/empty rows slice produces an empty entries slice for
  each builder

**Verification:** `go test -tags=fast -race ./...` — all tests pass, no races.

---

### Step 7 — Commit

**Action:** Stage all changes and commit with a conventional commit message covering both
sensors.

**Verification:** Pre-commit hooks pass. `git log --oneline -3` shows the new commit.
`go test -tags=fast -race ./...` green.

---

## Rollback

Revert the commit and redeploy. No DB schema changes, no new NATS subjects, no
migrations. After rollback, `sensor.ada_diaper_history` and `sensor.ada_sleep_history`
will become unavailable in HA (`unknown`). Any HA frontend components reading these
sensors will need to handle the `unknown` state gracefully.

---

## Open Questions

_None — schema is confirmed, pattern is established, all data is available in existing
tables._
