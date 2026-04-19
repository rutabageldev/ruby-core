# ruby-core

Ruby Core is an event-driven control plane for home automation, built on NATS JetStream. It runs on a single Debian utility node alongside a shared infrastructure stack (Vault, Traefik, Postgres, observability).

## Services

| Service | Role |
|---|---|
| **gateway** | HA WebSocket ingress + HA REST actuation. Publishes lean-projected state changes to `ha.events.>`. |
| **engine** | Event processor host. Runs stateless and stateful processors against `ha.events.>` and `ruby_presence.events.>`. |
| **notifier** | Executes notification commands from `ruby_engine.commands.notify.>` via HA service calls. |
| **presence** | Multi-source presence fusion (phone + WiFi corroboration) with debounce. Publishes to `ruby_presence.events.>`. |
| **audit-sink** | Consumes `audit.>` and archives to NDJSON on disk. |

### Engine Processors

| Processor | Type | Responsibility |
|---|---|---|
| `presence_notify` | Stateless | Translates presence events to HA sensor state via NATS KV. |
| `ada` | Stateful (Postgres) | Baby tracking — feedings, diapers, sleep, tummy time. Pushes derived sensors to HA. Daily aggregates anchored to configurable bedtime boundary. |

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for the full component and deployment overview.

See [`docs/adr/`](docs/adr/) for all Architecture Decision Records.

## Environments

| Environment | Purpose | Compose file |
|---|---|---|
| dev | Bind mounts + Air live reload | `deploy/dev/` |
| staging | Pre-release smoke test gate | `deploy/staging/` |
| prod | Immutable images + pinned tags | `deploy/prod/` |

Copy `.env.example` to `.env` in the target environment directory before starting services.

## Build & Test

```bash
go build ./...                              # build all services
go test -tags=fast ./...                    # unit tests (fast-tagged)
go test -tags=integration ./...             # integration tests (requires Docker)
pre-commit run --all-files                  # run all linters
make dev-up                                 # start dev stack (NATS only by default)
make dev-up SERVICE=engine                  # start a specific service
make staging-up                             # start staging stack
make deploy-prod VERSION=vX.Y.Z             # deploy to prod with smoke test + auto-rollback
```

## Release

Releases follow a single monorepo version tag (`vX.Y.Z`) per [ADR-0016](docs/adr/0016-release-promotion-policy.md). CI builds versioned images to GHCR on tag push. The staging smoke test is a required gate before a GitHub release is created.
