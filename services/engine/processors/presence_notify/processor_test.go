//go:build fast

package presence_notify_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/config"
	"github.com/primaryrutabaga/ruby-core/services/engine/processor"
	pn "github.com/primaryrutabaga/ruby-core/services/engine/processors/presence_notify"
)

// ---------------------------------------------------------------------------
// Stub KV store for testing without a live NATS server
// ---------------------------------------------------------------------------

type stubKV struct {
	data map[string][]byte
}

func newStubKV() *stubKV { return &stubKV{data: make(map[string][]byte)} }

func (s *stubKV) Get(key string) ([]byte, error) {
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func (s *stubKV) Put(key string, val []byte) error {
	s.data[key] = val
	return nil
}

// ---------------------------------------------------------------------------
// Stub NATS publisher
// ---------------------------------------------------------------------------

type published struct {
	subject string
	data    []byte
}

type stubNC struct {
	msgs []published
}

func (s *stubNC) Publish(subj string, data []byte) error {
	s.msgs = append(s.msgs, published{subj, data})
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stateEvent builds a minimal CloudEvent representing an HA state_changed event.
func stateEvent(entityID, state string) []byte {
	evt := schemas.CloudEvent{
		SpecVersion:   "1.0",
		ID:            "test-" + entityID + "-" + state,
		Source:        "ha",
		Type:          "state_changed",
		Time:          time.Now().UTC().Format(time.RFC3339),
		CorrelationID: "corr-123",
		CausationID:   "corr-123",
		Data:          map[string]any{"state": state},
	}
	b, _ := json.Marshal(evt)
	return b
}

// minimalConfig returns a CompiledConfig with one rule for "person.wife".
func minimalConfig() *config.CompiledConfig {
	return &config.CompiledConfig{
		Rules: []schemas.Rule{
			{
				Name: "wife_arrives",
				Trigger: schemas.Trigger{
					Source:     "ha",
					Type:       "person",
					ID:         "wife",
					Attributes: []string{"state"},
				},
				Conditions: []schemas.Condition{
					{Type: schemas.ConditionTypeStateTransition, Field: "state", Value: "home"},
				},
				Actions: []schemas.Action{
					{Type: schemas.ActionTypeNotify, Params: map[string]string{
						"title":   "Welcome home",
						"message": "Wife arrived.",
						"device":  "mobile_app_phone",
					}},
				},
			},
			{
				Name: "wife_leaves",
				Trigger: schemas.Trigger{
					Source:     "ha",
					Type:       "person",
					ID:         "wife",
					Attributes: []string{"state"},
				},
				Conditions: []schemas.Condition{
					{Type: schemas.ConditionTypeStateTransition, Field: "state", Value: "not_home"},
				},
				Actions: []schemas.Action{
					{Type: schemas.ActionTypeNotify, Params: map[string]string{
						"title":   "Just left",
						"message": "Wife departed.",
						"device":  "mobile_app_phone",
					}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// newTestProcessor creates a Processor with injected stub dependencies.
// We use the exported constructor + InitializeWithDeps test seam.
func newTestProcessor(t *testing.T, kv *stubKV, nc *stubNC, cfg *config.CompiledConfig) *pn.Processor {
	t.Helper()
	p := pn.New(nil)
	if err := p.InitializeForTest(processor.Config{RuleCfg: cfg}, kv, nc); err != nil {
		t.Fatalf("processor.InitializeForTest: %v", err)
	}
	return p
}

func TestArrival_PublishesNotification(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	p := newTestProcessor(t, kv, nc, minimalConfig())

	if err := p.ProcessEvent("ha.events.person.wife", stateEvent("person.wife", "home")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nc.msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(nc.msgs))
	}
	var cmd schemas.CloudEvent
	if err := json.Unmarshal(nc.msgs[0].data, &cmd); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if cmd.Data["title"] != "Welcome home" {
		t.Errorf("title = %q, want %q", cmd.Data["title"], "Welcome home")
	}
	if !strings.HasPrefix(nc.msgs[0].subject, "ruby_engine.commands.notify.") {
		t.Errorf("subject %q does not start with ruby_engine.commands.notify.", nc.msgs[0].subject)
	}
}

func TestDeparture_PublishesNotification(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	// Pre-seed state as "home" so a "not_home" transition is detected.
	state, _ := json.Marshal(struct {
		State     string    `json:"state"`
		UpdatedAt time.Time `json:"updated_at"`
	}{"home", time.Now().UTC()})
	kv.data["person.wife"] = state

	p := newTestProcessor(t, kv, nc, minimalConfig())

	if err := p.ProcessEvent("ha.events.person.wife", stateEvent("person.wife", "not_home")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nc.msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(nc.msgs))
	}
	var cmd schemas.CloudEvent
	if err := json.Unmarshal(nc.msgs[0].data, &cmd); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if cmd.Data["title"] != "Just left" {
		t.Errorf("title = %q, want %q", cmd.Data["title"], "Just left")
	}
}

func TestSameState_NoNotification(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	// Pre-seed state as "home".
	state, _ := json.Marshal(struct {
		State     string    `json:"state"`
		UpdatedAt time.Time `json:"updated_at"`
	}{"home", time.Now().UTC()})
	kv.data["person.wife"] = state

	p := newTestProcessor(t, kv, nc, minimalConfig())

	// Receive another "home" event — no transition, no notification.
	if err := p.ProcessEvent("ha.events.person.wife", stateEvent("person.wife", "home")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nc.msgs) != 0 {
		t.Errorf("expected 0 notifications for same-state event, got %d", len(nc.msgs))
	}
}

func TestUnwatchedEntity_NoNotification(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	p := newTestProcessor(t, kv, nc, minimalConfig())

	// "person.stranger" is not in any rule.
	if err := p.ProcessEvent("ha.events.person.stranger", stateEvent("person.stranger", "home")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nc.msgs) != 0 {
		t.Errorf("expected 0 notifications for unwatched entity, got %d", len(nc.msgs))
	}
}

func TestMalformedEvent_AckedNotErrored(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	p := newTestProcessor(t, kv, nc, minimalConfig())

	// A non-JSON payload should not return an error (ack and move on).
	if err := p.ProcessEvent("ha.events.person.wife", []byte("not-json")); err != nil {
		t.Errorf("malformed payload should not return error, got: %v", err)
	}
}

func TestNoStateField_NoNotification(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	p := newTestProcessor(t, kv, nc, minimalConfig())

	// Event without a state field in data.
	evt := schemas.CloudEvent{
		SpecVersion:   "1.0",
		ID:            "no-state",
		Source:        "ha",
		Type:          "state_changed",
		Time:          time.Now().UTC().Format(time.RFC3339),
		CorrelationID: "corr-x",
		CausationID:   "corr-x",
		Data:          map[string]any{"brightness": 100},
	}
	data, _ := json.Marshal(evt)

	if err := p.ProcessEvent("ha.events.person.wife", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nc.msgs) != 0 {
		t.Errorf("expected 0 notifications for event without state, got %d", len(nc.msgs))
	}
}

func TestCorrelationID_PropagatedToCommand(t *testing.T) {
	kv := newStubKV()
	nc := &stubNC{}
	p := newTestProcessor(t, kv, nc, minimalConfig())

	evt := schemas.CloudEvent{
		SpecVersion:   "1.0",
		ID:            "cause-id",
		Source:        "ha",
		Type:          "state_changed",
		Time:          time.Now().UTC().Format(time.RFC3339),
		CorrelationID: "original-corr-999",
		CausationID:   "original-corr-999",
		Data:          map[string]any{"state": "home"},
	}
	data, _ := json.Marshal(evt)

	if err := p.ProcessEvent("ha.events.person.wife", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nc.msgs) == 0 {
		t.Fatal("expected a notification, got none")
	}

	var cmd schemas.CloudEvent
	if err := json.Unmarshal(nc.msgs[0].data, &cmd); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if cmd.CorrelationID != "original-corr-999" {
		t.Errorf("correlationID = %q, want %q", cmd.CorrelationID, "original-corr-999")
	}
	if cmd.CausationID != "cause-id" {
		t.Errorf("causationID = %q, want %q", cmd.CausationID, "cause-id")
	}
}
