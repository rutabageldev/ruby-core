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
        setup-creds setup-creds-force setup-staging-creds nats-validate \
        ada-db-snapshot ada-db-seed ada-db-clear-test \
        openapi-bundle openapi-gen openapi-lint openapi-diff openapi-verify

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

# =============================================================================
# API / OpenAPI (spec-first; ADR-0041)
# =============================================================================
# Pinned tool versions. ogen is pinned in services/api/oas/generate.go.
# redocly + spectral run via npx (Node); oasdiff via go run; the Python client is
# expected on PATH (install once: pipx install openapi-python-client).
REDOCLY_VERSION  ?= 1.34.5
SPECTRAL_VERSION ?= 6.15.0
OASDIFF_VERSION  ?= v1.20.1
OPENAPI_PY_CLIENT ?= openapi-python-client

openapi-bundle: ## Bundle the OpenAPI fragments into api/openapi.gen.yaml
	npx --yes @redocly/cli@$(REDOCLY_VERSION) bundle api/openapi/openapi.root.yaml -o api/openapi.gen.yaml

openapi-gen: openapi-bundle ## Regenerate all OpenAPI artifacts: bundle, ogen Go server/client, Python client
	go generate ./services/api/oas/...
	@mkdir -p clients
	@if ! command -v $(OPENAPI_PY_CLIENT) >/dev/null 2>&1; then \
		echo "ERROR: $(OPENAPI_PY_CLIENT) not found. Install with: pipx install openapi-python-client"; exit 1; \
	fi
	$(OPENAPI_PY_CLIENT) generate --path api/openapi.gen.yaml --output-path clients/python \
		--meta none --overwrite --config api/openapi-python-client.yaml

openapi-lint: openapi-bundle ## Lint the bundled spec (Spectral: description + example required everywhere)
	npx --yes @stoplight/spectral-cli@$(SPECTRAL_VERSION) lint api/openapi.gen.yaml --ruleset api/.spectral.yaml

openapi-diff: openapi-bundle ## Detect breaking API changes vs origin/main (oasdiff)
	@base=$$(mktemp); \
	if git show origin/main:api/openapi.gen.yaml > $$base 2>/dev/null && [ -s $$base ]; then \
		go run github.com/oasdiff/oasdiff@$(OASDIFF_VERSION) breaking $$base api/openapi.gen.yaml; \
	else \
		echo "openapi-diff: no base spec on origin/main yet — skipping breaking-change check"; \
	fi; \
	rm -f $$base

openapi-verify: openapi-gen ## Regenerate and fail if anything drifted (mirrors the CI codegen gate)
	@git diff --exit-code -- api/openapi.gen.yaml services/api/oas clients/python \
		|| { echo "ERROR: generated OpenAPI artifacts are out of date. Run 'make openapi-gen' and commit."; exit 1; }
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

setup-staging-creds: ## Generate staging NKEY seeds in Vault (secret/ruby-core/staging/nats/*). TLS is PKI-issued at runtime.
	@( . deploy/dev/.env && \
	   VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER" \
	   VAULT_SECRET_PREFIX=secret/ruby-core/staging \
	   ./scripts/setup-credentials.sh )

staging-up: ## Start staging stack (requires deploy/staging/.env with VAULT_TOKEN)
	$(COMPOSE_CMD) -f $(STAGING_COMPOSE) up -d $(COMPOSE_SERVICE)

staging-down: ## Stop staging stack and remove volumes
	$(COMPOSE_CMD) -f $(STAGING_COMPOSE) down -v

deploy-staging: ## Pull images, rolling-restart permanent staging, run smoke test (requires VERSION=)
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

setup-creds: ## Generate NKEY seeds in Vault. TLS is PKI-issued at runtime (PLAN-0008 Stage 4.B).
	@( . deploy/dev/.env && VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER" scripts/setup-credentials.sh )

setup-creds-force: ## Regenerate ALL NKEY seeds (overwrites existing)
	@( . deploy/dev/.env && VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER" FORCE_REGEN=true scripts/setup-credentials.sh )

cleanup-mkcert-kv-bundles: ## Delete the legacy mkcert KV bundles at secret/ruby-core/{,staging/}tls/*. Requires CONFIRM=yes. One-shot post-Stage-4.B.
	@if [ "$(CONFIRM)" != "yes" ]; then \
	  echo "DRY RUN — pass CONFIRM=yes to actually delete:"; \
	  echo ""; \
	  for prefix in secret/ruby-core/tls secret/ruby-core/staging/tls; do \
	    for svc in admin gateway engine notifier presence audit-sink nats-server; do \
	      echo "  vault kv metadata delete $$prefix/$$svc"; \
	    done; \
	  done; \
	  echo ""; \
	  echo "These bundles were the rollback target during PLAN-0008 Stages 2/3/4.A."; \
	  echo "Stage 4.B (PR including this target) cuts the cord — admin migrates to"; \
	  echo "PKI, the transitional bundle code is removed, and these KV paths are no"; \
	  echo "longer read by any service or script."; \
	  echo ""; \
	  echo "To execute: make cleanup-mkcert-kv-bundles CONFIRM=yes"; \
	else \
	  set -e; \
	  . deploy/dev/.env; \
	  export VAULT_TOKEN="$$VAULT_TOKEN_RUBY_CORE_WRITER"; \
	  export VAULT_ADDR="$${VAULT_ADDR:-https://127.0.0.1:8200}"; \
	  export VAULT_CACERT="$${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"; \
	  for prefix in secret/ruby-core/tls secret/ruby-core/staging/tls; do \
	    for svc in admin gateway engine notifier presence audit-sink nats-server; do \
	      echo "Deleting $$prefix/$$svc..."; \
	      vault kv metadata delete "$$prefix/$$svc" 2>&1 | grep -v "^$$" || true; \
	    done; \
	  done; \
	  echo ""; \
	  echo "Done. All mkcert KV bundles removed."; \
	fi

ada-db-snapshot: ## Snapshot Ada tables to a local file (requires ENV=dev|staging|prod)
	@ENV="$(ENV)" scripts/ada-db-snapshot.sh

ada-db-seed: ## Clear-then-seed representative Ada test data (requires ENV= and DOB=<RFC3339>)
	@ENV="$(ENV)" DOB="$(DOB)" scripts/ada-db-seed.sh

ada-db-clear-test: ## DESTRUCTIVE: delete only test=true Ada rows (ENV=; dry-run unless CONFIRM=yes)
	@ENV="$(ENV)" CONFIRM="$(CONFIRM)" scripts/ada-db-clear-test.sh

# =============================================================================
# Validation
# =============================================================================

nats-validate: ## Validate NATS configuration (checks auth.conf, TLS, etc.)
	@deploy/base/nats/validate-config.sh
