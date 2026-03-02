package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"

	gatewayNats "github.com/primaryrutabaga/ruby-core/services/gateway/nats"
)

// haStateResponse is the HA REST API /api/states/{entity_id} response shape.
type haStateResponse struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged string         `json:"last_changed"` // RFC3339 UTC
}

// Reconciler fetches fresh state from HA for each critical entity and publishes
// an update if the HA state is newer than the last CloudEvent stored in the
// gateway_state KV bucket (ADR-0008).
type Reconciler struct {
	haURL     string
	haToken   string
	stateKV   nats.KeyValue
	norm      *Normalizer
	publisher *gatewayNats.Publisher
	log       *slog.Logger
	client    *http.Client
}

// NewReconciler creates a Reconciler.
func NewReconciler(
	haURL, haToken string,
	stateKV nats.KeyValue,
	norm *Normalizer,
	publisher *gatewayNats.Publisher,
	log *slog.Logger,
) *Reconciler {
	return &Reconciler{
		haURL:     haURL,
		haToken:   haToken,
		stateKV:   stateKV,
		norm:      norm,
		publisher: publisher,
		log:       log,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Run performs targeted reconciliation for each entity in criticalEntities.
// It is called once after the HA WebSocket client successfully reconnects.
func (r *Reconciler) Run(ctx context.Context, criticalEntities []string) {
	if len(criticalEntities) == 0 {
		r.log.Info("reconciler: no critical entities configured, skipping")
		return
	}
	r.log.Info("reconciler: starting", slog.Int("entities", len(criticalEntities)))

	for _, entityID := range criticalEntities {
		if ctx.Err() != nil {
			return
		}
		if err := r.reconcileOne(entityID); err != nil {
			r.log.Warn("reconciler: entity reconcile failed",
				slog.String("entity_id", entityID),
				slog.String("error", err.Error()),
			)
		}
	}
	r.log.Info("reconciler: complete")
}

// reconcileOne reconciles a single entity.
func (r *Reconciler) reconcileOne(entityID string) error {
	haState, err := r.fetchHAState(entityID)
	if err != nil {
		return fmt.Errorf("fetch HA state: %w", err)
	}

	haTime, err := ParseUTC(haState.LastChanged)
	if err != nil {
		return fmt.Errorf("parse HA last_changed: %w", err)
	}

	kvTime, err := r.loadLastSeen(entityID)
	if err != nil {
		return fmt.Errorf("load KV timestamp: %w", err)
	}

	if kvTime != nil && !haTime.After(*kvTime) {
		r.log.Debug("reconciler: entity up to date, skipping",
			slog.String("entity_id", entityID),
		)
		return nil
	}

	// HA is newer (or we have no record): publish an update.
	r.log.Info("reconciler: publishing update for entity",
		slog.String("entity_id", entityID),
		slog.String("ha_last_changed", haState.LastChanged),
	)

	domain, _, err := SplitEntityID(entityID)
	if err != nil {
		return err
	}
	filtered := r.norm.Apply(domain, haState.Attributes)
	return r.publisher.PublishHAEvent(entityID, haState.State, filtered, haState.LastChanged)
}

// fetchHAState calls the HA REST API to retrieve the current state of an entity.
func (r *Reconciler) fetchHAState(entityID string) (*haStateResponse, error) {
	url := r.haURL + "/api/states/" + entityID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.haToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HA returned HTTP %d for entity %q", resp.StatusCode, entityID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var state haStateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// loadLastSeen retrieves the timestamp of the last CloudEvent published for
// an entity from the gateway_state KV bucket. Returns nil if no entry exists.
func (r *Reconciler) loadLastSeen(entityID string) (*time.Time, error) {
	entry, err := r.stateKV.Get(entityID)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, nil
		}
		return nil, err
	}
	t, err := ParseUTC(string(entry.Value()))
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ParseUTC parses a time string as RFC3339, normalising to UTC.
// Both timestamps must go through this function before comparison to prevent
// timezone ambiguity (ADR-0008 requirement).
func ParseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Also try RFC3339Nano for sub-second precision from HA.
		t, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
		}
	}
	return t.UTC(), nil
}

// SplitEntityID splits a Home Assistant entity ID into (domain, name).
// e.g. "person.wife" → ("person", "wife")
// e.g. "device_tracker.wife_phone" → ("device_tracker", "wife_phone")
func SplitEntityID(entityID string) (domain, name string, err error) {
	for i, c := range entityID {
		if c == '.' {
			return entityID[:i], entityID[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("ha: invalid entity ID %q: missing domain separator", entityID)
}
