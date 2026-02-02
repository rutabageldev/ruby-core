# ADR-0015 - Use Vault for Secrets and Environment Files for Configuration

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

Our services require both sensitive secrets (e.g., API keys, auth tokens) and non-sensitive, environment-specific configuration (e.g., NATS URLs, log levels). A policy is needed to manage both securely and effectively across different environments. The context that a HashiCorp Vault instance is already deployed in the production environment is a primary factor in this decision.

## Decision

We will adopt a hybrid strategy where HashiCorp Vault is the exclusive source of truth for secrets, and non-sensitive configuration is managed via environment files.

1. **Production Environment Policy:**
    * **Secrets:** For production, Vault **MUST** be the only source of truth for all secrets. Storing secrets in `.env` files or any other plaintext format in production is forbidden.
    * **Configuration:** Non-sensitive, environment-specific settings **MUST** be provided via a `prod.env` file and injected as environment variables by Docker Compose.

2. **Local Development Policy:**
    * To maintain consistency with production, local development **MUST** also use Vault for secrets management. Developers are required to run a local, dev-mode Vault server (e.g., via `vault server -dev`). The dev-mode server is for local workstation use only and **MUST never be used for shared environments.**
    * A local `.env` file will be used for non-sensitive configuration and to provide the application with the address and token for the local Vault instance. The local Vault root token **MUST be treated as a secret** and be git-ignored.

3. **Startup & Failure Policy:**
    * All services **MUST** integrate with a Vault client and fetch their required secrets upon initialization.
    * If a service cannot connect to, authenticate with, or fetch its required secrets from Vault at startup, it **MUST fail fast** and exit immediately. Running in a degraded or insecure state with missing secrets is explicitly forbidden.

4. **Offline Development Fallback:**
    * An "offline" development mode that does not require Vault is permissible **only under the following strict conditions**:
        * Secrets required by the service are fully mocked within the code and are only enabled by a specific build tag (e.g., `offline`).
        * The service configuration for this mode does not attempt to connect to any external, authenticated systems (like a production Home Assistant instance).
    * This mode is intended only for disconnected development (e.g., on a plane) and is not the default local workflow.

## Consequences

### Positive Consequences

* **Production-Grade Security:** Provides a consistent, best-practice security model for secrets in all environments by leveraging an industry-standard tool.
* **Leverages Existing Infrastructure:** Utilizes the already-deployed production Vault instance, minimizing new infrastructure setup.
* **Prevents Insecure States:** The "fail fast" policy ensures that services cannot run in a partially configured or insecure state.
* **Clear Separation:** Enforces a clean and unambiguous separation between secrets and non-sensitive configuration.

### Negative Consequences

* **Increased Local Setup Complexity:** The requirement to run a local Vault dev server adds a step to the default local development setup process. This is an acceptable trade-off for security and consistency.
* **Startup Dependency:** All services now have a hard dependency on the availability of a Vault instance (local or production) to start successfully, unless using the explicit offline mode.

### Neutral Consequences

* This decision formally establishes HashiCorp Vault as a critical, foundational component of the Ruby Core infrastructure.
