# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Ruby Core is an event-driven control plane for home automation. It uses NATS JetStream as the message broker.

## Architecture

### Core Services (in `services/`)

- **Gateway**: HA WebSocket ingest + HA REST actuation for external communication
- **Engine**: Rules/automation logic via pure pub/sub patterns
- **Notifier**: Notification handling service
- **Presence**: Presence detection service
- **Adapters**: Integration adapters (UniFi, Zigbee2MQTT)

### Shared Packages (in `pkg/`)

- **events**: Event type definitions
- **obs**: Observability utilities
- **util**: Common utilities

### Deployment (in `deploy/`)

Docker Compose stacks with base/dev/prod configurations:

- **dev**: Bind mounts + hot reload
- **prod**: Immutable images + pinned tags

## Build Commands

```bash
# Standard Go commands (Go 1.22+)
go build ./...
go test ./...
go test -v ./path/to/package -run TestName  # single test
```

## Development

Copy `.env.example` to `.env` in the appropriate deploy environment directory before starting services.
