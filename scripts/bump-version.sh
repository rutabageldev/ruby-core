#!/usr/bin/env bash
# bump-version.sh — increment the project semver and update all version references.
#
# Usage: scripts/bump-version.sh [major|minor|patch]
# Default level: patch
#
# Updates:
#   deploy/prod/compose.prod.yaml
#   deploy/staging/compose.staging.yaml
#   deploy/prod/.env.example
#   deploy/staging/.env.example
#   deploy/prod/.env      (if present; gitignored)
#   deploy/staging/.env   (if present; gitignored)

set -euo pipefail

LEVEL="${1:-patch}"

# Canonical source of truth for the current version.
CANONICAL="deploy/prod/.env.example"

CURRENT=$(grep '^VERSION=' "$CANONICAL" | cut -d= -f2 | tr -d 'v')
if [[ -z "$CURRENT" ]]; then
  echo "error: could not read VERSION from $CANONICAL" >&2
  exit 1
fi

IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT"

case "$LEVEL" in
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  patch) PATCH=$((PATCH + 1)) ;;
  *)
    echo "Usage: $0 [major|minor|patch]" >&2
    exit 1
    ;;
esac

OLD="v${CURRENT}"
NEW="v${MAJOR}.${MINOR}.${PATCH}"

FILES=(
  deploy/prod/compose.prod.yaml
  deploy/staging/compose.staging.yaml
  deploy/prod/.env.example
  deploy/staging/.env.example
)

# Also update local .env files when present (gitignored; must stay in sync).
[[ -f deploy/prod/.env     ]] && FILES+=(deploy/prod/.env)
[[ -f deploy/staging/.env  ]] && FILES+=(deploy/staging/.env)

echo "Bumping ${OLD} → ${NEW} across ${#FILES[@]} files"

for f in "${FILES[@]}"; do
  sed -i "s/${OLD}/${NEW}/g" "$f"
  echo "  updated $f"
done

echo "Done. Next step: commit and tag ${NEW}."
