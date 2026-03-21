# Secure Infrastructure & Minimal CI

* **Status:** Complete
* **Date:** 2026-02-01
* **Project:** ruby-core
* **Related ADRs:** ADR-0026, ADR-0011, ADR-0013
* **Linked Plan:** none

---

**Goal:** Deploy secure infrastructure (NATS, Vault) and establish the minimum viable CI safety net to support foundational development.

---

## Efforts

1. **NATS:** Deploy a single, persistent, TLS-enabled NATS server with NKEY/ACL auth.
2. **Vault:** Set up the local Vault dev server for managing secrets.
3. **Dev Tooling:** Create a basic dev container and a pre-commit hook (golangci-lint with gofumpt, govet, staticcheck, gosec; gitleaks; hadolint, yamllint, actionlint, markdownlint, shellcheck).
4. **CI Gates:** Add a basic test gate that runs `go test ./...` and verifies the build on every pull request.
5. **Operations:** Validate that the target host is configured to use NTP for time synchronization (ADR-0026).

---

## Done When

A secure NATS server is running, `git commit` enforces all quality checks, and a pull request cannot be merged if the build or tests fail.

---

## Acceptance Criteria

* `[X]` A secure NATS server is running (TLS + NKEY auth + JetStream).
* `[X]` `git commit` triggers formatting, linting (Go + non-Go), and secret scanning.
* `[X]` A pull request is blocked if basic tests or the build fails.
* `[X]` Host time synchronization is confirmed (`docs/ops/ntp.md`).
* `[X]` The dev container can be built and opened successfully, and supports running `go test ./...` and `pre-commit run --all-files`.

---

## Implementation Notes

* NATS config: `deploy/base/nats/nats.conf` with JetStream, TLS, NKEY ACLs
* Compose: `deploy/dev/compose.dev.yaml` with NATS + Vault services
* Vault docs: `docs/ops/vault-dev.md`
* Pre-commit: `.pre-commit-config.yaml`
* CI: `.github/workflows/ci.yml`
* Release: `.github/workflows/release.yml`
* NTP docs: `docs/ops/ntp.md`
