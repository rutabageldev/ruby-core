# PLAN-0030 - API Foundation (`services/api`)

* **Status:** Draft
* **Date:** 2026-06-27
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0012-home-calendar.md (effort 0012.1)
* **Branch:** feat/api-foundation
* **Related ADRs:** ADR-0040 (read-API service + defense-in-depth auth), ADR-0041 (OpenAPI lifecycle & codegen governance)

---

## Scope

Stand up `services/api` — ruby-core's first synchronous HTTP read plane — as a spec-first platform
with **no real domain data yet**. This plan delivers: the OpenAPI tooling chain (Redocly bundle →
ogen → Spectral → oasdiff → openapi-python-client), a minimal served spec (`/v1/ping` placeholder
endpoint plus the shared `Problem` component and `bearerAuth` scheme), the service skeleton
(Vault + read-only Postgres boot, ogen server mounted on `http.Server`, RFC 9457 error handler,
constant-time bearer middleware, Scalar docs, `/health`), the Makefile codegen targets, the new CI
gate stage, and deploy wiring. **Out of scope:** calendar/directory/childcare endpoints and any
query against real tables (PLAN-0032/0033); cursor-pagination implementation; mTLS server config on
`services/api` (tracked here as a follow-up but the bearer + network isolation land now).

---

## Pre-conditions

* [ ] On branch `feat/api-foundation` cut from latest `main`.
* [ ] A read-only Postgres role exists on foundation Postgres and its DSN is stored at Vault
      `secret/ruby-core/postgres_readonly` (host-side provisioning; see Open Questions if not yet
      done — the skeleton can be built and unit-tested without it, but integration tests and deploy
      need it).
* [ ] A bearer token for the API is generated and stored at Vault `secret/ruby-core/api` field
      `token` (via `ruby-core-writer`).
* [ ] Node (for `@redocly/cli`, `@stoplight/spectral-cli`) and Go toolchain available locally and on
      the self-hosted runner; `oasdiff`, `ogen` go-installable.

---

## Steps

### Step 1 — Scaffold the spec tree and Redocly bundle

**Action:** Create `api/openapi/openapi.root.yaml` (info, `servers: [{url: /v1}]`, the `bearerAuth`
security scheme, a `$ref` to a placeholder `paths/ping.yaml` with `GET /ping`), `api/openapi/components/problem.yaml`
(RFC 9457 `Problem` schema with description + example), and `api/openapi/paths/ping.yaml` (fully
documented: summary, description, 200 response with example, default → `Problem`). Add a pinned
`package.json` with `@redocly/cli` and `@stoplight/spectral-cli`, and `api/.spectral.yaml` requiring
description + example on every operation/parameter/response.

**Verification:** `npx @redocly/cli bundle api/openapi/openapi.root.yaml -o api/openapi.gen.yaml`
produces a single valid file; `npx spectral lint api/openapi.gen.yaml` exits 0.

### Step 2 — Generate the ogen server + Go client

**Action:** Add `services/api/oas/generate.go` with
`//go:generate go run github.com/ogen-go/ogen/cmd/ogen -target . -package oas -clean ../../../api/openapi.gen.yaml`.
Add ogen to `go.mod` tooling (a `tools.go` build-tagged import, or the Go 1.25 `go tool` directive).
Run generation.

**Verification:** `go generate ./services/api/oas/...` produces `services/api/oas/*.go`;
`go build ./services/api/oas/...` compiles; the generated `Handler` interface contains a `Ping`
method and a `SecurityHandler` with `HandleBearerAuth`.

### Step 3 — Generate the Python client

**Action:** Add a pinned `openapi-python-client` invocation (documented in the Makefile target,
Step 7) generating into `clients/python/`. Run it. Check the output in.

**Verification:** `openapi-python-client generate --path api/openapi.gen.yaml --output-path clients/python --overwrite`
produces a package; `git status` shows the generated tree; a trivial import of the generated package
under Python succeeds (documented manual check; CI re-generates and diffs).

### Step 4 — Service skeleton: boot + read-only pgxpool

**Action:** Create `services/api/main.go` mirroring `services/gateway/main.go`'s boot order:
`logging.NewLogger("api")` + `slog.SetDefault`, `boot.LoadConfig("api")`, fetch the read-only DSN via
`boot.FetchPostgresConfig(addr, token, "secret/data/ruby-core/postgres_readonly")`, `pgxpool.New`,
signal handling, graceful shutdown. **No `MigrateUp`.** NATS is not wired (read API does not publish
in this slice).

**Verification:** `go build ./services/api/...` compiles; running locally against the read-only DSN
opens the pool and logs "api: ready" without attempting any write.

### Step 5 — App wiring: ogen mux, error handler, auth, health, docs

**Action:** Create `services/api/app/{app.go,auth.go,problem.go,docs.go}`:

* `problem.go`: implement the ogen `ErrorHandler` rendering `application/problem+json` from the shared
  `Problem` shape; centralize status→Problem mapping.
