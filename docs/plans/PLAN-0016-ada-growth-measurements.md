# PLAN-0016 — Ada: Growth Measurements

* **Status:** Approved
* **Date:** 2026-04-29
* **Project:** ruby-core (+ homeassistant, tracked separately)
* **Roadmap Item:** none (standalone feature)
* **Branch:** feat/ada-growth-measurements
* **Related ADRs:** ADR-0029 (stateful processors), ADR-0027 (subject naming)

---

## Scope

Implements the ruby-core side of growth measurement tracking for the Ada dashboard:
persistent storage, WHO LMS percentile computation, HA sensor push for latest values and
full history, and a gateway event route for growth log events.

Explicitly out of scope for this plan: all HA/dashboard frontend work (Log Growth modal,
growth chart component, blush color token, unit toggle UI). Those are tracked in the
homeassistant repo.

---

## Spec Review: Errors and Bad Assumptions

The following issues were identified in the spec before planning. These are corrections,
not open questions — they should be treated as overrides on the spec.

### 1. Event payload format is wrong

The spec shows:

```json
{ "event_type": "growth.measurement", "data": { ... } }
```

The existing gateway pattern uses a flat payload with an `"event"` key, not `"event_type"`,
and the data fields are flat at the top level of the payload (not nested under `"data"`).
The gateway's `Publish()` function routes by `payload["event"]` and passes the entire
payload map as the CloudEvent `Data`. A nested `"data"` field would require the processor
to unwrap an extra level of indirection.

**Correct event format:**

```json
{
  "event": "ada.growth.log",
  "weight_oz": 134.5,
  "length_in": 20.5,
  "head_circumference_in": 14.2,
  "source": "home",
  "timestamp": "2026-09-15T14:30:00Z",
  "logged_by": "user-id-here"
}
```

### 2. "Go service" implies a new standalone service — it must be the Ada processor

The spec says "A Go service computes percentiles." This must be the Ada processor within
the engine, not a new standalone service. ADR-0029 explicitly rejects new services for
domain features that require storage. Growth measurement logic lives in
`services/engine/processors/ada/`.

### 3. Monthly WHO cron check is unnecessary complexity

The spec proposes infrastructure to check for WHO table updates monthly/quarterly. WHO
Growth Standards for 0–2 years have been stable since 2006. Updates are effectively
never. The correct approach for a personal project is:

* Embed LMS tables as static JSON files in the binary (`//go:embed`)
* A WHO update requires editing the JSON file and cutting a new release
* No cron, no database storage of LMS values, no auto-refresh

### 4. "Source not surfaced in UI" contradicts chart tooltip spec

Section 1 says source "is stored but not currently surfaced in the UI — it's captured for
future use." Section 4 says chart tooltips show "exact value, percentile, date, and source
(home/pediatrician)." Source is surfaced. The spec should say source is captured and
displayed in tooltips but not surfaced in summary views (e.g., the main-page latest
measurement strip).

### 5. Unit preference — resolved as localStorage, no ruby-core involvement

The original spec described this as a per-user server-side preference, which had no
existing storage mechanism. This was resolved: unit preference lives in browser
localStorage and is a frontend-only concern. Ruby-core has no work to do here.

---

## Pre-conditions

* [x] Branch `feat/ada-growth-measurements` created from `main`
* [ ] Engine running and healthy in dev (`make dev-up`)
* [ ] Ada profile (`birth_at`) is set in dev — required for percentile age calculation
* [x] WHO LMS table data files sourced from WHO CDN, converted to imperial units, committed
      (`services/engine/processors/ada/who/` — 25 rows each, months 0–24)

---

## Steps

### Step 1 — Add event subject constant and Go schema struct

**Action:** In `pkg/schemas/ada.go`, add:

```go
const AdaEventGrowthLogged = "ha.events.ada.growth_logged"
```

Add a data struct:

```go
// AdaGrowthLoggedData is the payload for a logged growth measurement.
// Missing fields are omitted — a weight-only entry has no length_in or head_circumference_in.
// Timestamp is the measurement date/time (supports backdating). Source is "home" or "pediatrician".
type AdaGrowthLoggedData struct {
    WeightOz            *float64 `json:"weight_oz,omitempty"`
    LengthIn            *float64 `json:"length_in,omitempty"`
    HeadCircumferenceIn *float64 `json:"head_circumference_in,omitempty"`
    Source              string   `json:"source"`
    Timestamp           string   `json:"timestamp"` // RFC3339; measurement date
    LoggedBy            string   `json:"logged_by,omitempty"`
}
```

