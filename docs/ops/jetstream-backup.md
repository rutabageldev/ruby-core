# JetStream Backup and Restore

**ADR reference:** ADR-0021 (Resilient Single-Node NATS Deployment)

Ruby Core's single-node NATS deployment stores all durable state — JetStream streams,
KV buckets, and consumer positions — on a single host path. This document covers how
to back up that state, restore from a backup, and verify the procedure works.

---

## Production Storage Path

```
/var/lib/ruby-core/nats   ← host bind mount → container /data/jetstream
```

Defined in `deploy/prod/compose.prod.yaml`:

```yaml
volumes:
  - /var/lib/ruby-core/nats:/data/jetstream
```

---

## What Is and Is NOT Backed Up

| Component | Backed up? | Notes |
|---|---|---|
| JetStream streams (`HA_EVENTS`, `DLQ`, `AUDIT_EVENTS`) | **Yes** | All message data and consumer positions |
| KV buckets (`idempotency`) | **Yes** | Part of JetStream storage |
| NATS TLS certs + auth.conf | **No** | Ephemeral volume; regenerated from Vault at every container start by `nats-init` |
| Vault secrets (NKEY seeds, TLS certs) | **Separately** | Managed by the node's Vault instance; back up Vault independently |

---

## Backup Procedure

> Estimated downtime: 30–60 seconds.

### Step 1: Verify NATS is healthy

```bash
docker exec ruby-core-prod-nats wget -qO- http://localhost:8222/healthz
# Expected: {"status":"ok"}
```

### Step 2: Stop NATS gracefully

```bash
docker compose -f /opt/ruby-core/deploy/prod/compose.prod.yaml stop nats
```

Gateway and engine will lose connectivity and begin reconnect attempts. This is expected.

### Step 3: Archive JetStream data

```bash
BACKUP_FILE="/backups/nats-$(date +%Y%m%d%H%M%S).tar.gz"
tar -czf "${BACKUP_FILE}" /var/lib/ruby-core/nats
echo "Backup written to ${BACKUP_FILE} ($(du -sh ${BACKUP_FILE} | cut -f1))"
```

### Step 4: (Recommended) Copy archive off-node

```bash
scp "${BACKUP_FILE}" user@backup-host:/backups/ruby-core/
```

### Step 5: Restart NATS

```bash
docker compose -f /opt/ruby-core/deploy/prod/compose.prod.yaml start nats
```

Wait for the health check to pass (≤30s):

```bash
docker compose -f /opt/ruby-core/deploy/prod/compose.prod.yaml ps nats
# Status should show "healthy"
```

### Step 6: Verify streams are intact

```bash
# Requires NATS CLI with a configured TLS context (see docs/ops/phase3-verification.md)
nats --context ruby-core-prod stream ls
nats --context ruby-core-prod stream info HA_EVENTS
nats --context ruby-core-prod stream info AUDIT_EVENTS
```

---

## Restore Procedure

Use this procedure to restore from a backup after data loss or host migration.

> This procedure causes service downtime. Alert users before proceeding.

### Step 1: Stop all services

```bash
docker compose -f /opt/ruby-core/deploy/prod/compose.prod.yaml down
```

### Step 2: Clear the current JetStream data

```bash
rm -rf /var/lib/ruby-core/nats/*
```

### Step 3: Extract the backup

```bash
BACKUP_FILE="/backups/nats-<timestamp>.tar.gz"
tar -xzf "${BACKUP_FILE}" -C /
# This restores /var/lib/ruby-core/nats/ from the archive.
```

Verify the restore looks correct:

```bash
ls -lh /var/lib/ruby-core/nats/
# Expect: jetstream directory with stream data subdirectories
```

### Step 4: Start the stack

```bash
docker compose -f /opt/ruby-core/deploy/prod/compose.prod.yaml up -d
```

### Step 5: Verify message counts

After services reconnect, compare stream message counts against the pre-backup state
(record stream info before any backup as a baseline):

```bash
nats --context ruby-core-prod stream report
```

Key fields to check per stream: `Messages`, `Bytes`, `Last Sequence`.

---

## Validating the Procedure (Validated in Dev Environment)

Run this test periodically to confirm the procedure works. The test was validated on
2026-02-24 in the dev environment using the procedure described above.

### Test steps

1. Start the dev stack and publish a known number of messages to `HA_EVENTS`.
2. Record stream info: `nats stream info HA_EVENTS` → note `Messages` count.
3. Run backup steps 1–3 against the dev volume (substitute dev paths).
4. Clear the volume and run restore steps 2–3.
5. Restart the dev stack.
6. Verify `nats stream info HA_EVENTS` shows the same `Messages` count.
7. Subscribe and confirm a known message is recoverable.

A passing test confirms the archive format is correct and the restore path is exercised.

**Note:** Production restores have not been exercised on live production data.
The dev validation is the current assurance level. Schedule a planned maintenance
window for a full production dry-run annually or after any major NATS version upgrade.

---

## Backup Schedule Recommendation

| Frequency | Method | Retention |
|---|---|---|
| Daily | Automated `cron` job (steps 2–4 above) | Keep 7 days on-node |
| Weekly | Copy off-node (step 4) | Keep 4 weeks off-node |

Example cron entry (as root or a user with Docker access):

```cron
0 3 * * * /opt/ruby-core/scripts/backup-nats.sh >> /var/log/ruby-core-backup.log 2>&1
```

A `scripts/backup-nats.sh` script implementing the above steps should be created
as a Phase 4 follow-up operational task.

---

## Disk Health Monitoring

Per ADR-0021 §4, the host's physical storage is the primary single point of failure.
Monitor disk health proactively:

```bash
# Check SMART status of the data disk
sudo smartctl -a /dev/sda

# If using ZFS
sudo zpool status
sudo zpool scrub rpool
```

Set up alerts for SMART errors or ZFS scrub failures. Proactive detection is the
primary defence against data loss on a single-node deployment.

---

## Audit Data Backup

In addition to JetStream, the audit-sink writes to `/var/lib/ruby-core/audit/audit.ndjson`.
Include this path in the same backup schedule:

```bash
tar -czf "${BACKUP_FILE}" /var/lib/ruby-core/nats /var/lib/ruby-core/audit
```

The audit file does not need to be restored to recover operational state — services
will continue appending to it on restart. Restoring it preserves the historical audit trail.
