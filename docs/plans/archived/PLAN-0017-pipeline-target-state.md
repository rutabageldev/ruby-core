# PLAN-0017 — CI/CD Pipeline: Target State

* **Status:** Approved
* **Date:** 2026-04-29
* **Project:** ruby-core
* **Roadmap Item:** none (infrastructure)
* **Branch:** feat/pipeline-streamline (suggested)
* **Related ADRs:** ADR-0013 (CI/CD test gates — evolves), ADR-0016 (release promotion — evolves)

---

## Scope

Replace the current "build-everywhere, deploy-manually" pipeline with a tag-driven auto-promote flow. Five goals:

1. Cut wall-clock from merge → prod from ~7–10 min to ~3 min on typical changes
2. Eliminate the manual `make deploy-prod` step — tag becomes the trigger, prod the destination
3. Add a stability watcher to close the delayed-failure detection gap
4. Shift fast checks to a local pre-push hook so CI becomes an authoritative backstop, not the primary feedback loop
5. Stop tracking version in repo files — the git tag is the version

Goals 1–3 reduce ceremony. Goal 4 keeps feedback warm-cache-fast and removes the "wait for cloud CI to tell you about a missing gofumpt run" pattern. Goal 5 eliminates the "forgot to bump VERSION → CI rerun" pain. Net: faster *and* better-defended *and* no manual versioning.

---

## Confidence model

Four authoritative gates. The pre-push hook is a fast-feedback layer in front of the first gate, not a gate itself (bypassable with `--no-verify`).

| Layer | Catches | On failure | When |
|------|---------|------------|------|
| **Local pre-push hook** *(new — fast feedback, not a gate)* | Lint regressions and unit-test breaks spanning the full branch | Blocks push (bypassable with `--no-verify`) | Every `git push` |
| **PR CI** *(authoritative)* | Same as hook + integration + security | Block merge | PR opened/updated |
| **Staging smoke** | Image start-up, NATS chain, Vault auth | Tag fails to promote; HA notify | After image build |
| **Prod smoke** | Traefik routing, live HA auth, network attachment | Auto-rollback to previous version | After prod compose up |
| **Stability watcher** *(new)* | Delayed failures: leaks, wedged consumers, error-rate spikes | HA notify; optional auto-rollback | Async, 10 min after prod deploy |

Stability watcher and the local pre-push hook are the only additions. Everything else exists; we're just removing redundancy around them.

---

## The flow

```mermaid
flowchart TD
    PUSH[git push] --> HOOK{pre-commit<br/>pre-push stage<br/>lint-changed + unit}
    HOOK -->|fail| FIX[fix locally]
    FIX --> PUSH
    HOOK -->|pass| PR[PR opened]
    PR --> PFL[paths-filter]
    PFL --> CHG{changes?}
    CHG -->|go code| CI[lint + unit + integration + security + docker-build]
    CHG -->|deploy/configs only| LITE[lint + security]
    CHG -->|docs only| SKIP[skip downstream]
    CI --> MERGE[merge to main]
    LITE --> MERGE
    SKIP --> MERGE
    MERGE --> TAG[release-please PR<br/>or git tag vX.Y.Z]
    TAG --> BUILD[build + push to GHCR<br/>matrix, gha cache]
    BUILD --> STG[deploy staging<br/>compose up -d, warm]
    STG --> SMK1{staging smoke}
    SMK1 -->|fail| ABORT[abort + HA notify]
    SMK1 -->|pass| PROD[deploy prod<br/>pull, up, SIGHUP, smoke]
    PROD --> SMK2{prod smoke}
    SMK2 -->|fail| RB[auto-rollback]
    SMK2 -->|pass| WATCH[stability watcher<br/>10 min, async]
    WATCH --> DONE[done]
    WATCH -.->|degradation| ALERT[HA notify]
```

---

## Safe push model — prerequisites

This plan enables Claude to push feature and fix branches directly, removing the manual push step from the collaboration workflow. Two controls make this safe: a remote enforcement layer that cannot be bypassed, and a local enforcement layer that catches mistakes before they reach GitHub. Both must be in place before Claude is granted push credentials.

### A. GitHub branch protection on `main` *(prerequisite — configure first)*

GitHub branch protection is the authoritative enforcement layer. Even with `contents: write` on the repo, branch protection rules block direct push to `main` for all actors — no override exists at the PAT scope level.

Configure in repo **Settings → Branches → Add branch protection rule**, pattern `main`:

* [x] **Require a pull request before merging** — no direct push path
* [x] **Require status checks to pass before merging** — add: `lint`, `unit-tests`, `integration-tests`
* [x] **Require branches to be up to date before merging**
* [x] **Do not allow bypassing the above settings** — this applies to admins too; the owner can temporarily disable the rule via the UI if an emergency bypass is ever needed

This is the only control that is truly non-bypassable by Claude. The local hook (below) adds friction but cannot be relied upon alone — `--no-verify` defeats it.

### B. Claude push credential *(prerequisite — set up before first Claude push)*

Create a **fine-grained PAT** at github.com/settings/personal-access-tokens/new:

* **Resource owner:** rutabageldev
* **Repository access:** Only selected repositories → ruby-core
* **Permissions:**
  * Contents → **Read and Write** (enables branch create/push)
  * Metadata → Read (implicitly required)
  * Everything else → No access
* **Expiry:** Set an expiry (90 days recommended); calendar a renewal

Store in Vault using the ruby-core-writer token:

```bash
VAULT_ADDR=https://127.0.0.1:8200 \
VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt \
VAULT_TOKEN=$VAULT_TOKEN_RUBY_CORE_WRITER \
vault kv put secret/ruby-core/github push_token=ghp_...
```

Claude reads the token at push time using the standard ruby-core reader token from `deploy/dev/.env`. Push syntax:

```bash
PUSH_TOKEN=$(VAULT_ADDR=https://127.0.0.1:8200 \
  VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt \
  vault kv get -field=push_token secret/ruby-core/github)

git push "https://x-access-token:${PUSH_TOKEN}@github.com/rutabageldev/ruby-core.git" \
  HEAD:refs/heads/<branch-name>
```

The fine-grained PAT cannot bypass branch protection even with `contents: write`. Attempting to push directly to `main` returns HTTP 403 from GitHub — the local hook (below) is belt-and-suspenders, not the primary control.

**Workflow file constraint:** GitHub requires an additional `workflow` scope to push changes to `.github/workflows/`. The PAT intentionally omits this scope — keeping CI/CD pipeline modifications in human hands is the right security boundary. Branches that touch `.github/workflows/` must be pushed manually by the owner. All other branches (Go code, scripts, configs, docs) can be pushed by Claude via the PAT.

### C. Local main-push guard *(belt-and-suspenders — catches accidental pushes before they reach GitHub)*

Added to `.pre-commit-config.yaml` in the pre-push stage alongside the lint and test hooks (see Section 1). Raises an error immediately if the current branch is `main`, before any network call is made. Uses `always_run: true` so it fires even on doc-only pushes where no files match the Go type filter.

---

## What changes

### 1. Local-first feedback — pre-push stage in `.pre-commit-config.yaml` *(new)*

Fast checks belong on the dev box, not in cloud CI. The warm environment already has Go modules cached, golangci-lint cache populated, and test binaries compiled. CI cold-starts pay all those costs every run — same checks, but locally they take seconds; in CI they take ~90s.

The point isn't to replace CI — CI remains the authoritative gate. The point is to find issues *before* you push, so by the time CI runs you already know it'll pass.

**Tool: the existing pre-commit framework.** The repo already runs pre-commit on commit (golangci-lint, gitleaks, hadolint, yamllint, fast unit tests, etc.). There is no reason to introduce a second hook manager. pre-commit supports `stages: [pre-push]` natively — the three new hooks drop straight in as a pre-push extension to the existing config.

**Hook: pre-*push*, not pre-commit.** You commit too frequently for heavy-per-commit checks to be tolerable. The per-commit hooks catch format/secret issues immediately. The pre-push stage runs once per push regardless of commit count and checks the *cumulative* delta against `origin/main`.

Add to `.pre-commit-config.yaml`:

```yaml
- repo: local
  hooks:
    - id: no-push-to-main
      name: Prevent direct push to main
      language: system
      entry: bash -c 'branch=$(git rev-parse --abbrev-ref HEAD);
        if [ "$branch" = "main" ]; then
          echo "ERROR: direct push to main is not allowed — open a PR.";
          exit 1;
        fi'
      stages: [pre-push]
      pass_filenames: false
      always_run: true
    - id: golangci-lint-from-main
      name: golangci-lint (changed since main)
      language: system
      entry: golangci-lint run --new-from-rev=origin/main --timeout=2m
      types: [go]
      stages: [pre-push]
      pass_filenames: false
    - id: go-test-fast-push
      name: go test -tags=fast (pre-push)
      language: system
      entry: go test -tags=fast -race -short ./...
      types: [go]
      stages: [pre-push]
      pass_filenames: false
```

