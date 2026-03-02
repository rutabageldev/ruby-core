package ha

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	goNats "github.com/nats-io/nats.go"

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

// haEvent wraps a HA state_changed event payload.
type haEvent struct {
	EventType string      `json:"event_type"`
	Data      haEventData `json:"data"`
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

// Client connects to the Home Assistant WebSocket API, subscribes to
// state_changed events, normalises them via the Normalizer, and publishes
// CloudEvents to NATS. On each successful reconnect it triggers the Reconciler
// (ADR-0008 targeted reconciliation).
type Client struct {
	haURL        string
	haToken      string
	norm         *Normalizer
	publisher    *gatewayNats.Publisher
	stateKV      goNats.KeyValue
	critEntities []string
	reconciler   *Reconciler
	log          *slog.Logger
}

// NewClient creates a Client.
func NewClient(
	haURL, haToken string,
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
		norm:         norm,
		publisher:    publisher,
		stateKV:      stateKV,
		critEntities: critEntities,
		reconciler:   reconciler,
		log:          log,
	}
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

	// ── subscribe to state_changed ─────────────────────────────────────────
	const subID = 1
	if err := conn.WriteJSON(haWSMessage{
		ID:        subID,
		Type:      "subscribe_events",
		EventType: "state_changed",
	}); err != nil {
		return fmt.Errorf("write subscribe_events: %w", err)
	}

	var subResp haWSMessage
	if err := conn.ReadJSON(&subResp); err != nil {
		return fmt.Errorf("read subscribe result: %w", err)
	}
	if !subResp.Success {
		return fmt.Errorf("ha websocket: subscribe_events request rejected")
	}
	c.log.Info("ha websocket: subscribed to state_changed")

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
		if err := c.handleEvent(msg.Event); err != nil {
			c.log.Warn("ha websocket: handle event error",
				slog.String("error", err.Error()),
			)
		}
	}
}

// handleEvent processes a single state_changed event from HA.
func (c *Client) handleEvent(ev *haEvent) error {
	if ev.Data.NewState == nil {
		return nil // entity removed; nothing to publish
	}
	ns := ev.Data.NewState
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

// haWSURL converts an HTTP(S) HA base URL to a WebSocket URL.
func haWSURL(haURL string) string {
	u := strings.TrimRight(haURL, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return u + "/api/websocket"
}
