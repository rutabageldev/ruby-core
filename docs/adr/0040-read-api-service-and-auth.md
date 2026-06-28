# ADR-0040 - Synchronous Read API Service (`services/api`) with Defense-in-Depth Auth

* **Status:** Accepted
* **Date:** 2026-06-27
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

Until now every ruby-core service is event-driven: services consume and publish NATS subjects, and the only HTTP surfaces are operational (`/health`) or a single write-translation endpoint on the gateway (`POST /ada/events`). There has been no synchronous read plane — nothing a client can query to ask "what is the current state of X."

The home-calendar effort (ROADMAP-0012) needs exactly that: a client (Home Assistant, dashboards, future domains) must be able to `GET` calendar instances over an arbitrary date range, the people directory, and the childcare roster. That is a request/response read API, not an event stream. It is also explicitly intended to be **the first instance of a reusable read plane** that future domains (Ada, finance) will extend — so its shape, auth, and placement set precedent.

This forces two decisions: **where** the read API lives, and **how** it authenticates.

On placement: the gateway (ADR-0009) is a protocol translator for Home Assistant — a WebSocket ingest and a thin REST actuation surface. A typed, spec-first read API that must hold a Postgres connection and serve many domains has a different shape, a different blast radius, and a different dependency set (it must join the `postgres` network; the gateway must not). Folding it into the gateway would overload a service whose responsibilities are deliberately narrow.

On auth: ADR-0020 established "edge authentication" for the gateway — authentication MUST be handled by Traefik (JWT in prod), the application MUST NOT contain primary auth logic, and the service is protected by network isolation (no published host port) plus Traefik↔service mTLS. That posture is correct and should extend to the new service. But ROADMAP-0012's brief also calls for the API to model a bearer security scheme **in the OpenAPI spec** — the spec is the source of truth, and a self-documenting API must declare how it is authenticated. A spec that declares `bearerAuth` while the app enforces nothing is dishonest documentation; an app that enforces a bearer in addition to the edge gate is strictly more secure. The tension is only apparent: ADR-0020 forbids the app from being the *primary* auth layer, not from adding a *second* one.

## Alternatives Considered

**Extend the gateway with the read endpoints** — Overloads a service whose ADR-0009 scope is HA protocol translation, forces the gateway onto the `postgres` network (widening its blast radius and credential scope), and couples the read plane's release cadence to the gateway's. Rejected: the read API is a distinct concern that future domains share.

**A read API per domain (e.g. a calendar service that serves its own HTTP)** — Each new domain would re-solve OpenAPI bundling, codegen, auth middleware, Problem Details, and docs. Rejected: the explicit goal is one reusable read plane; per-domain HTTP servers defeat it and fragment the spec.

**Edge-only auth, no in-app enforcement (strict ADR-0020 as written)** — Honors ADR-0020 literally but leaves the spec's declared `bearerAuth` unenforced and the service trusting any caller that reaches it on the Docker network. Rejected: a second, cheap enforcement layer meaningfully reduces blast radius if the edge is ever misconfigured or bypassed, and makes the spec truthful.

**In-app auth only, drop the edge gate (supersede ADR-0020)** — Moves primary auth into the application, exactly what ADR-0020 rejected for sound reasons (mixed concerns, duplicated logic, no centralized policy). Rejected: the edge gate stays primary; we are adding depth, not relocating it.

**OAuth2/JWT validation inside the app** — Full JWT validation (JWKS fetch, claim checks) in-app duplicates what Traefik already does at the edge and adds operational weight for a LAN-only, service-to-service API. Rejected as over-engineered for the second layer; a Vault-issued static bearer compared in constant time is sufficient depth behind the edge.

## Decision

We will introduce a dedicated read-API service and authenticate it in two layers.

1. **New service.** Ruby-core's synchronous read plane **MUST** live in a new service at `services/api`, separate from the gateway. It **MUST** be the shared home for read endpoints across domains (calendar first; Ada/finance later), not a per-domain HTTP server.

2. **Read-only.** `services/api` **MUST NOT** perform any database writes and **MUST** connect to Postgres using a role granted only `SELECT` (and `USAGE`/`CONNECT`), via a dedicated Vault path (`secret/data/ruby-core/postgres_readonly`). It **MUST NOT** run schema migrations — migrations remain owned by the engine.

3. **Edge auth stays primary (ADR-0020 extended).** Authentication and network posture from ADR-0020 **MUST** apply to `services/api`: the container port **MUST NOT** be published to the host; the connection from Traefik to `services/api` **MUST** be secured with mTLS using an internal-CA client certificate; Traefik edge authentication (JWT in production) remains the primary gate. ADR-0020 remains in force for the gateway and is hereby generalized to cover `services/api`.

4. **In-app bearer as defense-in-depth.** `services/api` **MUST** additionally enforce a bearer token, modeled in the OpenAPI spec as an `http`/`bearer` security scheme and implemented as the ogen `SecurityHandler`. The expected token is a Vault-issued service token read at startup (`secret/data/ruby-core/api/token`) and compared in **constant time**. This layer is secondary to the edge gate; a request that fails it **MUST** receive an RFC 9457 `application/problem+json` 401. The OpenAPI spec **MUST NOT** declare a security scheme the service does not enforce.

5. **Operational endpoints stay unauthenticated and ungenerated.** `GET /health` **MUST** be served outside the authed, ogen-generated mux so Traefik and Uptime Kuma can probe it. The Scalar docs (`/docs`) and raw spec (`/openapi.yaml`) **SHOULD** sit behind the bearer (operator tooling, not public).

## Consequences

### Positive

* A single, reusable read plane: future domains add fragments + handlers instead of re-solving HTTP, auth, and docs.
* Three independent layers must all fail for unauthorized data access: network isolation, Traefik↔api mTLS, and the in-app bearer — strictly stronger than ADR-0020 alone.
* The OpenAPI spec is honest: the declared security scheme is actually enforced, so generated clients and docs reflect reality.
* A `SELECT`-only role makes a read endpoint physically incapable of mutating state even under a handler bug — the read/write split is enforced by Postgres, not convention.
* The gateway keeps its narrow ADR-0009 scope and stays off the `postgres` network.

### Negative

* A new service to build, deploy, monitor, and back the auth material for — more compose wiring, another PKI role, another Uptime Kuma monitor, another image in the CI matrix.
* The in-app bearer is one more secret to issue, store in Vault, inject, and rotate; a stale token is a new failure mode (mitigated: it surfaces as a clean 401, not data exposure).
* A read-only Postgres role and the `postgres_readonly` Vault path must be provisioned on the shared foundation Postgres before `services/api` can deploy — a sequencing dependency outside this repo.

### Neutral

* Formalizes `services/api` as a long-lived architectural component and a critical read-path dependency for Home Assistant and dashboards.
* Establishes that in-app auth in ruby-core is *additive* defense-in-depth, never a replacement for the edge gate — a precedent future services inherit.
* The bearer is a single shared service token, not per-caller identity; finer-grained authorization is deferred until a second distinct consumer needs different access.