Note: pointer types for optional fields — nil means "not provided," not zero. This
prevents accidentally plotting a zero-value measurement.

**Verification:** `go build ./...` passes cleanly.

---

### Step 2 — Add gateway event route

**Action:** In `services/gateway/ada/publish.go`, add to `eventRoutes`:

```go
"ada.growth.log": schemas.AdaEventGrowthLogged,
```

**Verification:** `go build ./...` passes. Manual POST to the gateway with
`{"event": "ada.growth.log", "weight_oz": 134.5, "source": "home", "timestamp": "..."}` returns 202.

---

### Step 3 — PostgreSQL migration for growth_measurements

**Action:** Create
`services/engine/processors/ada/store/migrations/000005_growth_measurements.up.sql`:

```sql
-- growth_measurements stores all logged growth data for Ada.
-- Fields are nullable — a weight-only entry has NULL for length_in and head_circumference_in.
-- Percentiles are stored alongside raw values for fast display; they are recomputable from
-- the WHO LMS tables if the source data is updated.
-- This table is retained indefinitely (no 24h window, no deletion on data clear).
CREATE TABLE IF NOT EXISTS growth_measurements (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    measured_at           TIMESTAMPTZ NOT NULL,
    weight_oz             NUMERIC(7,2),
    length_in             NUMERIC(5,2),
    head_circumference_in NUMERIC(5,2),
    source                TEXT NOT NULL DEFAULT 'home',
    weight_pct            NUMERIC(5,2),
    length_pct            NUMERIC(5,2),
    head_pct              NUMERIC(5,2),
    logged_by             TEXT NOT NULL DEFAULT '',
    deleted_at            TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_growth_measurements_measured_at
    ON growth_measurements(measured_at)
    WHERE deleted_at IS NULL;
```

Create the corresponding down migration:
`services/engine/processors/ada/store/migrations/000005_growth_measurements.down.sql`:

```sql
DROP TABLE IF EXISTS growth_measurements;
```

**Verification:** Run `go test ./services/engine/processors/ada/store/...` with a live
Postgres instance (integration) to confirm the migration applies cleanly. Alternatively,
inspect the table exists after engine startup in dev.

---

### Step 4 — sqlc query file for growth operations

**Action:** Create `services/engine/processors/ada/store/queries/growth.sql`:

```sql
-- name: InsertGrowthMeasurement :one
INSERT INTO growth_measurements (
    measured_at, weight_oz, length_in, head_circumference_in,
    source, weight_pct, length_pct, head_pct, logged_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: GetLatestWeight :one
SELECT id, measured_at, weight_oz, weight_pct, source
FROM growth_measurements
WHERE weight_oz IS NOT NULL AND deleted_at IS NULL
ORDER BY measured_at DESC
LIMIT 1;

-- name: GetLatestLength :one
SELECT id, measured_at, length_in, length_pct, source
FROM growth_measurements
WHERE length_in IS NOT NULL AND deleted_at IS NULL
ORDER BY measured_at DESC
LIMIT 1;

-- name: GetLatestHeadCircumference :one
SELECT id, measured_at, head_circumference_in, head_pct, source
FROM growth_measurements
WHERE head_circumference_in IS NOT NULL AND deleted_at IS NULL
ORDER BY measured_at DESC
LIMIT 1;

-- name: GetAllGrowthMeasurements :many
SELECT id, measured_at, weight_oz, length_in, head_circumference_in,
       source, weight_pct, length_pct, head_pct, logged_by
FROM growth_measurements
WHERE deleted_at IS NULL
ORDER BY measured_at DESC;
```

Run `sqlc generate` from `services/engine/processors/ada/store/` to produce
`growth.sql.go`.

**Nullable parameter handling:** `InsertGrowthMeasurement` accepts nullable columns for
all six optional fields (weight_oz, length_in, head_circumference_in, weight_pct,
length_pct, head_pct). Add a small helper alongside the existing `numericFromFloat`:

