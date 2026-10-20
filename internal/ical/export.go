package ical

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	goical "github.com/emersion/go-ical"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/settings"
)

// Export resolves a secret unit token and emits only public occupancy details.
// Unknown and regenerated tokens return ErrNotFound, without requiring a session.
func Export(ctx context.Context, database *sql.DB, token string, now func() time.Time, writer io.Writer) error {
	unitID, err := New(database).unitForExport(ctx, token)
	if err != nil {
		return feedError(err)
	}
	today, err := settings.Today(ctx, database, now)
	if err != nil {
		return err
	}
	blocks, err := exportOccupancy(ctx, database, unitID, today)
	if err != nil {
		return err
	}
	calendar := goical.NewCalendar()
	calendar.Props.SetText(goical.PropVersion, "2.0")
	calendar.Props.SetText(goical.PropProductID, "-//wingmanager//Occupancy//EN")
	for _, block := range blocks {
		event := goical.NewEvent()
		event.Props.SetText(goical.PropUID, block.uid)
		event.Props.SetDateTime(goical.PropDateTimeStamp, now().UTC())
		event.Props.SetText(goical.PropSummary, "Not available")
		event.Props.SetDate(goical.PropDateTimeStart, block.from.MidnightUTC())
		event.Props.SetDate(goical.PropDateTimeEnd, block.to.MidnightUTC())
		calendar.Children = append(calendar.Children, event.Component)
	}
	return goical.NewEncoder(writer).Encode(calendar)
}

// exportBlock contains only public occupancy, with an exclusive end date.
type exportBlock struct {
	uid      string
	from, to civildate.Date
}

// exportOccupancy is the M5 active-lease export seam: collect anonymous blocks
// here without changing the encoder. Inclusive lease endpoints must be converted
// through occupancy; no lease model or query is introduced before M5.
func exportOccupancy(ctx context.Context, database *sql.DB, unitID int64, today civildate.Date) ([]exportBlock, error) {
	values, err := booking.List(ctx, database, booking.Filter{UnitID: unitID, Status: booking.StatusConfirmed})
	if err != nil {
		return nil, err
	}
	var blocks []exportBlock
	for _, value := range values {
		to, err := civildate.Parse(value.CheckOut)
		if err != nil {
			return nil, err
		}
		if to.Compare(today) < 0 {
			continue
		}
		from, err := civildate.Parse(value.CheckIn)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, exportBlock{uid: fmt.Sprintf("booking-%d@wingmanager", value.ID), from: from, to: to})
	}
	return blocks, nil
}
