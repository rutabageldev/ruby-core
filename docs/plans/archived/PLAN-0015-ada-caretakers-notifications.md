# PLAN-0015 — Ada: Caretaker Management, User Sync, and Notification Dispatch

* **Status:** Draft
* **Date:** 2026-04-19
* **Project:** ruby-core
* **Roadmap Item:** none (standalone effort)
* **Branch:** feat/ada-caretakers-notifications
* **Related ADRs:** ADR-0029 (stateful processors)

---

## Scope

Delivers caretaker management, HA user sync, caretaker config events, and
ruby-core feeding alert dispatch. After this work, all Ada notification
logic lives in ruby-core. HA is purely UI and device integration.

**Out of scope:** Twilio/SMS notifications. Any schema changes to existing
tables. Retiring `ada_feeding_alert.yaml` is a separate HA-repo action
(see Step 10) and is not part of this commit.

---

## Issues and corrections to the source brief

### 1. CRITICAL — Gateway sync architecture is wrong

The brief proposes that `ada.sync_users` is published to NATS as
`ha.events.ada.sync_users` and then the gateway separately subscribes to
that JetStream subject to trigger the HA user query. **This cannot work.**
The gateway is a publisher of HA_EVENTS JetStream — it holds no JetStream
consumer and has no pull-consumer infrastructure. Adding a JetStream
subscription to the gateway would require significant new machinery and
would create a bizarre loop where the gateway publishes and then immediately
re-consumes its own message.

**Correct approach:** Intercept `ada.sync_users` inline in
`handleAdaEvent` in `services/gateway/ha/client.go`. When the event type
is `"ada.sync_users"`, bypass `ada.Publish()` entirely. Instead, call a
new `syncUsers()` method on the client that queries HA users via the
existing authenticated WebSocket connection and the HA REST API, then
publishes `ha.events.ada.users_synced` directly to NATS. No JetStream
subscription, no gateway consumer, no round-trip.

The `publishToNATS` helper function mentioned in the brief does not exist
in the codebase and should not be created. This is removed.

### 2. Ada processor also receives `ha.events.ada.sync_users`

Since the ada processor subscribes to `ha.events.ada.>`, it will receive
the `sync_users` event published to NATS and fall through to the default
case, logging "unknown event type". A no-op case must be added to
`ProcessEvent` to suppress the warning.

### 3. Handler signatures must follow established pattern

The brief shows all three new processor handlers as
`(ctx context.Context, data []byte)` with `json.Unmarshal(data, &payload)`.
Every existing handler uses `(ctx context.Context, evt schemas.CloudEvent)`
with `remarshal(evt.Data, &d)`. All three handlers are corrected below.

### 4. `Subscriptions()` changes are unnecessary

`Subscriptions()` already returns `"ha.events.ada.>"`. Adding individual
subjects is a no-op and has been omitted.

### 5. `p.ha.PushSensor` does not exist

The ada processor's HA client (`adaha.Client`) only exposes `PushState`.
All uses of `p.ha.PushSensor` in the brief are replaced with `p.ha.PushState`.

### 6. Race condition on `alertTimer`

`time.AfterFunc` fires its callback in a new goroutine. The `alertTimer`
field on `Processor` is not protected by a mutex. Concurrent feeding
events (e.g., a supplement arriving while a feeding is being processed)
can race on `p.alertTimer.Stop()` and the subsequent assignment. A
`sync.Mutex` field `alertMu` is added to protect all access to
`alertTimer`.

### 7. `lastFeedingTime` not in scope during timer restore

The brief's restore snippet references `lastFeedingTime` inside
`restoreSensors` where it is not defined. The restore code must call
`p.q.GetLastFeeding(ctx)` to retrieve it from the DB.

### 8. `time.Local` in alert message uses container timezone

`lastFeedingTime.Local().Format("3:04 PM")` uses the engine container's
local timezone, which is UTC in production. The formatted time will always
show UTC, not the user's local time. For now the message uses UTC with an
explicit label (`"3:04 PM UTC"`). A configurable timezone is out of scope
here but should be tracked as a follow-up.

### 9. `restoreSensors` → `refreshAllSensors`

The brief references `restoreSensors` throughout. The actual method is
`refreshAllSensors`. Corrected below.