```go
func numericFromFloatPtr(f *float64) pgtype.Numeric {
    if f == nil {
        return pgtype.Numeric{} // zero value = NULL
    }
    return numericFromFloat(*f)
}
```

Use this when building the `InsertGrowthMeasurementParams` in the handler for any field
sourced from a pointer in `AdaGrowthLoggedData` or a percentile that may not have been
computed.

**Verification:** Generated file compiles (`go build ./...`). Inspect generated code to
confirm nullable columns produce `pgtype.Numeric` parameters. Confirm
`numericFromFloatPtr(nil)` produces a `pgtype.Numeric` with `Valid = false`.

---

### Step 5 — WHO LMS data files

**Action:** Create directory `services/engine/processors/ada/who/`. Source the WHO Child
Growth Standards LMS parameter tables for girls (0–24 months, monthly intervals) from the
WHO website. Convert each to a JSON file:

* `who_weight_girls.json`
* `who_length_girls.json`
* `who_head_girls.json`

Each file stores M in **imperial units** (oz for weight, inches for length/head) so the
LMS formula can be applied directly to measurements without unit conversion:

```json
// who_weight_girls.json — M in oz (not kg); L varies per month
[
  {"age_days": 0,    "L": 0.3809,  "M": 114.0125, "S": 0.14171},
  {"age_days": 30.4, "L": 0.1714,  "M": 147.7027, "S": 0.13724},
  ...
  {"age_days": 730.5,"L": -0.2941, "M": 404.8569, "S": 0.12390}
]
// who_length_girls.json — M in inches (not cm); L=1.0 throughout
[{"age_days": 0, "L": 1.0, "M": 19.3495, "S": 0.03790}, ...]
// who_head_girls.json — M in inches (not cm); L=1.0 throughout
[{"age_days": 0, "L": 1.0, "M": 13.3381, "S": 0.03496}, ...]
```

**Corrections from spec v2 example values:**

* `L` for weight-for-age is NOT constant — it ranges from +0.3809 (birth) to -0.2941
  (24 months). The spec showed `L: -0.3521` as illustrative; it is wrong. Go code must
  read L from each row.
* `L` for length-for-age and head-for-age is 1.0 throughout.
* `M` is in oz/inches, not kg/cm as the spec example implied.

Files are already written. Age uses 1 month = 30.4375 days; 25 rows covering months 0–24.

Add a `tables.go` file in the `who/` package:

```go
package who

import (
    _ "embed"
    "encoding/json"
    "fmt"
    "math"
    "sort"
)

//go:embed who_weight_girls.json
var weightData []byte

//go:embed who_length_girls.json
var lengthData []byte

//go:embed who_head_girls.json
var headData []byte

// LMSRow is one age entry in a WHO LMS table.
type LMSRow struct {
    AgeDays float64 `json:"age_days"`
    L       float64 `json:"l"`
    M       float64 `json:"m"`
    S       float64 `json:"s"`
}

// Table is a parsed WHO LMS table, sorted by AgeDays.
type Table []LMSRow

// Load parses the embedded JSON data.
func loadTable(data []byte) (Table, error) { ... }

// Interpolate returns the interpolated L, M, S values for the given age in days.
// Uses linear interpolation between the two nearest table rows.
// Returns an error if ageDays is outside the table range.
func (t Table) Interpolate(ageDays float64) (L, M, S float64, err error) { ... }

// Percentile computes the WHO LMS percentile for a given measurement value and age.
// Returns 0–100.
// When |L| < 1e-6 (L approaches 0), uses the limit form Z = ln(value/M) / S to avoid
// a 0/0 division. This applies to weight-for-age around months 3–5 where L crosses zero.
func Percentile(t Table, ageDays, value float64) (float64, error) {
    L, M, S, err := t.Interpolate(ageDays)
    if err != nil {
        return 0, err
    }
    var z float64
    if math.Abs(L) < 1e-6 {
        z = math.Log(value / M) / S
    } else {
        z = (math.Pow(value/M, L) - 1) / (L * S)
    }
    return phi(z) * 100, nil
}

// phi is the standard normal CDF.
func phi(z float64) float64 {
    return 0.5 * (1 + math.Erf(z/math.Sqrt2))
}

// Package-level initialized tables (loaded once at package init).
var (
    WeightTable Table
    LengthTable  Table
    HeadTable    Table
)

func init() {
    var err error
    if WeightTable, err = loadTable(weightData); err != nil {
        panic(fmt.Sprintf("who: load weight table: %v", err))
    }
    if LengthTable, err = loadTable(lengthData); err != nil {
        panic(fmt.Sprintf("who: load length table: %v", err))
    }
    if HeadTable, err = loadTable(headData); err != nil {
        panic(fmt.Sprintf("who: load head table: %v", err))
    }
}
```

