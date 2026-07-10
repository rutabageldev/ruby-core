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
-- Default privileges are keyed to the role that CREATES the table. Migrations run as the
-- `ruby_core` app user, so the FOR ROLE clause is mandatory — without it, every table a
-- future migration creates is invisible to ruby_core_ro (see "Known failure mode" below).
ALTER DEFAULT PRIVILEGES FOR ROLE ruby_core IN SCHEMA public GRANT SELECT ON TABLES TO ruby_core_ro;
```

> ⚠️ **Gotcha — run this with `psql -c`, not a heredoc.** `docker run` (without `-i`)
> does **not** connect stdin to the container, so `docker run … psql … <<SQL` silently
> discards the SQL: psql gets no input, runs nothing, and **exits 0** — looks like it
> worked, but no role is created. Pass each statement with `-c` instead (or add `-i` if
> you must use a heredoc). The ruby-core app user is not a superuser and cannot
> `CREATE ROLE`; use the `postgres` superuser (its password is the
> `POSTGRES_PASSWORD` env on the `foundation-postgres` container). Embed the role
> password as a literal (hex from `openssl rand` is shell-safe) so it definitely
> matches what you store in Vault:
>
> ```bash
> RO_PW="$(openssl rand -hex 24)"
> ADMIN_PW="$(docker inspect foundation-postgres \
>   --format '{{range .Config.Env}}{{println .}}{{end}}' | grep '^POSTGRES_PASSWORD=' | cut -d= -f2-)"
> docker run --rm --network postgres -e PGPASSWORD="$ADMIN_PW" postgres:16-alpine \
>   psql -h foundation-postgres -p 5432 -U postgres -d ruby_core -v ON_ERROR_STOP=1 \
>   -c "CREATE ROLE ruby_core_ro LOGIN PASSWORD '$RO_PW'" \
>   -c "GRANT CONNECT ON DATABASE ruby_core TO ruby_core_ro" \
>   -c "GRANT USAGE ON SCHEMA public TO ruby_core_ro" \
>   -c "GRANT SELECT ON ALL TABLES IN SCHEMA public TO ruby_core_ro" \
>   -c "ALTER DEFAULT PRIVILEGES FOR ROLE ruby_core IN SCHEMA public GRANT SELECT ON TABLES TO ruby_core_ro"
> # then store the same $RO_PW in Vault (next section), and verify:
> docker run --rm --network postgres -e PGPASSWORD="$RO_PW" postgres:16-alpine \
>   psql -h foundation-postgres -p 5432 -U ruby_core_ro -d ruby_core -tAc "SELECT count(*) FROM calendar_event"
> ```

(For the staging/dev databases, `ruby_core_ro` already exists — do **not** re-`CREATE` it.
Connect to that database and repeat the `GRANT CONNECT … <db>` + `USAGE`/`SELECT` schema
grants, and set `ALTER DEFAULT PRIVILEGES FOR ROLE <that env's app user>` — `ruby_core_staging`
or `ruby_core_dev`, not `ruby_core`. This is how staging + dev were provisioned 2026-07-10.)

