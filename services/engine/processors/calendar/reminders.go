package calendar

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	rubycal "github.com/primaryrutabaga/ruby-core/pkg/calendar"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

const (
	reminderInterval  = 60 * time.Second
	reminderLookahead = 36 * time.Hour
	statusSensor      = "sensor.ruby_home_calendar_status"
)

// runReminders ticks reminder evaluation until ctx is cancelled.
func (p *Processor) runReminders(ctx context.Context) {
	ticker := time.NewTicker(reminderInterval)
	defer ticker.Stop()

	p.tickReminders(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tickReminders(ctx)
		}
	}
}

// tickReminders computes reminders over the mirror — ruby-core owns reminder policy
// and ignores Google's per-event reminder overrides (ADR-0042). Each due reminder
// fires once (deduped) on NATS, and the always-on status sensor is refreshed with
// the next event and active-reminder flag so HA automations work with no card open.
func (p *Processor) tickReminders(ctx context.Context) {
	now := time.Now().UTC()
	instances, rows, err := rubycal.ExpandRange(ctx, p.q, now.Add(-2*reminderInterval), now.Add(reminderLookahead))
	if err != nil {
		p.log.Warn("calendar: reminders expand failed", slog.String("error", err.Error()))
		return
	}

	d := evaluateReminders(instances, now, p.reminderLead)
	for _, in := range d.due {
		p.fireReminder(in, rows[in.GoogleEventID])
	}
	p.pushStatus(ctx, d.next, rows, d.active)
}

// reminderDecision is the pure result of evaluating instances against the clock.
type reminderDecision struct {
	due    []expand.Instance
	next   *expand.Instance
	active bool
}

// evaluateReminders selects occurrences whose start is within [now, now+lead] as
// due (active), and the first occurrence starting after now as the next event.
func evaluateReminders(instances []expand.Instance, now time.Time, lead time.Duration) reminderDecision {
	var d reminderDecision
	for i := range instances {
		in := instances[i]
		until := in.Start.Sub(now)
		if until >= 0 && until <= lead {
			d.active = true
			d.due = append(d.due, in)
		}
		if d.next == nil && in.Start.After(now) {
			cp := in
			d.next = &cp
		}
	}
	return d
}

// statusState maps the decision to the sensor state value.
func statusState(next *expand.Instance, active bool) string {
	switch {
	case active:
		return "reminder"
	case next != nil:
		return "upcoming"
	default:
		return "idle"
	}
}

// fireReminder publishes calendar.reminder.due for one occurrence, deduped by
// event id + occurrence start so it fires exactly once.
func (p *Processor) fireReminder(in expand.Instance, row *store.CalendarEvent) {
	key := "reminder:" + in.GoogleEventID + ":" + in.Start.UTC().Format(time.RFC3339)
	if seen, err := p.idStore.Seen(key); err != nil || seen {
		return
	}

	summary := ""
	if row != nil && row.Summary.Valid {
		summary = row.Summary.String
	}

	evt := schemas.CloudEvent{
		SpecVersion:   schemas.CloudEventsSpecVersion,
		ID:            key,
		Source:        "ruby_engine",
		Type:          schemas.HomeEventCalendarReminderDue,
		Time:          time.Now().UTC().Format(time.RFC3339),
		DataSchema:    schemas.CloudEventDataSchemaVersionV1,
		CorrelationID: key,
		CausationID:   key,
		Data: map[string]any{
			"google_event_id": in.GoogleEventID,
			"summary":         summary,
			"start":           in.Start.UTC().Format(time.RFC3339),
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		p.log.Warn("calendar: marshal reminder failed", slog.String("error", err.Error()))
		return
	}
	if err := p.nc.Publish(schemas.HomeEventCalendarReminderDue, b); err != nil {
		p.log.Warn("calendar: publish reminder failed", slog.String("error", err.Error()))
		return
	}
	if err := p.idStore.Mark(key); err != nil {
		p.log.Warn("calendar: mark reminder failed", slog.String("error", err.Error()))
	}
	p.log.Info("calendar: reminder due",
		slog.String("google_event_id", in.GoogleEventID),
		slog.String("summary", summary),
	)
}

// pushStatus refreshes sensor.ruby_home_calendar_status: state is reminder|upcoming|idle,
// with the next event summary/start and the active-reminder flag as attributes.
func (p *Processor) pushStatus(ctx context.Context, next *expand.Instance, rows map[string]*store.CalendarEvent, active bool) {
	attrs := map[string]any{"active_reminder": active}
	if next != nil {
		summary := ""
		if row := rows[next.GoogleEventID]; row != nil && row.Summary.Valid {
			summary = row.Summary.String
		}
		attrs["next_event"] = summary
		attrs["next_start"] = next.Start.UTC().Format(time.RFC3339)
	}

	if err := p.ha.pushState(ctx, statusSensor, statusState(next, active), attrs); err != nil {
		p.log.Warn("calendar: push status sensor failed", slog.String("error", err.Error()))
	}
}
