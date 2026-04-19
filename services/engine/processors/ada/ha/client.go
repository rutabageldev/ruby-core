// Package ha provides a minimal Home Assistant REST client for pushing
// sensor state from the ada processor to HA's volatile state machine.
package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Client pushes sensor state to Home Assistant via the REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	log        *slog.Logger
}

// NewClient returns a Client targeting the given HA base URL.
func NewClient(baseURL, token string, log *slog.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

type statePayload struct {
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// PushState updates an HA sensor entity via POST /api/states/{entity_id}.
// Errors are logged at Warn and returned so callers can decide how to handle them;
// the ada processor logs and continues rather than treating push failures as fatal.
func (c *Client) PushState(ctx context.Context, entityID, state string, attributes map[string]any) error {
	body, err := json.Marshal(statePayload{State: state, Attributes: attributes})
	if err != nil {
		return fmt.Errorf("ha: marshal state payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/states/%s", c.baseURL, entityID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ha: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Warn("ha: push state failed",
			slog.String("entity_id", entityID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("ha: push state: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		c.log.Warn("ha: push state unexpected status",
			slog.String("entity_id", entityID),
			slog.Int("status", resp.StatusCode),
		)
		return fmt.Errorf("ha: push state %s: HTTP %d", entityID, resp.StatusCode)
	}
	return nil
}

// Notify sends a push notification via HA's notify service REST API.
// service is the full notify service name, e.g. "mobile_app_mikes_iphone".
func (c *Client) Notify(ctx context.Context, service, title, message string) error {
	body, err := json.Marshal(map[string]string{
		"title":   title,
		"message": message,
	})
	if err != nil {
		return fmt.Errorf("ha: marshal notify payload: %w", err)
	}

	url := strings.TrimRight(c.baseURL, "/") + "/api/services/notify/" + service
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ha: build notify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ha: notify %s: %w", service, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("ha: notify %s: HTTP %d", service, resp.StatusCode)
	}
	return nil
}
