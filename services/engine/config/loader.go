// Package config loads and validates YAML automation rule files for the engine
// service (ADR-0006). The loader reads *.yaml files from RULES_DIR, validates
// the schema version, and compiles them into a CompiledConfig that the engine
// and gateway consume via NATS KV.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

const defaultRulesDir = "configs/rules"

// CompiledConfig is derived from all loaded rule files.
//
// The Passlist and CriticalEntities fields are published to NATS KV for the
// gateway (ADR-0008/0009). The Rules field is kept in memory only (json:"-")
// and is used by processors that need the raw action params (e.g. title,
// message, device) which are not needed by the gateway.
type CompiledConfig struct {
	// Passlist maps HA entity domain (e.g. "person", "device_tracker") to the
	// set of attribute names that the gateway must forward in lean projection.
	// Published to NATS KV key config.engine.passlist as JSON.
	Passlist map[string][]string `json:"passlist"`

	// CriticalEntities is the list of HA entity IDs (e.g. "person.wife") that
	// the gateway reconciles on reconnect.
	// Published to NATS KV key config.engine.critical_entities as JSON.
	CriticalEntities []string `json:"critical_entities"`

	// Rules holds the raw rule definitions for use by processors that need
	// action params (e.g. title, message, device). Not published to NATS KV.
	Rules []schemas.Rule `json:"-"`
}

// Load reads all *.yaml files from RULES_DIR (default: "configs/rules"),
// validates each file's schema version, and returns a CompiledConfig derived
// from all rules.
//
// Returns an error if RULES_DIR is empty, any file fails to parse, or any
// file has an unrecognised schemaVersion. Callers must treat errors as fatal.
func Load() (*CompiledConfig, error) {
	dir := os.Getenv("RULES_DIR")
	if dir == "" {
		dir = defaultRulesDir
	}
	return LoadDir(dir)
}

// LoadDir is the testable core of Load; it accepts an explicit directory path.
func LoadDir(dir string) (*CompiledConfig, error) {
	pattern := filepath.Join(dir, "*.yaml")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("config: glob %q: %w", pattern, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("config: no rule files found in %q (RULES_DIR=%q)", dir, dir)
	}

	cfg := &CompiledConfig{
		Passlist: make(map[string][]string),
	}
	entitySeen := make(map[string]struct{})
	attrSeen := make(map[string]map[string]struct{}) // domain → attribute set

	for _, path := range paths {
		rf, err := parseFile(path)
		if err != nil {
			return nil, err
		}
		cfg.Rules = append(cfg.Rules, rf.Rules...)
		compileRules(rf.Rules, cfg, entitySeen, attrSeen)
		mergeExplicit(rf, cfg, entitySeen, attrSeen)
	}

	return cfg, nil
}

// parseFile reads and validates a single rule file.
func parseFile(path string) (*schemas.RuleFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from filepath.Glob on a trusted directory
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var rf schemas.RuleFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}

	if rf.SchemaVersion != schemas.RulesSchemaVersionV1 {
		return nil, fmt.Errorf(
			"config: %q: unsupported schemaVersion %q (want %q)",
			path, rf.SchemaVersion, schemas.RulesSchemaVersionV1,
		)
	}
	if len(rf.Rules) == 0 {
		return nil, fmt.Errorf("config: %q: no rules defined", path)
	}

	return &rf, nil
}

// mergeExplicit adds top-level passlist and critical_entities from a RuleFile into
// the compiled config, deduplicating against already-seen entries from rule triggers.
func mergeExplicit(
	rf *schemas.RuleFile,
	cfg *CompiledConfig,
	entitySeen map[string]struct{},
	attrSeen map[string]map[string]struct{},
) {
	for _, entityID := range rf.CriticalEntities {
		if entityID == "" {
			continue
		}
		if _, seen := entitySeen[entityID]; !seen {
			entitySeen[entityID] = struct{}{}
			cfg.CriticalEntities = append(cfg.CriticalEntities, entityID)
		}
	}

	for domain, attrs := range rf.Passlist {
		if domain == "" {
			continue
		}
		if attrSeen[domain] == nil {
			attrSeen[domain] = make(map[string]struct{})
		}
		for _, attr := range attrs {
			if attr == "" {
				continue
			}
			if _, seen := attrSeen[domain][attr]; !seen {
				attrSeen[domain][attr] = struct{}{}
				cfg.Passlist[domain] = append(cfg.Passlist[domain], attr)
			}
		}
	}
}

// compileRules extracts the passlist and critical entity list from a set of rules.
func compileRules(
	rules []schemas.Rule,
	cfg *CompiledConfig,
	entitySeen map[string]struct{},
	attrSeen map[string]map[string]struct{},
) {
	for _, rule := range rules {
		t := rule.Trigger

		// Only HA source triggers contribute to the passlist and critical entity list.
		// Triggers from other sources (e.g. ruby_presence) reference internal subjects,
		// not HA entity IDs, so they must not be included in the gateway config.
		if t.Source != "ha" {
			continue
		}

		// Critical entities: reconstruct the HA entity ID as "{type}.{id}".
		// Only triggers with both Type and ID contribute a critical entity.
		if t.Type != "" && t.ID != "" {
			entityID := t.Type + "." + t.ID
			if _, seen := entitySeen[entityID]; !seen {
				entitySeen[entityID] = struct{}{}
				cfg.CriticalEntities = append(cfg.CriticalEntities, entityID)
			}
		}

		// Passlist: aggregate attributes per entity domain (trigger Type).
		if t.Type != "" && len(t.Attributes) > 0 {
			if attrSeen[t.Type] == nil {
				attrSeen[t.Type] = make(map[string]struct{})
			}
			for _, attr := range t.Attributes {
				if attr == "" {
					continue
				}
				if _, seen := attrSeen[t.Type][attr]; !seen {
					attrSeen[t.Type][attr] = struct{}{}
					cfg.Passlist[t.Type] = append(cfg.Passlist[t.Type], attr)
				}
			}
		}
	}
}
