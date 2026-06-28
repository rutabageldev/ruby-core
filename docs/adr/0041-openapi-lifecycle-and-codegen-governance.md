# ADR-0041 - OpenAPI Lifecycle & Codegen Governance for the HTTP API

* **Status:** Proposed
* **Date:** 2026-06-27
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

ROADMAP-0012 introduces ruby-core's first synchronous HTTP API (`services/api`, ADR-0040). An HTTP API is a long-lived external contract: Home Assistant, dashboards, and future domains will couple to its shapes. The global engineering standards require APIs to be "structured and self-documenting" with an OpenAPI spec maintained alongside the code, and require code generation to be enforced in CI ("regenerate, fail on diff" — the same discipline already applied to sqlc/migrate for the database).

This raises a governance question distinct from the one ADR-0014 already answered. ADR-0014 governs **shared data schemas carried over NATS** (YAML rules, CloudEvents) with a *schema-in-code* strategy: Go structs are the source of truth. That decision is correct for the event bus and remains in force — including for the calendar write-event contracts, which are NATS payloads. But the HTTP read surface is a different artifact with different consumers (including a non-Go Home Assistant client), and "Go structs are the truth" produces a spec that is reverse-engineered from handlers and drifts from documentation. For a self-documenting API the contract must come first and the code must be provably derived from it.

We also need machine enforcement, not discipline. The system has no human reviewer besides the solo developer; a documentation standard that depends on remembering to update a spec, add examples, and avoid breaking changes will erode. The standards already call for generation enforced in CI; the API surface additionally needs lint enforcement (every operation documented) and breaking-change detection (the contract can't silently break consumers).

Today none of this tooling exists in the repo: no OpenAPI spec, no ogen/oapi-codegen, no Spectral, no oasdiff, and no codegen fail-on-diff gate (not even for sqlc, despite the discipline being followed manually).

## Alternatives Considered

**Schema-in-code for HTTP too (extend ADR-0014, generate the spec from Go)** — Keeps one mental model, but makes the spec a derived artifact that lags the code, produces awkward/under-documented OpenAPI, and offers no natural place to require descriptions/examples or to diff for breaking changes. Rejected: defeats the "self-documenting, spec-is-the-contract" standard for the API surface.

**oapi-codegen instead of ogen** — A capable generator, but ogen produces a stricter typed server interface with built-in request/response validation and a first-class error handler, which fits "validation generated from the spec" with less hand-written glue. Rejected as the lesser fit; either would work, ogen is chosen for the stronger generated contract.

**A single hand-authored monolithic `openapi.yaml`** — Simpler tooling (no bundler), but one file becomes unmanageable as domains accumulate (calendar, then Ada, finance) and makes per-domain ownership unclear. Rejected: per-domain fragments bundled into one served spec scale better and match the multi-domain intent.

**Lint/breaking-change checks as advisory (warn, don't block)** — Lower friction, but advisory gates are ignored gates for a solo developer; drift and accidental breaking changes would still ship. Rejected: the whole point is to make drift structurally impossible, which requires blocking.

**A runtime schema registry / contract-testing service** — Overkill for a single-node V0, the same conclusion ADR-0014 reached for event schemas. Rejected: static spec + CI gates deliver the guarantees without new infrastructure.

## Decision

We adopt a spec-first lifecycle for the HTTP API, enforced in CI.

1. **Spec is the source of truth.** For the HTTP API surface, the hand-authored OpenAPI document **MUST** be the source of truth; the Go server interface, Go client, request/response validation, and the Python client **MUST** be generated from it — never hand-edited. This applies to the HTTP surface only; **ADR-0014 (schema-in-code) remains the governance for NATS/CloudEvents contracts**, including the calendar write-event payloads.

2. **Fragmented spec, bundled.** The spec **MUST** be authored as per-domain fragments under `api/openapi/` (`calendar.yaml`, later `ada.yaml`, `finance.yaml`, plus shared `components/`) and bundled into a single served document (`api/openapi.gen.yaml`) with **Redocly CLI** (`@redocly/cli bundle`). The bundled file **MUST** be checked in and is the input to code generation and the served `/openapi.yaml`.

3. **Generator.** Code generation **MUST** use **ogen** (go-installable, pinned), driven by a `//go:generate` directive, emitting into `services/api/oas` (package `oas`). The Python client for the Home Assistant read-proxy **MUST** be generated with `openapi-python-client` (pinned) into `clients/python/` and checked in.

4. **Versioning & errors.** The API **MUST** be versioned by URL path (`/v1/`). All error responses **MUST** use RFC 9457 Problem Details (`application/problem+json`), defined once as a shared component and referenced by every operation's error responses.

5. **CI gates (all blocking on merge).** A CI stage **MUST** enforce:
   * **Codegen fail-on-diff** — re-bundle, re-generate (ogen + python client), and re-run `sqlc generate`, then fail if `git diff` is non-empty. (This gate also closes the existing manual sqlc discipline gap.)
   * **Spectral lint** — a ruleset requiring a `description` **and** an `example` on every operation, parameter, and response; missing documentation fails the build.
   * **oasdiff breaking-change detection** — the PR spec is compared against the base branch; an unapproved breaking change fails the build.

6. **Pagination convention.** A cursor-pagination convention **MUST** be documented in the API style guide (`docs/api/style-guide.md`) for future endpoints. Only the date-range filter that the calendar read endpoint needs is implemented now; cursor pagination is not built until an endpoint requires it.

7. **Deferred, with room left.** Schemathesis fuzzing and mock servers are **out of scope** now. AsyncAPI is the intended future machine-readable home for the NATS write-event contracts; the payload schemas **SHOULD** be authored so they port cleanly, but AsyncAPI is not adopted here.

## Consequences

### Positive

* Drift between the API, its documentation, its Go server/client, and its Python client becomes structurally impossible — CI rejects any mismatch.
* Every endpoint is documented (description + example) by construction, satisfying the self-documenting-API standard automatically.
* Breaking changes are caught mechanically before merge, protecting Home Assistant and dashboard consumers.
* Adding a domain is "add a fragment + handlers + regenerate," a repeatable path future domains inherit.
* The fail-on-diff gate retroactively hardens the previously-manual sqlc generation discipline.

### Negative

* New tooling spanning three ecosystems: ogen/oasdiff/sqlc (Go), Redocly/Spectral (Node), openapi-python-client (pip) — all must be pinned and available to CI (and ideally pre-commit), adding setup and maintenance surface.
* Spec-first adds an authoring step before code: a change starts in YAML, then regenerates — slower than editing a handler directly, and a different workflow from the schema-in-code model used elsewhere in the repo.
* Two governance models now coexist (spec-first for HTTP, schema-in-code for NATS); the boundary must be understood to avoid applying the wrong one.

### Neutral

* Establishes ogen + Redocly + Spectral + oasdiff as the standard API toolchain future domains build on.
* Checked-in generated code (Go `oas/`, `clients/python/`, sqlc output) grows the diff surface of routine changes, in exchange for reviewable, buildable-offline artifacts.
* Formalizes a clear split: ADR-0041 owns the synchronous HTTP contract; ADR-0014 owns the asynchronous event contract.