### 10. HA token admin permission required for `config/auth/list`

The WebSocket command `config/auth/list` requires the authenticated user
to be an HA admin. The existing `haToken` in Vault at
`secret/ruby-core/ha` must be an admin long-lived access token for user
sync to work. **Verify this before executing.** If the token is a
non-admin token, the WebSocket call will return an error and sync will
fail silently. No code change is needed — this is a deployment
pre-condition.

### 11. `GET /api/services` response format

`GET /api/services` returns an array of domain objects:

```json
[{"domain": "notify", "services": {"mobile_app_mikes_iphone": {...}}}, ...]
```

To extract notify service names, find the entry where `domain == "notify"`
and take the keys of its `services` map. The brief does not show this
parsing. It is implemented correctly in Step 5 below.

### 12. `ada_feeding_alert.yaml` retirement is out of scope for this commit

Retiring the HA automation is a separate HA-repo change that should only
happen after ruby-core notification dispatch is verified working in
production. It is tracked as Step 10 (HA-side action) and excluded from
the ruby-core commit.

---

## Pre-conditions

* [ ] `ada_profile` migration 000002 applied (Effort 1 deployed)
* [ ] `go build ./...` passes clean
* [ ] `sqlc` available (`sqlc version` exits 0)
* [ ] **Verify HA token in Vault has admin permissions** — required for
      `config/auth/list` WebSocket command (see Issue 10)

---

## Step 1 — Branch

**Action:** `git checkout -b feat/ada-caretakers-notifications`

**Verification:** `git branch --show-current` returns
`feat/ada-caretakers-notifications`

---

## Step 2 — Migration 000003

**Action:** Create
`services/engine/processors/ada/store/migrations/000003_caretakers.up.sql`:

```sql
-- caretakers tracks HA user accounts and their notification preferences.
-- Populated and kept in sync via the ada.sync_users event.
-- is_caretaker defaults to false — users must be explicitly enabled.
-- notify_service is auto-discovered during sync (mobile_app_* matching)
-- and may be null if no Companion app is linked.
-- This table is excluded from pre-birth data clear operations.
CREATE TABLE IF NOT EXISTS caretakers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ha_user_id      TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    username        TEXT NOT NULL,
    is_caretaker    BOOLEAN NOT NULL DEFAULT false,
    notify_service  TEXT,
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_caretakers_active
    ON caretakers(is_caretaker)
    WHERE deleted_at IS NULL AND is_caretaker = true;
```

Create `000003_caretakers.down.sql`:

```sql
DROP TABLE IF EXISTS caretakers;
```

**Verification:** Files exist. `go build ./...` passes clean.

---

## Step 3 — Caretaker SQL queries and sqlc regenerate

**Action:** Create
`services/engine/processors/ada/store/queries/caretakers.sql`:

```sql
-- name: UpsertCaretaker :exec
INSERT INTO caretakers (id, ha_user_id, display_name, username, notify_service, updated_at)
VALUES (gen_random_uuid(), @ha_user_id, @display_name, @username, @notify_service, NOW())
ON CONFLICT (ha_user_id) DO UPDATE
    SET display_name   = EXCLUDED.display_name,
        username       = EXCLUDED.username,
        notify_service = EXCLUDED.notify_service,
        deleted_at     = NULL,
        updated_at     = NOW();

-- name: SoftDeleteCaretaker :exec
UPDATE caretakers
SET deleted_at = NOW(), updated_at = NOW()
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: UpdateCaretakerStatus :exec
UPDATE caretakers
SET is_caretaker = @is_caretaker, updated_at = NOW()
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: GetActiveCaretakers :many
SELECT ha_user_id, display_name, notify_service
FROM caretakers
WHERE deleted_at IS NULL
  AND is_caretaker = true
  AND notify_service IS NOT NULL
ORDER BY display_name;

-- name: GetAllCaretakers :many
SELECT ha_user_id, display_name, username, is_caretaker, notify_service
FROM caretakers
WHERE deleted_at IS NULL
ORDER BY display_name;

-- name: GetCaretakerHAUserIDs :many
SELECT ha_user_id FROM caretakers
WHERE deleted_at IS NULL;
```

**Action:** `cd services/engine/processors/ada/store && sqlc generate`

