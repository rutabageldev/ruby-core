# Phase 5 Verification

## Acceptance Criteria

| # | Criterion | Status | Date |
|---|---|---|---|
| AC-1 | Any exposed API on the `gateway` is protected by Traefik | `[X]` | 2026-03-02 |
| AC-2 | The `gateway` can connect to HA, process events, and reconcile state | `[X]` | 2026-03-02 |
| AC-3 | The `engine` can load a YAML rule and execute a simple automation | `[X]` | 2026-03-02 |

---

## Prerequisites

All checks below require the dev stack running with services:

```bash
make dev-up           # Start NATS
make dev-services-up  # Build and start gateway, engine, notifier, audit-sink
```

Confirm all containers are healthy:

```bash
make dev-ps
# Expect: gateway, engine, notifier, audit-sink all "Up"
```

> **Note on Traefik (AC-1):** In dev the gateway publishes directly to `127.0.0.1:8090` (ADR-0020).
> `traefik_proxy` is a prod-only network. AC-1 is verified by prod compose inspection, not by
> running the dev stack.

---

## Smoke: HA Credential Bootstrap

Before testing AC-1 and AC-2, confirm the gateway can fetch its HA credentials from Vault.

**Check gateway logs for the Vault fetch step:**

```bash
docker logs ruby-core-dev-gateway 2>&1 | grep "vault: fetched HA config"
# Expect:  {"level":"INFO","service":"gateway","msg":"vault: fetched HA config","ha_url":"http://..."}
```

If this line is absent, the gateway has not yet bootstrapped. Check for errors:

```bash
docker logs ruby-core-dev-gateway 2>&1 | grep -E "ERROR|WARN" | head -10
```

Common issue: the `secret/data/ruby-core/ha` Vault path does not exist. Store credentials:

```bash
vault kv put secret/ruby-core/ha url="http://homeassistant:8123" token="<long-lived-token>"
```

Then restart the gateway: `docker compose -f deploy/dev/compose.dev.yaml restart gateway`

---

## AC-1: Traefik Edge Auth

**Goal:** The gateway is wired for Traefik routing in prod (no host port, `traefik_proxy` network,
correct labels). This AC is verified by **prod compose inspection** — it cannot be tested against
the dev stack, which uses a direct localhost port by design (ADR-0020).

### Test Steps

**1. Confirm prod gateway has no host-published ports:**

```bash
# Check gateway service block only (NATS has localhost ports for ops access — that's expected)
awk '/^  gateway:/,/^  [a-z]/' deploy/prod/compose.prod.yaml | grep "127.0.0.1"
# Expect: no output (gateway has no host port)
```

**2. Confirm prod gateway is on the `traefik_proxy` network:**

```bash
grep -A10 "networks:" deploy/prod/compose.prod.yaml | grep "traefik_proxy"
# Expect: traefik_proxy listed under gateway networks
```

**3. Confirm Traefik Docker labels are present in prod compose:**

```bash
grep "traefik" deploy/prod/compose.prod.yaml
# Expect: labels for enable, router rule, entrypoint, middleware (forwardauth)
```

**4. Confirm dev gateway is reachable directly on localhost (dev-only convenience):**

```bash
curl -s http://localhost:8090/health
# Expect: {"status":"ok"}
```

### Pass Criteria

- [X] Prod compose has no `ports:` entry for the gateway service (2026-03-02)
- [X] Prod compose has `traefik_proxy` in gateway `networks:` (2026-03-02)
- [X] Prod compose has Traefik Docker labels (router rule, forwardauth middleware) (2026-03-02)
- [X] Dev health endpoint reachable at `http://localhost:8090/health` (2026-03-02)

---

## AC-2: Gateway HA Connection, Event Processing, and Reconciliation

**Goal:** The gateway connects to Home Assistant via WebSocket, publishes state events to NATS,
and performs targeted reconciliation after reconnect.

### Test Steps

**1. Check gateway WebSocket connection:**

```bash
docker logs ruby-core-dev-gateway 2>&1 | grep -E "ha websocket:"
# Expect lines like:
# {"msg":"ha websocket: connecting","url":"ws://homeassistant:8123/api/websocket",...}
# {"msg":"ha websocket: authenticated",...}
# {"msg":"ha websocket: subscribed to state_changed",...}
```

**2. Check reconciliation triggered on connect:**

```bash
docker logs ruby-core-dev-gateway 2>&1 | grep "reconciler:"
# Expect:
# {"msg":"reconciler: starting","entities":N,...}
# {"msg":"reconciler: complete",...}
```