* `auth.go`: implement `HandleBearerAuth` doing a `crypto/subtle.ConstantTimeCompare` against the
  Vault-issued token (read at startup from `secret/data/ruby-core/api/token`); failure → 401 Problem.
* `docs.go`: `GET /openapi.yaml` serves the `go:embed`-ed `api/openapi.gen.yaml`; `GET /docs` serves a
  small HTML page loading the pinned Scalar standalone JS against `/openapi.yaml` (behind bearer).
* `app.go`: build the ogen `Server` (handlers + security + error handler), chain it behind the bearer,
  add a plain `GET /health` **outside** the generated mux, mount on `http.Server` `:8080` with the
  gateway's timeouts (`services/gateway/app/app.go` pattern).
* `services/api/handlers/ping.go`: implement the placeholder `Ping`.

**Verification:** `go build ./...`; `go test -tags=fast ./services/api/...` (Step 6) green; locally
`curl -sf localhost:8080/health` → 200, `curl localhost:8080/v1/ping` without bearer → 401
`application/problem+json`, with the correct bearer → 200.

### Step 6 — Unit tests (`-tags=fast`)

**Action:** Add `//go:build fast` tests: bearer 401 vs 200 (constant-time path), Problem rendering for
each mapped status, `Ping` handler, and `/openapi.yaml` serving bytes identical to the embedded
bundle.

**Verification:** `go test -tags=fast -race ./services/api/...` passes.

### Step 7 — Makefile codegen targets

**Action:** Add commented targets: `openapi-bundle` (redocly), `openapi-gen` (bundle → `go generate`
→ python client), `openapi-lint` (spectral), `openapi-diff` (oasdiff vs `origin/main`). Keep them
grouped under an "API / OpenAPI" header.

**Verification:** `make openapi-gen` runs end-to-end on a clean tree and leaves `git diff` empty;
`make openapi-lint` exits 0; `make openapi-diff` runs (no breaking change vs main → exit 0).

### Step 8 — CI gate stage

**Action:** In `.github/workflows/ci.yml` add a path-filtered stage (filter on `api/**`,
`services/api/**`, `clients/**`, and `**/*.sql.go`) with three jobs: **codegen-diff** (re-bundle,
`go generate`, python client, `sqlc generate` across existing configs, then `git diff --exit-code`),
**spectral**, **oasdiff** (vs the PR base). Add `api` to the existing docker-build matrix.

**Verification:** Push the branch; the new jobs appear and pass. Deliberately edit `api/openapi.gen.yaml`
by hand in a scratch commit → codegen-diff fails (then revert). Remove an example from a fragment →
spectral fails (then revert).

### Step 9 — Dockerfile + deploy wiring

**Action:** Add `services/api/Dockerfile` (copy `services/gateway/Dockerfile`, build `./services/api`,
distroless nonroot). Add an `api` service to `deploy/{base,dev,staging,prod}` compose: networks
`postgres` + `traefik_proxy`; Vault role-id/secret-id mounts like the engine; env incl.
`VAULT_PG_PATH=secret/data/ruby-core/postgres_readonly`; Traefik labels mirroring the gateway
(`compose.prod.yaml`) for `Host(...) && PathPrefix(/v1,/docs,/openapi.yaml)`, `tls=true`,
auth middleware, `loadbalancer.server.port=8080`; **no host port published**. Register an Uptime Kuma
monitor on `/health` (manual).

**Verification:** `docker compose -f deploy/dev/... config` validates; `make dev-up` (or the dev
target that includes api) starts the container; `docker logs` shows "api: ready"; from a container on
`traefik_proxy`, `/health` returns 200.

### Step 10 — README + Pre-PR checklist

**Action:** Add `services/api/README.md` (purpose, endpoints, auth posture, how to regenerate). Update
`docs/plans/README.md` to list PLAN-0030. Run the Pre-PR Checklist; archive this plan to
`docs/plans/archived/` (status → Complete) as the final commit.

**Verification:** Pre-commit hooks pass (`golangci-lint`, gitleaks, formatting); README reflects actual
endpoints.

---

## Rollback

The service is additive and read-only. Rollback = remove the `api` service from the compose files and
redeploy; no schema or stateful change is introduced by this slice (the read-only role and Vault
paths are provisioned out-of-band and harmless if unused). CI gate changes are revertable by reverting
the workflow commit. No data migration → clean rollback.

---

## Open Questions

* **Read-only Postgres role provisioning** is a host/foundation step outside this repo. If it is not
  yet done, Steps 1–8 (skeleton, codegen, gates, unit tests) proceed without it; Steps 9 integration
  startup and deploy require it. Confirm the role + `secret/ruby-core/postgres_readonly` path before
  Step 9.
* **mTLS on `services/api`** (Traefik→api) per ADR-0040: should it land in this slice or as a
  fast-follow? This plan implements network isolation + in-app bearer now and recommends mTLS as a
  scoped follow-up using the gateway's PKI-role pattern — confirm acceptable, or fold it into Step 9.
