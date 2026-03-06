package natsx

import (
	"fmt"
	"strings"
)

// Standard NATS classes allowed in subject naming convention.
var AllowedClasses = map[string]struct{}{
	"events":   {},
	"commands": {},
	"audit":    {},
	"metrics":  {},
	"logs":     {},
}

// IsValidToken checks if a given string is a valid NATS subject token
// (lowercase alphanumeric and underscores, no dots).
func IsValidToken(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// BuildSubject constructs a NATS subject string according to the defined convention.
// It validates tokens and returns an error if any token is invalid.
func BuildSubject(source, class, typ, id, action string) (string, error) {
	if !IsValidToken(source) {
		return "", fmt.Errorf("invalid source token: %w", ErrInvalidToken)
	}
	if !IsValidToken(class) {
		return "", fmt.Errorf("invalid class token: %w", ErrInvalidToken)
	}
	if _, ok := AllowedClasses[class]; !ok {
		return "", fmt.Errorf("class %q is not allowed: %w", class, ErrInvalidClass)
	}
	if !IsValidToken(typ) {
		return "", fmt.Errorf("invalid type token: %w", ErrInvalidToken)
	}

	subject := source + "." + class + "." + typ

	if id != "" {
		if !IsValidToken(id) {
			return "", fmt.Errorf("invalid id token: %w", ErrInvalidToken)
		}
		subject += "." + id
	}

	if action != "" {
		if !IsValidToken(action) {
			return "", fmt.Errorf("invalid action token: %w", ErrInvalidToken)
		}
		subject += "." + action
	}

	return subject, nil
}

// SubjectToken normalises s to a valid ADR-0027 subject token by converting to lowercase
// and replacing hyphens with underscores. It does not strip other invalid characters;
// callers should verify the result with IsValidToken if the input is not controlled.
//
// Use this when constructing subjects from service or connection names that may contain
// hyphens (e.g., normalising "ruby-core-engine" to "ruby_engine" for use as a source token).
func SubjectToken(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", "_"))
}

// BuildAuditSubject constructs an audit subject: audit.{source}.{type}.
//
// Audit subjects use an inverted namespace (class first) rather than the standard
// {source}.{class}.{type} format defined in ADR-0027. The inversion is required to
// avoid JetStream stream subject overlap: a leading-wildcard filter *.audit.> conflicts
// with the reserved dlq.> namespace. Using audit.> as the stream filter has no conflicts.
// The audit-sink consumer and all audit ACLs follow this audit.{source}.{type} format.
func BuildAuditSubject(source, typ string) (string, error) {
	if !IsValidToken(source) {
		return "", fmt.Errorf("invalid source token: %w", ErrInvalidToken)
	}
	if !IsValidToken(typ) {
		return "", fmt.Errorf("invalid type token: %w", ErrInvalidToken)
	}
	return "audit." + source + "." + typ, nil
}

// BuildDLQSubject constructs a standard subject name for a Dead-Letter Queue.
func BuildDLQSubject(streamName, consumerName string) (string, error) {
	if !IsValidToken(streamName) {
		return "", fmt.Errorf("invalid stream name token: %w", ErrInvalidToken)
	}
	if !IsValidToken(consumerName) {
		return "", fmt.Errorf("invalid consumer name token: %w", ErrInvalidToken)
	}
	return fmt.Sprintf("dlq.%s.%s", streamName, consumerName), nil
}
