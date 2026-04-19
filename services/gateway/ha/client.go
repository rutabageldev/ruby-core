package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/gateway/ada"
	gatewayNats "github.com/primaryrutabaga/ruby-core/services/gateway/nats"
)

// haWSMessage is a generic HA WebSocket protocol message envelope.
type haWSMessage struct {
	Type        string   `json:"type"`
	ID          int      `json:"id,omitempty"`
	AccessToken string   `json:"access_token,omitempty"`
	EventType   string   `json:"event_type,omitempty"`
	Success     bool     `json:"success,omitempty"`
	Event       *haEvent `json:"event,omitempty"`
}

// haEvent wraps a HA WebSocket event. Data is left as raw JSON so the
// routing logic can unmarshal it into the appropriate type per event_type.
type haEvent struct {
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
}

// haEventData is the data field of a state_changed event.
type haEventData struct {
	EntityID string         `json:"entity_id"`
	NewState *haEntityState `json:"new_state"`
}

// haEntityState is the new_state field of a state_changed event.
type haEntityState struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged string         `json:"last_changed"`
}

// haUserEntry is one HA user account from the config/auth/list WebSocket response.
type haUserEntry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Username        string `json:"username"`
	IsActive        bool   `json:"is_active"`
	SystemGenerated bool   `json:"system_generated"`
}

// Client connects to the Home Assistant WebSocket API, subscribes to
// state_changed and ada_event events, normalises state_changed events via the
// Normalizer, and publishes CloudEvents to NATS. On each successful reconnect
// it triggers the Reconciler (ADR-0008 targeted reconciliation).
type Client struct {
	haURL        string
	haToken      string
	nc           *goNats.Conn
	norm         *Normalizer
	publisher    *gatewayNats.Publisher
	stateKV      goNats.KeyValue
	critEntities []string
	reconciler   *Reconciler
	log          *slog.Logger
	haConnected  atomic.Bool
	httpClient   *http.Client
}

// NewClient creates a Client.
func NewClient(
	haURL, haToken string,
	nc *goNats.Conn,
	norm *Normalizer,
	publisher *gatewayNats.Publisher,
	stateKV goNats.KeyValue,
	critEntities []string,
	reconciler *Reconciler,
	log *slog.Logger,
) *Client {
	return &Client{
		haURL:        haURL,
		haToken:      haToken,
		nc:           nc,
		norm:         norm,
		publisher:    publisher,
		stateKV:      stateKV,
		critEntities: critEntities,
		reconciler:   reconciler,
		log:          log,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Connected reports whether the HA WebSocket is currently authenticated and
// subscribed. Safe to call concurrently from the health heartbeat goroutine.
func (c *Client) Connected() bool {
	return c.haConnected.Load()
}

// Run connects to the HA WebSocket and processes events until ctx is cancelled.
// It reconnects with exponential backoff capped at 30 s (ADR-0008).
func (c *Client) Run(ctx context.Context) {
	backoff := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
	}
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.runOnce(ctx); err != nil {
			c.haConnected.Store(false)
			c.log.Warn("ha websocket: disconnected",
				slog.String("error", err.Error()),
				slog.Int("attempt", attempt+1),
			)
		}
		delay := backoff[min(attempt, len(backoff)-1)]
		attempt++
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// runOnce opens one WebSocket session: authenticates, subscribes to
// state_changed events, and loops over incoming messages.
func (c *Client) runOnce(ctx context.Context) error {
	wsURL := haWSURL(c.haURL)
	c.log.Info("ha websocket: connecting", slog.String("url", wsURL))

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// ── auth flow ───────────────────────────────────────────────────────────
	// HA sends auth_required first.
	var authReq haWSMessage
	if err := conn.ReadJSON(&authReq); err != nil {
		return fmt.Errorf("read auth_required: %w", err)
	}
	if authReq.Type != "auth_required" {
		return fmt.Errorf("expected auth_required, got %q", authReq.Type)
	}

	if err := conn.WriteJSON(haWSMessage{Type: "auth", AccessToken: c.haToken}); err != nil {
		return fmt.Errorf("write auth: %w", err)
	}

	var authResp haWSMessage
	if err := conn.ReadJSON(&authResp); err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	switch authResp.Type {
	case "auth_ok":
		c.log.Info("ha websocket: authenticated")
	case "auth_invalid":
		return fmt.Errorf("ha websocket: authentication rejected")
	default:
		return fmt.Errorf("ha websocket: unexpected auth response type %q", authResp.Type)
	}

	// ── subscribe to events ────────────────────────────────────────────────
	// IDs must be unique per connection; 1 and 2 are used by the two subscriptions.
	// msgID is incremented for any subsequent command (e.g. config/auth/list).
	const subID = 1    // state_changed
	const adaSubID = 2 // ada_event (Phase 3b dashboard write path)
	msgID := 3         // next available ID for on-demand commands

	if err := conn.WriteJSON(haWSMessage{
		ID:        subID,
		Type:      "subscribe_events",
		EventType: "state_changed",
	}); err != nil {
		return fmt.Errorf("write subscribe_events state_changed: %w", err)
	}
	var subResp haWSMessage
	if err := conn.ReadJSON(&subResp); err != nil {
		return fmt.Errorf("read subscribe state_changed result: %w", err)
	}
	if !subResp.Success {
		return fmt.Errorf("ha websocket: subscribe state_changed rejected")
	}
	c.log.Info("ha websocket: subscribed to state_changed")

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

	// Mark connected only after both subscriptions are confirmed — the health
	// heartbeat reads this flag to publish ha_connected, which the engine watches
	// to trigger restoreSensors on the false→true transition.
	c.haConnected.Store(true)

	// Trigger targeted reconciliation after a successful reconnect (ADR-0008).
	go c.reconciler.Run(ctx, c.critEntities)

	// ── event loop ─────────────────────────────────────────────────────────
	for {
		if ctx.Err() != nil {
			return nil
		}
		var msg haWSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read event: %w", err)
		}
		if msg.Type != "event" || msg.Event == nil {
			continue
		}
		if err := c.handleEvent(ctx, conn, &msgID, msg.Event); err != nil {
			c.log.Warn("ha websocket: handle event error",
				slog.String("error", err.Error()),
			)
		}
	}
}

// handleEvent routes an incoming HA WebSocket event by event_type.
func (c *Client) handleEvent(ctx context.Context, conn *websocket.Conn, nextID *int, ev *haEvent) error {
	switch ev.EventType {
	case "ada_event":
		return c.handleAdaEvent(ctx, conn, nextID, ev)
	default:
		return c.handleStateChanged(ev)
	}
}

// handleStateChanged processes a state_changed event from HA.
func (c *Client) handleStateChanged(ev *haEvent) error {
	var data haEventData
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		return fmt.Errorf("ha: unmarshal state_changed data: %w", err)
	}

	if data.NewState == nil {
		return nil // entity removed; nothing to publish
	}
	ns := data.NewState
	if ns.LastChanged == "" {
		return nil
	}

	domain, _, err := SplitEntityID(ns.EntityID)
	if err != nil {
		return err
	}

	filtered := c.norm.Apply(domain, ns.Attributes)
	if err := c.publisher.PublishHAEvent(ns.EntityID, ns.State, filtered, ns.LastChanged); err != nil {
		return fmt.Errorf("publish event for %s: %w", ns.EntityID, err)
	}

	// Record the last-seen timestamp so the reconciler can detect drift (ADR-0008).
	if _, err := c.stateKV.Put(ns.EntityID, []byte(ns.LastChanged)); err != nil {
		c.log.Warn("ha websocket: stateKV.Put failed",
			slog.String("entity_id", ns.EntityID),
			slog.String("error", err.Error()),
		)
	}
	return nil
}

