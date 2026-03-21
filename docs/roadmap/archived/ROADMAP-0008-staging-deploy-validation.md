# Staging Environment & Deploy Validation

* **Status:** Complete
* **Date:** 2026-03-06
* **Project:** ruby-core
* **Related ADRs:** none
* **Linked Plan:** none

---

**Goal:** Eliminate manual back-and-forth during releases by automatically validating deployability before prod.

---

## Efforts

1. Add `deploy/staging/compose.staging.yaml` — same images as prod, separate container names/ports, ephemeral named volumes, no Traefik labels.
2. Add a `deploy-staging` job to the release workflow: deploys to the node over SSH, runs `scripts/smoke-test.sh` against staging, and blocks the release on failure.
3. Gate `make deploy-prod` on a passing staging run (GitHub environment protection rule).

---

## Done When

Pushing a version tag auto-deploys to staging and runs smoke tests; a broken deploy fails in staging before reaching prod; `make deploy-prod` requires a green staging run.

---

## Acceptance Criteria

* `[X]` Pushing a version tag auto-deploys to staging and runs smoke tests.
* `[X]` A broken deploy (e.g. missing rule files, bad ACLs) fails in staging before reaching prod.
* `[X]` `make deploy-prod` requires a green staging run.

---

## Implementation Notes

* Staging compose: `deploy/staging/compose.staging.yaml` (NATS on 4224/8224, container prefix `ruby-core-staging-*`)
* Deploy script: `scripts/deploy-staging.sh` — pull → up → poll healthz → smoke test → `down -v` on exit
* Staging credentials: `secret/ruby-core/staging/*` in Vault; provisioned via `make setup-staging-creds`
* Self-hosted runner: `~/actions-runner` registered as `ruby-core-node-01`
* Release gate: `create-release` job needs `deploy-staging` to pass
* Verification: end-to-end validated with v0.4.3
