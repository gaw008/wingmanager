// Package demo creates sample data through the owning domain packages.
package demo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

// Credentials contains the generated demo owner's login details.
type Credentials struct {
	Email, Password string
}

// Seed requires an instance without an owner and dates samples in its timezone.
// Domain writes commit separately: failures retain completed records and return
// the created owner's credentials together with the error. Reruns are refused.
func Seed(ctx context.Context, database *sql.DB) (Credentials, error) {
	today, err := settings.Today(ctx, database, time.Now)
	if err != nil {
		return Credentials{}, err
	}
	return seed(ctx, database, today)
}

func seed(ctx context.Context, database *sql.DB, today civildate.Date) (Credentials, error) {
	secret := make([]byte, 18)
	if _, err := rand.Read(secret); err != nil {
		return Credentials{}, err
	}
	credentials := Credentials{Email: "demo@example.com", Password: base64.RawURLEncoding.EncodeToString(secret)}
	// Setup's IMMEDIATE transaction is the sole owner claim. Concurrent or repeat
	// seed attempts stop before creating any properties or sample money records.
	if err := auth.NewService(database).Setup(ctx, credentials.Email, credentials.Password); err != nil {
		return Credentials{}, err
	}
	return credentials, seedData(ctx, database, today)
}

func seedData(ctx context.Context, database *sql.DB, today civildate.Date) error {
	cabinID, err := property.Create(ctx, database, property.Details{
		Name: "Demo Lake Cabin", Address: "100 Example Lake Road", Notes: "Synthetic short-term rental",
	}, "Cabin")
	if err != nil {
		return err
	}
	cabinUnits, err := property.ListUnits(ctx, database, cabinID, false)
	if err != nil {
		return err
	}
	cabinUnitID := cabinUnits[0].ID
	for _, stay := range []struct {
		guest         string
		from, through int
	}{
		{"Demo departing guest", -2, 0},
		{"Demo arriving guest", 0, 3},
	} {
		if _, err := booking.Create(ctx, database, booking.Details{
			UnitID: cabinUnitID, Kind: booking.KindStay, Channel: booking.ChannelDirect,
			CheckIn: today.AddDays(stay.from).String(), CheckOut: today.AddDays(stay.through).String(),
			NewGuest: &person.Details{Name: stay.guest}, Notes: "Synthetic demo booking",
		}); err != nil {
			return err
		}
	}
	// The reserved .invalid domain labels this as a sample feed. No external
	// calendar is fetched; the regular scheduler can display its sync error.
	feedID, err := ical.Add(ctx, database, cabinUnitID, booking.ChannelAirbnb, "https://example.invalid/demo-calendar.ics")
	if err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := booking.ApplySnapshot(ctx, tx, feedID, cabinUnitID, booking.ChannelAirbnb, []booking.ImportedEvent{
		{UID: "demo-airbnb-stay", Kind: booking.KindStay, CheckIn: today.AddDays(5).String(), CheckOut: today.AddDays(7).String()},
	}, today); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	apartmentID, err := property.Create(ctx, database, property.Details{
		Name: "Demo City Apartment", Address: "200 Example Avenue", Notes: "Synthetic long-term rental",
	}, "Apartment")
	if err != nil {
		return err
	}
	apartmentUnits, err := property.ListUnits(ctx, database, apartmentID, false)
	if err != nil {
		return err
	}
	// Start in the previous month so debt remains overdue even on January 1.
	startsOn := today.MonthStart().AddDays(-1).MonthStart()
	leaseID, err := lease.Create(ctx, database, lease.Details{
		UnitID: apartmentUnits[0].ID, StartsOn: startsOn.String(), RentCents: 150000, DueDay: 1,
		Notes: "Synthetic demo lease with partial rent payment",
	})
	if err != nil {
		return err
	}
	if err := lease.AddTenant(ctx, database, leaseID, 0, &person.Details{Name: "Demo overdue tenant", Email: "tenant@example.com"}); err != nil {
		return err
	}
	if err := lease.Activate(ctx, database, leaseID); err != nil {
		return err
	}
	if err := ledger.PostLeaseRent(ctx, database, leaseID, today); err != nil {
		return err
	}
	if _, err := ledger.Record(ctx, database, leaseID, ledger.Details{
		Kind: ledger.KindPayment, Category: ledger.CategoryBankTransfer, AmountCents: 75000,
		OccurredOn: today.String(), Memo: "Synthetic partial rent payment",
	}); err != nil {
		return err
	}
	_, err = ledger.RecordProperty(ctx, database, cabinID, 0, ledger.Details{
		Kind: ledger.KindExpense, Category: ledger.CategoryRepairs, AmountCents: 12500,
		OccurredOn: today.String(), Memo: "Synthetic cabin repair expense",
	})
	return err
}
