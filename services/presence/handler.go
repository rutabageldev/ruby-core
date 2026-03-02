package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// handler implements multi-source presence fusion with debounce and WiFi
// corroboration for uncertain states (unknown/unavailable).
type handler struct {
	cfg     *PresenceConfig
	haURL   string
	haToken string
	client  *http.Client
	nc      *nats.Conn
	kv      nats.KeyValue
	log     *slog.Logger

	mu           sync.Mutex
	currentState string
	debounce     *time.Timer
}

func newHandler(
	cfg *PresenceConfig,
	haURL, haToken string,
	nc *nats.Conn,
	kv nats.KeyValue,
	log *slog.Logger,
) *handler {
	return &handler{
		cfg:     cfg,
		haURL:   haURL,
		haToken: haToken,
		client:  &http.Client{Timeout: 10 * time.Second},
		nc:      nc,
		kv:      kv,
		log:     log,
	}
}

// initState loads the persisted state from KV on startup, defaulting to "unknown".
func (h *handler) initState() {
	entry, err := h.kv.Get(h.cfg.PersonID)
	if err != nil {
		h.currentState = "unknown"
		h.log.Info("presence: no persisted state, defaulting to unknown",
			slog.String("person", h.cfg.PersonID),
		)
		return
	}
	h.currentState = string(entry.Value())
	h.log.Info("presence: recovered state from KV",
		slog.String("person", h.cfg.PersonID),
		slog.String("state", h.currentState),
	)
}

// process handles a NATS message from the HA_EVENTS pull consumer.
func (h *handler) process(subject string, data []byte) error {
	var evt schemas.CloudEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		h.log.Warn("presence: unmarshal event",
			slog.String("subject", subject),
			slog.String("error", err.Error()),
		)
		return nil // malformed: ack and skip
	}

	if evt.Data == nil {
		return nil
	}
	stateVal, ok := evt.Data["state"]
	if !ok {
		return nil
	}
	newState, ok := stateVal.(string)
	if !ok || newState == "" {
		return nil
	}

	if h.cfg.isUncertain(newState) {
		h.handleUncertain(newState)
	} else {
		h.handleTrusted(newState)
	}
	return nil
}

// handleTrusted processes a definitive state change (e.g. "home", "away", "work").
func (h *handler) handleTrusted(newState string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.currentState == newState {
		return // no change
	}

	// Cancel any pending debounce — the authoritative state overrides it.
	if h.debounce != nil {
		h.debounce.Stop()
		h.debounce = nil
	}

	oldState := h.currentState
	h.log.Info("presence: state changed",
		slog.String("person", h.cfg.PersonID),
		slog.String("entity", h.cfg.PhoneEntity),
		slog.String("old_state", oldState),
		slog.String("new_state", newState),
	)

	if err := h.persistState(newState); err != nil {
		h.log.Error("presence: KV write failed",
			slog.String("person", h.cfg.PersonID),
			slog.String("entity", h.cfg.PhoneEntity),
			slog.String("state", newState),
			slog.String("error", err.Error()),
		)
		// Continue: in-memory state is still updated and event published.
	}

	h.currentState = newState
	h.publishState(newState)
}

// handleUncertain processes an uncertain state (unknown/unavailable).
// It corroborates via WiFi before starting a debounce timer.
func (h *handler) handleUncertain(uncertainState string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Already away or debounce already running — nothing to do.
	if h.currentState == "away" {
		h.log.Info("presence: ignoring uncertain state",
			slog.String("person", h.cfg.PersonID),
			slog.String("entity", h.cfg.PhoneEntity),
			slog.String("uncertain_state", uncertainState),
			slog.String("reason", "already_away"),
		)
		return
	}
	if h.debounce != nil {
		h.log.Info("presence: ignoring uncertain state",
			slog.String("person", h.cfg.PersonID),
			slog.String("entity", h.cfg.PhoneEntity),
			slog.String("uncertain_state", uncertainState),
			slog.String("reason", "debounce_active"),
		)
		return
	}

	// Corroborate via WiFi (lock released during HTTP call to avoid holding it).
	h.mu.Unlock()
	wifiState, err := h.queryWifiState()
	h.mu.Lock()

	if err != nil {
		h.log.Warn("presence: WiFi corroboration failed, treating as not connected",
			slog.String("person", h.cfg.PersonID),
			slog.String("wifi_entity", h.cfg.WifiEntity),
			slog.String("triggering_state", uncertainState),
			slog.String("error", err.Error()),
		)
		// Treat as not connected — start debounce (safer default).
	} else if h.isTrustedWifi(wifiState) {
		h.log.Info("presence: WiFi connected, ignoring uncertain state",
			slog.String("person", h.cfg.PersonID),
			slog.String("wifi_entity", h.cfg.WifiEntity),
			slog.String("wifi_state", wifiState),
			slog.String("reason", "wifi_connected_ignoring_uncertain"),
		)
		return
	}

	h.log.Info("presence: starting debounce",
		slog.String("person", h.cfg.PersonID),
		slog.String("wifi_entity", h.cfg.WifiEntity),
		slog.String("wifi_state", wifiState),
		slog.Duration("debounce_seconds", h.cfg.DebounceDur),
	)

	h.debounce = time.AfterFunc(h.cfg.DebounceDur, h.commitAway)
}

