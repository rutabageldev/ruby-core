package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// haPusher updates Home Assistant sensor state via the REST API. It is a minimal,
// self-contained client so the calendar processor does not depend on another
// processor's HA client (ADR-0007). An empty baseURL/token makes pushes no-ops
// (non-prod, where HA push is disabled).
type haPusher struct {
	baseURL string
	token   string
	client  *http.Client
}

func newHAPusher(baseURL, token string) *haPusher {
	return &haPusher{baseURL: baseURL, token: token, client: &http.Client{Timeout: 10 * time.Second}}
}

// pushState POSTs /api/states/{entityID}. No-op when HA is not configured.
func (h *haPusher) pushState(ctx context.Context, entityID, state string, attrs map[string]any) error {
	if h.baseURL == "" || h.token == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{"state": state, "attributes": attrs})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/states/%s", h.baseURL, entityID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ha push %s: status %d", entityID, resp.StatusCode)
	}
	return nil
}
