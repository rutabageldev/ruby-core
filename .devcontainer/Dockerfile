FROM golang:1.25.6

# DL3008: apt version pinning is impractical for devcontainers - versions change
# with base image updates and security patches, causing build failures.
# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    python3-venv \
    pipx \
    git \
    curl \
    ca-certificates \
    unzip \
    && rm -rf /var/lib/apt/lists/* \
    && pipx install pre-commit==4.5.1 \
    && git config --global --add safe.directory /workspace

# Install Docker CLI for remote Docker host communication over TLS
# Uses official Docker convenience script, installs CLI only (no daemon)
# hadolint ignore=DL3008,DL4006
RUN curl -fsSL https://get.docker.com | sh \
    && rm -rf /var/lib/apt/lists/*

ENV PATH="/root/.local/bin:$PATH"

WORKDIR /workspace
