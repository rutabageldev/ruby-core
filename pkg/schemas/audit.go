package schemas

import "time"

const (
	// AuditEventType is the CloudEvents "type" for all Ruby Core audit records (ADR-0019).
	AuditEventType = "dev.rubycore.audit.v1"
)

// AuditData is the structured payload embedded in an AuditEvent (ADR-0019).
// It contains the mandatory context required for forensic analysis.
type AuditData struct {
	// Actor identifies the service performing the action.
	// Phase 4: service name (e.g., "ruby_engine").
	// Phase 5: will be replaced with the service's NKEY public key.
	Actor string `json:"actor"`

	// Action is a dot-separated description of the action performed,
	// e.g., "event.processed", "event.discarded", "event.failed".
	Action string `json:"action"`

	// Subject is the NATS subject the original message arrived on.
	Subject string `json:"subject"`

	// Outcome is the result of the action: "success", "failure", or "duplicate".
	Outcome string `json:"outcome"`

	// Details holds optional supplementary context for the specific action.
	Details map[string]any `json:"details,omitempty"`
}

// AuditEvent is a CloudEvents envelope for audit records (ADR-0019).
// It uses a typed Data field rather than the generic map[string]any in CloudEvent,
// enforcing the mandatory fields required by the audit schema.
type AuditEvent struct {
	SpecVersion   string    `json:"specversion"`
	ID            string    `json:"id"`
	Source        string    `json:"source"`
	Type          string    `json:"type"`
	Time          string    `json:"time"`
	CorrelationID string    `json:"correlationid"`
	CausationID   string    `json:"causationid"`
	Data          AuditData `json:"data"`
}

// NewAuditEvent constructs an AuditEvent ready for publication.
// id must be a globally unique identifier for this specific audit record.
// source must be an ADR-0027-compliant token (lowercase, digits, underscores only).
// Use natsx.SubjectToken() to normalise a display name such as "ruby-core-engine" to "ruby_engine".
func NewAuditEvent(id, source, correlationID, causationID string, data AuditData) AuditEvent {
	return AuditEvent{
		SpecVersion:   CloudEventsSpecVersion,
		ID:            id,
		Source:        source,
		Type:          AuditEventType,
		Time:          time.Now().UTC().Format(time.RFC3339),
		CorrelationID: correlationID,
		CausationID:   causationID,
		Data:          data,
	}
}
