# Ruby Core Makefile
# Phase 1: Minimal build, test, and environment management
#
# Usage: make help

.PHONY: help build test fmt lint clean \
        dev-up dev-down dev-restart dev-logs \
        prod-up prod-down prod-restart prod-logs \
        nats-validate

# Default target
.DEFAULT_GOAL := help

# =============================================================================
# Configuration
# =============================================================================

# Compose files
DEV_COMPOSE  := deploy/dev/compose.dev.yaml
PROD_COMPOSE := deploy/prod/compose.prod.yaml

# Optional service filter (e.g., make dev-up SERVICE=nats)
SERVICE ?=

# Compose command with optional service
COMPOSE_CMD = docker compose
ifdef SERVICE
  COMPOSE_SERVICE = $(SERVICE)
else
  COMPOSE_SERVICE =
endif

# =============================================================================
# Help
# =============================================================================

help: ## Show this help message
	@echo "Ruby Core - Phase 1 Makefile"
	@echo ""
	@echo "Usage: make [target] [SERVICE=<service>]"
	@echo ""
	@echo "Build & Test:"
	@grep -E '^(build|test|fmt|lint|clean):.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'
	@echo ""
	@echo "Development Environment:"
	@grep -E '^dev-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'
	@echo ""
	@echo "Production Environment:"
	@grep -E '^prod-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'
	@echo ""
	@echo "Validation:"
	@grep -E '^nats-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make dev-up              # Start all dev services"
	@echo "  make dev-up SERVICE=nats # Start only NATS in dev"
	@echo "  make dev-logs SERVICE=vault # Tail Vault logs"
	@echo "  make prod-restart SERVICE=nats # Restart NATS in prod"

# =============================================================================
# Build & Test
# =============================================================================

build: ## Build all Go packages
	go build ./...

test: ## Run all tests
	go test ./...

fmt: ## Format code with gofumpt (if installed)
	@if command -v gofumpt >/dev/null 2>&1; then \
		gofumpt -w .; \
	else \
		echo "gofumpt not installed. Install with: go install mvdan.cc/gofumpt@latest"; \
	fi

lint: ## Run linter (Phase 6 - not yet enabled)
	@echo "Linting is scheduled for Phase 6."
	@echo "To run manually: golangci-lint run ./..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo ""; \
		echo "golangci-lint is installed. Running..."; \
		golangci-lint run ./...; \
	fi

clean: ## Remove build artifacts
	go clean ./...
	rm -f coverage.out

# =============================================================================
# Development Environment
# =============================================================================

dev-up: ## Start dev environment (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) up -d $(COMPOSE_SERVICE)

dev-down: ## Stop dev environment (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) down $(COMPOSE_SERVICE)

dev-restart: ## Restart dev environment (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) restart $(COMPOSE_SERVICE)

dev-logs: ## Tail dev logs (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) logs -f $(COMPOSE_SERVICE)

dev-ps: ## Show dev container status
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) ps

# =============================================================================
# Production Environment
# =============================================================================

prod-up: ## Start prod environment (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) up -d $(COMPOSE_SERVICE)

prod-down: ## Stop prod environment (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) down $(COMPOSE_SERVICE)

prod-restart: ## Restart prod environment (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) restart $(COMPOSE_SERVICE)

prod-logs: ## Tail prod logs (or SERVICE=<name>)
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) logs -f $(COMPOSE_SERVICE)

prod-ps: ## Show prod container status
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) ps

# =============================================================================
# Validation (Phase 1)
# =============================================================================

nats-validate: ## Validate NATS configuration (checks for placeholders, TLS, etc.)
	@deploy/base/nats/validate-config.sh
