# Phase 3 Verification

This document provides manual acceptance-test steps for Phase 3 reliability patterns.
Integration tests are deferred to Phase 6 per ROADMAP.md. These steps require a running
dev environment (`make dev-up`) and the `nats` CLI tool.

---

## Prerequisites

```bash
make dev-up          # start NATS + infrastructure
                     # Expected: exits 0; containers ruby-core-dev-nats healthy
make setup-creds     # regenerate auth.conf with Phase 3 ACL additions
                     # Expected: exits 0; prints "All validations passed"
make nats-validate   # verify auth.conf and NATS config
                     # Expected: exits 0; no errors
make dev-services-up # build and start gateway + engine
                     # Expected: ruby-core-dev-engine logs "consumer and DLQ forwarder started"
```

### Connecting the nats CLI (mTLS required)

The NATS server requires mTLS. Fetch admin credentials from Vault and save a context:

```bash
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault kv get -field=seed secret/ruby-core/nats/admin > /tmp/nats-admin-seed.txt
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault kv get -format=json secret/ruby-core/tls/admin \
  | jq -r '.data.data.cert' > /tmp/nats-admin-cert.pem
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault kv get -format=json secret/ruby-core/tls/admin \
  | jq -r '.data.data.key'  > /tmp/nats-admin-key.pem
VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root \
  vault kv get -format=json secret/ruby-core/tls/admin \
  | jq -r '.data.data.ca'   > /tmp/nats-admin-ca.pem
chmod 600 /tmp/nats-admin-*.pem /tmp/nats-admin-seed.txt

nats context save ruby-core-dev \
  --server tls://127.0.0.1:4222 \
  --nkey    /tmp/nats-admin-seed.txt \
  --tlscert /tmp/nats-admin-cert.pem \
  --tlskey  /tmp/nats-admin-key.pem \
  --tlsca   /tmp/nats-admin-ca.pem
nats context select ruby-core-dev
```

Verify connectivity:

```bash
nats stream ls
# Expected: table listing HA_EVENTS and DLQ streams
```

---

## Acceptance Criterion 1: DLQ

**Goal:** A poison-pill message is moved to the DLQ stream after MaxDeliver (5) attempts,
with exponential backoff delays between retries (1s → 2s → 4s → 8s).

### Setup

There is no `deploy/dev/.env` file; pass `ENGINE_FORCE_FAIL=true` directly to the
container. Stop and remove the compose-managed container, then run it manually:

```bash
docker stop ruby-core-dev-engine && docker rm ruby-core-dev-engine

docker run -d \
  --name ruby-core-dev-engine \
  --network ruby-core-dev_default \
  --network vault_default \
  --restart unless-stopped \
  -e ENVIRONMENT=development \
  -e VAULT_ADDR=http://vault:8200 \
  -e VAULT_TOKEN=root \
  -e NATS_URL=tls://nats:4222 \
  -e NATS_REQUIRE_MTLS=true \
  -e VAULT_NKEY_PATH=secret/data/ruby-core/nats/engine \
  -e VAULT_TLS_PATH=secret/data/ruby-core/tls/engine \
  -e ENGINE_FORCE_FAIL=true \
  ruby-core-dev-engine
```

Confirm the engine started cleanly before publishing:

```bash
docker logs ruby-core-dev-engine --tail 5
# Expected last line: "consumer and DLQ forwarder started (workers=20, batch=20)"
```

### Test

```bash
nats pub ha.events.test.poison \
  '{"specversion":"1.0","id":"poison-001","source":"test","type":"test.poison","data":{}}'
```

Watch engine logs with timestamps for 5 NAK lines with increasing delays (~15s total):

```bash
docker logs ruby-core-dev-engine --timestamps --follow
```

Expected log output. The five process-error lines must span approximately 15 seconds
(the sum of the 1s+2s+4s+8s backoff schedule), not collapse to a single timestamp:

