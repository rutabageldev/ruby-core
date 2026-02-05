# Docker TLS Setup for Devcontainer

This document describes how to configure TLS-secured Docker API access from the
devcontainer to the host Docker daemon using Vault-managed certificates.

## Architecture

```
┌─────────────────────┐         TLS (mTLS)        ┌─────────────────────┐
│   Devcontainer      │ ──────────────────────────▶│   Host Docker       │
│                     │        tcp://:2376         │   Daemon            │
│ DOCKER_HOST=tcp://  │                            │                     │
│ DOCKER_TLS_VERIFY=1 │                            │ /etc/docker/certs/  │
│ DOCKER_CERT_PATH=   │                            │   ca.pem            │
│   /run/secrets/     │                            │   server.pem        │
│   docker/           │                            │   server-key.pem    │
│     ca.pem          │                            │                     │
│     cert.pem        │◀─────── Vault PKI ────────▶│                     │
│     key.pem         │                            │                     │
└─────────────────────┘                            └─────────────────────┘
```

## Prerequisites

- Ubuntu host with Docker installed
- Vault server with PKI secrets engine enabled
- Network connectivity between devcontainer and host on chosen port

## 1. Host Docker TLS Configuration

### 1.1 Choose and Verify Port

Default port is 2376. Verify it's available:

```bash
# Check if port 2376 is in use
sudo ss -tlnp | grep :2376

# If occupied, use alternative port 12376
PORT=2376  # or 12376 if 2376 is taken
```

### 1.2 Create Certificate Directory

```bash
sudo mkdir -p /etc/docker/certs
sudo chmod 700 /etc/docker/certs
```

### 1.3 Issue Server Certificate from Vault

```bash
# Set Vault address and authenticate
export VAULT_ADDR="https://vault.example.com:8200"
vault login -method=<your-auth-method>

# Get host IP/hostname for SAN
HOST_IP=$(hostname -I | awk '{print $1}')
HOST_FQDN=$(hostname -f)

# Issue server certificate
vault write -format=json pki/issue/docker-server \
  common_name="docker-server" \
  ip_sans="${HOST_IP},127.0.0.1" \
  alt_names="${HOST_FQDN},localhost" \
  ttl="720h" > /tmp/docker-server-cert.json

# Extract and install certificates
jq -r '.data.certificate' /tmp/docker-server-cert.json | sudo tee /etc/docker/certs/server.pem > /dev/null
jq -r '.data.private_key' /tmp/docker-server-cert.json | sudo tee /etc/docker/certs/server-key.pem > /dev/null
jq -r '.data.issuing_ca' /tmp/docker-server-cert.json | sudo tee /etc/docker/certs/ca.pem > /dev/null

# Set permissions
sudo chmod 644 /etc/docker/certs/ca.pem /etc/docker/certs/server.pem
sudo chmod 600 /etc/docker/certs/server-key.pem

# Cleanup
rm /tmp/docker-server-cert.json
```

### 1.4 Configure Docker Daemon

Create or update `/etc/docker/daemon.json`:

```bash
sudo tee /etc/docker/daemon.json << 'EOF'
{
  "hosts": ["unix:///var/run/docker.sock", "tcp://0.0.0.0:2376"],
  "tlsverify": true,
  "tlscacert": "/etc/docker/certs/ca.pem",
  "tlscert": "/etc/docker/certs/server.pem",
  "tlskey": "/etc/docker/certs/server-key.pem"
}
EOF
```

**Note:** If using port 12376, replace `2376` in the hosts array.

### 1.5 Fix systemd Override (Required)

Docker's systemd unit may override daemon.json hosts. Create override:

```bash
sudo mkdir -p /etc/systemd/system/docker.service.d
sudo tee /etc/systemd/system/docker.service.d/override.conf << 'EOF'
[Service]
ExecStart=
ExecStart=/usr/bin/dockerd
EOF
```

### 1.6 Restart Docker and Verify

```bash
sudo systemctl daemon-reload
sudo systemctl restart docker

# Verify Docker is listening on TLS port
sudo ss -tlnp | grep :2376
# Expected: LISTEN ... *:2376 ... users:(("dockerd",...))

# Verify Docker is still accessible locally
docker ps
```

## 2. Vault PKI Configuration

### 2.1 Enable PKI (if not already enabled)

```bash
vault secrets enable pki
vault secrets tune -max-lease-ttl=87600h pki
```

### 2.2 Generate Root CA (or use existing)

```bash
vault write -field=certificate pki/root/generate/internal \
  common_name="Docker PKI CA" \
  ttl=87600h > /tmp/ca.pem
```

### 2.3 Create Server Role

```bash
vault write pki/roles/docker-server \
  allowed_domains="docker-server,localhost" \
  allow_any_name=true \
  allow_ip_sans=true \
  allow_localhost=true \
  server_flag=true \
  client_flag=false \
  key_type="rsa" \
  key_bits=2048 \
  max_ttl="720h"
```

### 2.4 Create Client Role