**3. Confirm HA events are flowing to NATS:**

```bash
# Subscribe to all HA events (requires NATS CLI with ruby-core-dev context):
nats --context ruby-core-dev sub "ha.events.>" &
# Wait ~30 seconds for a state_changed event from HA
# Then Ctrl+C

# Or check stream message count is growing:
nats --context ruby-core-dev stream info HA_EVENTS | grep Messages
# Wait 60 seconds:
nats --context ruby-core-dev stream info HA_EVENTS | grep Messages
# Expect: count increased
```

**4. Verify lean projection (only configured attributes forwarded):**

Publish a test event that simulates what the gateway receives (if HA is not available):

```bash
# Simulate a state_changed event with extra attributes:
nats --context ruby-core-dev pub ha.events.phone.katie \
  '{"specversion":"1.0","id":"p5-test-001","source":"ha","type":"state_changed",
    "time":"2026-03-01T10:00:00Z","correlationid":"p5-test-001","causationid":"p5-test-001",
    "subject":"phone.katie",
    "data":{"state":"home","gps_lat":51.5,"gps_lon":-0.1,"last_changed":"2026-03-01T10:00:00Z"}}'
```

Check the engine received it and processed it:

```bash
docker logs ruby-core-dev-engine 2>&1 | grep "presence_notify" | tail -5
```

**5. Test reconnect + reconciliation:**

```bash
# Restart gateway to force a fresh WebSocket connection:
docker compose -f deploy/dev/compose.dev.yaml restart gateway

# Wait for gateway to reconnect:
sleep 10
docker logs ruby-core-dev-gateway 2>&1 | grep "reconciler:" | tail -5
# Expect: "reconciler: starting" and "reconciler: complete"
```

**6. Verify health heartbeat is published:**

```bash
nats --context ruby-core-dev stream add HEALTH_CHECK \
  --subjects "gateway.health" --storage memory --retention limits \
  --max-age 60s --replicas 1 2>/dev/null || true

nats --context ruby-core-dev sub "gateway.health" &
# Wait 20 seconds (heartbeat every 15s):
sleep 20 && kill %1

# Expect: at least 1 heartbeat message with {"ha_connected":true}
```

### Pass Criteria

- [X] Gateway logs show successful WebSocket authentication to HA (2026-03-02)
- [X] Gateway logs show reconciler started and completed after connect (2026-03-02)
- [X] `HA_EVENTS` stream message count increases over time — 36→38 during session, 17 active subjects (2026-03-02)
- [X] `gateway.health` subject receives heartbeat messages every ~15 s — confirmed `ha_connected:true` (2026-03-02)

**Notes:**

- Reconciler logs a 404 WARN for `phone.katie` — expected, placeholder entity not in HA; reconcile loop itself behaved correctly
- Gateway URL was `homeassistant:8123` (unresolvable from bridge network); corrected to `172.18.0.1:8123` in `secret/ruby-core/ha`

---

## AC-3: Engine Loads YAML Rule and Executes Automation

**Goal:** The engine loads `configs/rules/katie_presence.yaml`, publishes the compiled config to
NATS KV, and executes the presence-based notification automation when a matching event is observed.

The full pipeline is:
`ha.events.phone.katie` → **presence service** (debounce + WiFi corroboration) → `ruby_presence.events.state.katie` → **engine** (presence_notify processor) → `ruby_engine.commands.notify.*` → **notifier** → HA mobile push notification.

### Test Steps

**1. Verify rule loading at startup:**

```bash
docker logs ruby-core-dev-engine 2>&1 | grep -E "config:|rules loaded"
# Expect:
# {"msg":"config: rules loaded","critical_entities":1,"passlist_domains":1,...}
# {"msg":"config: passlist and critical entities published to NATS KV",...}
```

**2. Check config KV bucket has the published entries:**

```bash
nats --context ruby-core-dev kv get config config.engine.passlist | jq .
# Expect: {"phone":["state"]} (or similar for your rule)

nats --context ruby-core-dev kv get config config.engine.critical_entities | jq .
# Expect: ["phone.katie"] (or your entity)
```

**3. Simulate Katie arriving home — publish a test event:**

> Adjust entity name to match your `katie_presence.yaml` trigger (or your own rule).

```bash
CORR_ID="p5-presence-$(date +%s)"
nats --context ruby-core-dev pub ha.events.phone.katie \
  "{\"specversion\":\"1.0\",\"id\":\"${CORR_ID}\",\"source\":\"ha\",
    \"type\":\"state_changed\",\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"correlationid\":\"${CORR_ID}\",\"causationid\":\"${CORR_ID}\",
    \"subject\":\"phone.katie\",
    \"data\":{\"state\":\"home\",\"last_changed\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}"
```