**Verification:** `sqlc generate` exits 0. `go build ./...` passes clean.

---

## Step 4 — Event schemas

**Action:** In `pkg/schemas/ada.go`, append new constants and structs:

```go
const (
    AdaEventSyncUsers       = "ha.events.ada.sync_users"
    AdaEventUsersSynced     = "ha.events.ada.users_synced"
    AdaEventCaretakerUpdate = "ha.events.ada.caretaker_update"
    AdaEventTummyTarget     = "ha.events.ada.config_tummy_target"
)

// AdaHAUser represents one HA user returned by the gateway user sync.
type AdaHAUser struct {
    ID            string `json:"id"`
    Name          string `json:"name"`
    Username      string `json:"username"`
    NotifyService string `json:"notify_service,omitempty"`
}

// AdaUsersSyncedData is published by the gateway after querying HA users.
type AdaUsersSyncedData struct {
    Users []AdaHAUser `json:"users"`
}

// AdaCaretakerUpdateData is fired by the HA config screen on toggle.
type AdaCaretakerUpdateData struct {
    HAUserID    string `json:"ha_user_id"`
    IsCaretaker bool   `json:"is_caretaker"`
    LoggedBy    string `json:"logged_by,omitempty"`
}

// AdaTummyTargetData is fired by the HA config screen on save.
type AdaTummyTargetData struct {
    TargetMin int    `json:"target_min"`
    LoggedBy  string `json:"logged_by,omitempty"`
}
```

**Verification:** `go build ./...` passes clean.

---

## Step 5 — Gateway: inline user sync

**Action:** In `services/gateway/ha/client.go`:

1. Add internal structs for HA's `config/auth/list` response and
   `GET /api/services` response:

```go
type haUserListResult struct {
    Users []haUserEntry `json:"users"`
}

type haUserEntry struct {
    ID       string `json:"id"`
    Name     string `json:"name"`
    Username string `json:"username"`
    IsActive bool   `json:"is_active"`
}
```

1. Add a `syncUsers` method that uses the open WebSocket connection to
   query users and the REST API to discover notify services, then publishes
   `ha.events.ada.users_synced` to NATS:

```go
func (c *Client) syncUsers(ctx context.Context, conn *websocket.Conn, nextID int) error {
    // Query HA users via config/auth/list WebSocket command.
    if err := conn.WriteJSON(map[string]any{
        "id":   nextID,
        "type": "config/auth/list",
    }); err != nil {
        return fmt.Errorf("ha: write config/auth/list: %w", err)
    }
    var result struct {
        ID     int              `json:"id"`
        Result haUserListResult `json:"result"`
    }
    if err := conn.ReadJSON(&result); err != nil {
        return fmt.Errorf("ha: read config/auth/list result: %w", err)
    }

    // Fetch notify services via GET /api/services.
    notifyServices, err := c.fetchNotifyServices(ctx)
    if err != nil {
        c.log.Warn("ha: fetch notify services failed", slog.String("error", err.Error()))
        // Non-fatal — continue with empty notify service map.
        notifyServices = map[string]bool{}
    }

    // Build user list, filtering to active users only.
    users := make([]schemas.AdaHAUser, 0)
    for _, u := range result.Result.Users {
        if !u.IsActive {
            continue
        }
        users = append(users, schemas.AdaHAUser{
            ID:            u.ID,
            Name:          u.Name,
            Username:      u.Username,
            NotifyService: matchNotifyService(u, notifyServices),
        })
    }

    // Publish users_synced to NATS.
    return ada.PublishUsersSynced(c.nc, users, c.log)
}

// fetchNotifyServices calls GET /api/services and returns a set of
// notify service names (e.g. "mobile_app_mikes_iphone").
func (c *Client) fetchNotifyServices(ctx context.Context) (map[string]bool, error) {
    url := strings.TrimRight(c.haURL, "/") + "/api/services"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+c.haToken)
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var domains []struct {
        Domain   string         `json:"domain"`
        Services map[string]any `json:"services"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
        return nil, err
    }

    result := map[string]bool{}
    for _, d := range domains {
        if d.Domain == "notify" {
            for svc := range d.Services {
                result[svc] = true
            }
        }
    }
    return result, nil
}