`no-push-to-main` uses `always_run: true` so it fires on every push regardless of file changes. It is listed first so it fails fast before lint or tests run. fmt-check is omitted — already enforced at commit time by the existing golangci-lint hook.

One-time install after adding the hooks: `pre-commit install --hook-type pre-push`. Bypass for emergencies: `git push --no-verify`.

`--new-from-rev=origin/main` lints only code changed relative to main — typically <5s warm. A docs-only push skips the Go type filter and just goes through. Integration tests stay out of the hook (testcontainers spin-up doesn't belong in a 30-second feedback loop) — they live in CI.

Expected wall-clock: 10–30s warm. CI runs the same checks plus integration + security as the authoritative backstop.

---

### 2. Version management — git tag is the source of truth *(new)*

Today version lives in four committed files (both `.env.example`s and both `compose.*.yaml` files) plus two gitignored `.env`s on the host. The compose files don't hold the canonical version but they do contain stale `${VERSION:-v0.7.0}` default literals that the bump script doesn't touch and which would silently downgrade prod if VERSION were ever unset. `bump-version.sh` papers over multi-source-of-truth with sed and forces a separate manual ceremony as a distinct gesture from the code change. Hence: forgetting the bump → CI rerun.

Reframe: **version doesn't belong in code PRs.** Version is a property of releases, not commits. Track it in exactly one place — the git tag — and have everything else read from it.

**a. `compose.prod.yaml` and `compose.staging.yaml`: require VERSION, fail loud if missing.**

Replace `${VERSION:-v0.7.0}` with `${VERSION:?VERSION env var required}` in both prod and staging compose files. The `:?` syntax makes compose error out clearly when VERSION is unset, instead of silently falling back to a five-version-old image. `compose.dev.yaml` and `compose.air.yaml` build images locally rather than pulling tagged releases — leave them unchanged. Smoke-verify with `make dev-services-up` and `make dev-air-up` after the change.

**b. Remove `VERSION=` lines from `.env.example` files.**

Replace with a comment:

```bash
# VERSION is supplied at deploy time from the git tag.
# CI sets it from github.ref_name. Locally, deploy-prod.sh derives it via git describe.
```

**c. `deploy-prod.sh` and `deploy-staging.sh` derive VERSION from git tag if unset.**

Add near the top:

```bash
VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --abbrev=0 2>/dev/null || true)}"
[ -z "$VERSION" ] && { echo "ERROR: VERSION not set and no git tag found" >&2; exit 1; }
```

CI passes `VERSION=${{ github.ref_name }}` explicitly, so this branch matters only for manual `make deploy-prod` runs — and "deploy whatever the latest tag is" is the right default.

**d. Delete `scripts/bump-version.sh` and `make bump-version`.**

Their job no longer exists. Releasing becomes:

```bash
git tag v0.11.2 && git push --tags
```

**e. Adopt release-please.**

(Was "optional" pre-rewrite; now strongly recommended.) With version no longer baked into files, release-please has nothing to merge-conflict against. It reads conventional commits since the last tag, opens a PR titled "chore: release vX.Y.Z" with auto-generated changelog, and tags automatically when you merge that PR.

See Step 10 implementation notes for PAT/App requirements before enabling.

---

### 3. CI on PR — `.github/workflows/ci.yml`

CI is now the *authoritative backstop*, not the primary feedback loop. Same checks the pre-push hook runs, plus integration tests and security scanning.

**Add:** `paths-filter` gates downstream work.

**Change:** Keep the `docker-build` matrix but add `cache-from: type=gha`. The release pipeline already populates the GHA cache; with it warm, the matrix runs ~30s instead of ~90s. Gate it on the `go` filter so it skips on deploy/docs-only PRs. Dropping `docker-build` entirely would push Dockerfile validation past the merge gate; keeping it with caching preserves the safety at acceptable cost.

**Drop:** `push: main` trigger. With no-dev-on-main, main can't change without going through PR.

```yaml
on:
  pull_request:
    branches: [main]

concurrency:
  group: ${{ github.workflow }}-${{ github.head_ref }}
  cancel-in-progress: true

jobs:
  changes:
    runs-on: ubuntu-latest
    outputs:
      go: ${{ steps.filter.outputs.go }}
      deploy: ${{ steps.filter.outputs.deploy }}
    steps:
      - uses: actions/checkout@v4
      - uses: dorny/paths-filter@v3
        id: filter
        with:
          filters: |
            go:
              - '**/*.go'
              - 'go.mod'
              - 'go.sum'
              - 'services/**/Dockerfile'
              - 'configs/**'
            deploy:
              - 'deploy/**'
              - 'scripts/**'
              - 'Makefile'

  lint:
    needs: changes
    if: needs.changes.outputs.go == 'true' || needs.changes.outputs.deploy == 'true'
    # ...existing job body

  security:
    needs: changes
    if: needs.changes.outputs.go == 'true' || needs.changes.outputs.deploy == 'true'
    # ...existing job body

  unit-tests:
    needs: changes
    if: needs.changes.outputs.go == 'true'
    # ...existing job body

  integration-tests:
    needs: [changes, unit-tests]
    if: needs.changes.outputs.go == 'true' && (github.event.pull_request.draft == false)
    # ...existing job body

  docker-build:
    needs: [changes, unit-tests]
    if: needs.changes.outputs.go == 'true'
    # ...existing job body, with cache-from: type=gha added to each matrix step
```

`configs/**` belongs in the `go` filter (not `deploy`) because rule files in `configs/rules/` are loaded and validated by `services/engine/config/loader_test.go`. A broken rule file must trigger the unit suite, not just lint.

Outcomes:

* **Doc-only PR:** `changes` runs (~5s), everything else skipped. Mergeable in seconds.
* **Deploy/config-only PR:** lint + security only. ~30–60s.
* **Go-code PR:** full Stage 1+2 including docker-build (cached). ~2:00 down from 2:45 — and by the time it runs you already know it'll pass because the pre-push hook ran.

---

### 4. Release on tag — `.github/workflows/release.yml`

**Add:** `deploy-prod` job after `deploy-staging`. This is the auto-promote.

**Add:** stability watcher invocation via `systemd-run --no-block` as the last step of the prod deploy job. Using `systemd-run` rather than `nohup ... disown` ensures the watcher survives GitHub Actions runner process-group cleanup (which `disown` does not guarantee) and gives journal output and `systemctl status` for free.

**Add:** `-rc` escape hatch — tags matching `v*-rc*` deploy to staging only.

```yaml
jobs:
  build-and-push:
    # unchanged: matrix build + push to GHCR with gha cache

  deploy-staging:
    needs: build-and-push
    runs-on: self-hosted
    if: startsWith(github.ref, 'refs/tags/')
    steps:
      - run: |
          cd /opt/ruby-core
          git fetch --tags && git checkout ${{ github.ref_name }}
          VERSION=${{ github.ref_name }} ./scripts/deploy-staging.sh ${{ github.ref_name }}

  deploy-prod:
    needs: deploy-staging
    runs-on: self-hosted
    if: ${{ startsWith(github.ref, 'refs/tags/') && !contains(github.ref_name, '-rc') }}
    steps:
      - name: Deploy to prod
        run: |
          cd /opt/ruby-core
          git checkout ${{ github.ref_name }}
          VERSION=${{ github.ref_name }} make deploy-prod
      - name: Start stability watcher
        run: |
          systemd-run --no-block \
            --unit=ruby-core-stability-watch-${{ github.ref_name }} \
            /opt/ruby-core/scripts/stability-watch.sh \
            ${{ github.ref_name }} 600

  create-release:
    needs: deploy-prod
    if: ${{ !contains(github.ref_name, '-rc') }}
    # ...unchanged
```

`make deploy-prod` runs the existing `deploy-prod.sh` with auto-rollback on smoke failure.

---

### 5. Staging — `scripts/deploy-staging.sh`

**Drop:** the `down -v` teardown trap. Staging becomes a permanent warm stack.

Smoke uses unique millisecond IDs, so persistent JetStream state doesn't pollute it. A warm-stack upgrade smoke is *closer to what prod does* than a cold-start smoke — it validates the rolling restart path prod will take 60 seconds later.

Side effect: ~1 GB additional memory footprint on the node from idle staging containers. Acceptable on the current hardware; flag for review if node headroom tightens.

---

### 6. Stability watcher — `scripts/stability-watch.sh` *(new)*

Detached process started via `systemd-run` after a successful prod deploy. Polls health and scans logs for 10 minutes. Notifies HA directly on degradation — deliberately bypasses the ruby-core notifier service, which may itself be degraded at the moment the alert fires. HA URL, token, and notify target are read from Vault at startup, same pattern as `deploy-prod.sh`.

Add `notify_service` field to `secret/ruby-core/ha` in Vault before deploying the watcher (e.g., `mobile_app_phone_michael`). This is a pre-condition for Step 8.

```bash
#!/usr/bin/env bash
# stability-watch.sh VERSION DURATION_SECONDS
# Watches prod stack post-deploy. Notifies HA on degradation.
set -euo pipefail

VERSION="${1:?Usage: stability-watch.sh VERSION DURATION}"
DURATION="${2:-600}"
START=$(date +%s)
DEADLINE=$((START + DURATION))

ERROR_THRESHOLD=10  # errors per minute, sustained
SERVICES=(gateway engine notifier presence audit-sink)

VAULT_TOKEN="${VAULT_TOKEN:?}"
VAULT_CMD="vault kv get -field"
VAULT_ADDR=https://127.0.0.1:8200
VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt
export VAULT_ADDR VAULT_CACERT

HA_URL=$(${VAULT_CMD}=url secret/ruby-core/ha)
HA_TOKEN=$(${VAULT_CMD}=token secret/ruby-core/ha)
NOTIFY_SVC=$(${VAULT_CMD}=notify_service secret/ruby-core/ha)

notify_ha() {
  curl -s -o /dev/null -X POST \
    "${HA_URL}/api/services/notify/${NOTIFY_SVC}" \
    -H "Authorization: Bearer ${HA_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"title\":\"$1\",\"message\":\"$2\"}" || true
}

while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  for svc in "${SERVICES[@]}"; do
    status=$(docker inspect -f '{{.State.Health.Status}}' "ruby-core-prod-${svc}" 2>/dev/null || echo "missing")
    if [ "$status" != "healthy" ]; then
      notify_ha "ruby-core ${VERSION} degraded" "${svc} health: ${status}"
      exit 1
    fi
  done

  for svc in "${SERVICES[@]}"; do
    errs=$(docker logs --since 1m "ruby-core-prod-${svc}" 2>&1 | grep -c '"level":"error"' || true)
    if [ "$errs" -gt "$ERROR_THRESHOLD" ]; then
      notify_ha "ruby-core ${VERSION} error spike" "${svc}: ${errs} errors/min (threshold ${ERROR_THRESHOLD})"
      exit 1
    fi
  done

  sleep 30
done

echo "$(date -Iseconds) ${VERSION} stable for ${DURATION}s" >> /var/log/ruby-core/stability.log
notify_ha "ruby-core ${VERSION} stable" "${DURATION}s post-deploy window clean"
```

**Notify-only is intentional for v1.** False-positive auto-rollback is worse than false-negative notify. Run for ~1 month, tune thresholds against real signal, then add auto-rollback in v2.

---

## Implementation order

Each step is independently shippable and reversible. Steps 1, 2, 4, 5 can ship today and land most of the perceived-speed improvement.

| # | Change | Effort | Wall-clock impact | Risk |
|---|---|---|---|---|
| **0a** | **Enable branch protection on `main`** (GitHub Settings → Branches) — **do first** | 10 min | N/A — safety prerequisite | None |
| **0b** | **Create fine-grained PAT + store in Vault** (`secret/ruby-core/github push_token`) — **do before first Claude push** | 10 min | N/A — Claude push prerequisite | None |
| 1 | Gate `docker-build` on go filter + add gha caching | 20 min | −60s per Go PR run | None |
| 2 | Drop `push: main` trigger | 2 min | −2:45 per merge | None — PR was the gate |
| 3 | Add `paths-filter` to CI; move `configs/**` to go filter | 30 min | 0 to −2:30 per PR | Low |
| 4 | Add pre-push stage to `.pre-commit-config.yaml` (including `no-push-to-main` guard); `pre-commit install --hook-type pre-push` | 30 min | −1:00 effective wait time | None — bypassable locally, but GitHub protection is non-bypassable |
| 5 | Refactor version mgmt (`${VERSION:?}` in prod/staging compose, drop `.env.example` VERSION, derive from git in deploy scripts, delete `bump-version.sh`) | 1 hr | Eliminates "forgot to bump" reruns | Low — fail-loud is safer than current silent fallback |
| **5a** | **Install GH runner as systemd service (`svc.sh install`)** — **prerequisite for Step 6** | 15 min | N/A — reliability prereq | None |
| 6 | Wire `deploy-prod` after `deploy-staging` in release.yml | 1 hr | Removes the manual step | Low — auto-rollback already exists |
| 7 | Switch staging to warm/permanent (drop `down -v` trap) | 1 hr | −45s per release | Low |
| 8 | Build stability watcher; add `notify_service` to Vault secret | 3 hr | N/A — closes safety gap | Low |
| 9 | Add `-rc` escape hatch | 15 min | N/A — useful for risky tags | None |
| 10 | Adopt release-please | 2 hr | Removes manual tag step | Low |

**Step 5a is a hard prerequisite for Step 6.** Auto-prod-deploy via CI is a reliability regression if the runner has no watchdog — an untimely runner death between tag push and job completion leaves prod in an uncontrolled state with no rollback. `svc.sh install` is one-time and requires a sudo terminal.

---

## Decisions you should make

**1. Claude push access — branch push yes, main push never.**
Claude is granted `contents: write` on the repo via a fine-grained PAT stored in Vault. This enables pushing feature and fix branches without manual intervention. Main is protected by GitHub branch protection (non-bypassable) and a local pre-push hook (belt-and-suspenders). The PAT cannot bypass branch protection regardless of its scope — this is enforced at the GitHub API layer, not the credential layer.

**2. PAT expiry — 90 days, calendar-driven renewal.**
Fine-grained PATs can expire. 90 days is short enough to limit exposure if the token leaks, long enough not to be operationally disruptive. Set a calendar reminder. If the token expires, Claude falls back to asking the user to push manually until it's renewed.

**3. Should the pre-push hook run integration tests?**
No. They take 60–90s with testcontainer spin-up. Hooks should stay under 30s warm. Integration tests live in CI.

**2. Stability watcher: notify-only or auto-rollback in v1?**
Notify-only. Auto-rollback after a month of tuning thresholds against real signal.

**3. Stability window length: 5, 10, or 30 min?**
10. Long enough for slow leaks and consumer wedge. Short enough you're not sitting on uncertain state when you want to ship a follow-up.

**4. Should `-rc` tags also auto-promote on staging pass?**
No. The point of `-rc` is "I want to look at it in staging first."

**5. Drop CI on `push: main` entirely, or keep a fast lint+unit pass as a safety net?**
Drop entirely. PR was the gate. If you ever bypass PR, you'll know.

**6. Manual `make deploy-prod` — keep or remove?**
Keep. Used roughly never, but valuable when GitHub Actions is degraded or you need to deploy a hotfix without tagging.

**7. Conventional commits via release-please?**
Yes, given the version-management refactor makes it nearly free. If commit prefixes feel like overhead after a few weeks, fall back to manual tagging — pipeline doesn't care.

---

## Step 10 implementation notes — release-please

release-please requires a GitHub token with write access to contents and pull_requests scopes. The default `GITHUB_TOKEN` in Actions can open PRs but **cannot trigger other workflows** on those PRs (a GitHub security constraint). This means the CI workflow won't automatically run on a release-please PR unless you use either:

* **Fine-grained PAT** (recommended): create a PAT with `contents: write` and `pull-requests: write` on this repo. Store as a repo secret (e.g., `RELEASE_PLEASE_TOKEN`). Pass as `token: ${{ secrets.RELEASE_PLEASE_TOKEN }}` to the release-please action.
* **GitHub App**: more robust for organizations; overkill for a single-repo personal project.

This is a one-time setup step. Until it's done, manual `git tag && git push --tags` continues to work identically.

---

## Out of scope

* **Staging↔prod gap** (no Traefik or live HA in staging). Prod smoke covers routing; stability watcher covers live-services. Layered defense, not a structural fix.
* **Single-node blast radius.** Architecture conversation, not pipeline.
* **Vault availability dependency.** Every deploy depends on Vault being up. Worth knowing, not addressing here.

---

## End-state wall-clock estimate

For a typical Go change touching one service:

| Stage | Today | Target |
|-------|-------|--------|
| Local pre-push hook | 0 (none) | 10–30s warm |
| CI on PR | 2:45 | ~2:00 (docker-build cached; known-passing by then) |
| CI on merge to main | 2:45 | 0 (dropped) |
| Manual `make bump-version` + tag + push | ~1 min | 0 (release-please) or `git tag && git push --tags` |
| Build & push to GHCR | ~1:30 | ~1:30 |
| Staging deploy + smoke | ~1:30 | ~30s (warm) |
| Prod deploy + smoke | ~1:00 (manual trigger) | ~45s (auto) |
| **Total merge → prod** | **~7–10 min** | **~3 min** |

Plus a 10-minute background stability watcher you don't have to wait on. For a doc-only change: under a minute total.
