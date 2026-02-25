# Phase 4 Verification

## Acceptance Criteria

| # | Criterion | Status | Date |
|---|---|---|---|
| AC-1 | A critical action correctly produces a message in the audit log | `[X]` | 2026-02-25 |
| AC-2 | The JetStream restore procedure is documented and validated | `[X]` | 2026-02-24 |
| AC-3 | Logs are structured and contain correlation IDs, enabling basic distributed debugging | `[X]` | 2026-02-25 |

---

## AC-1: Audit Log Population

**Goal:** Engine processes an event → audit event appears in `AUDIT_EVENTS` stream and in the audit-sink's NDJSON file.

### Prerequisites

- Dev stack running: `make dev-up && make dev-services-up`
- NATS CLI context configured for dev (see `docs/ops/phase3-verification.md`)
- `audit-sink` container running and healthy

### Test Steps

**1. Confirm audit-sink is running:**

```bash
docker ps --filter name=ruby-core-dev-audit-sink
```

**2. Publish a test event to `ha.events.test.verify`:**

```bash
nats --context ruby-core-dev pub ha.events.test.verify \
  '{"specversion":"1.0","id":"phase4-test-001","source":"test","type":"test","time":"2026-02-24T00:00:00Z","correlationid":"corr-phase4-001","causationid":"","data":{}}'
```

**3. Check the `AUDIT_EVENTS` stream for the resulting audit record:**

```bash
nats --context ruby-core-dev stream info AUDIT_EVENTS
# Expect: Messages >= 1

nats --context ruby-core-dev stream view AUDIT_EVENTS --last 5
# Expect: at least one message with type "dev.rubycore.audit.v1" on subject audit.ruby_engine.*
```

**4. Verify the audit-sink NDJSON file:**

```bash
docker exec ruby-core-dev-audit-sink cat /data/audit/audit.ndjson | tail -5 | jq .
```

Expected output (example):

```json
{
  "specversion": "1.0",
  "id": "<random hex>",
  "source": "ruby_engine",
  "type": "dev.rubycore.audit.v1",
  "time": "2026-02-24T...",
  "correlationid": "corr-phase4-001",
  "causationid": "",
  "data": {
    "actor": "ruby_engine",
    "action": "event.processed",
    "subject": "ha.events.test.verify",
    "outcome": "success"
  }
}
```

### Pass Criteria

- [X] `AUDIT_EVENTS` stream message count increases after test event publish
- [X] Audit record contains `type: "dev.rubycore.audit.v1"`
- [X] Audit record `data.correlationid` matches the test event's `correlationid`
- [X] NDJSON file on audit-sink contains the same record

---

## AC-2: JetStream Backup and Restore

**Goal:** Backup procedure is documented and a restore from backup returns NATS to the pre-backup state.

See `docs/ops/jetstream-backup.md` for the full procedure.

### Test Steps (Dev Environment)

**1. Record pre-backup stream state:**

```bash
nats --context ruby-core-dev stream info HA_EVENTS | grep Messages
# Record the message count
```

**2. Run backup (dev variant):**

```bash
# Stop dev NATS
docker compose -f deploy/dev/compose.dev.yaml stop nats

# Archive the named volume data
docker run --rm \
  -v ruby-core_ruby-core-dev-nats-data:/data \
  -v /tmp:/backup \
  alpine tar -czf /backup/nats-test-backup.tar.gz /data

# Restart dev NATS
docker compose -f deploy/dev/compose.dev.yaml start nats
```

**3. Clear volume and restore:**

```bash
docker compose -f deploy/dev/compose.dev.yaml stop nats

docker run --rm \
  -v ruby-core_ruby-core-dev-nats-data:/data \
  alpine sh -c "rm -rf /data/*"

docker run --rm \
  -v ruby-core_ruby-core-dev-nats-data:/data \
  -v /tmp:/backup \
  alpine tar -xzf /backup/nats-test-backup.tar.gz -C /

docker compose -f deploy/dev/compose.dev.yaml start nats
```

**4. Verify post-restore state:**

```bash
nats --context ruby-core-dev stream info HA_EVENTS | grep Messages
# Must match pre-backup count
```

### Pass Criteria

- [X] Post-restore `Messages` count matches pre-backup count
- [X] Stream `Last Sequence` matches pre-backup value
- [X] Engine and gateway reconnect successfully after restore

---

## AC-3: Structured Logging with Correlation IDs

**Goal:** Service logs are JSON-structured and include `correlationid` when processing CloudEvents.

### Test Steps

**1. Send a test event with a known correlationid (re-use the event from AC-1).**

**2. Inspect engine logs:**

```bash
docker logs ruby-core-dev-engine 2>&1 | grep "event processed" | tail -5
```

Expected (JSON, one line per entry):

```json
{"time":"2026-02-24T...","level":"INFO","service":"engine","msg":"engine: event processed","eventid":"phase4-test-001","correlationid":"corr-phase4-001","subject":"ha.events.test.verify"}
```

**3. Filter logs by correlationid using jq:**

```bash
docker logs ruby-core-dev-engine 2>&1 | \
  jq -c 'select(.correlationid == "corr-phase4-001")'
```

**4. Verify audit-sink logs are also structured:**

```bash
docker logs ruby-core-dev-audit-sink 2>&1 | tail -5 | jq .
# Expect: JSON with "service":"audit-sink" field
```

### Pass Criteria

- [X] Engine logs are valid JSON (not plain text prefixed with `[engine]`)
- [X] Engine log entries for event processing include `"correlationid"` field
- [X] `jq 'select(.correlationid == "...")'` returns matching log entries
- [X] All service logs include `"service"` field identifying the emitter

---

## Notes

- `[ ]` items above are filled in during the manual test run.
- Replace `[ ]` with `[X]` and add the date when each criterion passes.
- If a criterion fails, document the failure and resolution before marking complete.
