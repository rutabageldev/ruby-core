# Runbook — idempotency KV bucket (purge / recreate / TTL change)

The engine dedups processed events in a NATS KV bucket named `idempotency` (shared by the
`engine_processor` and `engine_presence_processor` consumers). See ADR-0025 and PLAN-0034.

A KV bucket's **TTL is fixed at creation**. `idempotency.CreateOrBindKVBucket` binds an
existing bucket as-is, so lowering `DefaultIdempotencyTTL` in code has **no effect** on a
live bucket — the engine logs a WARN at startup when the live TTL differs from the
configured value. To actually apply a new TTL (or to drain a bloated bucket), either edit
the backing stream's max-age in place (Option A, preferred) or recreate the bucket with the
engine stopped (Option B).

> ⚠️ **Do not `kv del` while the engine is consuming.** A running consumer whose bound
> bucket disappears gets `idempotency check: nats: no responders available` on every
> `Seen()` → the event NAKs → and events that exhaust `MaxDeliver` (5, within ~15s of
> backoff) during the gap land in the **DLQ**. A live purge of the 24h bucket once DLQ'd
> ~52 `state_changed` telemetry events this way (benign — superseded — but noisy). Use one
> of the two safe procedures below instead, and do it **off-peak**.

## Inspect

```bash
ENV=prod scripts/nats-admin.sh kv ls
ENV=prod scripts/nats-admin.sh kv status idempotency   # shows TTL + entry count (Values)
```

A healthy bucket at the 30m TTL holds low-thousands of entries. Tens of thousands means the
old 24h TTL is still in effect (or marking volume has grown) — recreate it.

## Option A — edit the backing stream's max-age in place (preferred, zero disruption)

A KV bucket is backed by a stream named `KV_<bucket>`. Editing that stream's `max-age`
changes the effective TTL **without** deleting the bucket, so the engine keeps running and
nothing DLQs. Existing entries older than the new age are evicted on the next enforcement
pass. Verify the command on a non-prod bucket first if you haven't used it before.

```bash
ENV=prod scripts/nats-admin.sh stream edit KV_idempotency --max-age=30m --force
ENV=prod scripts/nats-admin.sh kv status idempotency   # Maximum Age = 30m0s; count falls as entries age out
```

The engine still logs the startup TTL-mismatch WARN until its next restart re-binds and
sees the matching age — cosmetic once the stream is edited.

## Option B — recreate the bucket (stop the engine FIRST)

Use this for a clean empty bucket. The engine **must not** be running while the bucket is
absent (see the warning above); its startup recreates the bucket at the configured TTL
*before* the consumer loop starts, so stop → delete → start is the safe order.

```bash
ENV=prod scripts/nats-admin.sh kv status idempotency      # note current TTL/count
docker stop ruby-core-prod-engine                         # stop consuming FIRST
ENV=prod scripts/nats-admin.sh kv del idempotency --force # now safe to delete
docker start ruby-core-prod-engine                        # startup recreates it at 30m before consuming
```

> The earlier "delete the live bucket, then `docker restart`" procedure is **unsafe** — the
> old container keeps consuming against the missing bucket during the restart and DLQs
> events (see the warning at the top). Always stop first.

**Verify** (either option):

```bash
ENV=prod scripts/nats-admin.sh kv status idempotency   # Maximum Age = 30m0s, low/again-growing count
docker logs ruby-core-prod-engine --since 1m 2>&1 | grep -iE "idempotency|no responders"  # no errors
ENV=prod scripts/nats-admin.sh stream info DLQ 2>&1 | grep Messages   # no purge-window DLQ growth
```

Note: the calendar processor's separate `calendar_idempotency` bucket (reminder-firing
dedup) is low-volume and unaffected — do not touch it.

## Rollback

Set the prior TTL back with Option A (`stream edit … --max-age=24h`), or deploy the previous
engine image and recreate via Option B. No data rollback — the bucket holds only transient
dedup markers.
