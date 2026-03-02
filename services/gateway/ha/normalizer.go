// Package ha implements the Home Assistant WebSocket client and supporting
// components for the Ruby Core gateway service.
package ha

// Normalizer applies lean projection to HA entity attributes, dropping any
// attribute not in the passlist for the given entity domain (ADR-0009).
//
// The passlist is keyed by entity domain (e.g. "person", "device_tracker") and
// maps to the set of attribute names to keep. An empty passlist for a domain
// means all attributes are passed through (safe default for V0 operation when
// the engine config has not yet been published to KV).
type Normalizer struct {
	passlist map[string]map[string]struct{}
}

// NewNormalizer creates a Normalizer from the passlist JSON delivered via the
// config KV bucket. passlist maps entity domain → allowed attribute names.
// Passing nil is safe: all attributes are passed through.
func NewNormalizer(passlist map[string][]string) *Normalizer {
	n := &Normalizer{passlist: make(map[string]map[string]struct{})}
	for domain, attrs := range passlist {
		set := make(map[string]struct{}, len(attrs))
		for _, a := range attrs {
			set[a] = struct{}{}
		}
		n.passlist[domain] = set
	}
	return n
}

// Apply filters attrs to only those allowed for the given entity domain.
// "state" is always included regardless of the passlist (the presence processor
// and most automations require it). If no passlist entry exists for the domain,
// all attributes are passed through.
func (n *Normalizer) Apply(domain string, attrs map[string]any) map[string]any {
	allowed, hasList := n.passlist[domain]
	if !hasList || len(allowed) == 0 {
		return attrs // no filter configured: pass all through
	}

	result := make(map[string]any, len(allowed)+1)
	// Always include state.
	if v, ok := attrs["state"]; ok {
		result["state"] = v
	}
	for k, v := range attrs {
		if _, ok := allowed[k]; ok {
			result[k] = v
		}
	}
	return result
}
