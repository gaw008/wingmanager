package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestCalendarPrototypeNavigationFilterAndLeaseBar(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			u, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			leaseID, err := lease.Create(t.Context(), a.database, lease.Details{UnitID: a.unitID, StartsOn: "2026-10-01", RentCents: 225000, DueDay: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := lease.AddTenant(t.Context(), a.database, leaseID, 0, &person.Details{Name: "Lease Tenant"}); err != nil {
				t.Fatal(err)
			}
			if err := lease.Activate(t.Context(), a.database, leaseID); err != nil {
				t.Fatal(err)
			}
			otherProperty, err := property.Create(t.Context(), a.database, property.Details{Name: "Other property"}, "Other unit")
			if err != nil {
				t.Fatal(err)
			}
			units, err := property.ListUnits(t.Context(), a.database, otherProperty, false)
			if err != nil {
				t.Fatal(err)
			}
			bookingID, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: units[0].ID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-10-31", CheckOut: "2026-11-02"})
			if err != nil {
				t.Fatal(err)
			}
			body := a.request(t, "GET", "/calendar?month=2026-10", nil, 200,
				`class="cal-toolbar"`, `id="cal-wrap"`, `id="today-col"`, `aria-current="date"`,
				`class="bar lease cut-right channel-"`, fmt.Sprintf(`href="/leases/%d"`, leaseID), "Lease Tenant",
				catalogs[locale]["calendar_open_ended"], `grid-column:2/64;grid-row:1`,
				fmt.Sprintf(`href="/bookings/%d"`, bookingID), fmt.Sprintf(`hx-get="/bookings/%d" hx-target="#drawer-body" data-drawer`, bookingID),
				fmt.Sprintf(`/bookings/new?unit=%d&amp;date=2026-10-31`, a.unitID), catalogs[locale]["calendar_property_filter"])
			if strings.Contains(body, "ZgotmplZ") {
				t.Fatal("unsafe dynamic style placeholder")
			}
			filtered := a.request(t, "GET", fmt.Sprintf("/calendar?month=2026-10&property=%d", u.PropertyID), nil, 200,
				fmt.Sprintf(`/calendar?month=2026-09&amp;property=%d`, u.PropertyID), fmt.Sprintf(`/calendar?month=2026-11&amp;property=%d`, u.PropertyID))
			if strings.Contains(filtered, fmt.Sprintf(`data-unit-id="%d"`, units[0].ID)) || strings.Contains(filtered, fmt.Sprintf(`href="/bookings/%d"`, bookingID)) {
				t.Fatal("filtered calendar includes another property's occupancy")
			}
			a.request(t, "GET", "/calendar?month=2026-02", nil, 200, `repeat(56, minmax(15px,1fr))`, `data-date="2026-02-28"`)
			a.request(t, "GET", "/calendar?property=bad", nil, 400, catalogs[locale]["invalid_form"])
		})
	}
}