**4. Check engine processed the event and dispatched a notification:**

```bash
docker logs ruby-core-dev-engine 2>&1 | grep "presence_notify" | tail -5
# Expect:
# {"msg":"presence_notify: notification dispatched","title":"Welcome home",...}
```

**5. Check notifier received the command and called HA:**

```bash
docker logs ruby-core-dev-notifier 2>&1 | grep "notifier:" | tail -5
# Expect:
# {"msg":"notifier: notification sent","entity_id":"phone.katie","device":"phone_michael","title":"Welcome home",...}
```

If HA is unreachable or the token is invalid, expect a warning with HTTP status:

```bash
# {"msg":"notifier: HA returned non-2xx","entity_id":"phone.katie","http_status":404,...}
# Indicates HA is reachable but the notify service name is wrong.

# {"msg":"notifier: HA REST call failed","error":"...connection refused...",...}
# Indicates HA is not reachable at all.
```

**6. Simulate departure and confirm second notification:**

```bash
CORR_ID="p5-presence-leave-$(date +%s)"
nats --context ruby-core-dev pub ha.events.phone.katie \
  "{\"specversion\":\"1.0\",\"id\":\"${CORR_ID}\",\"source\":\"ha\",
    \"type\":\"state_changed\",\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"correlationid\":\"${CORR_ID}\",\"causationid\":\"${CORR_ID}\",
    \"subject\":\"phone.katie\",
    \"data\":{\"state\":\"not_home\",\"last_changed\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}"

# Check engine + notifier logs:
docker logs ruby-core-dev-engine 2>&1 | grep "presence_notify" | tail -5
docker logs ruby-core-dev-notifier 2>&1 | grep "notifier:" | tail -5
# Expect: second notification dispatched with title "Just left"
```

**7. Verify idempotency — publish the same event twice:**

```bash
# Re-publish the same arrival event using the same ID:
nats --context ruby-core-dev pub ha.events.phone.katie \
  "{\"specversion\":\"1.0\",\"id\":\"${CORR_ID}\",\"source\":\"ha\",
    \"type\":\"state_changed\",\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"correlationid\":\"${CORR_ID}\",\"causationid\":\"${CORR_ID}\",
    \"subject\":\"phone.katie\",
    \"data\":{\"state\":\"home\",\"last_changed\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}"

docker logs ruby-core-dev-engine 2>&1 | grep -E "duplicate|skipped" | tail -3
# Expect: engine consumer skips the duplicate (idempotency dedup on event ID)
```

### Pass Criteria

- [X] Engine logs show rules loaded with correct `critical_entities` count — `critical_entities:1, passlist_domains:1` (2026-03-02)
- [X] `config` KV bucket contains `passlist` and `critical_entities` entries — `{"phone":["state"]}` and `["phone.katie"]` (2026-03-02)
- [X] Arrival event triggers `presence_notify: notification dispatched` in engine logs — title "Welcome home" (2026-03-02)
- [X] Notifier logs show `notifier: notification sent` — device=phone_michael, push notification received on Michael's phone (2026-03-02)
- [X] Departure event triggers a second distinct notification — title "Just left" (2026-03-02)
- [X] Duplicate event ID is silently deduplicated — `engine: duplicate event, discarding` logged; no second notification sent (2026-03-02)

**Architecture note:** AC-3 is satisfied via the presence fusion service introduced in this phase.
HA phone events go through the presence state machine (debounce + WiFi corroboration) before
reaching the engine, eliminating false "left home" notifications from transient signal drops.

**Known issue (non-blocking):** After a NATS SIGHUP reload, the engine's JetStream pull-consumer
goroutines exit with "Server Shutdown" but the DLQ forwarder's push subscription holds `wg.Wait()`,
leaving the engine alive but idle. Recovery: `docker restart ruby-core-dev-engine`. Root cause is
the absence of an errgroup-style supervisory structure — deferred to a future phase.

---

## Notes

- `[ ]` items are filled in during the manual test run.
- Replace `[ ]` with `[X]` and add the date when each criterion passes.
- Placeholder values in `configs/rules/katie_presence.yaml` (`phone.katie`, `phone_michael`)
  must be updated to match your own HA entity and notify device before AC-3 can produce real push notifications.
- See `ADRs/0008-gateway-health-and-reconciliation.md` and
  `ADRs/0009-gateway-responsibilities.md` for architectural context.
