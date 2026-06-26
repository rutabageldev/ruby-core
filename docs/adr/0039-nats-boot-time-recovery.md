# ADR-0039 - NATS Boot-Time Recovery: Decouple Startup from the nats-init One-Shot Gate

* **Status:** Proposed
* **Date:** 2026-06-26
* **Supersedes:** *none*
* **Superseded by:** *none*

---

## Context

After a host power outage (2026-06-23) and a follow-up controlled reboot (2026-06-26),
the ruby-core NATS servers (dev/staging/prod) failed to restart automatically — despite
each carrying `restart: unless-stopped`. The NATS containers stayed `Exited` until started
by hand, which left every dependent service (gateway/engine/notifier/presence/audit-sink)
crash-looping — they cannot fetch their NKEY seed or connect without NATS — and the
cert-renewer sidecar stuck in a retry loop.

**Root cause (confirmed by comparative inspection of which containers the daemon
auto-started at boot):** the `nats` service declares
`depends_on: { nats-init: { condition: service_completed_successfully } }`. `nats-init` is a
`restart: "no"` one-shot that bootstraps the NATS TLS material into the **persistent**
`nats-certs` volume on first deploy. On reboot, `nats-init` does not re-run, so its
"completed successfully" state is never re-established in the current boot — and the Docker
daemon will not start a container whose `service_completed_successfully` dependency is
unsatisfied. The cert already persists in the named volume, so NATS would start fine; only
the stale one-shot gate holds it down.

The asymmetry is diagnostic: at boot the daemon **did** start the services that depend on
`nats: service_healthy` (they came up and crash-looped), and **did** start the cert-renewer
(no `depends_on`). Only the containers gated on the *completed one-shot* were held. Manually
`docker start`-ing NATS — which bypasses `depends_on` entirely — brought it up instantly
using the already-present cert.

A second, compounding fault: the cert-renewer's `post-render.sh` recovery path can only
`SIGHUP` a **running** NATS. When NATS is stopped, the Docker API `kill?signal=SIGHUP` call
returns a non-204 and the script `exit 1`s — so the one service that reliably survives a
reboot can never bring NATS back; it just retry-loops. The system therefore has **no
self-healing path** after a cold boot.

This is the ruby-core analogue of the foundation Vault boot-race (foundation#110): a
multi-service dependency chain whose ordering guarantees exist only under `docker compose up`
and evaporate under the daemon's restart-on-boot.

## Alternatives Considered

**Host-level systemd boot unit (`docker compose up -d` on boot)** — Reliably re-imposes the
intended start order, but lives outside the repo, requires host sudo, and leaves the in-repo
anti-pattern in place. Retained as optional defense-in-depth, not the primary fix.

**Make `nats-init` re-run on every boot (`restart: on-failure`/`always`)** — A one-shot that
loops is itself an anti-pattern, and it does not address the gate semantics cleanly. The cert
already persists, so re-running the bootstrap on every boot is unnecessary work.

**Remove `nats-init` entirely and fold cert-fetch into the NATS entrypoint** — Larger change
that mixes cert-issuance concerns into the NATS container and loses the clean separation of
first-deploy bootstrap from runtime. Over-scoped for the problem.

**Change the dependency condition to `service_started`** — Still a declared gate; it happens
to be ignored by the daemon on boot today, but that is implementation-dependent behavior, not
a guarantee. An explicit wait-for-cert in the NATS entrypoint is the robust correctness
mechanism.

## Decision

1. The `nats` service **MUST NOT** gate its container startup on `nats-init`'s
   `service_completed_successfully` condition. The hard `depends_on` gate **MUST** be removed
   from all three environment compose files.

2. The `nats` service **MUST** guard its own startup with an entrypoint that waits for its
   TLS material (`server-cert.pem`, `server-key.pem`, `ca.pem`) to be present and non-empty
   in the `nats-certs` volume before exec'ing `nats-server`. This makes NATS tolerant of start
   ordering on both fresh deploy (waits for `nats-init` to populate the volume) and reboot
   (cert already present → starts immediately).

3. The cert-renewer `post-render.sh` **MUST** be able to recover a stopped NATS: after writing
   the cert bundle, if the NATS container is running it **MUST** `SIGHUP` it (reload in place);
   if it is not running it **MUST** start it. `post-render.sh` **MUST NOT** hard-fail
   (deadlock) solely because NATS was unreachable at signal time.

4. `nats-init` **SHOULD** retain `restart: "no"` and its first-deploy bootstrap role,
   unchanged.

## Consequences

### Positive

* NATS auto-recovers after a cold boot with no human intervention, regardless of daemon
  restart ordering — closing the failure mode that took down ruby-core in the 2026-06-23
  outage.
* The cert-renewer becomes a genuine self-healing agent for NATS, eliminating the deadlock.
* Removes a latent boot anti-pattern; the comparative-inspection method recorded here
  documents how to detect the same class of fault elsewhere.

### Negative

* NATS startup now includes a wait loop. On a genuinely certless fresh deploy with Vault
  unavailable, NATS waits (with logged progress and a bounded timeout) rather than failing
  fast — trading fast-fail for self-heal-when-ready.
* The cert-renewer now exercises container **start** (not just signal) over NATS via the
  Docker socket it already mounts — a slightly broader blast radius for that sidecar, though
  no new privilege is granted (the socket mount is unchanged).

### Neutral

* Formalizes the persistent `nats-certs` volume as the source of truth for NATS TLS material
  at boot, with `nats-init` reduced to a first-deploy bootstrap rather than a per-boot gate.