```bash
vault write pki/roles/docker-client \
  allowed_domains="docker-client" \
  allow_any_name=true \
  client_flag=true \
  server_flag=false \
  key_type="rsa" \
  key_bits=2048 \
  max_ttl="24h"
```

### 2.5 Create Policy for Client Cert Issuance

```bash
vault policy write docker-client-cert - << 'EOF'
path "pki/issue/docker-client" {
  capabilities = ["create", "update"]
}
path "pki/cert/ca" {
  capabilities = ["read"]
}
EOF
```

## 3. Devcontainer Client Certificate Injection

### 3.1 Vault Agent Sidecar (Recommended)

Configure Vault Agent to inject client certs at container start:

```hcl
# vault-agent-config.hcl
auto_auth {
  method "kubernetes" {
    config = {
      role = "devcontainer"
    }
  }
}

template {
  destination = "/run/secrets/docker/ca.pem"
  contents = <<EOF
{{- with secret "pki/cert/ca" }}{{ .Data.certificate }}{{ end }}
EOF
}

template {
  destination = "/run/secrets/docker/cert.pem"
  contents = <<EOF
{{- with secret "pki/issue/docker-client" "common_name=docker-client" "ttl=24h" }}{{ .Data.certificate }}{{ end }}
EOF
}

template {
  destination = "/run/secrets/docker/key.pem"
  contents = <<EOF
{{- with secret "pki/issue/docker-client" "common_name=docker-client" "ttl=24h" }}{{ .Data.private_key }}{{ end }}
EOF
}
```

### 3.2 Manual Injection (Development)

For local development without Vault Agent:

```bash
# Issue client cert
vault write -format=json pki/issue/docker-client \
  common_name="docker-client" \
  ttl="24h" > /tmp/docker-client-cert.json

# Extract certs
mkdir -p ~/.docker-tls
jq -r '.data.certificate' /tmp/docker-client-cert.json > ~/.docker-tls/cert.pem
jq -r '.data.private_key' /tmp/docker-client-cert.json > ~/.docker-tls/key.pem
jq -r '.data.issuing_ca' /tmp/docker-client-cert.json > ~/.docker-tls/ca.pem

# Mount into devcontainer via docker run or compose
# -v ~/.docker-tls:/run/secrets/docker:ro
```

## 4. Environment Variables

Set these before launching devcontainer (or in your shell profile):

```bash
# Host IP or DNS name accessible from devcontainer
export DOCKER_TLS_HOST="192.168.1.100"  # or hostname

# Port (default 2376, or 12376 if using alternative)
export DOCKER_TLS_PORT="2376"
```

The devcontainer.json uses these with fallbacks:

- `DOCKER_TLS_HOST` defaults to `host.docker.internal`
- `DOCKER_TLS_PORT` defaults to `2376`

## 5. Validation

### 5.1 Host Validation

```bash
# Confirm port is listening
sudo ss -tlnp | grep :2376

# Test TLS connection with openssl
openssl s_client -connect localhost:2376 -CAfile /etc/docker/certs/ca.pem \
  -cert /path/to/client-cert.pem -key /path/to/client-key.pem
```

### 5.2 Devcontainer Validation

```bash
# Inside devcontainer
docker version
docker ps

# Expected output: server version info, container list
```

## 6. Troubleshooting

### Connection Refused

- Verify Docker is listening: `sudo ss -tlnp | grep :2376`
- Check firewall: `sudo ufw status` or `sudo iptables -L`
- Verify systemd override is applied: `systemctl cat docker`

### Certificate Errors

- Verify CA matches: compare ca.pem on host and in devcontainer
- Check cert validity: `openssl x509 -in cert.pem -noout -dates`
- Verify SANs include host IP: `openssl x509 -in server.pem -noout -text | grep -A1 "Subject Alternative Name"`

### Permission Denied

- Client key must be readable: `chmod 600 /run/secrets/docker/key.pem`
- Ensure secrets mount is not empty

## 7. Certificate Renewal

Vault-issued certs are short-lived by design. For production:

1. Use Vault Agent with auto-renewal templates
2. Set up systemd timer to refresh host server cert before expiry
3. Consider cert-manager or similar for Kubernetes deployments

### Host Cert Renewal Script

```bash
#!/bin/bash
# /usr/local/bin/renew-docker-cert.sh

set -euo pipefail

vault write -format=json pki/issue/docker-server \
  common_name="docker-server" \
  ip_sans="$(hostname -I | awk '{print $1}'),127.0.0.1" \
  alt_names="$(hostname -f),localhost" \
  ttl="720h" > /tmp/docker-server-cert.json

jq -r '.data.certificate' /tmp/docker-server-cert.json > /etc/docker/certs/server.pem
jq -r '.data.private_key' /tmp/docker-server-cert.json > /etc/docker/certs/server-key.pem

chmod 644 /etc/docker/certs/server.pem
chmod 600 /etc/docker/certs/server-key.pem

rm /tmp/docker-server-cert.json

systemctl reload docker
```

Add to cron or systemd timer to run weekly.
