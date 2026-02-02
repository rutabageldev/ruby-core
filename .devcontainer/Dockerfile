FROM golang:1.25

# DL3008: apt version pinning is impractical for devcontainers - versions change
# with base image updates and security patches, causing build failures.
# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    python3-venv \
    git \
    curl \
    ca-certificates \
    unzip \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --no-cache-dir --break-system-packages pre-commit==4.5.1 \
    && git config --global --add safe.directory /workspace

WORKDIR /workspace
