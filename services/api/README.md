# api

ruby-core's synchronous HTTP **read plane** — the spec-first, self-documenting read API
that every domain inherits (ROADMAP-0012). The calendar read endpoint is served here;
directory and childcare endpoints are added by the household-overlay slice.

See ADR-0040 (service + auth) and ADR-0041 (OpenAPI lifecycle & codegen governance).

## Endpoints

| Path | Auth | Notes |
|---|---|---|
| `GET /health` | none | Liveness for Traefik + Uptime Kuma. Outside the generated/versioned surface. |
| `GET /v1/ping` | bearer | Confirms reachability + accepted token. Placeholder. |
| `GET /v1/calendar/events?start=&end=` | bearer | Flat, sorted, tz-aware calendar instances in the range (recurring expanded; max-window guarded). |
| `GET /openapi.yaml` | bearer | The bundled OpenAPI document (embedded at build time). |
| `GET /docs` | bearer | Scalar API reference rendering `/openapi.yaml`. |

All errors are RFC 9457 Problem Details (`application/problem+json`).

## Auth (defense-in-depth, ADR-0040)

Traefik edge auth + Traefik→api mTLS is the **primary** gate (mTLS is a planned fast-follow).
The service additionally enforces a Vault-issued **bearer** token (constant-time compare) as a
second layer, modeled in the spec as the `bearerAuth` security scheme. The container port is
never published to the host (prod/staging); dev publishes `127.0.0.1:8090` for local testing.

## Data access

Read-only. The service connects to the shared Postgres with a **SELECT-only** role and runs
**no migrations** (migrations are owned by the engine). It cannot mutate state even on a bug.

## Configuration (env)

| Var | Default | Purpose |
|---|---|---|
| `VAULT_ADDR` / `VAULT_TOKEN` / `VAULT_CACERT` | — | Vault access (scoped `ruby-core` token). |
| `VAULT_PG_PATH` | `secret/data/ruby-core/postgres_readonly` | Read-only Postgres credentials. |
| `VAULT_API_TOKEN_PATH` | `secret/data/ruby-core/api` | KV path holding the bearer (`token` field). |
| `HTTP_ADDR` | `:8080` | Listen address. |

## Spec-first workflow (ADR-0041)

The hand-authored OpenAPI fragments under [`api/openapi/`](../../api/openapi/) are the source
of truth. Generated code (`services/api/oas/`, the Python client in `clients/python/`, and the
bundled `api/openapi.gen.yaml`) is **never hand-edited**.

```bash
make openapi-gen     # bundle (Redocly) -> ogen Go server/client -> Python client
make openapi-lint    # Spectral: description + example required on everything
make openapi-diff    # oasdiff: block breaking changes vs origin/main
make openapi-verify  # regenerate and fail if anything drifted (mirrors CI)
```

CI blocks merge on codegen drift, Spectral lint, and oasdiff breaking changes.

Tooling (pinned): ogen `v1.22.0` (in `services/api/oas/generate.go`), Redocly/Spectral (npm,
`package.json`), oasdiff `v1.20.1` (`go run`), `openapi-python-client` (`pipx install`).

## Run locally

```bash
make dev-up                 # infra (NATS not required by api, but starts the stack)
make dev-services-up        # builds + starts services incl. api on 127.0.0.1:8090
curl -s localhost:8090/health
curl -s -H "Authorization: Bearer <token>" localhost:8090/v1/ping
```

Requires `secret/ruby-core/dev/postgres_readonly` and `secret/ruby-core/dev/api` (field
`token`) to exist in Vault.

## Deploy

Defined in `deploy/{dev,staging,prod}`. Prod/staging pull the GHCR image; prod adds Traefik
labels (`ruby-api` router, `ruby-api-auth` middleware) and joins `traefik_proxy` +
`traefik-public`. **Host-side pre-conditions** before first prod deploy: the SELECT-only
Postgres role, the bearer token in Vault, and the `ruby-api-auth` Traefik middleware. Register
a `/health` monitor in Uptime Kuma.
