# PLAN-0011 — Ada Phase 3b: Replace HTTP Gateway Calls with HA Event Bus

* **Status:** Complete
* **Date:** 2026-04-01
* **Project:** ruby-core + homeassistant
* **Branch:** feat/ada-phase3b-event-bus
* **Related ADRs:** ADR-0008 (targeted reconciliation), ADR-0017 (NKEY auth), ADR-0027 (subject naming)

---

## Scope

Replaces the `fetch()` → `POST /ada/events` path in all four Ada dashboard cards with
`hass.fireEvent('ada_event', payload)`. The gateway WebSocket client adds a second
subscription for `ada_event` and forwards matching events to NATS via the same shared
publish logic as the HTTP handler. The rest of the pipeline — NATS, engine, ada processor,
Postgres, sensor push — is completely unchanged.

**Result:** Writes never touch the public internet. Works identically on LAN and remotely
via Nabu Casa or any other HA remote access method.

**Out of scope:** Read endpoints for data tables (GET /ada/history etc.). Those belong in a
future brief when data tables are scoped.

---

## Architecture

### Before (Phase 3)

```
Browser (remote) → public internet → ruby-gateway.rutabagel.com
  → Traefik JWT → gateway HTTP handler → nc.Publish → NATS
```

### After (Phase 3b)

```
Browser (remote) → HA WebSocket (proxied by Nabu Casa / existing remote access)
  → HA event bus → gateway WS client receives ada_event
  → nc.Publish → NATS
```

### Why the HTTP handler stays

`POST /ada/events` remains in place and unchanged. It is useful for server-side tooling
(scripts, smoke tests, future automations). It is simply no longer called from the browser.

---

## Pre-conditions

* Phase 3 committed and deployed — all cards have `_postAdaEvent` wired, gateway `/ada/events` is live
* Gateway WebSocket client is connected to HA and receiving `state_changed` events
* `go build ./...` passes in ruby-core

---

## Implementation Notes (from codebase review)

Three gaps were identified during review that the brief did not cover:

**1. `haEvent.Data` is typed, not a map.**
The `haEvent` struct has `Data haEventData`, a `state_changed`-specific struct. An
`ada_event` payload unmarshal to empty values in that struct. Step 2 changes `Data` to
`json.RawMessage` and explicitly unmarshals by event type in the routing logic.

**2. The subscription ID is hardcoded.**
`const subID = 1` is used for `state_changed`. The second subscription requires a distinct
ID. Step 4 uses `const adaSubID = 2` with an inline comment noting the assumption.

**3. The WS client has `publisher`, not `nc`.**
The shared `ada.Publish(nc, payload, log)` function requires `nc`. The WS client holds
`publisher *gatewayNats.Publisher`, not `nc` directly. Step 3 adds `nc *goNats.Conn` to
the `Client` struct, passed in at construction alongside `publisher`. App wiring already
has `nc` at that point.

---

## Steps

### Step 1 — Branch

**Action:** `git checkout -b feat/ada-phase3b-event-bus`

**Verification:** `git branch --show-current` returns `feat/ada-phase3b-event-bus`

---

### Step 2 — Refactor `haEvent.Data` to `json.RawMessage`

**Action:** In `services/gateway/ha/client.go`, change:

```go
type haEvent struct {
    EventType string      `json:"event_type"`
    Data      haEventData `json:"data"`
}
```

to:

```go
type haEvent struct {
    EventType string          `json:"event_type"`
    Data      json.RawMessage `json:"data"`
}
```

Update `handleEvent` to unmarshal `ev.Data` into `haEventData` explicitly before using it.
All existing `state_changed` behaviour is preserved — this is a mechanical refactor.

**Verification:** `go build ./...` passes. `go test -tags=fast -race ./services/gateway/...` passes.

---

### Step 3 — Add `nc` to the WS client and extract shared ada publish logic

**Action:**

**In `services/gateway/ha/client.go`:** Add `nc *goNats.Conn` to the `Client` struct and
`NewClient` signature. Update the call site in `app.go` (one line — `nc` is already in
scope there).

**Create `services/gateway/ada/publish.go`:** Extract the CloudEvent-wrapping and NATS
publish logic from `handler.go` into a shared function:

```go
// Publish wraps payload in a CloudEvent and publishes to the appropriate
// ha.events.ada.* NATS subject. Used by both the HTTP handler and the
// gateway WebSocket ada_event handler.
func Publish(nc *goNats.Conn, payload map[string]any, log *slog.Logger) error
```

`eventRoutes` moves to `publish.go` (one place to add new event types going forward).

**Update `handler.go`:** Replace the inline CloudEvent logic with a call to
`ada.Publish(h.nc, raw, h.log)`. `ServeHTTP` retains only the HTTP-specific concerns
(method check, body decode, response writing).

**Verification:** `go build ./...` passes. `go test -tags=fast -race ./services/gateway/...`
passes. Behaviour of `POST /ada/events` is unchanged.

---

### Step 4 — Add `ada_event` subscription and routing to the WS client

**Action:**

In `runOnce`, after the existing `state_changed` subscription and its `result` response,
add a second subscription:

