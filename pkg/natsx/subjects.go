package natsx

import "fmt"

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
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' {
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
