// Package presence_notify implements a Logical Processor (ADR-0007) that detects
// when a tracked person arrives home or leaves, and publishes a notification
// command for the notifier service to dispatch as a push notification.
//
// State ownership: this processor is the sole writer of the "presence" NATS KV
// bucket (ADR-0002). Each key is the HA entity ID (e.g. "person.wife"); the value
// is a JSON-encoded presenceState struct.
//
// NATS subjects:
//
//	Subscribes: ha.events.device_tracker.> , ha.events.person.>
//	Publishes:  ruby_engine.commands.notify.{eventID}
package presence_notify

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/config"
	"github.com/primaryrutabaga/ruby-core/services/engine/processor"
)

// kvStore is a narrow interface over nats.KeyValue, used for dependency
// injection in tests without requiring a live NATS server.
type kvStore interface {
	Get(key string) ([]byte, error)
	Put(key string, val []byte) error
}

// natsPub is a narrow interface over *nats.Conn for publishing, allowing
// test injection without a live NATS connection.
type natsPub interface {
	Publish(subject string, data []byte) error
}

// kvAdapter adapts nats.KeyValue to the kvStore interface.
type kvAdapter struct{ kv nats.KeyValue }

func (a *kvAdapter) Get(key string) ([]byte, error) {
	entry, err := a.kv.Get(key)
	if err != nil {
		return nil, err
	}
	return entry.Value(), nil
}

func (a *kvAdapter) Put(key string, val []byte) error {
	_, err := a.kv.Put(key, val)
	return err
}

// presenceState is persisted to the "presence" KV bucket for each watched entity.
type presenceState struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ruleTarget captures the resolved notification params for one watched entity.
type ruleTarget struct {
	entityID string
	// notifications maps target state → notification params.
	// e.g. "home" → arrival params, "not_home" → departure params.
	notifications map[string]notifyParams
}

type notifyParams struct {
	title   string
	message string
	device  string
}

// Processor implements processor.Processor for presence-based notifications.
type Processor struct {
	targets []ruleTarget
	kv      kvStore
	nc      natsPub
	log     *slog.Logger
}

// compile-time interface check
var _ processor.Processor = (*Processor)(nil)

// New returns a new Processor. Register it with the ProcessorHost before Initialize.
func New(log *slog.Logger) *Processor {
	if log == nil {
		log = slog.Default()
	}
	return &Processor{log: log}
}

// Initialize resolves rule targets from the compiled config and binds the
// presence KV bucket. Returns an error if NATS/JetStream access fails.
func (p *Processor) Initialize(cfg processor.Config) error {
	p.nc = cfg.NC
	p.targets = buildTargets(cfg.RuleCfg)

	kv, err := natsx.EnsurePresenceKV(cfg.JS)
	if err != nil {
		return fmt.Errorf("presence_notify: ensure KV: %w", err)
	}
	p.kv = &kvAdapter{kv}

	p.log.Info("presence_notify: initialized",
		slog.Int("targets", len(p.targets)),
	)
	return nil
}

// InitializeForTest is a test seam that injects stub dependencies directly.
// Only call from tests; do not use in production code.
func (p *Processor) InitializeForTest(cfg processor.Config, kv kvStore, nc natsPub) error {
	p.kv = kv
	p.nc = nc
	p.targets = buildTargets(cfg.RuleCfg)
	return nil
}

// Subscriptions returns the NATS subjects this processor handles.
func (p *Processor) Subscriptions() []string {
	return []string{
		"ha.events.device_tracker.>",
		"ha.events.person.>",
		"ruby_presence.events.state.>",
	}
}

// ProcessEvent receives a CloudEvent from the HA gateway, determines whether
// a presence transition occurred for a watched entity, and publishes a
// notification command if so.
func (p *Processor) ProcessEvent(subject string, data []byte) error {
	var evt schemas.CloudEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		p.log.Warn("presence_notify: unmarshal event",
			slog.String("subject", subject),
			slog.String("error", err.Error()),
		)
		return nil // malformed payload: ack and move on, do not NAK
	}

	entityID := entityIDFromSubject(subject)
	if entityID == "" {
		return nil
	}

	newState, ok := stateFromEvent(evt)
	if !ok {
		return nil // event has no usable state field
	}

	target := p.targetFor(entityID)
	if target == nil {
		return nil // entity not watched by any rule
	}

	prev, err := p.loadState(entityID)
	if err != nil {
		return fmt.Errorf("presence_notify: load state %q: %w", entityID, err)
	}

	if prev != nil && prev.State == newState {
		return nil // no transition
	}

	// Persist new state before publishing to avoid re-notifying on restart.
	if err := p.saveState(entityID, newState); err != nil {
		return fmt.Errorf("presence_notify: save state %q: %w", entityID, err)
	}

	params, hasNotif := target.notifications[newState]
	if !hasNotif {
		return nil // transition to an unmonitored state (e.g. "unavailable")
	}

	return p.publishNotify(evt, params)
}

