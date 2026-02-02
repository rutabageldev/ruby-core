# ruby-core

Ruby Core is the event-driven control plane for the home.

Core components:

- NATS JetStream broker
- Gateway: HA WebSocket ingest + HA REST actuation
- Engine: rules/automation logic (pure pub/sub)

Environments:

- dev: bind mounts + hot reload
- prod: immutable images + pinned tags

See deploy/ for docker compose stacks.

## Devcontainer

Open in VS Code with the Dev Containers extension or use the GitHub Codespaces button. The container includes Go and pre-commit tooling.

```bash
go test ./...              # run tests
pre-commit run --all-files # run linters
```
