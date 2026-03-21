# Developer Experience & CI Polish

* **Status:** Complete
* **Date:** 2026-03-05
* **Project:** ruby-core
* **Related ADRs:** ADR-0010, ADR-0011, ADR-0013
* **Linked Plan:** none

---

**Goal:** Fully build out the CI pipeline and enhance the developer experience to improve velocity and safety.

---

## Efforts

1. **DevEx:** Enhance `docker-compose.dev.yml` to use `air` for a fast, live-reload development experience (ADR-0010).
2. **Pre-commit:** Add fast unit tests to the pre-commit hook (ADR-0011).
3. **CI Polish:** Expand the CI pipeline to run full comprehensive test gates, including integration tests, on all pull requests (ADR-0013).

---

## Done When

A live-reload dev environment is available via `make dev-air-up`, pre-commit enforces all defined quality checks including fast tests, and no PR can merge without passing unit and integration test gates.

---

## Acceptance Criteria

* `[X]` A productive live-reload environment is available.
* `[X]` `git commit` enforces all defined quality checks.
* `[X]` No PR can be merged without passing all unit and integration tests.

---

## Implementation Notes

* Air: `Dockerfile.dev` per service + `deploy/dev/compose.air.yaml` overlay + `.air.toml` per service
* Fast tests: `//go:build fast` tag on all unit test files; `//go:build integration` for integration tests
* Integration tests: `pkg/natsx/consumer_integration_test.go` using testcontainers-go NATS module
* CI pipeline: 5 jobs — lint + security + unit-tests (Stage 1, parallel); integration-tests + docker-build (Stage 2)