// Shutdown is a no-op; the KV bucket and NATS connection are owned by the engine.
func (p *Processor) Shutdown() {}

// --- internal helpers ---

func (p *Processor) targetFor(entityID string) *ruleTarget {
	for i := range p.targets {
		if p.targets[i].entityID == entityID {
			return &p.targets[i]
		}
	}
	return nil
}

func (p *Processor) loadState(entityID string) (*presenceState, error) {
	data, err := p.kv.Get(entityID)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var st presenceState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (p *Processor) saveState(entityID, state string) error {
	st := presenceState{State: state, UpdatedAt: time.Now().UTC()}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return p.kv.Put(entityID, data)
}

func (p *Processor) publishNotify(cause schemas.CloudEvent, params notifyParams) error {
	corrID := cause.CorrelationID
	if corrID == "" {
		corrID = cause.ID
	}
	evtID := newID()

	cmd := schemas.CloudEvent{
		SpecVersion:   schemas.CloudEventsSpecVersion,
		ID:            evtID,
		Source:        "ruby_engine",
		Type:          "command.notify",
		Time:          time.Now().UTC().Format(time.RFC3339),
		CorrelationID: corrID,
		CausationID:   cause.ID,
		Data: map[string]any{
			"title":   params.title,
			"message": params.message,
			"device":  params.device,
		},
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("presence_notify: marshal notify command: %w", err)
	}

	subj := "ruby_engine.commands.notify." + evtID
	if err := p.nc.Publish(subj, data); err != nil {
		return fmt.Errorf("presence_notify: publish notify command: %w", err)
	}

	p.log.Info("presence_notify: notification dispatched",
		slog.String("subject", subj),
		slog.String("title", params.title),
		slog.String("correlationid", corrID),
	)
	return nil
}

// buildTargets derives ruleTargets from the raw rules in cfg.
// Only rules with a "notify" action and a "state_transition" condition are included.
// Supported trigger sources: "ha" (person/device_tracker), "ruby_presence" (state).
func buildTargets(cfg *config.CompiledConfig) []ruleTarget {
	if cfg == nil {
		return nil
	}

	index := make(map[string]int)
	var targets []ruleTarget

	for _, rule := range cfg.Rules {
		t := rule.Trigger
		if t.ID == "" {
			continue
		}

		switch t.Source {
		case "ha":
			if t.Type != "person" && t.Type != "device_tracker" {
				continue
			}
		case "ruby_presence":
			// any type is valid (e.g. "state")
		default:
			continue
		}

		entityID := t.Type + "." + t.ID

		var np *notifyParams
		for _, action := range rule.Actions {
			if action.Type != schemas.ActionTypeNotify {
				continue
			}
			np = &notifyParams{
				title:   action.Params["title"],
				message: action.Params["message"],
				device:  action.Params["device"],
			}
			break
		}
		if np == nil {
			continue
		}

		targetState := ""
		for _, cond := range rule.Conditions {
			if cond.Type == schemas.ConditionTypeStateTransition && cond.Field == "state" {
				targetState = cond.Value
				break
			}
		}
		if targetState == "" {
			continue
		}

		if idx, ok := index[entityID]; ok {
			targets[idx].notifications[targetState] = *np
		} else {
			index[entityID] = len(targets)
			targets = append(targets, ruleTarget{
				entityID:      entityID,
				notifications: map[string]notifyParams{targetState: *np},
			})
		}
	}

	return targets
}

// isNotFound reports whether a KV error indicates a missing key.
func isNotFound(err error) bool {
	return err == nats.ErrKeyNotFound || strings.Contains(err.Error(), "not found")
}

// entityIDFromSubject derives the entity ID from a NATS subject.
// Strips the leading source+".events." prefix, returning "{type}.{id}".
// e.g. "ha.events.person.wife"           → "person.wife"
//
//	"ruby_presence.events.state.katie" → "state.katie"
func entityIDFromSubject(subject string) string {
	_, after, found := strings.Cut(subject, ".events.")
	if !found {
		return ""
	}
	return after
}

// stateFromEvent extracts the "state" field from a CloudEvent's data payload.
func stateFromEvent(evt schemas.CloudEvent) (string, bool) {
	if evt.Data == nil {
		return "", false
	}
	v, ok := evt.Data["state"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok && s != ""
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
