package ada

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// Emergency card (ROADMAP-0011 effort 0011.4, ADR-0037). Standing config: rows use
// eventTest(evt) only (not pre-birth-forced), so a real pediatrician/poison-control
// contact entered before birth survives the clean-slate. We persist rows + order
// only — live-field rows resolve their value client-side off existing sensors.

const sensorEmergencyCard = "sensor.ada_emergency_card"

// handleEmergencyRowUpsert persists (insert-or-update) one card row.
func (p *Processor) handleEmergencyRowUpsert(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaEmergencyRowUpsertData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode emergency_row_upsert: %w", err)
	}
	if d.ID == "" {
		return fmt.Errorf("ada: emergency_row_upsert missing id")
	}
	if err := p.q.UpsertEmergencyRow(ctx, &store.UpsertEmergencyRowParams{
		ID:       d.ID,
		Type:     d.Type,
		Label:    d.Label,
		Name:     textFromString(d.Name),
		Phone:    textFromString(d.Phone),
		Address:  textFromString(d.Address),
		FieldKey: textFromString(d.FieldKey),
		LoggedBy: d.LoggedBy,
		Test:     eventTest(evt),
	}); err != nil {
		return fmt.Errorf("ada: upsert emergency row: %w", err)
	}
	p.pushEmergencyCard(ctx)
	return nil
}

// handleEmergencyRowDelete soft-deletes one card row.
func (p *Processor) handleEmergencyRowDelete(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaDeleteData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode emergency_row_delete: %w", err)
	}
	if err := p.q.SoftDeleteEmergencyRow(ctx, d.ID); err != nil {
		return fmt.Errorf("ada: soft-delete emergency row: %w", err)
	}
	p.pushEmergencyCard(ctx)
	return nil
}

// handleEmergencyReorder sets each row's position from the supplied ordered id list.
func (p *Processor) handleEmergencyReorder(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaEmergencyReorderData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode emergency_reorder: %w", err)
	}
	if len(d.Order) == 0 {
		return nil
	}
	if err := p.q.ReorderEmergencyRows(ctx, d.Order); err != nil {
		return fmt.Errorf("ada: reorder emergency rows: %w", err)
	}
	p.pushEmergencyCard(ctx)
	return nil
}

// ── Projection ────────────────────────────────────────────────────────────────

// emergencyRowItem mirrors the adaEmergency.ts EmergencyRow type. Optional fields
// are omitted when empty so contacts and live-fields carry only their own keys.
type emergencyRowItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Label    string `json:"label"`
	Name     string `json:"name,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Address  string `json:"address,omitempty"`
	FieldKey string `json:"field_key,omitempty"`
}

// pushEmergencyCard projects the ordered rows to sensor.ada_emergency_card. No-op
// when HA push is disabled (HA_INGEST_ENABLED=false, ADR-0033).
func (p *Processor) pushEmergencyCard(ctx context.Context) {
	rows, err := p.q.ListEmergencyRows(ctx)
	if err != nil {
		p.log.Warn("ada: list emergency rows", slog.String("error", err.Error()))
		return
	}
	items := make([]emergencyRowItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, emergencyRowItem{
			ID:       r.ID,
			Type:     r.Type,
			Label:    r.Label,
			Name:     r.Name.String,
			Phone:    r.Phone.String,
			Address:  r.Address.String,
			FieldKey: r.FieldKey.String,
		})
	}
	attrs := map[string]any{"rows": items, "last_updated": time.Now().UTC().Format(time.RFC3339)}
	if err := p.ha.PushState(ctx, sensorEmergencyCard, strconv.Itoa(len(items)), attrs); err != nil {
		p.log.Warn("ada: push emergency card", slog.String("error", err.Error()))
	}
}
