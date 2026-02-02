# ADR-0017 - Per-Service Authentication and Authorization using NKEYs and ACLs

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

To secure the Ruby Core message bus, we must ensure that only trusted services can connect (Authentication) and that they can only perform their intended actions (Authorization). A shared token is too insecure, while a decentralized JWT-based model is too complex for V0. A solution is needed that provides strong, per-service identity and enforces the principle of least privilege.

## Decision

We will adopt NATS **NKEY-based authentication** with a **per-service Access Control List (ACL)** model.

1.  **Identity and Authentication:** Each service (e.g., `gateway`, `engine`) **MUST** have its own unique NKEY key pair. The private "seed" key, which is a secret, **MUST** be stored in and retrieved from Vault (per ADR-0015). Services authenticate by signing a server-provided nonce with their seed key.

2.  **Authorization (ACLs):** The NATS server configuration **MUST** define a `permissions` block for each service's public NKEY. These permissions **MUST** enforce the principle of least privilege, explicitly defining the `publish` and `subscribe` subjects required for that service's specific function. All other subjects are implicitly denied.

3.  **Subject Naming Dependency:** The effectiveness of this ACL policy is directly dependent on a formal, predictable subject naming convention. The implementation of these ACLs **MUST** adhere to the patterns to be defined in the future **ADR for Subject Naming Convention**.

4.  **Key Rotation Policy:** To maintain security, service NKEYs should be rotated periodically. The rotation **MUST** be performed without downtime using the following process:
    a. Add the service's *new* public key to its user configuration on the NATS server (allowing both old and new keys to authenticate).
    b. Deploy the service with its *new* private seed key.
    c. Once all instances of the service have been updated and are using the new key, remove the *old* public key from the NATS server configuration.

## Consequences

### Positive Consequences

*   **Strong Security Posture:** Provides strong, cryptographic, per-service identity and enforces the principle of least privilege at the infrastructure level.
*   **Damage Containment:** A compromised service is limited to its own permissions, preventing it from taking over the entire system.
*   **Uses Core NATS Features:** Leverages a mature, proven feature set without adding new infrastructure dependencies.
*   **Clear Maintenance Path:** Defines a clear, zero-downtime process for security maintenance via key rotation.

### Negative Consequences

*   **Configuration Management:** The central NATS server configuration file becomes more complex and must be managed carefully as part of the deployment process.
*   **Dependency on Naming Convention:** The security model is tightly coupled to the structure of the NATS subject names, making the future Subject Naming ADR a critical document.

### Neutral Consequences

*   This decision formalizes a critical security policy and procedure for the project.