```
T+0s  [engine] engine: process error for "poison-001": ENGINE_FORCE_FAIL: ... (will nak)
T+1s  [engine] engine: process error for "poison-001": ENGINE_FORCE_FAIL: ... (will nak)
T+3s  [engine] engine: process error for "poison-001": ENGINE_FORCE_FAIL: ... (will nak)
T+7s  [engine] engine: process error for "poison-001": ENGINE_FORCE_FAIL: ... (will nak)
T+15s [engine] engine: process error for "poison-001": ENGINE_FORCE_FAIL: ... (will nak)
T+15s [engine] engine: dlq: routed stream=HA_EVENTS seq=N to dlq.ha_events.engine_processor (deliveries=5)
```

Confirm the message landed in the DLQ stream:

```bash
nats stream info DLQ
# Expected: Messages: 1 (or incremented by 1 from before the test)

nats stream get DLQ <seq>
# Expected: Subject dlq.ha_events.engine_processor, payload contains original event body
```

### Teardown

Remove the force-fail container and restore compose management, then verify the engine
reconnected cleanly before proceeding to the next test:

```bash
docker stop ruby-core-dev-engine && docker rm ruby-core-dev-engine
docker compose -f deploy/dev/compose.dev.yaml --profile services up -d engine
sleep 8
docker logs ruby-core-dev-engine --tail 5
# Expected last line: "consumer and DLQ forwarder started (workers=20, batch=20)"
```

### Run result (2026-02-24)

**PASS.** Timestamped log output showing the backoff delays between each of the 5 delivery
attempts (`NakWithDelay` confirmed working — see "NakWithDelay and the BackOff schedule"
note):

```
2026-02-24T18:59:51Z [engine] engine: process error for "poison-002": ENGINE_FORCE_FAIL: forced failure for DLQ verification (will nak)
2026-02-24T18:59:52Z [engine] engine: process error for "poison-002": ENGINE_FORCE_FAIL: forced failure for DLQ verification (will nak)
2026-02-24T18:59:55Z [engine] engine: process error for "poison-002": ENGINE_FORCE_FAIL: forced failure for DLQ verification (will nak)
2026-02-24T19:00:02Z [engine] engine: process error for "poison-002": ENGINE_FORCE_FAIL: forced failure for DLQ verification (will nak)
2026-02-24T19:00:17Z [engine] engine: process error for "poison-002": ENGINE_FORCE_FAIL: forced failure for DLQ verification (will nak)
2026-02-24T19:00:17Z [engine] engine: dlq: routed stream=HA_EVENTS seq=409 to dlq.ha_events.engine_processor (deliveries=5)
```

Backoff intervals measured from timestamps: **+1s, +3s, +7s, +15s** (cumulative from first
delivery). These correspond to the configured `DefaultBackOff = [1s, 2s, 4s, 8s]`; slight
variance is normal server-side scheduling jitter. The 5th delivery used plain `Nak()` (delay
= 0) so the max-delivery advisory fired immediately after.

DLQ stream confirmed:

```
nats stream get DLQ <seq>
Subject: dlq.ha_events.engine_processor
{"specversion":"1.0","id":"poison-002","source":"test","type":"test.poison","data":{}}
```

---

## Acceptance Criterion 2: Backpressure

**Goal:** The pull consumer enforces a maximum of 128 outstanding unacknowledged messages
(`MaxAckPending=128`). The server withholds further deliveries until pending slots free up.

**Scope note:** With the Phase 3 stub processor (`processEvent` just logs), messages are
processed faster than they can be published via a CLI loop, so the server-side hold-back
cannot be directly observed at runtime. This test confirms the enforcement configuration
is correctly applied to the consumer. Behavioural verification (processing under load with
real latency) is deferred to Phase 6 integration tests per ROADMAP.md.

### Test

```bash
# Publish 200 messages
for i in $(seq 1 200); do
  nats pub ha.events.load.test \
    "{\"specversion\":\"1.0\",\"id\":\"load-$i\",\"source\":\"test\",\"type\":\"load\",\"data\":{}}"
done

# Inspect consumer config and state
nats consumer info HA_EVENTS engine_processor
```

