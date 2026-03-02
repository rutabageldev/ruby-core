package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/primaryrutabaga/ruby-core/services/engine/config"
)

// writeYAML writes content to a temp file in dir and returns its path.
func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
}

func TestLoadDir_Valid(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "presence.yaml", `
schemaVersion: "1.0"
rules:
  - name: wife_arrives
    trigger:
      source: ha
      type: person
      id: wife
      attributes:
        - state
    conditions:
      - type: state_transition
        field: state
        value: home
    actions:
      - type: notify
        params:
          title: "Welcome home"
          message: "She's home!"
          device: mobile_app_phone
  - name: wife_leaves
    trigger:
      source: ha
      type: person
      id: wife
      attributes:
        - state
    conditions:
      - type: state_transition
        field: state
        value: not_home
    actions:
      - type: notify
        params:
          title: "Just left"
          message: "She left."
          device: mobile_app_phone
`)

	cfg, err := config.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Critical entity deduplicated: both rules share the same trigger entity.
	if len(cfg.CriticalEntities) != 1 {
		t.Errorf("CriticalEntities: want 1, got %d: %v", len(cfg.CriticalEntities), cfg.CriticalEntities)
	}
	if len(cfg.CriticalEntities) > 0 && cfg.CriticalEntities[0] != "person.wife" {
		t.Errorf("CriticalEntities[0]: want %q, got %q", "person.wife", cfg.CriticalEntities[0])
	}

	// Passlist deduplicated: both rules list "state" for domain "person".
	attrs := cfg.Passlist["person"]
	if len(attrs) != 1 || attrs[0] != "state" {
		t.Errorf("Passlist[person]: want [state], got %v", attrs)
	}
}

func TestLoadDir_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", `
schemaVersion: "1.0"
rules:
  - name: rule_a
    trigger:
      source: ha
      type: device_tracker
      id: tracker_one
      attributes:
        - state
        - battery_level
    actions:
      - type: notify
        params:
          title: "T"
          message: "M"
          device: d
`)
	writeYAML(t, dir, "b.yaml", `
schemaVersion: "1.0"
rules:
  - name: rule_b
    trigger:
      source: ha
      type: device_tracker
      id: tracker_two
      attributes:
        - state
    actions:
      - type: notify
        params:
          title: "T"
          message: "M"
          device: d
`)

	cfg, err := config.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two distinct critical entities.
	if len(cfg.CriticalEntities) != 2 {
		t.Errorf("CriticalEntities: want 2, got %d: %v", len(cfg.CriticalEntities), cfg.CriticalEntities)
	}

	// Passlist for device_tracker: "state" and "battery_level" (no duplicates).
	attrs := cfg.Passlist["device_tracker"]
	if len(attrs) != 2 {
		t.Errorf("Passlist[device_tracker]: want 2 attrs, got %d: %v", len(attrs), attrs)
	}
	if !slices.Contains(attrs, "state") || !slices.Contains(attrs, "battery_level") {
		t.Errorf("Passlist[device_tracker] missing expected attrs: %v", attrs)
	}
}

func TestLoadDir_WrongSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `
schemaVersion: "2.0"
rules:
  - name: r
    trigger:
      source: ha
      type: person
      id: x
    actions:
      - type: notify
        params:
          title: t
          message: m
          device: d
`)

	_, err := config.LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for wrong schemaVersion, got nil")
	}
}

func TestLoadDir_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `this: is: not: valid: yaml: [`)

	_, err := config.LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoadDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	_, err := config.LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

func TestLoadDir_EmptyRulesList(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "empty.yaml", `
schemaVersion: "1.0"
rules: []
`)

	_, err := config.LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for file with no rules, got nil")
	}
}

func TestLoadDir_ExplicitPasslistAndCriticalEntities(t *testing.T) {
	// Top-level passlist and critical_entities are merged in addition to rule triggers.
	dir := t.TempDir()
	writeYAML(t, dir, "katie.yaml", `
schemaVersion: "1.0"
passlist:
  phone:
    - state
critical_entities:
  - phone.katie
rules:
  - name: katie_arrives
    trigger:
      source: ruby_presence
      type: state
      id: katie
      attributes:
        - state
    conditions:
      - type: state_transition
        field: state
        value: home
    actions:
      - type: notify
        params:
          title: "Welcome home"
          message: "Katie just arrived home."
          device: mobile_app_phone_michael
`)

	cfg, err := config.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Explicit critical entity must appear.
	if !slices.Contains(cfg.CriticalEntities, "phone.katie") {
		t.Errorf("CriticalEntities missing phone.katie: %v", cfg.CriticalEntities)
	}

	// Explicit passlist entry must appear.
	if !slices.Contains(cfg.Passlist["phone"], "state") {
		t.Errorf("Passlist[phone] missing state: %v", cfg.Passlist["phone"])
	}
}

func TestLoadDir_ExplicitDeduplication(t *testing.T) {
	// Explicit passlist/critical_entities that duplicate rule-derived entries are not added twice.
	dir := t.TempDir()
	writeYAML(t, dir, "dedup.yaml", `
schemaVersion: "1.0"
passlist:
  person:
    - state
critical_entities:
  - person.wife
rules:
  - name: wife_arrives
    trigger:
      source: ha
      type: person
      id: wife
      attributes:
        - state
    actions:
      - type: notify
        params:
          title: T
          message: M
          device: d
`)

	cfg, err := config.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, e := range cfg.CriticalEntities {
		if e == "person.wife" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("person.wife should appear exactly once in CriticalEntities, got %d: %v", count, cfg.CriticalEntities)
	}

	attrCount := 0
	for _, a := range cfg.Passlist["person"] {
		if a == "state" {
			attrCount++
		}
	}
	if attrCount != 1 {
		t.Errorf("state should appear exactly once in Passlist[person], got %d: %v", attrCount, cfg.Passlist["person"])
	}
}

func TestLoadDir_TriggerWithoutID(t *testing.T) {
	// A trigger with no ID should still compile; it just doesn't produce a critical entity.
	dir := t.TempDir()
	writeYAML(t, dir, "no_id.yaml", `
schemaVersion: "1.0"
rules:
  - name: catch_all
    trigger:
      source: ha
      type: sensor
      attributes:
        - state
    actions:
      - type: notify
        params:
          title: T
          message: M
          device: d
`)

	cfg, err := config.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.CriticalEntities) != 0 {
		t.Errorf("expected 0 critical entities for trigger without ID, got %d", len(cfg.CriticalEntities))
	}
	if !slices.Contains(cfg.Passlist["sensor"], "state") {
		t.Errorf("expected passlist to contain sensor.state, got %v", cfg.Passlist)
	}
}
