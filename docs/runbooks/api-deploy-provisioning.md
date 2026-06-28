# Runbook — `services/api` deploy provisioning

Host-side prerequisites for deploying `services/api` (ROADMAP-0012, ADR-0040). The api
service is **not** gated by a compose profile anymore — a release that includes it will
try to start it, so **provision these before the release deploys** or the container
crash-loops. All Vault writes use the `ruby-core-writer` token (see project memory) and
the `VAULT_CACERT` flag (Vault is TLS-only on the host).

```bash
export VAULT_ADDR=https://127.0.0.1:8200
export VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt
export VAULT_TOKEN="$VAULT_TOKEN_RUBY_CORE_WRITER"   # from deploy/prod/.env
```

## 1. Read-only Postgres role (ADR-0040)

The api connects with a `SELECT`-only role so a read endpoint can't mutate state even
on a bug. Run as a Postgres admin against foundation-postgres, once per database
(`ruby_core`, `ruby_core_staging`, `ruby_core_dev`):

```sql
CREATE ROLE ruby_core_ro LOGIN PASSWORD '<generated>';   -- one role; reuse across DBs
GRANT CONNECT ON DATABASE ruby_core TO ruby_core_ro;
GRANT USAGE ON SCHEMA public TO ruby_core_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO ruby_core_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO ruby_core_ro;
```

(For the staging/dev databases, repeat the `GRANT CONNECT … <db>` + the schema grants
while connected to that database.)

Store the DSN fields in Vault (the api reads `secret/data/ruby-core/postgres_readonly`):

```bash
vault kv put secret/ruby-core/postgres_readonly \
  host=<host> port=<port> dbname=ruby_core user=ruby_core_ro password='<generated>'
vault kv put secret/ruby-core/staging/postgres_readonly \
  host=<host> port=<port> dbname=ruby_core_staging user=ruby_core_ro password='<generated>'
vault kv put secret/ruby-core/dev/postgres_readonly \
  host=<host> port=<port> dbname=ruby_core_dev user=ruby_core_ro password='<generated>'
```

## 2. API bearer token (defense-in-depth, ADR-0040)

```bash
vault kv put secret/ruby-core/api         token="$(openssl rand -hex 32)"
vault kv put secret/ruby-core/staging/api token="$(openssl rand -hex 32)"
vault kv put secret/ruby-core/dev/api     token="$(openssl rand -hex 32)"
```

Callers send it as `Authorization: Bearer <token>`. Store the prod token wherever your
HA read-proxy / clients read it.

## 3. Traefik `ruby-api-auth` middleware (prod)

The prod compose labels reference a `ruby-api-auth` Traefik middleware (the primary edge
auth gate; ADR-0020). Define it in Traefik's dynamic config (foundation) — e.g. a
forward-auth or JWT middleware consistent with `ruby-gateway-auth`. Staging has no
Traefik labels, so this is prod-only.

## Verify

Before releasing, confirm the engine/api token can read the new paths (read-only):

```bash
vault kv get -field=user secret/ruby-core/postgres_readonly
vault kv get -field=token secret/ruby-core/api >/dev/null && echo "api bearer present"
```

After deploy: `curl -sf https://<api-host>/health` → 200, and `/v1/ping` with the bearer
→ 200. Register the `/health` monitor in Uptime Kuma.

## Related

- Traefik→api **mTLS** (#122) is defense-in-depth on top of this and is a separate
  follow-up — not required for the service to function.