**Pass condition:** Consumer info shows `Max Ack Pending: 128` and all 200 messages are
eventually acknowledged (`Unprocessed Messages: 0`). If you can catch the consumer mid-flight,
`Outstanding Acks` must not exceed 128.

### Run result (2026-02-24)

**PASS (configuration verified).** Consumer info after 200 messages published and drained:

```
Maximum Deliveries: 5
     Max Ack Pending: 128
             Backoff: 1s, 2s, 4s, 8s
    Outstanding Acks: 0 out of maximum 128
 Unprocessed Messages: 0
```

All 200 messages processed without error. `Max Ack Pending: 128` confirmed applied.

---

## Acceptance Criterion 3: Idempotency

**Goal:** A duplicate event is detected and discarded without being processed twice.
The idempotency key must be durably written to NATS KV so that deduplication survives
an engine restart.

### Setup

Delete any prior test key to ensure the test starts from a clean state:

```bash
nats kv purge idempotency <your-test-id> --force 2>/dev/null || true
```

### Test — Part A: in-process memory path

Verify that a duplicate within a single engine lifetime is caught by the in-memory cache
and that the KV entry is written durably on first processing:

```bash
# Publish the same CloudEvent id twice (no Nats-Msg-Id header — see note below)
nats pub ha.events.test.dup \
  '{"specversion":"1.0","id":"ce-dedup-test","source":"test","type":"test.dup","data":{}}'

nats pub ha.events.test.dup \
  '{"specversion":"1.0","id":"ce-dedup-test","source":"test","type":"test.dup","data":{}}'
```

Expected engine log output:

```
[engine] event received (N bytes) — Phase 5 TODO: implement rule engine
[engine] engine: duplicate event "ce-dedup-test", discarding
```

Confirm the idempotency key was durably written to NATS KV:

```bash
nats kv get idempotency ce-dedup-test
# Expected: entry present, value AQ== ([]byte{1})
```

### Test — Part B: durable KV path (restart survivability)

Restart the engine to clear the in-memory cache, then publish the duplicate again.
Deduplication must now be caught by the KV store, not memory:

```bash
docker restart ruby-core-dev-engine
sleep 8
docker logs ruby-core-dev-engine --tail 3
# Expected: "consumer and DLQ forwarder started (workers=20, batch=20)"

nats pub ha.events.test.dup \
  '{"specversion":"1.0","id":"ce-dedup-test","source":"test","type":"test.dup","data":{}}'
```

Expected engine log output (same as Part A — the discard log is identical regardless of
which store layer caught the duplicate):

```
[engine] engine: duplicate event "ce-dedup-test", discarding
```

**Important:** Do not use the `Nats-Msg-Id` header for this test. The `HA_EVENTS` stream
has a built-in 2-minute deduplication window on that header — a matching second publish
would be silently dropped by the server before reaching the consumer, short-circuiting
the application-level check. Use the CloudEvent `id` body field instead. See
"Idempotency test and the JetStream dedup window" in Notes.

### Run result (2026-02-24)

**PASS — both paths verified.**

**Part A (memory path):**

```
2026/02/24 19:02:35 [engine] event received (85 bytes) — Phase 5 TODO: implement rule engine
2026/02/24 19:02:35 [engine] engine: duplicate event "ce-dedup-201", discarding
```

KV entry confirmed on first publish (revision reflects shared bucket across all test runs):

```
idempotency > ce-dedup-201 revision: 406 created @ 2026-02-24 14:02:35
AQ==
```

**Part B (KV durable path — after engine restart):**

Engine restarted at 19:02:47; in-memory cache cleared. Second publish:

```
2026/02/24 19:03:06 [engine] engine: duplicate event "ce-dedup-201", discarding
```

Duplicate was caught by the NATS KV lookup (cold memory cache, key present in KV).

---

