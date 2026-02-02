package schemas

const (
	RulesSchemaVersionV1 = "1.0"
)

// RuleFile represents the top-level YAML file.
type RuleFile struct {
	SchemaVersion string `yaml:"schemaVersion"`
	Rules         []Rule `yaml:"rules"`
}

type Rule struct {
	Name       string      `yaml:"name"`
	Trigger    Trigger     `yaml:"trigger"`
	Conditions []Condition `yaml:"conditions,omitempty"`
	Actions    []Action    `yaml:"actions"`
}

type Trigger struct {
	Source string `yaml:"source"`
	Type   string `yaml:"type"`
	ID     string `yaml:"id,omitempty"`
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