func matchNotifyService(u haUserEntry, notifyServices map[string]bool) string {
    candidates := []string{
        "mobile_app_" + normalizeForNotify(u.Username),
        "mobile_app_" + normalizeForNotify(u.Name),
    }
    for _, c := range candidates {
        if notifyServices[c] {
            return c
        }
    }
    return ""
}

func normalizeForNotify(s string) string {
    s = strings.ToLower(s)
    s = strings.ReplaceAll(s, " ", "_")
    s = strings.ReplaceAll(s, "-", "_")
    return s
}
```

1. The `Client` needs an `httpClient *http.Client` field (for
   `fetchNotifyServices`). Add it to the struct and initialize it in
   `NewClient` with a 10s timeout.

1. Update `handleAdaEvent` to intercept `ada.sync_users` before forwarding
   to `ada.Publish`. Because `syncUsers` needs the open `conn` and a fresh
   WebSocket message ID, pass `conn` and a pointer to the current message
   counter down from `runOnce`. The cleanest approach is to move the
   `handleAdaEvent` call site into `runOnce` where `conn` is in scope,
   passing it through:

```go
// In the event loop in runOnce, replace:
if err := c.handleEvent(msg.Event); err != nil { ... }

// With:
if err := c.handleEvent(ctx, conn, &msgIDCounter, msg.Event); err != nil { ... }
```

   Where `msgIDCounter` starts at 3 (IDs 1 and 2 are already used by the
   two subscriptions). `handleAdaEvent` becomes:

```go
func (c *Client) handleAdaEvent(ctx context.Context, conn *websocket.Conn,
    nextID *int, ev *haEvent) error {

    var wrapper struct {
        Payload map[string]any `json:"payload"`
    }
    if err := json.Unmarshal(ev.Data, &wrapper); err != nil {
        return fmt.Errorf("ha: unmarshal ada_event wrapper: %w", err)
    }
    if wrapper.Payload == nil {
        return fmt.Errorf("ha: ada_event missing payload field")
    }

    eventType, _ := wrapper.Payload["event"].(string)
    if eventType == "ada.sync_users" {
        id := *nextID
        *nextID++
        return c.syncUsers(ctx, conn, id)
    }

    return ada.Publish(c.nc, wrapper.Payload, c.log)
}
```

1. In `services/gateway/ada/publish.go`, add `PublishUsersSynced` — a
   dedicated function (not routed through `eventRoutes`) that wraps the
   synced user list in a CloudEvent and publishes to
   `ha.events.ada.users_synced`:

```go
func PublishUsersSynced(nc *goNats.Conn, users []schemas.AdaHAUser,
    log *slog.Logger) error {

    subject := schemas.AdaEventUsersSynced
    id := newID()
    evt := schemas.CloudEvent{
        SpecVersion:   schemas.CloudEventsSpecVersion,
        ID:            id,
        Source:        "ruby_gateway",
        Type:          subject,
        Time:          time.Now().UTC().Format(time.RFC3339),
        DataSchema:    schemas.CloudEventDataSchemaVersionV1,
        CorrelationID: id,
        CausationID:   id,
        Data: map[string]any{
            "users": users,
        },
    }
    b, err := json.Marshal(evt)
    if err != nil {
        return fmt.Errorf("ada: marshal users_synced: %w", err)
    }
    if err := nc.Publish(subject, b); err != nil {
        return fmt.Errorf("ada: publish users_synced: %w", err)
    }
    log.Info("ada: users_synced published", slog.Int("count", len(users)))
    return nil
}
```

**Verification:** `go build ./...` passes clean.
`go test -tags=fast -race ./services/gateway/...` passes.

---

## Step 6 — Ada processor: new event handlers

**Action:** In `services/engine/processors/ada/processor.go`:

Add cases to `ProcessEvent` switch:

```go
case schemas.AdaEventSyncUsers:
    return nil // handled by gateway; processor ignores
case schemas.AdaEventUsersSynced:
    return p.handleUsersSynced(ctx, evt)
case schemas.AdaEventCaretakerUpdate:
    return p.handleCaretakerUpdate(ctx, evt)
case schemas.AdaEventTummyTarget:
    return p.handleTummyTarget(ctx, evt)
