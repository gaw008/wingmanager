package ical

import (
	"fmt"
	"io"
	"strings"
	"time"

	goical "github.com/emersion/go-ical"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/occupancy"
)

// parse validates every event before any write. A bad event invalidates the whole
// snapshot, because skipping it would look like a cancellation during reconciliation.
func parse(reader io.Reader, channel string, location *time.Location) ([]booking.ImportedEvent, error) {
	decoder := goical.NewDecoder(reader)
	calendar, err := decoder.Decode()
	if err != nil {
		return nil, fmt.Errorf("parse calendar: %w", err)
	}
	if _, err := decoder.Decode(); err != io.EOF {
		return nil, fmt.Errorf("unexpected content after calendar: %v", err)
	}
	var result []booking.ImportedEvent
	seen := make(map[string]bool)
	for i, event := range calendar.Events() {
		uid, err := event.Props.Text(goical.PropUID)
		if err != nil || strings.TrimSpace(uid) == "" || seen[uid] {
			return nil, fmt.Errorf("event %d: missing, malformed or duplicate UID", i+1)
		}
		seen[uid] = true
		for _, name := range []string{goical.PropUID, goical.PropDateTimeStart, goical.PropDateTimeEnd} {
			if len(event.Props[name]) > 1 {
				return nil, fmt.Errorf("event %d: duplicate %s", i+1, name)
			}
		}
		from, err := eventDate(event.Props.Get(goical.PropDateTimeStart), location)
		if err != nil {
			return nil, fmt.Errorf("event %d DTSTART: %w", i+1, err)
		}
		to := from.AddDays(1)
		if prop := event.Props.Get(goical.PropDateTimeEnd); prop != nil {
			to, err = eventDate(prop, location)
			if err != nil {
				return nil, fmt.Errorf("event %d DTEND: %w", i+1, err)
			}
		}
		if _, err := occupancy.Stay(from, to); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		// Ensure the implicit one-night endpoint also fits the persisted date format.
		if _, err := civildate.Parse(to.String()); err != nil {
			return nil, err
		}
		summary, err := event.Props.Text(goical.PropSummary)
		if err != nil {
			return nil, fmt.Errorf("event %d SUMMARY: %w", i+1, err)
		}
		result = append(result, booking.ImportedEvent{UID: uid, Kind: classify(channel, summary), CheckIn: from.String(), CheckOut: to.String()})
	}
	return result, nil
}

func eventDate(prop *goical.Prop, location *time.Location) (civildate.Date, error) {
	if prop == nil {
		return civildate.Date{}, fmt.Errorf("missing date")
	}
	value, err := prop.DateTime(location)
	if err != nil {
		return civildate.Date{}, err
	}
	return civildate.Parse(value.In(location).Format(time.DateOnly))
}
