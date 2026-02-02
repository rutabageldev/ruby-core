package schemas

const (
	CloudEventsSpecVersion = "1.0"
)

// CloudEvent represents the minimal CloudEvents envelope used internally.
type CloudEvent struct {
	SpecVersion string                 `json:"specversion"`
	ID          string                 `json:"id"`
	Source      string                 `json:"source"`
	Type        string                 `json:"type"`
	Time        string                 `json:"time"`
	DataSchema  string                 `json:"dataschema,omitempty"`
	Subject     string                 `json:"subject,omitempty"`
	Data        map[string]any         `json:"data,omitempty"`
	Extensions  map[string]string      `json:"-"` // Handled via map[string]any on Data for now
}

// Standard extension names used across Ruby Core.
const (
	ExtensionCorrelationID = "correlationid"
	ExtensionCausationID   = "causationid"
)