```go
// IDs must be unique per connection; increment if adding subscriptions.
const subID = 1    // state_changed
const adaSubID = 2 // ada_event (Phase 3b)

// ... existing state_changed subscribe/response ...

if err := conn.WriteJSON(haWSMessage{
    ID:        adaSubID,
    Type:      "subscribe_events",
    EventType: "ada_event",
}); err != nil {
    return fmt.Errorf("write subscribe_events ada_event: %w", err)
}
var adaSubResp haWSMessage
if err := conn.ReadJSON(&adaSubResp); err != nil {
    return fmt.Errorf("read subscribe ada_event result: %w", err)
}
if !adaSubResp.Success {
    return fmt.Errorf("ha websocket: subscribe ada_event rejected")
}
c.log.Info("ha websocket: subscribed to ada_event")
```

Add routing to `handleEvent`:

```go
func (c *Client) handleEvent(ev *haEvent) error {
    switch ev.EventType {
    case "ada_event":
        return c.handleAdaEvent(ev)
    default:
        return c.handleStateChanged(ev)
    }
}
```

Rename the current `handleEvent` body to `handleStateChanged`. Implement `handleAdaEvent`:

```go
func (c *Client) handleAdaEvent(ev *haEvent) error {
    var payload map[string]any
    if err := json.Unmarshal(ev.Data, &payload); err != nil {
        return fmt.Errorf("ha: unmarshal ada_event data: %w", err)
    }
    return ada.Publish(c.nc, payload, c.log)
}
```

**Verification:** `go build ./...` passes. `go test -tags=fast -race ./services/gateway/...` passes.

---

### Step 5 — Update frontend cards

**Action:** In all four cards (`ada-quick-actions.ts`, `ada-active-breast-card.ts`,
`ada-active-tummy-card.ts`, `ada-status-strip.ts`):

1. Remove `_postAdaEvent` method entirely
2. Replace all call sites:

```typescript
// Before:
await this._postAdaEvent({event: 'ada.diaper.log', type: 'wet', timestamp: ...});

// After:
this.hass.fireEvent('ada_event', {event: 'ada.diaper.log', type: 'wet', timestamp: ...});
```

1. Remove `async` from any handler that was only async because of `_postAdaEvent`

The payload structure is identical — only the delivery mechanism changes.

**Verification:** `cd /opt/homeassistant && npx tsc --noEmit` passes.
`grep -r '_postAdaEvent' src/` returns empty.

---

### Step 6 — Guard `_minutesSince` against `unknown`/`unavailable`

**Action:** In `ada-status-strip.ts`, confirm or add:

```typescript
private _minutesSince(isoString: string | undefined): number | null {
    if (!isoString || isoString === 'unknown' || isoString === 'unavailable') return null;
    const ms = Date.now() - new Date(isoString).getTime();
    if (isNaN(ms)) return null;
    return Math.floor(ms / 60000);
}
```

Any call site returning `null` renders a neutral/empty state rather than NaN or a crash.

**Verification:** `npx tsc --noEmit` passes clean.

---

### Step 7 — Build, deploy, validate

**Action:**

```bash
# ruby-core
go build ./...
go test -tags=fast -race ./...
make prod-restart SERVICE=gateway
make prod-logs SERVICE=gateway   # confirm "subscribed to ada_event"

# homeassistant
make build-ui && make ha-reload
```

**End-to-end validation checklist:**

| Check | Action | Expected |
|---|---|---|
| Gateway log | Observe on restart | `ha websocket: subscribed to ada_event` |
| LAN: diaper log | Tap Wet on local device | Row in `diapers`; `sensor.ada_last_diaper_time` updates |
| Remote: diaper log | Tap Wet via Nabu Casa / remote | Same result — row in DB, sensor updates |
| No fetch errors | Browser DevTools → Network | No requests to `ruby-gateway.rutabagel.com/ada/events` |
| HTTP handler | `curl -X POST .../ada/events` | Still returns 202 |
| Smoke test | `VAULT_TOKEN=... bash scripts/smoke-test.sh prod` | Both notifier and ada checks pass |

---

### Step 8 — Commit

**Action:**

```
fix: route ada dashboard events via HA event bus for remote compatibility
```

**Files:**

* `services/gateway/ha/client.go` — `json.RawMessage`, second subscription, routing, `nc` field
* `services/gateway/app/app.go` — pass `nc` to `NewClient`
* `services/gateway/ada/publish.go` — new: shared publish function + `eventRoutes`
* `services/gateway/ada/handler.go` — call `ada.Publish`; remove inline CloudEvent logic
* `frontend/src/cards/ada-quick-actions.ts` — `fireEvent` replacement
* `frontend/src/cards/ada-active-breast-card.ts` — `fireEvent` replacement
* `frontend/src/cards/ada-active-tummy-card.ts` — `fireEvent` replacement
* `frontend/src/cards/ada-status-strip.ts` — `fireEvent` replacement + `_minutesSince` guard

---

## Rollback

* **Revert the ruby-core commit** and redeploy gateway. HTTP handler is unmodified — no data loss.
* **Revert the HA commit** and rebuild. Cards fall back to `fetch()` calls — works on LAN only.
* No schema changes. No Postgres changes. Full rollback in under 5 minutes.

---

## Future: read endpoints for data tables

When history/trend charts are built, the read path is:

```
Card → fetch('https://ruby-gateway.rutabagel.com/ada/history?date=...', {Authorization: Bearer token})
     → Traefik JWT validation → Gateway read handler → Postgres query → JSON response
```

GET endpoints on the gateway are safe to expose via Traefik because they are authenticated,
read-only, and cannot modify state. A dedicated brief will cover query design and endpoint
structure when data tables are scoped.
