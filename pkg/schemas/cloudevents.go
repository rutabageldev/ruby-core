package schemas

const (
	// CloudEventsSpecVersion is the version of the CloudEvents spec itself.
	CloudEventsSpecVersion = "1.0"

	// CloudEventDataSchemaVersionV1 represents version 1 of our custom data payloads.
	CloudEventDataSchemaVersionV1 = "1.0"
)

// CloudEvent represents the CloudEvents envelope used internally, with required extensions.
type CloudEvent struct {
	SpecVersion   string         `json:"specversion"`
	ID            string         `json:"id"`
	Source        string         `json:"source"`
	Type          string         `json:"type"`
	Time          string         `json:"time"`
	DataSchema    string         `json:"dataschema,omitempty"`
	Subject       string         `json:"subject,omitempty"`
	CorrelationID string         `json:"correlationid"`
	CausationID   string         `json:"causationid"`
	Data          map[string]any `json:"data,omitempty"`
}

// Standard extension names used across Ruby Core.
const (
	ExtensionCorrelationID = "correlationid"
	ExtensionCausationID   = "causationid"
)
