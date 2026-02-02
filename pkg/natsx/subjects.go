package natsx

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
	// Validate required tokens
	if !IsValidToken(source) {
		return "", ErrInvalidToken
	}
	if !IsValidToken(class) {
		return "", ErrInvalidToken
	}
	if !IsValidToken(typ) {
		return "", ErrInvalidToken
	}

	if _, ok := AllowedClasses[class]; !ok {
		return "", ErrInvalidClass
	}

	subject := source + "." + class + "." + typ

	if id != "" {
		if !IsValidToken(id) {
			return "", ErrInvalidToken
		}
		subject += "." + id
	}

	if action != "" {
		if !IsValidToken(action) {
			return "", ErrInvalidToken
		}
		subject += "." + action
	}

	return subject, nil
}