// handleAdaEvent processes an ada_event fired from the dashboard via the
// script.fire_ada_event HA script intermediary. The script wraps the caller's
// payload under a "payload" key, so ev.Data arrives as:
//
//	{"payload": {"event": "ada.diaper.log", "type": "dirty", ...}}
//
// ada.sync_users is intercepted here and handled inline via the open WebSocket
// connection — it does not go through the standard eventRoutes publish path.
func (c *Client) handleAdaEvent(ctx context.Context, conn *websocket.Conn, nextID *int, ev *haEvent) error {
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

// syncUsers queries HA for all active users via the config/auth/list WebSocket
// command, discovers Companion app notify services via the REST API, and
// publishes ha.events.ada.users_synced to NATS.
func (c *Client) syncUsers(ctx context.Context, conn *websocket.Conn, id int) error {
	if err := conn.WriteJSON(map[string]any{
		"id":   id,
		"type": "config/auth/list",
	}); err != nil {
		return fmt.Errorf("ha: write config/auth/list: %w", err)
	}

	// The WebSocket is live — HA may send event messages before the command
	// result arrives. Loop until we get the message with our command ID.
	var result struct {
		ID      int  `json:"id"`
		Success bool `json:"success"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"` // HA returns a flat array, not an object
	}
	for {
		if err := conn.ReadJSON(&result); err != nil {
			return fmt.Errorf("ha: read config/auth/list result: %w", err)
		}
		if result.ID == id {
			break
		}
	}
	if !result.Success {
		msg := "unknown error"
		if result.Error != nil {
			msg = result.Error.Message
		}
		return fmt.Errorf("ha: config/auth/list failed: %s", msg)
	}

	// HA returns result as a flat array of user objects.
	var haUsers []haUserEntry
	if err := json.Unmarshal(result.Result, &haUsers); err != nil {
		return fmt.Errorf("ha: unmarshal config/auth/list users: %w", err)
	}

	availableServices, err := c.fetchMobileAppServices(ctx)
	if err != nil {
		c.log.Warn("ha: fetch notify services", slog.String("error", err.Error()))
		availableServices = nil
	}

	users := make([]schemas.AdaHAUser, 0, len(haUsers))
	for _, u := range haUsers {
		if !u.IsActive || u.SystemGenerated {
			continue
		}
		users = append(users, schemas.AdaHAUser{
			ID:       u.ID,
			Name:     u.Name,
			Username: u.Username,
		})
	}

	return ada.PublishUsersSynced(c.nc, users, availableServices, c.log)
}

// fetchMobileAppServices queries GET /api/services and returns all mobile_app_*
// notify service names. These are passed to the engine for the device picker;
// the engine stores them and subtracts already-linked addresses from the list.
func (c *Client) fetchMobileAppServices(ctx context.Context) ([]string, error) {
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
	defer func() { _ = resp.Body.Close() }()

	var domains []struct {
		Domain   string         `json:"domain"`
		Services map[string]any `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
		return nil, err
	}

	var services []string
	for _, d := range domains {
		if d.Domain == "notify" {
			for svc := range d.Services {
				if strings.HasPrefix(svc, "mobile_app_") {
					services = append(services, svc)
				}
			}
		}
	}
	return services, nil
}

// haWSURL converts an HTTP(S) HA base URL to a WebSocket URL.
func haWSURL(haURL string) string {
	u := strings.TrimRight(haURL, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return u + "/api/websocket"
}
