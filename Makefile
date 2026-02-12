# Ruby Core Makefile
# Build, test, and environment management
#
# Usage: make help

.PHONY: help build test fmt lint clean \
        dev-up dev-down dev-restart dev-logs dev-ps \
        dev-services-up dev-services-down dev-verify \
        prod-up prod-down prod-restart prod-logs prod-ps \
        deploy-prod deploy-prod-down \
        docker-ps docker-images docker-volumes docker-clean \
        setup-creds setup-creds-force nats-validate

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

# Stability test configuration
STABILITY_TIMEOUT ?= 300
STABILITY_POLL    ?= 15

# =============================================================================
# Help
# =============================================================================

help: ## Show this help message
	@echo "Ruby Core Makefile"
	@echo ""
	@echo "Usage: make [target] [SERVICE=<service>]"
	@echo ""
	@echo "Build & Test:"
	@grep -E '^(build|test|fmt|lint|clean):.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Development Environment:"
	@grep -E '^dev-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Production Environment:"
	@grep -E '^(prod-|deploy-).*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Docker:"
	@grep -E '^docker-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Setup:"
	@grep -E '^setup-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Validation:"
	@grep -E '^nats-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make dev-up              # Start dev NATS"
	@echo "  make dev-up SERVICE=nats # Start only NATS in dev"
	@echo "  make dev-services-up     # Build and start gateway+engine"
	@echo "  make deploy-prod         # Pull images and deploy to prod with stability test"
	@echo "  make setup-creds         # Generate/sync credentials from Vault"

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
# Development Services (profile: services)
# =============================================================================

dev-services-up: ## Build and start gateway+engine in dev (requires infra running)
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) --profile services up -d --build $(COMPOSE_SERVICE)

dev-services-down: ## Stop gateway+engine in dev
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) --profile services stop gateway engine

dev-verify: ## Verify gateway+engine connect via Vault-sourced mTLS
	@scripts/verify-tls-stack.sh

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
# Production Deployment
# =============================================================================

deploy-prod: nats-validate ## Pull GHCR images and deploy to prod with 5-min stability test
	@echo "=== Deploying to production ==="
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) pull
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) up -d
	@echo ""
	@echo "=== Running $(STABILITY_TIMEOUT)s stability test ==="
	@elapsed=0; \
	while [ $$elapsed -lt $(STABILITY_TIMEOUT) ]; do \
		unhealthy=$$(docker ps --filter "name=ruby-core-prod" \
			--format '{{.Names}} {{.Status}}' | grep -v "nats-init" | grep -v "Up" || true); \
		if [ -n "$$unhealthy" ]; then \
			echo "[FAIL] Unhealthy: $$unhealthy"; \
			$(COMPOSE_CMD) -f $(PROD_COMPOSE) logs --tail=30; \
			exit 1; \
		fi; \
		remaining=$$(($(STABILITY_TIMEOUT) - elapsed)); \
		echo "[$$elapsed/$(STABILITY_TIMEOUT)s] All prod containers healthy ($$remaining s remaining)"; \
		sleep $(STABILITY_POLL); \
		elapsed=$$((elapsed + $(STABILITY_POLL))); \
	done
	@echo ""
	@echo "=== Stability test PASSED ($(STABILITY_TIMEOUT)s) ==="
	@$(COMPOSE_CMD) -f $(PROD_COMPOSE) ps

deploy-prod-down: ## Stop and remove prod deployment
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) down

# =============================================================================
# Docker Utilities
# =============================================================================

# Container name prefix for filtering
CONTAINER_PREFIX := ruby-core

docker-ps: ## Show ruby-core containers only
	@docker ps -a --filter "name=$(CONTAINER_PREFIX)" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

docker-images: ## Show ruby-core images only
	@docker images --filter "reference=*ruby-core*" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}"
	@echo ""
	@echo "Base images used:"
	@docker images --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}" | grep -E "(nats|vault)" || true

docker-volumes: ## Show ruby-core volumes
	@docker volume ls --filter "name=ruby" --format "table {{.Name}}\t{{.Driver}}"

docker-clean: ## Remove stopped ruby-core containers and dangling images
	@echo "Removing stopped ruby-core containers..."
	@docker ps -a --filter "name=$(CONTAINER_PREFIX)" --filter "status=exited" -q | xargs -r docker rm || true
	@echo "Removing dangling images..."
	@docker image prune -f
	@echo "Done."

docker-nuke: ## Remove ALL ruby-core containers, images, and volumes (use with caution)
	@echo "WARNING: This will remove all ruby-core containers, images, and volumes!"
	@echo "Press Ctrl+C to cancel, or wait 5 seconds to continue..."
	@sleep 5
	@echo "Stopping and removing containers..."
	@docker ps -a --filter "name=$(CONTAINER_PREFIX)" -q | xargs -r docker rm -f || true
	@echo "Removing volumes..."
	@docker volume ls --filter "name=ruby" -q | xargs -r docker volume rm || true
	@echo "Done. Images preserved (remove manually if needed)."

# =============================================================================
# Setup
# =============================================================================

setup-creds: ## Generate and store credentials (NKEYs + TLS) in Vault
	@scripts/setup-credentials.sh

setup-creds-force: ## Regenerate ALL credentials (overwrites existing)
	@FORCE_REGEN=true scripts/setup-credentials.sh

# =============================================================================
# Validation
# =============================================================================

nats-validate: ## Validate NATS configuration (checks auth.conf, TLS, etc.)
	@deploy/base/nats/validate-config.sh