```

Add handlers, following the established `(ctx, evt schemas.CloudEvent)` +
`remarshal` pattern:

```go
func (p *Processor) handleUsersSynced(ctx context.Context, evt schemas.CloudEvent) error {
    var d schemas.AdaUsersSyncedData
    if err := remarshal(evt.Data, &d); err != nil {
        return fmt.Errorf("ada: decode users_synced: %w", err)
    }

    incomingIDs := make(map[string]bool, len(d.Users))
    for _, u := range d.Users {
        ns := pgtype.Text{String: u.NotifyService, Valid: u.NotifyService != ""}
        if err := p.q.UpsertCaretaker(ctx, &store.UpsertCaretakerParams{
            HaUserID:      u.ID,
            DisplayName:   u.Name,
            Username:      u.Username,
            NotifyService: ns,
        }); err != nil {
            p.log.Warn("ada: upsert caretaker",
                slog.String("user_id", u.ID),
                slog.String("error", err.Error()))
        }
        incomingIDs[u.ID] = true
    }

    existing, err := p.q.GetCaretakerHAUserIDs(ctx)
    if err != nil {
        p.log.Warn("ada: get caretaker ids", slog.String("error", err.Error()))
    } else {
        for _, id := range existing {
            if !incomingIDs[id] {
                if err := p.q.SoftDeleteCaretaker(ctx, id); err != nil {
                    p.log.Warn("ada: soft-delete caretaker",
                        slog.String("user_id", id),
                        slog.String("error", err.Error()))
                }
            }
        }
    }

    p.log.Info("ada: users synced", slog.Int("count", len(d.Users)))
    p.pushCaretakerList(ctx)
    return nil
}

func (p *Processor) handleCaretakerUpdate(ctx context.Context, evt schemas.CloudEvent) error {
    var d schemas.AdaCaretakerUpdateData
    if err := remarshal(evt.Data, &d); err != nil {
        return fmt.Errorf("ada: decode caretaker_update: %w", err)
    }
    if err := p.q.UpdateCaretakerStatus(ctx, &store.UpdateCaretakerStatusParams{
        HaUserID:    d.HAUserID,
        IsCaretaker: d.IsCaretaker,
    }); err != nil {
        return fmt.Errorf("ada: update caretaker status: %w", err)
    }
    p.log.Info("ada: caretaker updated",
        slog.String("user_id", d.HAUserID),
        slog.Bool("is_caretaker", d.IsCaretaker))
    p.pushCaretakerList(ctx)
    return nil
}

