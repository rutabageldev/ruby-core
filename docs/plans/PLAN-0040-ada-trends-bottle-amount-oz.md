# PLAN-0040 - Ada bottle trend: attribute unsplit `amount_oz` to a seg

* **Status:** Approved
* **Date:** 2026-07-22
* **Project:** ruby-core
* **Roadmap Item:** *(none — defect fix against #82/#161)*
* **Branch:** `fix/ada-trends-bottle-amount-oz`
* **Related ADRs:** ADR-0032 (trends acquisition), ADR-0043 (boundary-based Today rollover)

---

## Scope

The `feeding`/`bottle` trend view under-reports consumption because it sums only the
split columns (`breast_milk_oz`, `formula_oz`) and ignores `amount_oz`. Single-source
bottles are logged with `amount_oz` alone (`pkg/schemas/ada.go` — "AmountOz is set for
single-source bottles; BreastMilkOz and FormulaOz for mixed"), so every such feed
contributes zero to the chart. This plan attributes the unsplit amount to the correct
seg using the feed's source, and applies the same reconciliation rule to the Today
feeding-oz aggregate so the two projections agree on what a bottle's volume is.

**Out of scope:** bucket boundaries. The trend's calendar-midnight day buckets are
correct and verified against prod data (see Evidence); the residual difference between
a current-day bucket and `sensor.ada_today_feeding_oz` is ADR-0032 §5's deliberate,
bounded 19:00–24:00 divergence from ADR-0043, not a defect. Also out of scope: the
`breast`, `feeds`, `diapers`, `sleep`, and `tummy` views, which do not read bottle
amounts.

---

## Evidence

Prod `ruby_core`, week of 2026-07-19, ET calendar days. The `sum(split)` column
reproduces the reported `sensor.ada_trends` payload (14/10/14/9, grand 47) exactly,
which confirms bucket assignment is correct and the entire loss is unsplit `amount_oz`:

| day (ET) | trend today = `sum(milk+formula)` | truth = `sum(amount_oz)` |
|---|---|---|
| Sun 2026-07-19 | 14.00 | 19.00 |
| Mon 2026-07-20 | 10.00 | 18.00 |
| Tue 2026-07-21 | 14.00 | 19.00 |
| Wed 2026-07-22 (partial) | 9.00 | 12.00 |
| **grand** | **47.00** | **68.00** |

Repo-wide exposure at time of writing:

| source | rows | rows with unsplit residual | `sum(amount_oz)` | `sum(split)` |
|---|---|---|---|---|
| `bottle_formula` | 170 | 45 | 331.00 | 240.00 |
| `bottle_breast` | 4 | 4 | 8.00 | 0.00 |
| `mixed` | 1 | 0 | 0.00 | 1.00 |

Two consequences worth naming: the `milk` seg has never rendered non-zero for a
bottle-of-breastmilk feed (all 4 rows are amount-only), and the single `mixed` row has
`amount_oz = 0` with a populated split — the inverse defect, which makes
`sensor.ada_today_feeding_oz` (`SUM(amount_oz)`) *under*-count mixed bottles. Step 3
closes that direction too.

---

## Pre-conditions

* [x] Reproduction confirmed against prod data; ground truth for Tue 2026-07-21 agreed
      with the operator as 19 oz / 7 feeds.
* [x] Branch `fix/ada-trends-bottle-amount-oz` cut from `main`.
* [ ] `sqlc` available on PATH for step 3 regeneration (`make sqlc` or equivalent).

---

## Steps

### Step 1 — Add `bottleSegOz` reconciliation helper

**Action:** Add `bottleSegOz(source string, amountOz, milkOz, formulaOz float64) (milk, formula float64)`
to `services/engine/processors/ada/trends.go`. Seed `milk`/`formula` from the split
columns, then compute `residual := amountOz - (milk + formula)`. When `residual` exceeds
a float-noise epsilon, attribute it by `normalizeSource(source)`:

* `bottle_breast` / `breast_milk` → `milk`
* `bottle_formula` / `formula` → `formula`
* `mixed` → prorate across the existing split; if the split is empty, unattributable
* anything else (breast feed carrying a supplement residual) → unattributable

Unattributable residual is dropped and reported by the caller, never silently absorbed.
Clamping at zero means the `amount < split` anomaly cannot *reduce* a seg.

**Verification:** `go test ./services/engine/processors/ada -run TestBottleSegOz` passes
the table in step 4.

**Notes:** Attribution mirrors `supplementAmounts` (`processor.go:573`), which already
resolves source → seg at write time for supplements. This is the read-side counterpart
for rows that never went through that path.

### Step 2 — Wire it into `feedingEvents`

**Action:** Replace the `bottle` case in `feedingEvents` — currently
`segs = map[string]float64{"milk": r.BreastMilkOz, "formula": r.FormulaOz}` — with a
`bottleSegOz` call. Thread the processor logger (or return an `unattributed` signal) so
a dropped residual emits a `Warn` with the feeding id, source, and oz.

**Verification:** `go test ./services/engine/processors/ada -run TestFeedingEvents`
passes; a fixture row with `amount_oz=3, formula_oz=0, source=bottle_formula` yields
`{"milk":0,"formula":3}`.

**Notes:** The fix is per-row, so every bucket in the window is corrected by the same
code path — this satisfies the past-day audit requirement without separate work.

### Step 3 — Symmetric fix on the Today aggregate

**Action:** In `services/engine/processors/ada/store/queries/feedings.sql`, change
`GetTodayFeedingAggregates.total_oz` from `COALESCE(SUM(d.amount_oz), 0)` to
`COALESCE(SUM(GREATEST(COALESCE(d.amount_oz,0), COALESCE(d.breast_milk_oz,0) + COALESCE(d.formula_oz,0))), 0)`.
Regenerate with sqlc.

**Verification:** `git diff` on `feedings.sql.go` shows only the regenerated query text;
`go build ./...` passes. Against prod data the 2026-07-06 mixed row contributes 1.00
instead of 0.00.

**Notes:** This is what makes the two projections comparable at all — both sides now
answer "a bottle's volume is the greater of the recorded amount and its split."

### Step 4 — Unit tests

**Action:** Add table-driven cases to `services/engine/processors/ada/trends_test.go`,
matching the file's existing style:

1. amount-only formula bottle → all to `formula`
2. amount-only breast-milk bottle → all to `milk`
3. explicit split, amount equal to the sum → split preserved, no double-count
4. `mixed` with `amount_oz = 0` and a populated split → split preserved (no clamp damage)
5. partial residual from a supplement merge → residual attributed, split preserved
6. unattributable source with residual → residual dropped, warning path exercised
7. a week-shaped aggregation reproducing the Evidence table: 19/18/19/12, grand 68

**Verification:** `go test -tags=fast -race -short ./services/engine/processors/ada/...`
passes; case 7 asserts the exact repro numbers.

### Step 5 — Amend ADR-0032

**Action:** Amend ADR-0032 §4 to state the rule the ADR never specified: the `bottle`
view's `milk`/`formula` segs MUST reconcile to the feed's total volume, with unsplit
`amount_oz` attributed by feed source. Note in Consequences that the omission is what
allowed the undercount.

**Verification:** ADR-0032 §4 states the obligation in MUST language and the amendment
is dated.

**Notes:** ADR-0032 §5 (calendar-anchored windows) is untouched and remains in force.

### Step 6 — Live verification

**Action:** After deploy, re-fire
`ada.trends.query {metric:feeding, view:bottle, period:week, offset:0}` and read
`sensor.ada_trends`.

**Verification:** Buckets read Sun 19, Mon 18, Tue 19, Wed ≥ 12 (higher if more feeds
land), `totals.formula` + `totals.milk` equals the grand, and grand ≥ 68.

---

## Rollback

Revert the commit and redeploy. No migration, no stored derived state — both changes are
read-path only, so a revert restores the previous (undercounting) behaviour immediately
with no data to unwind.

---

## Open Questions

None. The boundary question is resolved: ADR-0032 §5 calendar buckets stand.
