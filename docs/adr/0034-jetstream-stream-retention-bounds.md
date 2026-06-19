# ADR-0034 - Bounded JetStream stream retention and config reconciliation

* **Status:** Accepted
* **Date:** 2026-06-18
* **Supersedes:** *(partially revises the HA_EVENTS retention choice in ADR-0021/ADR-0022)*
* **Superseded by:** *(none)*

---

## Context

The prod NATS JetStream store (1 GiB `max_file_store`) filled to 100% — `HA_EVENTS` alone held
1.59M messages / 937 MB (87% of the account). JetStream began returning `insufficient resources` on
new writes: the `discard=new` `KV_idempotency` bucket failed its marks (degrading deduplication),
and `HA_EVENTS` itself — having no per-stream limit to trigger its own `discard=old` eviction — was
at risk of dropping incoming events.

Two root causes:

1. **`HA_EVENTS` was created with no `MaxAge` and no `MaxBytes`.** The original rationale was to keep
   original payloads available "for DLQ routing during the full BackOff window." But that window is
   `MaxDeliver` (5) × `AckWait` (30s) plus the backoff schedule (1+2+4+8s) — a few minutes, not
   forever. The firehose of every HA `state_changed` event therefore grew without bound.
2. **`ensureStream` only created streams when absent; it never updated an existing stream.** So a
   retention limit added in code would silently *not* apply to an already-created stream — limits
   could be assumed in code yet never enforced in the running system.

All other streams are age-bounded (DLQ 7d, AUDIT 72h, COMMANDS 1h, PRESENCE 24h); only `HA_EVENTS`
was unbounded.

## Alternatives Considered

**Raise `max_file_store` only** — Buys time but does not fix the unbounded stream; it would refill.

**Switch `HA_EVENTS` to interest/work-queue retention** (delete after all consumers ack) — Changes
replay semantics that the gateway reconciler and any future consumer may rely on, and `Retention` is
an immutable field, so it cannot be applied to the existing stream without recreating it (losing
data and consumers). Higher risk than bounding by age.

**Bound `HA_EVENTS` by age + bytes, cap every stream, and reconcile existing streams (chosen).**

## Decision

1. `HA_EVENTS` MUST be bounded by `MaxAge` (`DefaultHAEventsMaxAge` = 48h — far beyond the retry/DLQ
   window, covering reconciliation and debugging) **and** `MaxBytes` (512 MiB) as a hard ceiling.
2. Every JetStream stream MUST set an explicit `MaxBytes` cap with `discard=old`, so it self-evicts
   at its own ceiling rather than failing writes. The sum of all stream caps MUST remain below the
   server `max_file_store`, so no stream can exhaust the account and starve the `discard=new` KV
   buckets.
3. `ensureStream` MUST reconcile an existing stream's mutable retention limits (`MaxAge`, `MaxBytes`,
   `MaxMsgs`) via `UpdateStream` when they drift from the desired config. Immutable fields (name,
   storage, retention policy, subjects) are never changed by reconciliation.
4. JetStream account storage utilization SHOULD be alerted on (e.g. >75%) in the shared
   observability stack so saturation surfaces before the account fills — this incident was only
   noticed after it was full.

## Consequences

### Positive

* The account can no longer be exhausted by a single stream; `discard=new` KV deduplication stays
  reliable.
* Retention-limit changes in code now take effect on deploy (and self-heal drift) rather than being
  silently ignored.
* `HA_EVENTS` retains ~48h (~tens of MB) — ample for consumer retries, reconciliation, and debugging.

### Negative

* `HA_EVENTS` no longer keeps full history; events older than 48h are purged (they have no
  operational value, but full-history replay is gone).
* Per-stream `MaxBytes` caps require occasional review as event volume grows; if a cap is hit before
  its age limit, the stream silently drops oldest messages.

### Neutral

* Stream-config reconciliation becomes a startup responsibility of every service that ensures
  streams. Relates to ADR-0021 (NATS), ADR-0022 (DLQ), ADR-0024 (backpressure).
* Raising `max_file_store` for additional headroom remains available but is not required: the sum of
  per-stream caps already sits below the current 1 GiB limit.
