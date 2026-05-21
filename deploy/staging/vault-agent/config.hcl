# Vault Agent — NATS server cert auto-renewer (PLAN-0008 follow-up)
#
# Authenticates via the foundation-agent-ruby-core-nats-server AppRole
# (created in foundation PR #78) and renders the NATS server cert + key + CA
# from a single pki_int/issue/ruby-core-nats-server call. The post-render
# script atomic-writes the three files into the shared `nats-certs` volume
# and SIGHUPs NATS via the Docker API. NATS reloads its TLS config in-place
# (~40ms); existing mTLS connections survive (only new handshakes use the
# rotated cert).
#
# Renewal cadence: Vault Agent's template engine refreshes at TTL/2 — same
# cadence as the in-process renewal goroutine on the 5 Go services (24h-ish
# with the default lease behavior).
#
# Image: foundation/vault-agent:1.21.4 — hashicorp/vault + curl baked in for
# the Docker API SIGHUP call (foundation drift #75 / PLAN-0007).

pid_file = "/tmp/vault-agent.pid"

vault {
  address = "https://vault:8200"
  ca_cert = "/vault/tls/vault-ca.crt"
}

auto_auth {
  method "approle" {
    config = {
      role_id_file_path                   = "/vault/role-id"
      secret_id_file_path                 = "/vault/secret-id"
      remove_secret_id_file_after_reading = false
    }
  }

  sink "file" {
    config = {
      path = "/tmp/.vault-agent-token"
    }
  }
}

# Single template stanza — NATS needs cert+key+ca, all three from one
# issuance (so the chain matches). Template emits them with ==CA== and
# ==KEY== separators; post-render.sh splits, atomic-renames into /certs/,
# and signals NATS.
template {
  source      = "/etc/vault-agent/nats-server.tpl"
  destination = "/rendered/nats-bundle.tmp"
  perms       = "0644"
  command     = ["/etc/vault-agent/post-render.sh"]
}
