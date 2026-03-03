package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/audit"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// notifyRequest is the HA mobile_app REST notification payload.
type notifyRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// handler processes ruby_engine.commands.notify.> CloudEvent commands by
// dispatching a push notification via the HA mobile_app REST API.
type handler struct {
	haURL   string
	haToken string
	client  *http.Client
	rec     *audit.Publisher
	log     *slog.Logger
}

func newHandler(haURL, haToken string, rec *audit.Publisher, log *slog.Logger) *handler {
	return &handler{
		haURL:   haURL,
		haToken: haToken,
		client:  &http.Client{Timeout: 10 * time.Second},
		rec:     rec,
		log:     log,
	}
}

// process is the consumer process func for the notifier pull consumer.
// subject is the NATS subject (e.g. "ruby_engine.commands.notify.{evtID}").
func (h *handler) process(subject string, data []byte) error {
	var evt schemas.CloudEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		h.log.Warn("notifier: unmarshal command",
			slog.String("subject", subject),
			slog.String("error", err.Error()),
		)
		return nil // malformed: ack and skip to avoid poison-pill loop
	}

	if evt.Data == nil {
		h.log.Warn("notifier: command has no data payload", slog.String("subject", subject))
		return nil
	}

	title, _ := evt.Data["title"].(string)
	message, _ := evt.Data["message"].(string)
	device, _ := evt.Data["device"].(string)

	if device == "" {
		h.log.Warn("notifier: missing device in command",
			slog.String("subject", subject),
			slog.String("correlationid", evt.CorrelationID),
		)
		return nil
	}

	if h.haURL == "" {
		h.log.Warn("notifier: HA not configured — skipping notification (add secret/ruby-core/ha to Vault)",
			slog.String("device", device),
			slog.String("correlationid", evt.CorrelationID),
		)
		return nil // ack: nothing useful to retry until HA is configured
	}

	if err := h.sendNotification(subject, title, message, device, evt); err != nil {
		// Non-nil error will trigger NAK + backoff redelivery in the consumer.
		return err
	}
	return nil
}

// sendNotification POSTs to HA's mobile_app notify service.
func (h *handler) sendNotification(subject, title, message, device string, cause schemas.CloudEvent) error {
	// HA service name: "mobile_app_{device}" — underscores, lowercase.
	svcName := "mobile_app_" + strings.ToLower(strings.ReplaceAll(device, "-", "_"))
	apiURL := strings.TrimRight(h.haURL, "/") + "/api/services/notify/" + svcName

	body, err := json.Marshal(notifyRequest{Title: title, Message: message})
	if err != nil {
		return fmt.Errorf("notifier: marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notifier: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.haToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		h.log.Warn("notifier: HA REST call failed",
			slog.String("entity_id", cause.Subject),
			slog.String("correlationid", cause.CorrelationID),
			slog.String("device", device),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("notifier: HA REST call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		h.log.Warn("notifier: HA returned non-2xx",
			slog.String("entity_id", cause.Subject),
			slog.String("correlationid", cause.CorrelationID),
			slog.String("device", device),
			slog.String("service", svcName),
			slog.Int("http_status", resp.StatusCode),
		)
		return fmt.Errorf("notifier: HA returned HTTP %d for service %q", resp.StatusCode, svcName)
	}

	h.log.Info("notifier: notification sent",
		slog.String("entity_id", cause.Subject),
		slog.String("device", device),
		slog.String("title", title),
		slog.String("correlationid", cause.CorrelationID),
	)

	// Publish audit event so the smoke test can confirm delivery via NATS.
	// correlationid falls back to cause.ID so the smoke test's SMOKE_ID is always present.
	corrID := cause.CorrelationID
	if corrID == "" {
		corrID = cause.ID
	}
	h.rec.Record(corrID, cause.ID, "notification_sent", subject, "success")

	return nil
}