## Notes

### DLQ stream retention

`DefaultDLQMaxAge` is set to 7 days. This is a starting default. As DLQ monitoring
tooling matures (ADR-0022 requires active monitoring of DLQ stream growth), adjust
`config.DefaultDLQMaxAge` and re-run `make dev-up` to apply.

### Advisory forwarder reliability

The `DLQForwarder` fetches the original message payload via `js.GetMsg(stream, seq)`.
If `HA_EVENTS` stream retention evicts a message before the advisory fires (backoff total
≈ 15 s), the DLQ routing logs a warning and the message is lost from DLQ (the consumer
is still unblocked). The current `EnsureHAEventsStream` sets no `MaxAge` or `MaxMsgs`
limit, so this is not a concern under normal single-node usage. If tight storage limits
are introduced on `HA_EVENTS`, re-evaluate this approach.

### Re-running setup-creds after ACL changes

`scripts/setup-credentials.sh` was updated in Phase 3 to add the following subjects to
the engine NATS account:

| Direction | Subject | Purpose |
|-----------|---------|---------|
| publish | `$JS.API.>` | JetStream API (stream/consumer setup, KV management) |
| publish | `$JS.ACK.>` | Message acknowledgements |
| publish | `$KV.idempotency.>` | NATS KV put (uses `$KV.<bucket>.<key>` directly, not under `$JS.API`) |
| publish | `dlq.>` | DLQ forwarder routing |
| subscribe | `_INBOX.>` | Reply-to subjects for JetStream API responses |
| subscribe | `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.HA_EVENTS.engine_processor` | Max-delivery advisory |

After pulling new code on a host that already ran Phase 2, run `make setup-creds` to
regenerate `auth.conf`, then **restart the NATS container** (SIGHUP alone is not
sufficient — see "Edit tool and Docker bind mounts" below).

### Edit tool and Docker bind mounts

When editing `auth.conf` with any tool that writes atomically (write to temp file, then
rename — which includes most editors and the Claude Code Edit tool), the file on disk gets
a new inode. Docker **file** bind mounts pin the original inode at container-start time, so
SIGHUP reloads will silently re-read the old content. Always restart the NATS container
after editing `auth.conf`:

```bash
docker compose -f deploy/dev/compose.dev.yaml restart nats
```

### Idempotency test and the JetStream dedup window

`HA_EVENTS` stream has a default 2-minute deduplication window on the `Nats-Msg-Id`
header. Publishing two messages with the same `Nats-Msg-Id` within that window causes the
server to silently drop the second before it reaches the consumer, making it impossible
to exercise application-level idempotency with that header. Use the CloudEvent `id` body
field (no `Nats-Msg-Id` header) to test app-level dedup, as described in AC3 above.

### NakWithDelay and the BackOff schedule

There are two independent backoff mechanisms in play — they are configured identically but
operate separately:

1. **`ConsumerConfig.BackOff`** (server-side): governs redelivery timing when a message's
   AckWait expires without an explicit ack or nak. This is the NATS server's own retry
   schedule.

2. **Application-side `nakDelay()` + `msg.NakWithDelay(d)`**: when the consumer explicitly
   NAKs a message, plain `msg.Nak()` triggers immediate redelivery regardless of
   `ConsumerConfig.BackOff`. To honour the intended 1s/2s/4s/8s schedule on explicit
   failure, the consumer's `nakDelay()` helper reads `DefaultBackOff` indexed by
   `meta.NumDelivered - 1` and passes that value to `msg.NakWithDelay(d)`.

Tuning retries requires changing both `config.DefaultBackOff` (application side, controls
observed retry timing under failure) and `ConsumerConfig.BackOff` (server side, controls
AckWait-expiry retries). Changing only one will not produce the intended behaviour in both
code paths.

On the final delivery attempt (delivery index ≥ `len(DefaultBackOff)`), the consumer uses
plain `msg.Nak()` so the max-delivery advisory fires promptly without an extra wait.
