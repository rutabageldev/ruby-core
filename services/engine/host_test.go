//go:build fast

package main

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/primaryrutabaga/ruby-core/services/engine/config"
	"github.com/primaryrutabaga/ruby-core/services/engine/processor"
)

// mockProcessor is a test double for processor.Processor.
type mockProcessor struct {
	subs       []string
	events     []string // subjects received by ProcessEvent
	initErr    error
	processErr error
}

func (m *mockProcessor) Initialize(_ processor.Config) error { return m.initErr }
func (m *mockProcessor) Subscriptions() []string             { return m.subs }
func (m *mockProcessor) ProcessEvent(subject string, _ []byte) error {
	m.events = append(m.events, subject)
	return m.processErr
}
func (m *mockProcessor) Shutdown() {}

// Compile-time check: mockProcessor satisfies processor.Processor.
var _ processor.Processor = (*mockProcessor)(nil)

func newHost(t *testing.T, procs ...*mockProcessor) *ProcessorHost {
	t.Helper()
	h := NewProcessorHost(slog.Default())
	for _, p := range procs {
		h.Register(p)
	}
	// nil NC/JS is fine for tests: mock processors don't use them.
	if err := h.Initialize(&config.CompiledConfig{}, nil, nil); err != nil {
		t.Fatalf("host.Initialize: %v", err)
	}
	return h
}

func TestHost_RoutesExactMatch(t *testing.T) {
	p := &mockProcessor{subs: []string{"ha.events.person.wife"}}
	h := newHost(t, p)

	if err := h.Process("ha.events.person.wife", []byte("{}")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.events) != 1 || p.events[0] != "ha.events.person.wife" {
		t.Errorf("events = %v, want [ha.events.person.wife]", p.events)
	}
}

func TestHost_RoutesWildcard(t *testing.T) {
	p := &mockProcessor{subs: []string{"ha.events.device_tracker.>"}}
	h := newHost(t, p)

	if err := h.Process("ha.events.device_tracker.tracker_one", []byte("{}")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.events) != 1 {
		t.Errorf("events = %v, want exactly one", p.events)
	}
}

func TestHost_NoMatchIsNotAnError(t *testing.T) {
	p := &mockProcessor{subs: []string{"ha.events.light.>"}}
	h := newHost(t, p)

	if err := h.Process("ha.events.person.wife", []byte("{}")); err != nil {
		t.Fatalf("no-match should not return error, got: %v", err)
	}
	if len(p.events) != 0 {
		t.Errorf("processor should not have been called, got events = %v", p.events)
	}
}

func TestHost_ProcessorNotCalledTwicePerEvent(t *testing.T) {
	// A processor with two matching subscriptions should still only be called once.
	p := &mockProcessor{subs: []string{"ha.events.person.>", "ha.events.person.wife"}}
	h := newHost(t, p)

	if err := h.Process("ha.events.person.wife", []byte("{}")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.events) != 1 {
		t.Errorf("processor called %d times, want exactly 1", len(p.events))
	}
}

func TestHost_MultipleProcessors(t *testing.T) {
	p1 := &mockProcessor{subs: []string{"ha.events.person.>"}}
	p2 := &mockProcessor{subs: []string{"ha.events.device_tracker.>"}}
	h := newHost(t, p1, p2)

	if err := h.Process("ha.events.person.wife", []byte("{}")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p1.events) != 1 {
		t.Errorf("p1: want 1 event, got %d", len(p1.events))
	}
	if len(p2.events) != 0 {
		t.Errorf("p2: want 0 events, got %d", len(p2.events))
	}
}

func TestHost_ReturnsFirstProcessorError(t *testing.T) {
	wantErr := errors.New("transient failure")
	p := &mockProcessor{
		subs:       []string{"ha.events.>"},
		processErr: wantErr,
	}
	h := newHost(t, p)

	err := h.Process("ha.events.person.wife", []byte("{}"))
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestHost_InitializeError(t *testing.T) {
	p := &mockProcessor{
		subs:    []string{"ha.events.>"},
		initErr: errors.New("init failed"),
	}
	h := NewProcessorHost(slog.Default())
	h.Register(p)
	if err := h.Initialize(&config.CompiledConfig{}, nil, nil); err == nil {
		t.Fatal("expected error from processor init, got nil")
	}
}
