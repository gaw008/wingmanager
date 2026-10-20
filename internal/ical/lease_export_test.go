package ical

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func TestExportActiveLeasesOnlyAndOpenEndedHorizon(t *testing.T) {
	d, u, _ := setup(t)
	create := func(start, end string, active bool) int64 {
		t.Helper()
		id, err := lease.Create(t.Context(), d, lease.Details{UnitID: u, StartsOn: start, EndsOn: end, RentCents: 123456, DueDay: 1, Notes: "Private lease notes"})
		if err != nil {
			t.Fatal(err)
		}
		if active {
			if err := lease.AddTenant(t.Context(), d, id, 0, &person.Details{Name: "Private tenant", Email: "private@example.com"}); err != nil {
				t.Fatal(err)
			}
			if err := lease.Activate(t.Context(), d, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	ended := create("2026-01-01", "2026-01-31", true)
	if err := lease.End(t.Context(), d, ended, "2026-01-31"); err != nil {
		t.Fatal(err)
	}
	draft := create("2026-10-01", "2026-10-31", false)
	finite := create("2026-10-01", "2026-10-31", true)
	open := create("2026-11-01", "", true)
	unit, err := property.GetUnit(t.Context(), d, u)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Export(t.Context(), d, unit.IcalExportToken, clock, &out); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{fmt.Sprintf("UID:lease-%d@wingmanager", finite), fmt.Sprintf("UID:lease-%d@wingmanager", open), "DTSTART;VALUE=DATE:20261001", "DTEND;VALUE=DATE:20261101", "DTSTART;VALUE=DATE:20261101", "DTEND;VALUE=DATE:20281019", "SUMMARY:Not available"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	for _, private := range []string{fmt.Sprintf("UID:lease-%d@wingmanager", ended), fmt.Sprintf("UID:lease-%d@wingmanager", draft), "Private tenant", "private@example.com", "Private lease notes", "123456"} {
		if strings.Contains(body, private) {
			t.Fatalf("unexpected %s in %s", private, body)
		}
	}
}
