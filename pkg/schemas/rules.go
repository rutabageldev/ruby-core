package schemas

const (
	RulesSchemaVersionV1 = "1.0"

	// ActionTypeNotify sends a push notification via the notifier service.
	// Required params: "title", "message", "device" (HA mobile_app device name).
	ActionTypeNotify = "notify"

	// ConditionTypeStateTransition matches when an entity transitions to a specific state value.
	// Required fields: "field" (attribute name, typically "state"), "value" (target state string).
	ConditionTypeStateTransition = "state_transition"
)

// RuleFile represents the top-level YAML file.
type RuleFile struct {
	SchemaVersion    string              `yaml:"schemaVersion"`
	Passlist         map[string][]string `yaml:"passlist,omitempty"`
	CriticalEntities []string            `yaml:"critical_entities,omitempty"`
	Rules            []Rule              `yaml:"rules"`
}

type Rule struct {
	Name       string      `yaml:"name"`
	Trigger    Trigger     `yaml:"trigger"`
	Conditions []Condition `yaml:"conditions,omitempty"`
	Actions    []Action    `yaml:"actions"`
}

// Trigger defines which NATS subject pattern activates this rule.
// Subject is derived as: {source}.events.{type}[.{id}]
//
// Attributes lists the HA entity attribute names the engine needs from this trigger.
// The engine's config loader aggregates these into a passlist published to NATS KV so
// the gateway can perform lean projection (ADR-0009): only listed attributes are
// forwarded in the CloudEvent payload; all others are dropped.
type Trigger struct {
	Source     string   `yaml:"source"`
	Type       string   `yaml:"type"`
	ID         string   `yaml:"id,omitempty"`
	Attributes []string `yaml:"attributes,omitempty"`
}

type Condition struct {
	Type  string `yaml:"type"`
	Field string `yaml:"field,omitempty"`
	Value string `yaml:"value,omitempty"`
}

type Action struct {
	Type   string            `yaml:"type"`
	Params map[string]string `yaml:"params,omitempty"`
}
