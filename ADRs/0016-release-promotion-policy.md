# ADR-0016 - Adopt a Git Tag-based Release and Promotion Policy

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

A clear, safe, and repeatable process is required for versioning, building, and deploying our services. This policy must define how release artifacts are created, tagged, and promoted to production, and how to roll back a release if necessary. This ADR defines this process, including the choice of container registry.

## Decision

We will adopt a **Semantic Versioning** strategy for the project, triggered by **annotated Git tags**, to create and promote release artifacts to the **GitHub Container Registry (GHCR)**.

1.  **Container Registry:** The official container registry for this project **MUST** be the GitHub Container Registry (GHCR).

2.  **Monorepo Versioning Policy:** A single version number applies to the entire monorepo. When a release tag (e.g., `v1.2.3`) is created, all services within the repository (e.g., `engine`, `gateway`) are versioned together under that same tag.

3.  **Promotion Boundary & Artifacts:**
    *   Builds on the `main` branch **MAY** produce development artifacts tagged with the Git SHA (e.g., `ghcr.io/my-org/engine:a1b2c3d`). These artifacts **MUST NOT** be used in production.
    *   A "release" is created *only* by pushing an annotated Git tag matching the pattern `v*` (e.g., `v1.2.3`) to the repository.
    *   This action **MUST** trigger a release CI workflow that builds and pushes versioned images for all services (e.g., `ghcr.io/my-org/engine:v1.2.3`).

4.  **Immutability Policy:** Release artifacts are immutable. A Git tag, once pushed, **MUST NOT** be moved or deleted. A versioned container image, once pushed to GHCR, **MUST NOT** be overwritten.

5.  **Tag Security:** Release tags **MUST** be annotated (`git tag -a`). For enhanced provenance and to verify the author, release tags **SHOULD** also be cryptographically signed (`git tag -s`).

6.  **Deployment and Rollback (V0):** For the V0 implementation, deployment is a manual process of updating the image tags in the production `docker-compose.yml` file to a specific version number and redeploying. Rollback is achieved by reverting the image tag in the compose file to a previous known-good version and redeploying.

## Consequences

### Positive Consequences

*   **Clear and Intentional Releases:** The process for cutting a release is an explicit, auditable action in Git. Semantic versioning clearly communicates the impact of a release.
*   **Immutable & Traceable Artifacts:** Every production deployment is tied to an immutable, versioned container image, which is in turn tied to a specific Git tag and commit.
*   **Safe Operations:** Makes both deployment and rollback simple, predictable, and low-risk procedures.
*   **Simplified Monorepo Management:** A single version for the whole repository is easy to understand and manage.

### Negative Consequences

*   **Process Overhead:** Requires developers to follow a formal process of creating and pushing versioned Git tags to produce a release.
*   **Minor Inefficiency:** Services that have no code changes will still be re-built and re-versioned as part of a new release. This is an acceptable trade-off for the simplicity of a single project version.

### Neutral Consequences

*   This decision formalizes GHCR as the project's official container registry and Git as the source of truth for release orchestration.
