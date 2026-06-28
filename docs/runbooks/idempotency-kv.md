# Runbook — idempotency KV bucket (purge / recreate / TTL change)

The engine dedups processed events in a NATS KV bucket named `idempotency` (shared by the
`engine_processor` and `engine_presence_processor` consumers). See ADR-0025 and PLAN-0034.

A KV bucket's **TTL is fixed at creation**. `idempotency.CreateOrBindKVBucket` binds an
existing bucket as-is, so lowering `DefaultIdempotencyTTL` in code has **no effect** on a
live bucket — the engine logs a WARN at startup when the live TTL differs from the
configured value. To actually apply a new TTL (or to purge a bloated bucket), delete the
bucket and let the engine recreate it.

> ⚠️ This is a destructive op on a live bucket (`del`). It is safe here because dedup only
> needs to outlive the ~165s redelivery window and the calendar write-through is now
> idempotent at Google (ADR-0042) — so reprocessing during the brief empty-history window
> is harmless. Still, do it **off-peak**.

## Inspect

```bash
ENV=prod scripts/nats-admin.sh kv ls
ENV=prod scripts/nats-admin.sh kv status idempotency   # shows TTL + entry count (Values)
```

A healthy bucket at the 30m TTL holds low-thousands of entries. Tens of thousands means the
old 24h TTL is still in effect (or marking volume has grown) — recreate it.

## Purge + recreate (apply a new TTL)

1. **Deploy the engine build** that sets the new `DefaultIdempotencyTTL`. On startup it
   binds the *existing* bucket and logs the TTL-mismatch WARN — expected at this step.
2. **Delete the bucket** (off-peak):

   ```bash
   ENV=prod scripts/nats-admin.sh kv del idempotency
   ```

3. **Restart the engine** so startup recreates the bucket at the new TTL:

   ```bash
   docker restart ruby-core-engine-prod    # adjust to the actual prod engine container name
   ```

4. **Verify**: the startup log shows no TTL-mismatch WARN, and:

   ```bash
   ENV=prod scripts/nats-admin.sh kv status idempotency   # TTL = 30m, low entry count
   ```

Note: the calendar processor's separate `calendar_idempotency` bucket (reminder-firing
dedup) is low-volume and unaffected — do not delete it.

## Rollback

Recreate at the prior TTL by deploying the previous engine image (its
`DefaultIdempotencyTTL`) and repeating the delete + restart, or `kv del` + let the running
binary recreate it. No data rollback — the bucket holds only transient dedup markers.