**Verification:** Unit tests in `who/tables_test.go` confirm:

* Known WHO reference percentile values match computed values within ±0.5%
* Interpolation between two known table rows produces a value between the bounding rows
* Age outside table range returns an error

---

### Step 6 — Growth event handler in Ada processor

**Action:** Add `AdaEventGrowthLogged` to the `Subscriptions()` return slice in
`processor.go`. Add a `case schemas.AdaEventGrowthLogged` in `ProcessEvent()` that calls
a new `handleGrowthLogged(ctx, evt)` method.

Implement `handleGrowthLogged`:

1. Unmarshal the payload into `AdaGrowthLoggedData`.
2. Parse `Timestamp` as RFC3339; return error on parse failure.
3. Load Ada's birth date from `ada_profile` (call `GetProfile`). On `pgx.ErrNoRows` OR
   any DB error, log a warning and return nil (soft-fail — measurement persists without a
   percentile; don't propagate a DB error that would cause an unnecessary NAK+retry).
4. Compute `ageDays = measuredAt.Sub(birthAt).Hours() / 24`.
5. If `ageDays < 0` (measurement backdated before birth date), log a warning and store nil
   percentiles — don't attempt LMS computation on a negative age.
   If `ageDays > 730`, log a warning and store nil percentiles — outside the WHO table range.
   Otherwise, compute percentile using the appropriate WHO table for each provided measurement.
6. Call `InsertGrowthMeasurement` with raw values and computed percentiles.
7. Call `pushGrowthSensors(ctx)`.

Advisory-only server-side validation: log a Warn if a measurement falls outside these
ranges, then persist regardless. Never reject the event.

* Weight: 4oz–480oz; Length: 14in–40in; Head circumference: 10in–20in.
The HA frontend is authoritative for user-facing validation (percentile-based prompt).

**Verification:** Unit test covers the handler with a mocked store: weight-only entry,
length-only entry, all-three entry, missing profile (soft-fail), and age-out-of-range
(warning but persists). `go test -tags=fast -race ./...` passes.

---

### Step 7 — HA sensor push for latest measurements

**Action:** Add constants for the new sensor entity IDs:

```go
sensorLatestWeight            = "sensor.ada_latest_weight"
sensorLatestLength            = "sensor.ada_latest_length"
sensorLatestHeadCircumference = "sensor.ada_latest_head_circumference"
```

Implement `pushGrowthSensors(ctx)`:

* Query `GetLatestWeight`, `GetLatestLength`, `GetLatestHeadCircumference`.
* If a query returns `pgx.ErrNoRows`, skip that sensor (no data yet — not an error).
* Percentile values are rounded to 1 decimal place before push.
* Push each sensor with the following exact shapes:

**`sensor.ada_latest_weight`**

```
state: "134.50"   // total oz, 2 decimal places
attributes: {
  "weight_oz": 134.5,           // float — HA card computes lb/oz split from this
  "percentile": 50.2,           // float, 1 decimal — ABSENT if no birth profile
  "measured_at": "2026-09-15T14:30:00Z",
  "source": "home"              // "home" | "pediatrician"
}
```

Do NOT pre-split into `weight_lb` / `weight_rem_oz`. The HA card computes those via
`Math.floor(oz / 16)` and `oz % 16`.

**`sensor.ada_latest_length`**

```
state: "20.50"    // inches, 2 decimal places
attributes: {
  "length_in": 20.5,
  "percentile": 62.1,           // ABSENT if no birth profile
  "measured_at": "2026-09-15T14:30:00Z",
  "source": "home"
}
```

**`sensor.ada_latest_head_circumference`**

```
state: "14.20"    // inches, 2 decimal places
attributes: {
  "head_circumference_in": 14.2,
  "percentile": 45.3,           // ABSENT if no birth profile
  "measured_at": "2026-09-15T14:30:00Z",
  "source": "home"
}
```

`percentile` is omitted (not null) when the birth profile has not been set. The HA
frontend must handle a missing `percentile` key gracefully.

Add `pushGrowthSensors(ctx)` to `refreshAllSensors(ctx)` so sensors are restored on
engine restart and HA reconnect.

**Verification:** In dev, log a growth event and verify all three sensors appear in HA
developer tools with correct state and attributes.

---

### Step 8 — HA sensor push for growth history

**Action:** Add constant `sensorGrowthHistory = "sensor.ada_growth_history"`.

The `GetAllGrowthMeasurements` query was already written in Step 4 as `ORDER BY measured_at DESC`.

Implement `pushGrowthHistory(ctx)`:

* Call `GetAllGrowthMeasurements` (all-time, descending by `measured_at`).
* Partition results into three separate slices: one per measurement type. A single DB
  row with all three values populated produces **one entry in each relevant slice**. A
  weight-only row produces one entry in `weight` only.
* Each slice contains only fields relevant to that type. No cross-type fields, no null
  or zero placeholders.

**`sensor.ada_growth_history`**

```
state: "12"   // count of distinct DB rows (measurement events), not total entries across arrays
attributes: {
  "weight": [
    {"id": "uuid", "measured_at": "2026-09-15T14:30:00Z", "weight_oz": 134.5, "percentile": 55.2, "source": "home"},
    ...
  ],
  "length": [
    {"id": "uuid", "measured_at": "2026-09-10T10:00:00Z", "length_in": 20.5, "percentile": 62.1, "source": "pediatrician"},
    ...
  ],
  "head": [
    {"id": "uuid", "measured_at": "2026-09-10T10:00:00Z", "head_circumference_in": 14.2, "percentile": 45.3, "source": "pediatrician"},
    ...
  ],
  "last_updated": "2026-09-15T14:30:00Z"
}
```

Key invariants:

* Arrays are sorted **descending** by `measured_at` (most recent first) — the history
  list renders directly without client-side filtering or sorting.
* A measurement row with weight only appears in `weight` only; it does not appear in
  `length` or `head` with null fields.
* A measurement row with all three values populated produces one entry in each array.
* `percentile` is omitted (not null) if the birth profile was not set at ingest time.
* `id` is always present (UUID string) — needed for chart ↔ list highlight interaction.
* `source` is always present.

Call `pushGrowthHistory(ctx)` from inside `pushGrowthSensors(ctx)` (so it fires on every
new measurement event). In `refreshAllSensors`, call `pushGrowthSensors` directly — do
NOT also call `pushGrowthHistory` directly from `refreshAllSensors`, as that would fire
it twice per refresh. The call chain is: `refreshAllSensors → pushGrowthSensors →
pushGrowthHistory`.

**Verification:** In dev, log several growth measurements (weight-only, all-three, one
pediatrician) and verify:

* `sensor.ada_growth_history.weight` array has entries sorted most-recent-first
* A weight-only entry appears only in `weight`, not in `length` or `head`
* A full-measurement entry appears in all three arrays

---

### Step 9 — WHO curve data sensor push

**Action:** Add constant `sensorGrowthCurves = "sensor.ada_growth_curves"`.

On engine startup (called once from `refreshAllSensors`), push BOTH the raw LMS tables
AND precomputed band values. The LMS tables are required by the HA frontend for client-side
percentile computation during input validation. The band values are required for chart rendering.

**Band computation:** Sample at 7-day intervals for 0–91 days (days 0, 7, 14, …, 91 →
14 points), then 30-day intervals from 91–730 days (days 91, 121, 152, …, 730 → 22
points) = 36 data points per curve. The 30-day series starts at 91 (not 121) so there is
no gap at the 3-month transition. Use the inverse LMS formula:
`value = M * (1 + L * S * Z)^(1/L)` where Z = probit(percentile/100). Apply the same
L→0 guard as in `Percentile`: when |L| < 1e-6, use `value = M * exp(Z * S)`. Implement
`probit(p)` via rational approximation (Beasley-Springer-Moro) — no external dependency.

Implement `pushGrowthCurves(ctx)` and call it explicitly from `refreshAllSensors` (after
`pushGrowthSensors`). It fires on engine startup, HA reconnect, and the 4-hour safety net
— all via `refreshAllSensors`. The data is static so repeated pushes are harmless.

**`sensor.ada_growth_curves`**

```
state: "ok"
attributes: {
  "weight": {
    "lms": [
      {"age_days": 0,    "L": 0.3809,  "M": 114.0125, "S": 0.14171},
      {"age_days": 30.4, "L": 0.1714,  "M": 147.7027, "S": 0.13724},
      ...
    ],
    "bands": {
      "p3":  [[0, 83.2], [7, 91.4], ..., [730, 450.1]],
      "p15": [[0, 97.3], ...],
      "p50": [[0, 113.7], ...],
      "p85": [[0, 133.4], ...],
      "p97": [[0, 151.0], ...]
    }
  },
  "length": {
    "lms": [...],
    "bands": { "p3": [...], "p15": [...], "p50": [...], "p85": [...], "p97": [...] }
  },
  "head": {
    "lms": [...],
    "bands": { "p3": [...], "p15": [...], "p50": [...], "p85": [...], "p97": [...] }
  }
}
```

Value units and precision:

* `lms` values: 4 decimal places (matches WHO table precision)
* Band `[age_days, value]` pairs: `age_days` is integer; `value` is total oz (weight) or
  inches (length/head) rounded to 2 decimal places — matching storage units so the frontend
  overlays Ada's data points without unit conversion. Frontend divides weight by 16 for lb axis.

**Verification:** Inspect `sensor.ada_growth_curves` attributes in HA developer tools after
engine startup. Spot-checks:

* `weight.lms[0].M` ≈ `114.01` (oz — WHO 3.2322 kg × 35.274; NOT kg)
* `weight.bands.p50[0]` ≈ `[0, 114.01]` (50th percentile at birth = the median = M)
* `length.lms[0].M` ≈ `19.35` (inches — WHO 49.15 cm ÷ 2.54)
* Confirm `lms`, `bands`, all 5 percentile keys, and all 3 measurement types are present
* Confirm `weight.bands` has 36 data points; first at age 0, transition at 91, last at 730

---

### Step 10 — Commit and handoff

**Action:** Commit all changes. Verify pre-commit hooks pass cleanly. Notify when ready
to push.

**Verification:** `go build ./...`, `go test -tags=fast -race ./...`, and
`golangci-lint run ./...` all pass. Pre-commit hook passes.

---

## Rollback

Steps 1–2 (event constant and gateway route): revert the commit and redeploy the gateway.
No data loss.

Steps 3–4 (DB migration): run the down migration
(`services/engine/processors/ada/store/migrations/000005_growth_measurements.down.sql`)
to drop the `growth_measurements` table. All persisted growth measurements are lost.
Revert and redeploy the engine.

Steps 5–10: revert and redeploy. No additional state beyond the DB table.

Growth measurements are the only new stateful data. If rollback happens after real data
has been entered, the down migration destroys it — communicate this risk before deploying
to production.

---

## Resolved Decisions

These were open questions at draft time, resolved before execution. See spec v2 for the
full resolved-questions log.

* **Historical data delivery:** Sensor attributes on `sensor.ada_growth_history`, split by
  measurement type (weight/length/head arrays), sorted descending. No gateway REST endpoint.
* **WHO curve delivery:** `sensor.ada_growth_curves` includes both `lms` tables (for
  client-side percentile validation) and precomputed `bands` (for chart rendering).
* **Unit preference:** Browser localStorage. No ruby-core involvement.
* **Server-side validation:** Advisory warn-only (never reject) for out-of-range values.
  HA frontend is authoritative for user-facing validation.
* **Weight lb/oz split:** NOT pre-computed by ruby-core. HA card computes from `weight_oz`.
* **DOB source:** `ada_profile.birth_at` from Postgres.
* **Sensor entity IDs:** Confirmed by HA agent.
* **History sort order:** Descending (most recent first) per HA agent requirement.
