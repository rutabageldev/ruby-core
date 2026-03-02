package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// PresenceConfig holds all entity references and tuning parameters for the
// presence service. All entity IDs are centralised here; no hardcoded names
// appear elsewhere in the service.
type PresenceConfig struct {
	PersonID        string        // PRESENCE_PERSON_ID (e.g. "katie")
	PhoneEntity     string        // PRESENCE_PHONE_ENTITY (e.g. "phone.katie")
	WifiEntity      string        // PRESENCE_WIFI_ENTITY (e.g. "network.phone.katie")
	TrustedNetworks []string      // PRESENCE_TRUSTED_WIFI comma-separated (e.g. "RubyGues,RubyNet,RIoT")
	DebounceDur     time.Duration // PRESENCE_DEBOUNCE_SECONDS (default 120)
	UncertainStates []string      // PRESENCE_UNCERTAIN_STATES (default "unknown,unavailable,none")
}

// phoneEntityDomain returns the domain part of PhoneEntity (e.g. "phone" from "phone.katie").
func (c *PresenceConfig) phoneEntityDomain() string {
	domain, _, _ := strings.Cut(c.PhoneEntity, ".")
	return domain
}

// phoneEntityName returns the name part of PhoneEntity (e.g. "katie" from "phone.katie").
func (c *PresenceConfig) phoneEntityName() string {
	_, name, _ := strings.Cut(c.PhoneEntity, ".")
	return name
}

// isUncertain reports whether state is one of the configured uncertain states.
func (c *PresenceConfig) isUncertain(state string) bool {
	return slices.Contains(c.UncertainStates, strings.ToLower(state))
}

// LoadPresenceConfig reads presence configuration from environment variables.
// Returns an error if required variables (PRESENCE_PERSON_ID, PRESENCE_PHONE_ENTITY,
// PRESENCE_WIFI_ENTITY) are missing.
func LoadPresenceConfig() (*PresenceConfig, error) {
	personID := os.Getenv("PRESENCE_PERSON_ID")
	if personID == "" {
		return nil, fmt.Errorf("presence: PRESENCE_PERSON_ID is required")
	}

	phoneEntity := os.Getenv("PRESENCE_PHONE_ENTITY")
	if phoneEntity == "" {
		return nil, fmt.Errorf("presence: PRESENCE_PHONE_ENTITY is required")
	}
	if !strings.Contains(phoneEntity, ".") {
		return nil, fmt.Errorf("presence: PRESENCE_PHONE_ENTITY must be in domain.name format (got %q)", phoneEntity)
	}

	wifiEntity := os.Getenv("PRESENCE_WIFI_ENTITY")
	if wifiEntity == "" {
		return nil, fmt.Errorf("presence: PRESENCE_WIFI_ENTITY is required")
	}

	var trustedNetworks []string
	if raw := os.Getenv("PRESENCE_TRUSTED_WIFI"); raw != "" {
		for n := range strings.SplitSeq(raw, ",") {
			if n = strings.TrimSpace(n); n != "" {
				trustedNetworks = append(trustedNetworks, n)
			}
		}
	}

	debounceDur := 120 * time.Second
	if s := os.Getenv("PRESENCE_DEBOUNCE_SECONDS"); s != "" {
		secs, err := strconv.Atoi(s)
		if err != nil || secs <= 0 {
			return nil, fmt.Errorf("presence: PRESENCE_DEBOUNCE_SECONDS must be a positive integer (got %q)", s)
		}
		debounceDur = time.Duration(secs) * time.Second
	}

	uncertainStates := []string{"unknown", "unavailable", "none"}
	if raw := os.Getenv("PRESENCE_UNCERTAIN_STATES"); raw != "" {
		uncertainStates = nil
		for s := range strings.SplitSeq(raw, ",") {
			if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
				uncertainStates = append(uncertainStates, s)
			}
		}
	}

	return &PresenceConfig{
		PersonID:        personID,
		PhoneEntity:     phoneEntity,
		WifiEntity:      wifiEntity,
		TrustedNetworks: trustedNetworks,
		DebounceDur:     debounceDur,
		UncertainStates: uncertainStates,
	}, nil
}
