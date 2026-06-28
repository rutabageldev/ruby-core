# ruby-core HTTP API — Style Guide

Conventions for ruby-core's synchronous HTTP read API (`services/api`, ADR-0040), governed
spec-first per ADR-0041. This guide is the human-readable companion to the OpenAPI spec: the spec
is the contract, this explains the conventions every fragment is expected to follow.

It applies to the HTTP read surface only. NATS write-event contracts are governed separately
(ADR-0014 schema-in-code; the calendar write payloads are documented with the calendar plan).

---

## Source of truth & generation

- The spec is hand-authored as per-domain fragments under `api/openapi/` and bundled into
  `api/openapi.gen.yaml` (Redocly). The bundle is the input to ogen (Go server/client) and
  `openapi-python-client` (HA read-proxy client). **Generated code is never hand-edited.**
- CI blocks merge on: codegen fail-on-diff, Spectral lint, and oasdiff breaking-change detection
  (ADR-0041). Treat a red gate as a contract violation, not a CI annoyance.

## Versioning

- The API is versioned by **URL path**: every operation lives under `/v1/`.
- A breaking change (removing/renaming a field, changing a type, tightening validation) requires a
  new path version, not an in-place edit. oasdiff enforces this mechanically.
- Additive changes (new optional fields, new endpoints) stay within the current version.

## Documentation (enforced)

Spectral fails the build if any of these is missing, so author them up front:

- Every **operation** has a `summary` and a `description`.
- Every **parameter** has a `description` and an `example`.
- Every **response** has a `description`, and every response body schema carries an `example`.

Write descriptions for a future reader with no context. Examples must be realistic (a plausible
event, person, provider) — not `"string"`.

## Errors — RFC 9457 Problem Details

- All error responses use `application/problem+json` and the shared `Problem` component (defined
  once, referenced everywhere). Never invent a per-endpoint error shape.
- Fields: `type` (a stable URI/slug identifying the error class), `title` (short, stable),
  `status` (matches the HTTP status), `detail` (human-readable, specific to this occurrence),
  `instance` (optional, the offending request path). Domain-specific context goes in extension
  members, not in `detail` string-munging.
- Conventions:
  - `400` — malformed/invalid request (failed validation, range exceeds the max window).
  - `401` — missing/invalid bearer (defense-in-depth layer; the edge gate is primary, ADR-0040).
  - `404` — addressed resource does not exist.
  - `422` — well-formed but semantically rejected (reserve for cases distinct from 400).
  - `500` — unexpected; `detail` must not leak internals.

## Naming & shapes

- JSON property names are `snake_case`, matching the underlying mirror columns and the NATS payloads
  (avoids a translation layer and keeps the spec legible against the schema).
- Timestamps are RFC 3339. The calendar surface mirrors Google's native shape deliberately: a date
  XOR a datetime+timezone, with `all_day` distinguishing, and **all-day `end` is exclusive** (a
  one-day event on the 26th ends on the 27th). This convention is surfaced to consumers, documented
  on the schema, not hidden — see ADR-0042.
- Enpoints return flat, sorted collections where a range is requested (calendar instances are
  expanded and sorted server-side).

## Filtering & ranges

- The calendar read endpoint takes explicit `start` and `end` query parameters (RFC 3339) and
  returns the expanded, sorted instances in that window.
- **Max-window guard:** range requests that would expand an excessive span MUST be rejected with a
  `400` Problem (`type` identifying the window-exceeded class). On-demand recurrence expansion is
  unbounded otherwise; the guard is mandatory, not advisory (ADR-0042).

## Pagination (convention now, implementation later)

No endpoint paginates today (calendar uses the date-range filter; the overlay collections are
small). When an endpoint needs pagination, it MUST follow this **opaque-cursor** convention rather
than offset/limit:

- Request: `?cursor=<opaque>&limit=<n>` — `cursor` omitted on the first page; `limit` bounded by a
  documented server maximum.
- Response: the page plus a `next_cursor` (string, omitted/null on the last page). Cursors are
  opaque tokens the client echoes back verbatim — never construct or parse them client-side.
- Rationale: opaque cursors are stable under concurrent inserts/deletes (offset pagination skips or
  repeats rows when the underlying set shifts) and let the server change the underlying ordering key
  without a contract break.

Offset/limit pagination is **not** to be added. Implement the cursor convention only when a real
endpoint requires it.

## Authentication

- Two layers (ADR-0040): Traefik edge auth + mTLS is primary; an in-app Vault-issued **bearer**
  (`http`/`bearer` security scheme) is defense-in-depth. The spec declares `bearerAuth` and the
  service enforces it — the spec never declares a scheme that isn't enforced.
- `GET /health` is unauthenticated and lives outside the generated mux (for Traefik/Uptime Kuma).

## Deferred (leave room, don't build)

- Schemathesis fuzzing and mock servers — out of scope (ADR-0041).
- AsyncAPI for the NATS write-event contracts — the eventual machine-readable home; author write
  payload schemas so they port cleanly, but do not adopt AsyncAPI yet.
