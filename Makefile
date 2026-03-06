# Ruby Core Makefile
# Build, test, and environment management
#
# Usage: make help

.PHONY: help build test test-fast test-integration fmt lint clean \
        dev-up dev-down dev-restart dev-logs dev-ps \
        dev-services-up dev-services-down dev-verify \
        dev-air-up dev-air-down \
        prod-up prod-down prod-restart prod-logs prod-ps \
        deploy-prod deploy-prod-down \
        staging-up staging-down deploy-staging \
        docker-ps docker-images docker-volumes docker-clean \
        setup-creds setup-creds-force setup-staging-creds nats-validate

# Default target
.DEFAULT_GOAL := help

# =============================================================================
# Configuration
# =============================================================================

# Compose files
DEV_COMPOSE     := deploy/dev/compose.dev.yaml
AIR_COMPOSE     := deploy/dev/compose.air.yaml
PROD_COMPOSE    := deploy/prod/compose.prod.yaml
STAGING_COMPOSE := deploy/staging/compose.staging.yaml

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
	@echo "Ruby Core Makefile"
	@echo ""
	@echo "Usage: make [target] [SERVICE=<service>]"
	@echo ""
	@echo "Build & Test:"
	@grep -E '^(build|test|test-fast|test-integration|fmt|lint|clean):.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Development Environment:"
	@grep -E '^dev-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Production Environment:"
	@grep -E '^(prod-|deploy-prod).*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Staging Environment:"
	@grep -E '^(staging-|deploy-staging).*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Docker:"
	@grep -E '^docker-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Setup & Credentials:"
	@grep -E '^setup-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Validation:"
	@grep -E '^nats-.*:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make dev-up              # Start dev NATS"
	@echo "  make dev-up SERVICE=nats # Start only NATS in dev"
	@echo "  make dev-services-up     # Build and start services (prod-like images)"
	@echo "  make dev-air-up          # Build and start services with air live-reload"
	@echo "  make deploy-prod         # Pull images and deploy to prod with stability test"
	@echo "  make setup-creds         # Generate/sync credentials from Vault"

# =============================================================================
# Build & Test
# =============================================================================

build: ## Build all Go packages
	go build ./...

test: ## Run all tests (fast unit tests; use test-integration for integration tests)
	go test -tags=fast -race ./...

test-fast: ## Run fast unit tests (same as test; for explicit pre-commit parity)
	go test -tags=fast -race ./...

test-integration: ## Run integration tests (requires Docker for testcontainers)
	go test -tags=integration -race ./...

fmt: ## Format code with gofumpt (if installed)
	@if command -v gofumpt >/dev/null 2>&1; then \
		gofumpt -w .; \
	else \
		echo "gofumpt not installed. Install with: go install mvdan.cc/gofumpt@latest"; \
	fi

lint: ## Run golangci-lint (enforced per ADR-0011/ADR-0013)
	golangci-lint run ./...

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

dev-services-down: ## Stop gateway+engine+notifier+presence in dev
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) --profile services stop gateway engine notifier presence

dev-verify: ## Verify gateway+engine connect via Vault-sourced mTLS
	@scripts/verify-tls-stack.sh

dev-air-up: ## Start services with air live-reload (run make dev-up first for NATS)
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) -f $(AIR_COMPOSE) --profile services up -d --build $(COMPOSE_SERVICE)

dev-air-down: ## Stop air live-reload services
	$(COMPOSE_CMD) -f $(DEV_COMPOSE) -f $(AIR_COMPOSE) --profile services stop $(COMPOSE_SERVICE)

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

ghcr-login: ## Authenticate Docker with GHCR using the local gh CLI token
	@gh auth token | docker login ghcr.io -u $$(gh api user --jq .login) --password-stdin

deploy-prod: nats-validate ghcr-login ## Pull GHCR images and deploy to prod with smoke test + auto-rollback
	@scripts/deploy-prod.sh

deploy-prod-down: ## Stop and remove prod deployment
	$(COMPOSE_CMD) -f $(PROD_COMPOSE) down

# =============================================================================
# Staging Environment
# =============================================================================

setup-staging-creds: ## Generate staging credentials in Vault (secret/ruby-core-staging/*)
	@( . deploy/dev/.env && \
	   VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER" \
	   VAULT_SECRET_PREFIX=secret/ruby-core-staging \
	   EXTRA_NATS_SANS=ruby-core-staging-nats \
	   ./scripts/setup-credentials.sh )

staging-up: ## Start staging stack (requires deploy/staging/.env with VAULT_TOKEN)
	$(COMPOSE_CMD) -f $(STAGING_COMPOSE) up -d $(COMPOSE_SERVICE)

staging-down: ## Stop staging stack and remove volumes
	$(COMPOSE_CMD) -f $(STAGING_COMPOSE) down -v

deploy-staging: ## Pull images, start staging, run smoke test, tear down (requires VERSION=)
	@VERSION="$(VERSION)" ./scripts/deploy-staging.sh "$(VERSION)"

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
	@( . deploy/dev/.env && VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER" scripts/setup-credentials.sh )

setup-creds-force: ## Regenerate ALL credentials (overwrites existing)
	@( . deploy/dev/.env && VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER" FORCE_REGEN=true scripts/setup-credentials.sh )

# =============================================================================
# Validation
# =============================================================================

nats-validate: ## Validate NATS configuration (checks auth.conf, TLS, etc.)
	@deploy/base/nats/validate-config.sh
