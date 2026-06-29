#!/usr/bin/env bash
# gen-docs-indexes.sh — regenerate the ADR index and the archived-plans table from the
# filesystem so they cannot go stale (drift #115, #116). Run via `make docs-index` after
# adding an ADR or archiving a plan. Idempotent: running twice produces no diff.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Title = the H1 with the "ADR-NNNN"/"PLAN-NNNN" prefix and its separator (space, -, –, —)
# stripped. Status = the value on the "**Status:**" line, trimmed before any "(...)".
title_of()  { grep -m1 '^# ' "$1" | sed -E 's/^# (ADR|PLAN)-[0-9]+[^A-Za-z0-9(`]*//'; }
status_of() { grep -m1 -E '\*\*Status' "$1" | sed -E 's/.*Status:\*\*[[:space:]]*//; s/[[:space:]]*\(.*//'; }
num_of()    { basename "$1" | grep -oE '[0-9]{4}' | head -1; }

# --- ADR index → docs/ARCHITECTURE_DECISIONS.md (every docs/adr/NNNN-*.md, not template.md) ---
{
  echo "# ADR Index"
  echo
  echo "Generated from \`docs/adr/*.md\` by \`make docs-index\` — do not hand-edit; run the"
  echo "target after adding an ADR. See the individual ADRs for full context."
  echo
  echo "| ADR | Title | Status |"
  echo "|---|---|---|"
  for f in docs/adr/[0-9]*.md; do
    printf '| [%s](adr/%s) | %s | %s |\n' "$(num_of "$f")" "$(basename "$f")" "$(title_of "$f")" "$(status_of "$f")"
  done
} > docs/ARCHITECTURE_DECISIONS.md

# --- archived-plans table → between the markers in docs/plans/README.md ---
readme=docs/plans/README.md
table=$(
  echo "| Plan | Title | Status |"
  echo "|---|---|---|"
  for f in docs/plans/archived/PLAN-[0-9]*.md; do
    printf '| [%s](archived/%s) | %s | %s |\n' "$(num_of "$f")" "$(basename "$f")" "$(title_of "$f")" "$(status_of "$f")"
  done
)
awk -v table="$table" '
  /<!-- BEGIN archived/ { print; print ""; print table; print ""; skip=1; next }
  /<!-- END archived/   { skip=0 }
  !skip
' "$readme" > "$readme.tmp" && mv "$readme.tmp" "$readme"

echo "docs-index: regenerated docs/ARCHITECTURE_DECISIONS.md and the archived table in $readme"
