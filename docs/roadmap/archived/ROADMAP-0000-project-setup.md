# Project Setup & Core Contracts

* **Status:** Complete
* **Date:** 2026-02-01
* **Project:** ruby-core
* **Related ADRs:** ADR-0014, ADR-0027
* **Linked Plan:** none

---

**Goal:** Establish the absolute foundational code structure and data contracts before any service logic is written.

---

## Efforts

1. Initialize the Go monorepo with the service directory structure.
2. Create a shared `pkg/schemas` package with Go structs for data contracts.
3. Create a shared `pkg/natsx` package to codify the subject naming convention.
4. Set up a basic `docker-compose.yml` with placeholders.

---

## Done When

Data schemas are centralized and versioned in code, and the monorepo structure supports adding services without restructuring.

---

## Acceptance Criteria

* `[X]` Data schemas are centralized and versioned in code (ADR-0014, ADR-0027).
