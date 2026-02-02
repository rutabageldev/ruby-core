# ADR-0020 - Edge Authentication via Traefik for Gateway APIs

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

Any HTTP endpoints exposed by the `gateway` service are a potential attack vector and must be secured against unauthorized access. Handling authentication within the application itself mixes concerns and leads to duplicated logic. A modern, secure architecture handles authentication at the edge, before requests reach the application. Given that Traefik is already part of our infrastructure, it is the logical choice for this role.

## Decision

We will adopt an **"Edge Authentication"** strategy for all externally-facing HTTP APIs on the `gateway` service.

1.  **Primary Authentication Layer:** All authentication **MUST** be handled by **Traefik middleware**. The `gateway` application itself **MUST NOT** contain any primary authentication logic and should operate on the assumption that incoming requests are pre-authenticated.

2.  **Authentication Mechanism:**
    *   For **production** environments, the default authentication mechanism **MUST** be **JSON Web Tokens (JWTs)**. Traefik will be configured to validate JWT signatures and claims before forwarding requests.
    *   For **local development and testing**, simpler mechanisms like static API keys managed by Traefik middleware are permissible for ease of use.

3.  **Defense in Depth:** To prevent attackers from bypassing the Traefik authentication gate and accessing the `gateway` service directly, the following two measures **MUST** be implemented:
    *   **Network Isolation:** The `gateway` service's container port **MUST NOT** be published to the host machine. It must only be accessible from within the shared Docker network where Traefik is also running.
    *   **Transport-Layer Authentication:** The connection between Traefik and the `gateway` service **MUST** be secured using **Mutual TLS (mTLS)**. The `gateway`'s HTTP server must be configured to only accept connections from clients that present a valid client certificate issued by our internal Certificate Authority.

## Consequences

### Positive Consequences

*   **Strong, Multi-Layered Security:** Provides a robust security posture by validating identity at the edge (Traefik/JWT) and at the transport layer (mTLS).
*   **Separation of Concerns:** Cleanly separates authentication concerns (handled by infrastructure) from the `gateway`'s core business logic (protocol translation).
*   **Centralized and Consistent Policy:** The authentication policy is defined in a single place (Traefik configuration) and can be consistently applied to other services in the future.
*   **Secure by Default:** The `gateway` application is shielded from all unauthenticated traffic, reducing its attack surface.

### Negative Consequences

*   **Increased Configuration Complexity:** Requires more complex configuration for both Traefik (to handle JWT validation middleware) and the `gateway` service (to act as an mTLS server). This is an accepted trade-off for the security guarantees provided.

### Neutral Consequences

*   This decision formalizes the role of Traefik as a critical security enforcement point in our architecture.
