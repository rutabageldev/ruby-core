# PLAN-0036 - Docs / Index Hygiene

* **Status:** Complete
* **Date:** 2026-06-29
* **Project:** ruby-core
* **Roadmap Item:** none (drift cleanup)
* **Branch:** docs/index-hygiene
* **Related ADRs:** ADR-0043 (new, written by this plan)

---

## Scope

Close the docs/index-hygiene issue cluster and make it non-recurring. In-scope: write the
missing ADR for the bedtime-boundary Today rollover (#32); bring `ARCHITECTURE_DECISIONS.md`
current through ADR-0043 (#115); index the 25 archived plans in `docs/plans/README.md` (#116);
and add a generator + `make docs-index` so both indexes regenerate from the filesystem and can't
re-stale. Out of repo: #118 is a 2-line fix in the global `~/.claude/CLAUDE.md`
(`docs/plans/archive/` → `archived/`), done as a separate edit. Not in scope: rewriting ADR
content, changing the ADR/plan templates beyond a one-line "run `make docs-index`" note.

---

## Pre-conditions

* [x] ADRs exist through 0042; `ARCHITECTURE_DECISIONS.md` lists only 0001–0028 (#115).
* [x] 25 archived plans (0010–0035) exist under `docs/plans/archived/`, unindexed (#116).
* [x] Bedtime-boundary is implemented (`computeTodayBoundary`, `sensor.ada_today_boundary`,
      `ada_config.bedtime_hhmm`, `GREATEST(start_time,@boundary)` clipping) — a real, shipped
      decision to document (#32). Next ADR number is **0043**.

---

## Steps

### Step 1 — Commit this plan

**Action:** Author this file + index it (it'll appear in the regenerated active-plans table).
**Verification:** pre-commit passes; committed on the branch.

### Step 2 — ADR-0043 (#32)

**Action:** Write `docs/adr/0043-ada-boundary-based-today-rollover.md` (mirror the 0034 format:
Status/Date/Supersedes; Context, Alternatives Considered, Decision, Consequences split). Document
the shipped decision: Today aggregates reset at the configurable bedtime (`ada_config.bedtime_hhmm`,
default `19:00`) computed by `computeTodayBoundary`; a daily ticker refreshes aggregates at the
boundary (not UTC midnight); overnight sessions are clipped with `GREATEST(start_time,@boundary)`;
the engine pushes `sensor.ada_today_boundary`. Alternatives: UTC midnight / local calendar
midnight / HA-computed — rejected. Status **Accepted** (documents v0.10.0).
**Verification:** markdownlint passes; file follows the ADR template sections.

### Step 3 — Docs-index generator + `make docs-index` (#115, #116)

**Action:** Add `scripts/gen-docs-indexes.sh` that regenerates, from the filesystem:

* `docs/ARCHITECTURE_DECISIONS.md` — a complete table of every `docs/adr/NNNN-*.md` (number ·
  title from the H1 · status from the `**Status:**` line · link), excluding `template.md`.
* the **Archived** table in `docs/plans/README.md` — every `docs/plans/archived/PLAN-*.md`
  (number · title · status), preserving the existing hand-maintained active-plans table above it
  (regenerate only the section under an `<!-- BEGIN archived -->`/`<!-- END archived -->` marker).
Add `make docs-index` (calls the script) to the Makefile, and a one-line note in the ADR + plan
templates / repo CLAUDE.md docs section to run it after adding an ADR/plan. Idempotent (running
twice is a no-op).
**Verification:** `make docs-index` then `git diff --exit-code` is clean on a second run;
`ARCHITECTURE_DECISIONS.md` lists 0001–0043 with correct titles/statuses; `plans/README.md` lists
all 25 archived plans; markdownlint passes.

### Step 4 — #118 global file (separate, not in the PR)

**Action:** Edit `~/.claude/CLAUDE.md` lines ~207 and ~237: `docs/plans/archive/` → `archived/`.
**Verification:** `grep -n "docs/plans/archive[^d]" ~/.claude/CLAUDE.md` returns nothing.

### Step 5 — Pre-Push + close issues

**Action:** Archive this plan to `docs/plans/archived/` (status Complete) as the final commit
(it'll be in the regenerated archived table). Run pre-commit/markdownlint. PR body uses
`Closes #115`, `Closes #116`, `Closes #32` (avoid the tracking-debt pattern). Close #118 directly
after the global edit with a note.
**Verification:** pre-commit clean; on merge the three repo issues auto-close; #118 closed.

---

## Rollback

Docs-only. Revert the PR (no code/schema/deploy). The `~/.claude/CLAUDE.md` edit is a 2-line
revert. Generated indexes can be regenerated at any time with `make docs-index`.

---

## Open Questions

None. (Decisions: durable generator approach; I edit the global file for #118 — both confirmed.)

---

## Completion Notes (2026-06-29)

Done on `docs/index-hygiene`; markdownlint + shellcheck clean. Commits: `4c1586e` (ADR-0043),
`f1c7758` (generator `scripts/gen-docs-indexes.sh` + `make docs-index` + regenerated
ARCHITECTURE_DECISIONS.md/plans-README). The generator is idempotent and its output is
markdownlint-clean, so the indexes can't re-stale. #118 fixed directly in the global
`~/.claude/CLAUDE.md` (both `archive/` → `archived/`) and closed. PR closes #115/#116/#32.
