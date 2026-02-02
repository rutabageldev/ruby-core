# ADR-0018 - Mandate End-to-End Transport Layer Security (TLS)

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

Unencrypted, plaintext communication between our services and infrastructure components is a critical security vulnerability. To protect against eavesdropping and man-in-the-middle attacks, we must adopt a "zero-trust" network policy where all data in transit is encrypted by default, regardless of whether the traffic is internal or external.

## Decision

We will adopt a **"TLS Everywhere"** policy.

1. **Scope:** All TCP-based network communication within, to, and from the Ruby Core system **MUST** be encrypted using TLS v1.2 or higher. This includes, but is not limited to:
    * Client connections from our services to the NATS server.
    * Server-to-server connections within a NATS cluster.
    * `gateway` API calls to Home Assistant.
    * Any future internal HTTP or gRPC APIs between services.

2. **Internal Communication (mTLS):** For internal service-to-service connections, specifically client connections to the NATS server, we **MUST** use **Mutual TLS (mTLS)**. This requires both the server and the connecting client to present and validate certificates from a trusted Certificate Authority (CA), providing strong, transport-level authentication in both directions.

3. **Local Development Environment:** The "TLS Everywhere" policy **MUST** also be enforced in the default local development environment. Plaintext communication is forbidden. Developers will use a local CA (e.g., via `mkcert` or a dev-mode Vault PKI engine) to generate the necessary certificates for their local services.

4. **Certificate Management:** The production internal CA will be managed using the existing HashiCorp Vault instance's PKI Secrets Engine. The existing Traefik proxy can be used for TLS termination and certificate management for any externally exposed endpoints.

## Consequences

### Positive Consequences

* **Strong Security Posture:** Provides robust, end-to-end encryption for all data in transit, protecting against eavesdropping and tampering from both internal and external network threats.
* **Defense in Depth:** The use of mTLS provides a strong, transport-level identity check that complements the application-level NKEY authentication for NATS connections (per ADR-0017).
* **Secure by Default:** Enforcing TLS in local development reduces environment drift between development and production, and fosters a security-first mindset.

### Negative Consequences

* **Increased Operational Overhead:** Requires the setup and management of a private CA (in Vault) and the distribution and rotation of TLS certificates for all services.
* **Increased Local Setup Complexity:** The requirement to generate and manage a local CA and certificates adds complexity to the local development setup process. This is an accepted trade-off for security and consistency.

### Neutral Consequences

* This decision formalizes TLS as a non-negotiable, baseline requirement for all network communication in the project.
