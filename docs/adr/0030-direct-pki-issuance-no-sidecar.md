# ADR-0030 — Direct-PKI Issuance for Ruby-Core mTLS (No Sidecar)

* **Status:** Accepted
* **Date:** 2026-05-19

## Context

Foundation's ADR-0008 (Vault Agent as the Secret Distribution Layer) prescribes a per-service Vault Agent sidecar that renders TLS material to disk for each consumer service. PLAN-0006 (mosquitto) and PLAN-0007 (FreeRADIUS) followed that pattern: a sidecar container speaks Vault, writes cert+key to a shared volume, and triggers a consumer restart on rotation.

Ruby-core's services are architecturally different. They already speak Vault natively via `pkg/boot/boot.go`: at startup each service calls `vault.KV.Get("secret/data/ruby-core/tls/<svc>")`, parses cert/key/ca from the JSON response, and builds a `tls.Config` in memory. Nothing is written to disk. Wedging a Vault Agent sidecar into ruby-core's compose would mean:

* 21 new sidecar containers (7 services × dev/staging/prod) running alongside their consumers.
* Refactoring `boot.go` to read cert/key/ca from filesystem rather than the Vault API.
* Adding shared-volume plumbing, fail-on-permissions edge cases (distroless UIDs vs sidecar root), and restart orchestration.

All of that duplicates infrastructure ruby-core already has — a fully-featured Go Vault client — in order to satisfy the letter of ADR-0008 rather than its spirit. ADR-0008's actual goals are (a) eliminating manual `chown`-based cert distribution, (b) auto-rotation at TTL/2, (c) scoped per-service auth via AppRole, and (d) fail-fast on Vault unavailability. None of those require a sidecar; they require *semantics*.

Foundation amended ADR-0008 in PR #78 with an explicit **direct-issuance exemption** for Vault-native applications. This ADR records the ruby-core-side implementation of that exemption.

## Decision

Ruby-core services **MUST** obtain their mTLS material directly from Vault PKI (`pki_int/issue/ruby-core-<svc>`) via the existing `pkg/boot/boot.go` Vault client. They **MUST NOT** introduce per-service Vault Agent sidecars to ruby-core compose files.

Concrete requirements, all enforced in `pkg/boot/pki.go`:

1. **Authentication via AppRole.** Each service logs in via `auth/approle/login` using a role-id + secret-id mounted into the container from foundation host paths (`/opt/foundation/vault/role-id-foundation-agent-ruby-core-<svc>` and `…-secret-id-…`). Static `VAULT_TOKEN` is NOT used for the TLS issuance path (it remains for legacy KV reads — NKEYs, HA, postgres — until Phase 17.7 retires it).

2. **Issuance from pki_int.** `IssueNATSCert` writes to `pki_int/issue/<role>` with the service's common name and TTL. The response yields `data.certificate`, `data.private_key`, and `data.issuing_ca`, all parsed into the existing `*TLSMaterial` struct so `ConnectNATS`'s downstream wiring is unchanged.

3. **In-process renewal at TTL/2.** A `RenewLoop` goroutine runs for the lifetime of the service. It ticks at TTL/2 (clamped to [1m, 24h] to prevent degenerate scheduling), re-issues a fresh cert, and atomically swaps it into a mutex-guarded `MaterialHolder`. Failures log a WARN and retry on the next tick; the loop never panics.

4. **Hot-swap via `tls.Config.GetClientCertificate`.** `ConnectNATSDynamicTLS` builds a `tls.Config` whose `GetClientCertificate` callback reads the current cert from the `MaterialHolder`. The NATS Go client clones the `tls.Config` per handshake (verified in `nats.go@v1.48.0/nats.go:2240`); Go's stdlib `Clone` is shallow, so function pointers like `GetClientCertificate` survive the clone. On reconnect handshakes the callback runs against the latest cert — rotation is invisible to NATS.

5. **Fail-fast at startup.** If AppRole login or the initial cert issuance fails, the service exits non-zero. Vault unavailability at startup is a configuration error, not a runtime issue to silently retry through.

6. **Graceful shutdown.** `RenewLoop` accepts a `context.Context`; signal handlers cancel it so the goroutine exits cleanly during shutdown.

7. **AppRole policy scope.** The bound policy (foundation-side `vault/policies/foundation-agent-ruby-core-<svc>.hcl`) grants only `update` on the service's own `pki_int/issue/<role>` path plus `auth/token/{lookup,renew}-self`. Negative scope verified live during PR #78: each AppRole cannot read the legacy `secret/ruby-core/tls/*` KV path and cannot cross-issue another service's role.

8. **Rollback path.** `boot.FetchNATSTLS` and `boot.ConnectNATS` (the legacy KV-bundle path) remain callable. `BootstrapNATSTLS` branches on `VAULT_PKI_ROLE` — when unset, it falls back to the legacy path. The mkcert KV bundles in `secret/ruby-core/tls/*` stay populated through Phase 17.7's decommission PR as the durable rollback target.

## Consequences

### Positive Consequences

* Zero sidecars for the 5 Go services across dev/staging/prod (15 sidecars avoided). One sidecar remains for the NATS server cert — `nats-cert-renewer` — because NATS is a third-party process that reads certs from disk and provides none of the in-process semantics this ADR requires for the exemption. The two-pattern split is exactly what's intended: direct-PKI for Vault-native consumers, sidecar pattern (per foundation's ADR-0008) for filesystem consumers.
* No shared-volume plumbing, no UID/perm coordination, no restart orchestration **for the Go services**. The NATS sidecar handles those concerns for the one process that does need them.
* Cert lifecycle scoped to the process: a service that's running has a fresh cert; a service that's not is irrelevant.
* Reuses the existing `boot` Vault client — no parallel infrastructure.
* Reconnect handshakes pick up rotated certs without restart; no operator action ever required for routine rotation.
* AppRole policy scope is identical to foundation's sidecar-pattern AppRoles, so the Vault-side blast radius is the same as if we'd used the sidecar.

### Negative Consequences

* Diverges from the broader homelab pattern (foundation's sidecar pattern is otherwise uniform). The exemption is recorded in both repos (ADR-0008 amendment in foundation; this ADR here) to prevent confusion.
* Adds `pkg/boot/pki.go` complexity that ruby-core didn't have before — AppRole login flow, renewal goroutine, mutex-guarded `MaterialHolder`. Mitigated by isolated tests (`pkg/boot/pki_test.go`).
* Process lifetime tied to cert health: if `RenewLoop` consistently fails over a longer window than `TTL/2`, the service eventually presents an expired cert and NATS rejects. Mitigated by Vault availability monitoring (foundation observability stack) and the 1m retry floor.

### Neutral Consequences (Trade-offs)

* `boot.go` retains its static `VAULT_TOKEN` for the other Vault paths (NKEYs, HA config, postgres credentials). Two auth methods coexist on the same client until Phase 17.7 either migrates the other reads to AppRole too or accepts the dual-auth posture.
* The secret-id host file must be readable by the in-container UID. For distroless `nonroot` (UID 65532) this means `chmod 0644` on the host — looser than foundation's `0640 root:primaryrutabaga` default for sidecar consumers. Acceptable because the security boundary is the Vault policy bound to the AppRole, not the filesystem mode. Foundation will add a `make tighten-vault-approle-perms-readable` target to make this reproducible across secret-id rotations.
* The `RenewLoop` goroutine survives in memory but does no work until its next tick — negligible.