> **Status:** all three environments are provisioned.
>
> - **prod** — role `ruby_core_ro` created, `secret/ruby-core/postgres_readonly` +
>   `secret/ruby-core/api` populated, api healthy, as of 2026-06-28 / v0.26.0. The
>   `FOR ROLE ruby_core` default-privileges fix was applied 2026-06-29 (v0.30.0) after
>   migration `000003` created `person_email` without a grant — see "Known failure mode".
> - **staging + dev** — provisioned 2026-07-10 after the v0.31.0 release un-gated the api
>   and `ruby-core-staging-api` crash-looped on the missing `secret/ruby-core/staging/postgres_readonly`
>   (rutabageldev/ruby-core#165). `ruby_core_ro` granted read access on `ruby_core_staging`
>   and `ruby_core_dev`; `secret/ruby-core/{staging,dev}/{postgres_readonly,api}` populated.
>   **The `FOR ROLE` clause names each env's own app user** — `ruby_core_staging` /
>   `ruby_core_dev`, **not** `ruby_core` — because migrations run as that env's app role and
>   default privileges only cover objects created by the named role. staging-api verified
>   healthy (`/health` 200); dev-api is `profiles: [services]`-gated and latent, but its
>   read-only role + secrets are in place.

Store the DSN fields in Vault (the api reads `secret/data/ruby-core/postgres_readonly`).
`ruby_core_ro` is a **single role shared across all three databases**, so the `password`
field MUST be identical in all three secrets — generate it once (when the role is created)
and reuse it. If you are provisioning a new env against an already-created role, read the
existing password from a populated secret rather than resetting the role:
`vault kv get -field=password secret/ruby-core/postgres_readonly`.

```bash
vault kv put secret/ruby-core/postgres_readonly \
  host=<host> port=<port> dbname=ruby_core user=ruby_core_ro password='<shared-ro-password>'
vault kv put secret/ruby-core/staging/postgres_readonly \
  host=<host> port=<port> dbname=ruby_core_staging user=ruby_core_ro password='<shared-ro-password>'
vault kv put secret/ruby-core/dev/postgres_readonly \
  host=<host> port=<port> dbname=ruby_core_dev user=ruby_core_ro password='<shared-ro-password>'
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

## Known failure mode — a read endpoint 500s after a new migration

**Symptom:** one endpoint returns HTTP 500 (often with *nothing logged* — ogen's error
handler swallows the underlying pg error) while sibling read endpoints return 200. Clean
api startup, healthy container.

**Cause:** `ruby_core_ro` is missing `SELECT` on a table a recent migration created. The
one-time `GRANT SELECT ON ALL TABLES` only covers tables that existed when it ran;
anything created later is covered **only** by `ALTER DEFAULT PRIVILEGES`, and that clause
applies solely to objects created by the role named in `FOR ROLE`. Migrations run as the
env's app user — `ruby_core` in prod, `ruby_core_staging` in staging, `ruby_core_dev` in
dev — so the `FOR ROLE` clause must name **that** env's app role. A grant scoped to
`postgres`, omitting `FOR ROLE`, or naming the wrong env's role silently covers nothing the
migrations create. This is exactly how
`person_email` (migration `000003`, v0.30.0) broke `GET /v1/calendar/events`: that handler
unconditionally reads `person_email` via `ListAllPersonEmails`, while `/directory/people`
and `/childcare/providers` don't touch it.

**Diagnose** (as the `postgres` superuser against the affected DB):

```bash
ADMIN_PW="$(docker inspect foundation-postgres \
  --format '{{range .Config.Env}}{{println .}}{{end}}' | grep '^POSTGRES_PASSWORD=' | cut -d= -f2-)"
# list every table ruby_core_ro can NOT read:
docker run --rm --network postgres -e PGPASSWORD="$ADMIN_PW" postgres:16-alpine \
  psql -h foundation-postgres -p 5432 -U postgres -d ruby_core -tAc \
  "SELECT tablename FROM pg_tables WHERE schemaname='public'
     AND NOT has_table_privilege('ruby_core_ro', schemaname||'.'||tablename, 'SELECT')"
```

**Remediate** (idempotent; closes the current gap and prevents recurrence):

```bash
docker run --rm --network postgres -e PGPASSWORD="$ADMIN_PW" postgres:16-alpine \
  psql -h foundation-postgres -p 5432 -U postgres -d ruby_core -v ON_ERROR_STOP=1 \
  -c "GRANT SELECT ON ALL TABLES IN SCHEMA public TO ruby_core_ro" \
  -c "ALTER DEFAULT PRIVILEGES FOR ROLE ruby_core IN SCHEMA public GRANT SELECT ON TABLES TO ruby_core_ro"
```

The two snippets above are prod-shaped — for staging/dev swap both the `-d` database
(`ruby_core_staging` / `ruby_core_dev`) and the `FOR ROLE` app user to match that env.

Once the `FOR ROLE` default privilege is in place for an env (prod since 2026-06-29;
staging + dev since 2026-07-10), future migrations there are covered automatically — no
per-migration grant needed.

## Related

- Traefik→api **mTLS** (#122) is defense-in-depth on top of this and is a separate
  follow-up — not required for the service to function.