func (p *Processor) handleTummyTarget(ctx context.Context, evt schemas.CloudEvent) error {
    var d schemas.AdaTummyTargetData
    if err := remarshal(evt.Data, &d); err != nil {
        return fmt.Errorf("ada: decode tummy_target: %w", err)
    }
    targetStr := strconv.Itoa(d.TargetMin)
    if err := p.q.UpsertConfig(ctx, &store.UpsertConfigParams{
        Key:   "tummy_time_target_min",
        Value: targetStr,
    }); err != nil {
        return fmt.Errorf("ada: upsert tummy target: %w", err)
    }
    p.log.Info("ada: tummy target updated", slog.Int("target_min", d.TargetMin))
    if err := p.ha.PushState(ctx, "sensor.ada_tummy_time_target_min", targetStr, nil); err != nil {
        p.log.Warn("ada: push tummy target sensor", slog.String("error", err.Error()))
    }
    return nil
}
```

**Verification:** `go build ./...` passes clean.

---

## Step 7 — `pushCaretakerList` and `refreshAllSensors` wiring

**Action:** Add `pushCaretakerList` to `processor.go`:

```go
func (p *Processor) pushCaretakerList(ctx context.Context) {
    rows, err := p.q.GetAllCaretakers(ctx)
    if err != nil {
        p.log.Warn("ada: get all caretakers", slog.String("error", err.Error()))
        return
    }

    type entry struct {
        HAUserID      string `json:"ha_user_id"`
        DisplayName   string `json:"display_name"`
        Username      string `json:"username"`
        IsCaretaker   bool   `json:"is_caretaker"`
        NotifyService any    `json:"notify_service"` // string or null
    }

    entries := make([]entry, 0, len(rows))
    for _, r := range rows {
        var ns any
        if r.NotifyService.Valid {
            ns = r.NotifyService.String
        }
        entries = append(entries, entry{
            HAUserID:      r.HaUserID,
            DisplayName:   r.DisplayName,
            Username:      r.Username,
            IsCaretaker:   r.IsCaretaker,
            NotifyService: ns,
        })
    }

    if err := p.ha.PushState(ctx, "sensor.ada_caretakers",
        strconv.Itoa(len(entries)),
        map[string]any{
            "caretakers":   entries,
            "last_updated": time.Now().UTC().Format(time.RFC3339),
        },
    ); err != nil {
        p.log.Warn("ada: push caretaker list", slog.String("error", err.Error()))
    }
}
```

Wire into `refreshAllSensors`:

```go
func (p *Processor) refreshAllSensors(ctx context.Context) {
    p.pushLastEventSensors(ctx)
    p.pushDailyAggregates(ctx)
    p.pushActiveSleepState(ctx)
    p.pushCaretakerList(ctx)  // add this line
}
```

**Verification:** `go build ./...` passes clean.

---

## Step 8 — Feeding alert dispatch

**Action:** Add `Notify` method to `services/engine/processors/ada/ha/client.go`:

```go
// Notify sends a push notification via HA's notify service REST API.
// service is the full service name, e.g. "mobile_app_mikes_iphone".
func (c *Client) Notify(ctx context.Context, service, title, message string) error {
    body, err := json.Marshal(map[string]string{
        "title":   title,
        "message": message,
    })
    if err != nil {
        return fmt.Errorf("ha: marshal notify payload: %w", err)
    }

    url := fmt.Sprintf("%s/api/services/notify/%s", c.baseURL, service)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("ha: build notify request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+c.token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("ha: notify %s: %w", service, err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("ha: notify %s: HTTP %d", service, resp.StatusCode)
    }
    return nil
}
```

**Action:** Add `alertMu sync.Mutex` and `alertTimer *time.Timer` to the
`Processor` struct. Update `Shutdown` to stop the timer under the lock:

```go
func (p *Processor) Shutdown() {
    p.alertMu.Lock()
    if p.alertTimer != nil {
        p.alertTimer.Stop()
    }
    p.alertMu.Unlock()
    if p.healthSub != nil {
        _ = p.healthSub.Unsubscribe()
    }
    if p.stopCh != nil {
        close(p.stopCh)
    }
    p.log.Info("ada: processor shut down")
}
```

**Action:** Add `dispatchFeedingAlert` and `setFeedingAlertTimer`:

```go
func (p *Processor) setFeedingAlertTimer(lastFeedingTime, nextTarget time.Time) {
    p.alertMu.Lock()
    defer p.alertMu.Unlock()
    if p.alertTimer != nil {
        p.alertTimer.Stop()
    }
    delay := time.Until(nextTarget)
    if delay <= 0 {
        return
    }
    p.alertTimer = time.AfterFunc(delay, func() {
        p.dispatchFeedingAlert(context.Background(), lastFeedingTime)
    })
}