// commitAway is called by the debounce timer after DebounceDur with no trusted state.
func (h *handler) commitAway() {
	h.mu.Lock()

	// If state changed to a known location during debounce, do nothing.
	if h.currentState != "" && !h.cfg.isUncertain(h.currentState) && h.currentState != "away" {
		h.log.Info("presence: debounce expired but state changed, not committing away",
			slog.String("person", h.cfg.PersonID),
			slog.String("current_state", h.currentState),
		)
		h.debounce = nil
		h.mu.Unlock()
		return
	}

	h.currentState = "away"
	h.debounce = nil
	h.mu.Unlock()

	if err := h.persistState("away"); err != nil {
		h.log.Error("presence: KV write failed on debounce commit",
			slog.String("person", h.cfg.PersonID),
			slog.String("state", "away"),
			slog.String("error", err.Error()),
		)
	}

	h.publishState("away")

	h.log.Info("presence: debounce expired, committed away",
		slog.String("person", h.cfg.PersonID),
		slog.String("reason", "debounce_expired"),
		slog.String("committed_state", "away"),
	)
}

// publishState publishes a CloudEvent to ruby_presence.events.state.{personID}.
func (h *handler) publishState(state string) {
	evt := schemas.CloudEvent{
		SpecVersion: schemas.CloudEventsSpecVersion,
		ID:          newID(),
		Source:      "ruby_presence",
		Type:        "state",
		Time:        time.Now().UTC().Format(time.RFC3339),
		Data: map[string]any{
			"state": state,
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		h.log.Error("presence: marshal CloudEvent",
			slog.String("person", h.cfg.PersonID),
			slog.String("error", err.Error()),
		)
		return
	}

	subject := "ruby_presence.events.state." + h.cfg.PersonID
	if err := h.nc.Publish(subject, data); err != nil {
		h.log.Error("presence: publish state event",
			slog.String("person", h.cfg.PersonID),
			slog.String("subject", subject),
			slog.String("error", err.Error()),
		)
		return
	}

	h.log.Info("presence: published state",
		slog.String("person", h.cfg.PersonID),
		slog.String("subject", subject),
		slog.String("state", state),
	)
}

// persistState writes the current state to the presence KV bucket.
func (h *handler) persistState(state string) error {
	_, err := h.kv.Put(h.cfg.PersonID, []byte(state))
	return err
}

// queryWifiState fetches the current state of the WiFi entity from HA REST API.
func (h *handler) queryWifiState() (string, error) {
	if h.haURL == "" {
		return "", fmt.Errorf("HA URL not configured")
	}
	url := strings.TrimRight(h.haURL, "/") + "/api/states/" + h.cfg.WifiEntity

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.haToken)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HA returned HTTP %d for %s", resp.StatusCode, h.cfg.WifiEntity)
	}

	var result struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.State, nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

// isTrustedWifi reports whether wifiState indicates the device is on a trusted network.
func (h *handler) isTrustedWifi(wifiState string) bool {
	if wifiState == "home" {
		return true
	}
	for _, n := range h.cfg.TrustedNetworks {
		if strings.EqualFold(wifiState, n) {
			return true
		}
	}
	return false
}
