# "Hello World" Production Release

* **Status:** Complete
* **Date:** 2026-02-10
* **Project:** ruby-core
* **Related ADRs:** ADR-0015, ADR-0016
* **Linked Plan:** none

---

**Goal:** Build minimum viable services and validate the full automated release path from Git tag to a running production container.

---

## Efforts

1. Build skeleton `gateway` and `engine` services that connect to NATS using secrets from Vault.
2. Implement the full release pipeline: automated pipeline triggered by `v*` tags that builds and pushes versioned images to GHCR.
3. Create and push a `v0.1.0` Git tag to trigger the new pipeline.
4. Manually deploy to production by updating `docker-compose.prod.yml` to use the `v0.1.0` images.

---

## Done When

A `v0.1.0` tag automatically builds and pushes images to GHCR, both services run in production using Vault-sourced credentials, and rollback instructions exist.

---

## Acceptance Criteria

* `[X]` Gateway and engine log successful NATS connection using Vault-sourced credentials (ADR-0015).
* `[X]` Pushing a `v0.1.0` tag builds and pushes `gateway:v0.1.0` and `engine:v0.1.0` to GHCR (ADR-0016).
* `[X]` `docker compose` deploy runs both services and they stay running for at least 5 minutes.
* `[X]` Rollback instructions exist for redeploying a prior tag.

---

## Implementation Notes

* Gateway: `services/gateway/main.go` — Vault-sourced NKEY + mTLS, graceful shutdown
* Engine: `services/engine/main.go` — same pattern
* Boot package: `pkg/boot/boot.go` — Vault fetch with retry, TLS 1.3, prod HTTPS guard
* Unit tests: `pkg/boot/boot_test.go` (28 test cases)
* Release pipeline: `.github/workflows/release.yml` (v* tag → GHCR matrix build)
* Prod compose: `deploy/prod/compose.prod.yaml`
* Rollback runbook: `docs/ops/deploy-rollback.md`
* Verification report: `docs/ops/phase2-verification.md`