func (p *Processor) dispatchFeedingAlert(ctx context.Context, lastFeedingTime time.Time) {
    caretakers, err := p.q.GetActiveCaretakers(ctx)
    if err != nil {
        p.log.Warn("ada: get active caretakers for alert", slog.String("error", err.Error()))
        return
    }
    if len(caretakers) == 0 {
        p.log.Debug("ada: no active caretakers — skipping feeding alert")
        return
    }

    // Use UTC explicitly — engine container timezone is not the user's timezone.
    timeStr := lastFeedingTime.UTC().Format("3:04 PM UTC")
    msg := fmt.Sprintf("Ada hasn't eaten since %s.", timeStr)

    for _, c := range caretakers {
        if err := p.ha.Notify(ctx, c.NotifyService, "Time to feed Ada 🍼", msg); err != nil {
            p.log.Warn("ada: notify caretaker",
                slog.String("service", c.NotifyService),
                slog.String("error", err.Error()))
        }
    }
}
```

**Action:** Call `setFeedingAlertTimer` from `pushFeedingSensors` after
`nextTarget` is computed:

```go
// After computing nextTarget and nextTargetStr in pushFeedingSensors:
p.setFeedingAlertTimer(lastFeedingTime, nextTarget)
```

**Action:** Restore the alert timer in `refreshAllSensors` by querying the
DB directly (not in scope via a parameter):

```go
// At the end of refreshAllSensors, after pushCaretakerList:
p.restoreAlertTimer(ctx)
```

```go
func (p *Processor) restoreAlertTimer(ctx context.Context) {
    cfg, err := p.q.GetConfig(ctx, cfgKeyNextFeedingTarget)
    if err != nil {
        return // no target stored yet
    }
    target, err := time.Parse(time.RFC3339, cfg.Value)
    if err != nil || !time.Now().Before(target) {
        return // target is in the past
    }
    last, err := p.q.GetLastFeeding(ctx)
    if err != nil {
        return
    }
    p.setFeedingAlertTimer(last.Timestamp.Time, target)
    p.log.Info("ada: feeding alert timer restored",
        slog.Duration("fires_in", time.Until(target)))
}
```

**Verification:** `go build ./...` passes clean.
`go test -tags=fast -race ./services/engine/processors/ada/...` passes.

---

## Step 9 — Commit

**Action:** Stage all changes and commit:

```
feat: caretaker management, user sync, and ruby-core feeding notifications
```

**Files:**

* `services/engine/processors/ada/store/migrations/000003_caretakers.{up,down}.sql`
* `services/engine/processors/ada/store/queries/caretakers.sql`
* `services/engine/processors/ada/store/caretakers.sql.go` (sqlc generated)
* `services/engine/processors/ada/store/models.go` (sqlc regenerated)
* `pkg/schemas/ada.go`
* `services/gateway/ada/publish.go` (PublishUsersSynced)
* `services/gateway/ha/client.go` (syncUsers, fetchNotifyServices, httpClient field)
* `services/engine/processors/ada/ha/client.go` (Notify method)
* `services/engine/processors/ada/processor.go` (all new handlers, pushCaretakerList,
  alertMu, alertTimer, setFeedingAlertTimer, dispatchFeedingAlert, restoreAlertTimer)

**Verification:** Pre-commit hooks pass. `go test -tags=fast -race ./...` green.

---

## Step 10 — Retire `ada_feeding_alert.yaml` (HA repo — separate action)

**Action (HA repo, after prod verification):** Remove
`prod/config/automations/ada_feeding_alert.yaml`. Keep
`ada_feeding_claim_reset.yaml` — it is a pure HA-native automation
(one entity watching another) and stays in HA.

**Verification:** HA config reloads without errors. Feeding alerts continue
to arrive via ruby-core.

---

## Validation (post-deploy)

| Check | Action | Expected |
|---|---|---|
| Migration | `psql -c "\dt"` | `caretakers` table present |
| HA token is admin | Trigger sync | No "unauthorized" error in gateway logs |
| User sync | Tap Sync Users in HA | Gateway logs user query; engine logs "users synced"; `sensor.ada_caretakers` updates |
| Notify service match | Check `sensor.ada_caretakers` attributes | Active Companion app users have non-null `notify_service` |
| Caretaker toggle | Toggle a user on | `UpdateCaretakerStatus` called; sensor updates |
| Tummy target | Save 45m in config | `sensor.ada_tummy_time_target_min` = 45 |
| Feeding alert | Log a feeding; wait for `next_feeding_target` | Push notification on active caretaker devices |
| Alert timer restore | Restart engine mid-interval | Alert fires at correct time; engine logs "feeding alert timer restored" |
| Idempotent sync | Tap Sync Users twice | No duplicate rows; caretaker `is_caretaker` flags preserved across sync |

---

## Rollback

* Revert the ruby-core commit and redeploy gateway and engine
* Re-enable `ada_feeding_alert.yaml` in HA if already retired
* Run down migration: `migrate -path .../migrations -database "..." down 1`
* No caretaker data lost — table can be re-synced on next deploy

---

## Open question: timezone in alert message

Alert notifications currently show UTC time (`"3:04 PM UTC"`). If a
timezone preference is needed (e.g. `America/Chicago`), the cleanest path
is to add a `timezone` key to `ada_config` and load it in
`dispatchFeedingAlert`. This is out of scope here but should be tracked as
a follow-up if UTC is not acceptable.
