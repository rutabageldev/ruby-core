# PLAN-0029 - NATS Boot Resilience

* **Status:** Complete
* **Date:** 2026-06-26
* **Project:** ruby-core
* **Roadmap Item:** none (operational reliability fix arising from the 2026-06-23 outage)
* **Branch:** fix/nats-boot-resilience
* **Related ADRs:** ADR-0039

---

## Scope

Make the ruby-core NATS servers recover automatically after a host reboot, and make the
cert-renewer able to heal a stopped NATS — removing the `service_completed_successfully`
one-shot gate that holds NATS down on boot (see ADR-0039).

**In scope:** the `nats` service definition, a new wait-for-cert entrypoint, and
`post-render.sh`, across dev/staging/prod.

**Out of scope:** the optional host-level systemd boot unit (a foundation/host concern,
tracked separately); navi's separate network-attach issue; and the foundation Vault static-IP
fix (already applied).

---

## Pre-conditions

* [ ] On branch `fix/nats-boot-resilience` off `main` (`v0.24.1`).
* [ ] Vault reachable and unsealed (foundation static-IP fix applied).
* [ ] The persistent `nats-certs` volumes contain current cert material (they do — NATS is
      currently running).

---

## Steps

### Step 1 — Add a wait-for-cert entrypoint for NATS

**Action:** Add `deploy/base/nats/wait-for-certs.sh` (POSIX `sh`, executable): waits — bounded
to ~120s, logging progress — until `/etc/nats/certs/server-cert.pem`, `server-key.pem`, and
`ca.pem` all exist and are non-empty, then `exec nats-server "$@"`. On timeout, exit non-zero
(so `restart: unless-stopped` retries with visibility rather than deadlocking — see Open
Questions).

**Verification:** `sh -n deploy/base/nats/wait-for-certs.sh` parses clean; run against a
populated dir it exec's `nats-server`; against an empty dir it logs "waiting for certs…" and
loops.

### Step 2 — Wire the entrypoint into the nats service and remove the gate (×3 envs)

**Action:** In `deploy/{dev,staging,prod}/compose.{dev,staging,prod}.yaml`, on the `nats`
service: mount `../base/nats/wait-for-certs.sh:/usr/local/bin/wait-for-certs.sh:ro`, set
`entrypoint: ["/usr/local/bin/wait-for-certs.sh"]`, keep
`command: ["-c", "/etc/nats/nats.conf"]`, and **remove** the
`depends_on: { nats-init: { condition: service_completed_successfully } }` block (replace with
a comment referencing ADR-0039).

**Verification:** `docker compose -f deploy/<env>/compose.<env>.yaml config` renders without
error and shows the new `entrypoint` and no `service_completed_successfully` gate on `nats`.

### Step 3 — Make post-render.sh recover a stopped NATS (×3 envs)

**Action:** In `deploy/{dev,staging,prod}/vault-agent/post-render.sh`, after the cert `mv`s:
query `GET /containers/<NATS_CONTAINER>/json`; if `.State.Running == true` →
`POST /kill?signal=SIGHUP` (as today); else → `POST /containers/<NATS_CONTAINER>/start`. Log
the action taken. Only `exit 1` on a genuine Docker API/transport error — never merely because
NATS was down.

**Verification:** `sh -n post-render.sh` parses clean. Functional test in Step 5.

### Step 4 — Fresh-deploy ordering test (dev)

**Action:** On dev, recreate with an emptied cert volume in a throwaway test and confirm NATS
waits for the cert, then comes healthy after `nats-init` populates it.

**Verification:** `docker logs ruby-core-dev-nats` shows the wait loop then `Server is ready`;
`docker ps` shows `ruby-core-dev-nats` healthy.

### Step 5 — Self-heal test (dev, no reboot)

**Action:** `docker stop ruby-core-dev-nats`, then trigger a cert-renewer render cycle (e.g.
`docker restart ruby-core-dev-nats-cert-renewer`) and confirm `post-render.sh` starts NATS
back up.

**Verification:** cert-renewer log shows a "NATS was stopped — started <container>" line;
`docker ps` shows `ruby-core-dev-nats` running again with **no** manual `docker start`.

### Step 6 — Controlled-reboot acceptance (the real test)

**Action:** After dev/staging validation and merge+deploy, schedule a controlled host reboot
(console reachable).

**Verification:** post-reboot, all three NATS come up unattended (StartedAt at the boot
timestamp, healthy), the dependent services connect without crash-looping, and no manual
intervention is required. This mirrors the original failure and is the acceptance criterion.

**Result (2026-06-26):** PASS. After a real host reboot, all three NATS came up **7s after
boot** — `running`, `healthy`, `restarts=0`, no manual `docker start`. The `wait-for-certs`
entrypoint ran and JetStream recovered (`Server is ready`). ruby-core app services landed at
`restarts=0–1` (vs 14–18 on the pre-fix reboot, where they crash-looped for ~5 min until NATS
was hand-started). The prod gateway reconnected to NATS and re-subscribed to `ada_event` at
boot. Vault (foundation static-IP fix) also came up unsealed on all 7 networks.

---

## Rollback

Revert the branch's commits and redeploy the previous image/compose. The change is
config + scripts only — `nats-certs` and the JetStream data volumes are untouched — so
rollback is `git revert` + `docker compose up -d`. No data migration, no stateful change.

---

## Open Questions

* **Entrypoint wait-timeout behavior.** *Resolved:* bounded ~120s, then exit non-zero so
  `restart: unless-stopped` retries — visibility without a hard deadlock. Implemented in
  `wait-for-certs.sh` (`CERT_WAIT_TIMEOUT`, default 120s); unit-tested both the timeout and
  cert-present paths.
