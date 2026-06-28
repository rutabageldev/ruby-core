# Runbook — Google Calendar OAuth bootstrap & token rotation

Operational guide for granting ruby-core access to the Google **Family** calendar (ROADMAP-0012,
ADR-0042). ruby-core syncs Google bidirectionally using an **OAuth user-consent offline refresh
token** — not a service account, because the household account is a consumer Gmail account and
cannot be impersonated via Workspace domain-wide delegation.

> ⚠️ **The OAuth app MUST be in `production` publishing status.** A refresh token issued while the
> app is in `Testing` status is revoked by Google after ~7 days, and sync will silently stop until
> re-consented. This is the single most important step.

All Vault writes run on the host (`ruby-z04-node-01`) using the `ruby-core-writer` token (see
project memory). The engine reads these secrets at `secret/data/ruby-core/google`.

---

## What lands in Vault (the goal)

A single KV path `secret/ruby-core/google` with four fields:

| Field | Source | Notes |
|---|---|---|
| `client_id` | GCP OAuth client | from the Desktop-app OAuth client |
| `client_secret` | GCP OAuth client | from the same client |
| `refresh_token` | one-time consent (`cmd/google-auth`) | long-lived; the thing that expires if app is in Testing |
| `calendar_id` | Google Calendar settings | the Family calendar's ID (often an `…@group.calendar.google.com` address) |

---

## Part 1 — Google Cloud setup (do once; yours to perform)

1. **Project.** In the [Google Cloud Console](https://console.cloud.google.com/), create or select a
   project for ruby-core (e.g. `ruby-core-home`).
2. **Enable the API.** APIs & Services → Library → enable **Google Calendar API** for that project.
3. **OAuth consent screen.** APIs & Services → OAuth consent screen:
   - User type **External**.
   - Add the shared household Google account as a user.
   - Add the scope `https://www.googleapis.com/auth/calendar.events`.
   - **Set publishing status to `In production`** (Publish app → confirm). Do **not** leave it in
     Testing.
4. **OAuth client.** APIs & Services → Credentials → Create credentials → OAuth client ID:
   - Application type **Desktop app** (this enables the `http://127.0.0.1:<port>` loopback redirect
     that `cmd/google-auth` uses — far simpler than hosting a web redirect).
   - Save the generated **client_id** and **client_secret**.
5. **Calendar ID.** In Google Calendar (web) → the Family calendar's **Settings and sharing** →
   **Integrate calendar** → copy the **Calendar ID**.

---

## Part 2 — Mint the refresh token (`cmd/google-auth`)

`cmd/google-auth` is a small loopback OAuth helper delivered in Slice C (ROADMAP-0012.3). It runs
the consent flow with `AccessType=offline` and forces a consent prompt so Google returns a
`refresh_token` (Google only returns one on first consent unless re-prompted).

```bash
# On the host, repo root. Provide the client credentials from Part 1.
go run ./cmd/google-auth \
  --client-id "<client_id>" \
  --client-secret "<client_secret>"
```

The helper prints a URL (or opens a browser), you sign in **as the household account** and grant
access, and it prints the `refresh_token` to stdout. Copy it.

> If it prints no `refresh_token` (only an access token), you have consented before without a forced
> prompt — re-run; the helper passes `prompt=consent` to force re-issuance.

---

## Part 3 — Store in Vault

```bash
# On the host. VAULT_TOKEN_RUBY_CORE_WRITER is in deploy/prod/.env (see project memory).
VAULT_ADDR=https://127.0.0.1:8200 \
VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt \
VAULT_TOKEN="$VAULT_TOKEN_RUBY_CORE_WRITER" \
vault kv put secret/ruby-core/google \
  client_id="<client_id>" \
  client_secret="<client_secret>" \
  refresh_token="<refresh_token>" \
  calendar_id="<calendar_id>"
```

Confirm (metadata only — do not print secrets into logs):

```bash
VAULT_ADDR=https://127.0.0.1:8200 VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt \
VAULT_TOKEN="$VAULT_TOKEN_RUBY_CORE_WRITER" \
vault kv get -field=calendar_id secret/ruby-core/google
```

The engine's Vault read policy must grant read on `secret/data/ruby-core/google` — added alongside
the existing `secret/ruby-core/*` grants in Slice C.

---

## Part 4 — Enable sync

Sync is gated by `CALENDAR_SYNC_ENABLED` (off by default outside production, so the single shared
calendar is never double-synced across dev/staging/prod — same pattern as Ada's `HA_INGEST_ENABLED`,
ADR-0033). Set `CALENDAR_SYNC_ENABLED=true` only in `deploy/prod/.env`, then restart the prod engine.

Verify the poller is healthy: the engine logs an incremental sync line roughly every ~60s and a
`SYNC_STATE` row exists for the calendar (`last_synced_at` advancing).

---

## Token rotation / recovery

Symptom: engine logs Google auth failures (`invalid_grant` / 401) and the `SYNC_STATE`
`last_synced_at` stops advancing.

Likely causes and fixes:

- **App reverted to Testing / token aged out** → set publishing status back to `In production`
  (Part 1.3), then re-mint (Part 2) and re-store (Part 3).
- **Consent revoked** (household removed the app at <https://myaccount.google.com/permissions>) →
  re-mint (Part 2) and re-store (Part 3).
- **Client secret rotated in GCP** → update `client_id`/`client_secret` in Vault (Part 3) and
  re-mint.

After re-storing, restart the prod engine. No code change is needed — the engine reads the new
secret at startup.

---

## Force a full resync

If the local mirror is suspected stale or diverged (and to recover from a stuck sync token), clear
the stored sync token so the next poll does a full page-through. A 410 from Google triggers this
automatically; to do it manually:

```sql
-- against ruby_core (prod), on the postgres network
UPDATE sync_state SET sync_token = NULL WHERE calendar_id = '<calendar_id>';
```

Then restart the prod engine. The next poll resyncs from scratch and records a fresh token. This is
non-destructive to `CALENDAR_EVENT` rows (they are re-upserted), but the overlay tables are local
and unaffected.

---

## Related

- ADR-0042 — calendar sync architecture (authority split, single writer, 410/412 handling).
- `docs/runbooks/` — add the new `CALENDAR_EVENT` / `SYNC_STATE` / overlay tables to the foundation
  Postgres backup set (tracked as a production-readiness blocker in ROADMAP-0012).
