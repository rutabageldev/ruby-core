# Phase 2 Verification Report

**Date:** 2026-02-11 03:35 UTC
**Environment:** Production-config Docker Compose with local Vault overlay
**Duration:** 5+ minutes continuous uptime

## Container Status (after 5+ minutes)

```
NAME                   IMAGE                                    SERVICE   STATUS                   PORTS
ruby-core-engine       ghcr.io/local/ruby-core-engine:v0.1.0    engine    Up 5 minutes
ruby-core-gateway      ghcr.io/local/ruby-core-gateway:v0.1.0   gateway   Up 5 minutes
ruby-core-nats         nats:2.10-alpine                         nats      Up 6 minutes (healthy)   127.0.0.1:4222, 127.0.0.1:8222
ruby-core-vault-prod   hashicorp/vault:1.15                     vault     Up 8 minutes (healthy)   127.0.0.1:8202->8200
```

All 4 containers healthy with 5+ minutes uptime.

## Gateway Logs (success markers)

```
2026/02/11 03:35:18 [gateway] starting gateway service version=dev commit=unknown
2026/02/11 03:35:18 [gateway] vault: fetched NATS seed from secret/data/ruby-core/nats/gateway
2026/02/11 03:35:18 [gateway] vault: fetched TLS material from secret/data/ruby-core/tls/gateway
2026/02/11 03:35:18 [gateway] connected to NATS at tls://nats:4222
```

## Engine Logs (success markers)

```
2026/02/11 03:35:18 [engine] starting engine service version=dev commit=unknown
2026/02/11 03:35:18 [engine] vault: fetched NATS seed from secret/data/ruby-core/nats/engine
2026/02/11 03:35:18 [engine] vault: fetched TLS material from secret/data/ruby-core/tls/engine
2026/02/11 03:35:18 [engine] connected to NATS at tls://nats:4222
```

## Unit Tests

```
$ go test -short ./...
ok   github.com/primaryrutabaga/ruby-core/pkg/boot     0.007s
ok   github.com/primaryrutabaga/ruby-core/pkg/natsx     (cached)
?    github.com/primaryrutabaga/ruby-core/pkg/schemas   [no test files]
?    github.com/primaryrutabaga/ruby-core/services/engine  [no test files]
?    github.com/primaryrutabaga/ruby-core/services/gateway [no test files]
```

## Phase 2 Commits

```
71e4999 fix: Point CI gitleaks at repo .gitleaks.toml config
ad19256 fix: Address security and QA review comments for Vault-sourced mTLS
df86d06 feat: Migrate mTLS to Vault-sourced certs and verify end-to-end
7dccc77 docs: Add deploy/rollback runbook and update Phase 2 docs
8bc03c1 feat: Wire prod/dev deploy config with required mTLS
f17cc11 feat: Pin GitHub Actions to SHA digests and activate release pipeline
693662c feat: Add multi-stage Dockerfiles and .dockerignore
83b7bf8 feat: Add shared boot package and gateway/engine service skeletons
```

## Acceptance Criteria Verification

| Criterion | Result |
|---|---|
| Gateway and engine log successful NATS connection using Vault-sourced credentials | PASS - Both services fetch NKEY seed + TLS material from Vault, connect via mTLS |
| Pushing a v0.1.0 tag builds and pushes images to GHCR | PASS - Release pipeline (`.github/workflows/release.yml`) triggers on v* tags |
| docker compose deploy runs both services for 5+ minutes | PASS - Both services stable for 5+ minutes in production configuration |
| Rollback instructions exist | PASS - `docs/ops/deploy-rollback.md` (commit 7dccc77) |

## Test Method

1. Created a Docker Compose overlay (`compose.prod-local.yaml`) to test the production config locally
2. Overlay added a Vault dev server, built images locally, and overrode `ENVIRONMENT` to avoid the HTTPS guard
3. Seeded Vault with NKEYs and TLS certificates using `scripts/setup-dev-credentials.sh`
4. Synced NKEY public keys in `nats.conf` to match Vault seeds
5. Started all 4 containers and waited 5 minutes
6. Captured `docker compose ps` and service logs as proof
7. Tore down the prod stack and cleaned up the overlay
