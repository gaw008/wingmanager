package occupancy

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/person"
)

// CalendarView is a month of occupancy, ready to render without date arithmetic.
type CalendarView struct {
	Month, PreviousMonth, NextMonth string
	Year, MonthNumber, HalfDays     int
	Days                            []CalendarDay
	Properties                      []CalendarProperty
	ConflictCount                   int
	FirstConflictURL                string
}

type CalendarDay struct {
	Date                    string
	Number, Weekday, Column int
	Today, Weekend, Past    bool
}

type CalendarProperty struct {
	ID    int64
	Name  string
	Units []CalendarUnit
}

type CalendarUnit struct {
	ID        int64
	Name      string
	FeedCount int64
	Lanes     int
	HasLease  bool
	Bars      []CalendarBar
}

// CalendarBar uses one-based CSS grid lines, including the unit-name column.
// Lane is one-based. CutLeft/CutRight mark endpoints clipped by the month.
type CalendarBar struct {
	ID                                   int64
	URL, Kind, Channel, GuestName, Notes string
	StartsOn, EndsOn, Initials, Color    string
	RentCents                            int64
	StartColumn, EndColumn, Lane         int
	CutLeft, CutRight, Past, Conflict    bool
}

// Calendar derives half-day bars, lanes and conflicts from persisted occupancy.
// today must be the settings-timezone date. propertyID zero includes every property.
func Calendar(ctx context.Context, database *sql.DB, month, today civildate.Date, propertyID int64) (CalendarView, error) {
	if month.IsZero() || today.IsZero() || propertyID < 0 {
		return CalendarView{}, ErrRange
	}
	start := month.MonthStart()
	end := start.AddDays(32).MonthStart()
	view := CalendarView{Month: start.String()[:7], Year: start.Year(), MonthNumber: int(start.Month())}
	if start.Year() > 1 || start.Month() > time.January {
		view.PreviousMonth = start.AddDays(-1).String()[:7]
	}
	if end.Year() <= 9999 {
		view.NextMonth = end.String()[:7]
	}
	for day := start; day.Compare(end) < 0; day = day.AddDays(1) {
		weekday := day.MidnightUTC().Weekday()
		view.Days = append(view.Days, CalendarDay{Date: day.String(), Number: day.Day(), Weekday: int(weekday), Column: 2 * day.Day(), Today: day.Compare(today) == 0, Weekend: weekday == time.Saturday || weekday == time.Sunday, Past: day.Compare(today) < 0})
	}
	view.HalfDays = len(view.Days) * 2
	monthRange, err := Stay(start, end)
	if err != nil {
		return CalendarView{}, err
	}
	q := New(database)
	units, err := q.calendarUnits(ctx, propertyID)
	if err != nil {
		return CalendarView{}, err
	}
	occupants, err := q.calendarOccupants(ctx)
	if err != nil {
		return CalendarView{}, err
	}
	type item struct {
		bar      CalendarBar
		interval Range
	}
	byUnit := make(map[int64][]item)
	for _, row := range occupants {
		from, err := civildate.Parse(row.StartsOn)
		if err != nil {
			return CalendarView{}, err
		}
		var to civildate.Date
		if row.EndsOn != "" {
			to, err = civildate.Parse(row.EndsOn)
			if err != nil {
				return CalendarView{}, err
			}
		}
		var interval Range
		if row.Kind == "lease" {
			var lastDay *civildate.Date
			if !to.IsZero() {
				lastDay = &to
			}
			interval, err = Lease(from, lastDay)
		} else {
			interval, err = Stay(from, to)
		}
		if err != nil {
			return CalendarView{}, err
		}
		// A checkout on day one still paints the morning half-cell.
		// Its occupancy interval remains unchanged for conflict detection.
		checkoutMorning := row.Kind != "lease" && to.Compare(start) == 0
		if !interval.Overlaps(monthRange) && !checkoutMorning {
			continue
		}
		bar := CalendarBar{ID: row.ID, Kind: row.Kind, Channel: row.Channel, GuestName: row.GuestName, StartsOn: row.StartsOn, EndsOn: row.EndsOn, RentCents: row.RentCents, Notes: row.Notes}
		bar.Initials, bar.Color = person.Appearance(row.GuestName)
		bar.URL = fmt.Sprintf("/bookings/%d", row.ID)
		if row.Kind == "lease" {
			bar.URL = fmt.Sprintf("/leases/%d", row.ID)
		}
		from, to = interval.Bounds()
		// A day occupies two columns after the unit label. Stays join at noon;
		// leases cover whole days, using the inclusive-end conversion above.
		offset := 1
		if row.Kind == "lease" {
			offset = 0
		}
		column := func(day civildate.Date) int {
			return int(day.MidnightUTC().Sub(start.MidnightUTC())/(24*time.Hour))*2 + 2 + offset
		}
		bar.StartColumn = 1
		if from.Compare(start) >= 0 {
			bar.StartColumn = column(from)
		}
		bar.EndColumn = view.HalfDays + 3
		if !to.IsZero() && to.Compare(end) <= 0 {
			bar.EndColumn = column(to)
		}
		bar.CutLeft, bar.CutRight = bar.StartColumn < 2, bar.EndColumn > view.HalfDays+2
		bar.StartColumn, bar.EndColumn = max(2, bar.StartColumn), min(view.HalfDays+2, bar.EndColumn)
		bar.Past = row.EndsOn != "" && row.EndsOn < today.String()
		byUnit[row.UnitID] = append(byUnit[row.UnitID], item{bar: bar, interval: interval})
	}
	for _, u := range units {
		items := byUnit[u.ID]
		sort.SliceStable(items, func(i, j int) bool { return items[i].interval.from.Compare(items[j].interval.from) < 0 })
		unit := CalendarUnit{ID: u.ID, Name: u.Name, FeedCount: u.FeedCount, Lanes: 1}
		var laneEnds []civildate.Date
		for i := range items {
			current := &items[i]
			lane := 0
			for lane < len(laneEnds) && (laneEnds[lane].IsZero() || laneEnds[lane].Compare(current.interval.from) > 0) {
				lane++
			}
			if lane == len(laneEnds) {
				laneEnds = append(laneEnds, current.interval.to)
			} else {
				laneEnds[lane] = current.interval.to
			}
			current.bar.Lane = lane + 1
			for j := i + 1; j < len(items); j++ {
				if current.interval.Overlaps(items[j].interval) {
					current.bar.Conflict, items[j].bar.Conflict = true, true
					view.ConflictCount++
					if view.FirstConflictURL == "" {
						view.FirstConflictURL = current.bar.URL
					}
				}
			}
			unit.HasLease = unit.HasLease || current.bar.Kind == "lease"
			unit.Bars = append(unit.Bars, current.bar)
		}
		unit.Lanes = max(1, len(laneEnds))
		if len(view.Properties) == 0 || view.Properties[len(view.Properties)-1].ID != u.PropertyID {
			view.Properties = append(view.Properties, CalendarProperty{ID: u.PropertyID, Name: u.PropertyName})
		}
		group := &view.Properties[len(view.Properties)-1]
		group.Units = append(group.Units, unit)
	}
	return view, nil
}
