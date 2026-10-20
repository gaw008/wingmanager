package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/occupancy"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

type unitChoice struct {
	ID    int64
	Label string
}
type bookingRow struct {
	booking.Booking
	UnitLabel, GuestName string
	Conflict             bool
}
type calendarDay struct {
	Date   string
	Number int
	Today  bool
}
type calendarCell struct {
	calendarDay
	Span     int
	Bookings []bookingRow
	Conflict bool
}
type calendarUnit struct {
	ID    int64
	Name  string
	Cells []calendarCell
}
type calendarProperty struct {
	Name  string
	Units []calendarUnit
}

// bookingPage holds the booking forms and the calendar derived from existing records.
type bookingPage struct {
	Booking                         booking.Booking
	Cleaning                        *task.Task
	SourceURL                       string
	NewGuest                        person.Details
	BookingRows, Conflicts          []bookingRow
	UnitChoices                     []unitChoice
	Kinds, Statuses, Channels       []string
	UnitFilter                      int64
	PeriodFilter, StatusFilter      string
	Month, PreviousMonth, NextMonth string
	Days                            []calendarDay
	CalendarProperties              []calendarProperty
}

func (s *server) registerBookingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /calendar", s.calendar)
	mux.HandleFunc("GET /bookings", s.listBookings)
	mux.HandleFunc("GET /bookings/new", s.editBooking)
	mux.HandleFunc("POST /bookings/new", s.editBooking)
	mux.HandleFunc("GET /bookings/{id}", s.editBooking)
	mux.HandleFunc("GET /bookings/{id}/edit", s.editBooking)
	mux.HandleFunc("POST /bookings/{id}/edit", s.editBooking)
	mux.HandleFunc("POST /bookings/{id}/cancel", func(w http.ResponseWriter, r *http.Request) { s.cancelBooking(w, r, true) })
	mux.HandleFunc("POST /bookings/{id}/restore", func(w http.ResponseWriter, r *http.Request) { s.cancelBooking(w, r, false) })
}

func (s *server) bookingChoices(ctx context.Context, data *pageData) error {
	properties, err := property.List(ctx, s.database, true)
	if err != nil {
		return err
	}
	for _, p := range properties {
		units, err := property.ListUnits(ctx, s.database, p.ID, true)
		if err != nil {
			return err
		}
		for _, u := range units {
			data.UnitChoices = append(data.UnitChoices, unitChoice{u.ID, p.Name + " / " + u.Name})
		}
	}
	data.People, err = person.List(ctx, s.database, "")
	data.Kinds, data.Statuses, data.Channels = booking.Kinds(), booking.Statuses(), booking.Channels()
	return err
}

func bookingRows(values []booking.Booking, data pageData) []bookingRow {
	units := make(map[int64]string, len(data.UnitChoices))
	people := make(map[int64]string, len(data.People))
	for _, u := range data.UnitChoices {
		units[u.ID] = u.Label
	}
	for _, p := range data.People {
		people[p.ID] = p.Name
	}
	rows := make([]bookingRow, len(values))
	for i, b := range values {
		rows[i] = bookingRow{Booking: b, UnitLabel: units[b.UnitID], GuestName: people[b.GuestID]}
	}
	return rows
}

func (s *server) listBookings(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "bookings"}
	if err := s.bookingChoices(r.Context(), &data); err != nil {
		s.fail(w, err)
		return
	}
	query := r.URL.Query()
	if query.Get("unit") != "" {
		id, err := strconv.ParseInt(query.Get("unit"), 10, 64)
		if err != nil || id <= 0 {
			s.bookingResult(w, r, data, booking.ErrInvalid)
			return
		}
		data.UnitFilter = id
	}
	data.PeriodFilter, data.StatusFilter = query.Get("period"), query.Get("status")
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	values, err := booking.List(r.Context(), s.database, booking.Filter{
		UnitID: data.UnitFilter, Status: data.StatusFilter, Period: data.PeriodFilter, Today: today,
	})
	if err != nil {
		s.bookingResult(w, r, data, err)
		return
	}
	data.BookingRows = bookingRows(values, data)
	s.render(w, r, data, http.StatusOK)
}

func (s *server) editBooking(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "new_booking"}
	if err := s.bookingChoices(r.Context(), &data); err != nil {
		s.fail(w, err)
		return
	}
	if r.PathValue("id") != "" {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			s.notFound(w, r)
			return
		}
		data.Booking, err = booking.Get(r.Context(), s.database, id)
		if err != nil {
			s.bookingResult(w, r, data, err)
			return
		}
		data.Page = "edit_booking"
		data.Cleaning, err = task.CleaningForBooking(r.Context(), s.database, id)
		if err != nil {
			s.fail(w, err)
			return
		}
		if data.Booking.Imported {
			feed, err := ical.Get(r.Context(), s.database, data.Booking.FeedID)
			if err != nil {
				s.fail(w, err)
				return
			}
			data.SourceURL = feed.Url
		}
	} else {
		data.Booking.Kind, data.Booking.Channel, data.Booking.Status = booking.KindStay, booking.ChannelDirect, booking.StatusConfirmed
	}
	if r.Method == http.MethodGet {
		if data.Page == "new_booking" {
			if raw := r.URL.Query().Get("unit"); raw != "" {
				id, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || id <= 0 {
					s.bookingResult(w, r, data, booking.ErrInvalid)
					return
				}
				if _, err := property.GetUnit(r.Context(), s.database, id); err != nil {
					s.renderDomainResult(w, r, data, err)
					return
				}
				data.Booking.UnitID = id
			}
			if raw := r.URL.Query().Get("date"); raw != "" {
				date, err := civildate.Parse(raw)
				if err != nil {
					s.bookingResult(w, r, data, booking.ErrInvalid)
					return
				}
				data.Booking.CheckIn, data.Booking.CheckOut = date.String(), date.AddDays(1).String()
			}
		}
		s.render(w, r, data, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		data.Error = "invalid_form"
		s.render(w, r, data, http.StatusBadRequest)
		return
	}
	b := &data.Booking
	b.Notes = r.PostForm.Get("notes")
	if !b.Imported {
		b.Kind, b.Channel, b.CheckIn, b.CheckOut = r.PostForm.Get("kind"), r.PostForm.Get("channel"), r.PostForm.Get("check_in"), r.PostForm.Get("check_out")
		unit, err := strconv.ParseInt(r.PostForm.Get("unit_id"), 10, 64)
		if err != nil || unit <= 0 {
			s.bookingResult(w, r, data, booking.ErrInvalid)
			return
		}
		b.UnitID = unit
	}
	b.GuestID = 0
	if raw := r.PostForm.Get("guest_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			s.bookingResult(w, r, data, booking.ErrInvalid)
			return
		}
		b.GuestID = id
	}
	data.NewGuest = person.Details{Name: r.PostForm.Get("guest_name"), Email: r.PostForm.Get("guest_email"), Phone: r.PostForm.Get("guest_phone")}
	details := booking.Details{UnitID: b.UnitID, Kind: b.Kind, CheckIn: b.CheckIn, CheckOut: b.CheckOut, Channel: b.Channel, GuestID: b.GuestID, Notes: b.Notes}
	if data.NewGuest.Name != "" || data.NewGuest.Email != "" || data.NewGuest.Phone != "" {
		details.NewGuest = &data.NewGuest
	}
	var err error
	if b.ID == 0 {
		b.ID, err = booking.Create(r.Context(), s.database, details)
	} else if b.Imported {
		err = booking.UpdateImported(r.Context(), s.database, b.ID, booking.OwnerDetails{GuestID: details.GuestID, NewGuest: details.NewGuest, Notes: details.Notes})
	} else {
		err = booking.Update(r.Context(), s.database, b.ID, details)
	}
	if err != nil {
		s.bookingResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/bookings/%d", b.ID), http.StatusSeeOther)
}

func (s *server) cancelBooking(w http.ResponseWriter, r *http.Request, cancelled bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data := pageData{Page: "edit_booking"}
	if err := s.bookingChoices(r.Context(), &data); err != nil {
		s.fail(w, err)
		return
	}
	data.Booking, err = booking.Get(r.Context(), s.database, id)
	if err != nil {
		s.bookingResult(w, r, data, err)
		return
	}
	if err := booking.SetCancelled(r.Context(), s.database, id, cancelled); err != nil {
		s.bookingResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/bookings/%d", id), http.StatusSeeOther)
}

func (s *server) bookingResult(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	var conflict *booking.ConflictError
	status := http.StatusBadRequest
	switch {
	case errors.As(err, &conflict):
		data.Error = "booking_conflict"
		data.Conflicts = bookingRows(conflict.Bookings, data)
		status = http.StatusConflict
	case errors.Is(err, booking.ErrNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, booking.ErrInvalid):
		data.Error = "invalid_booking"
	case errors.Is(err, booking.ErrImported):
		data.Error = "imported_booking_readonly"
	case errors.Is(err, person.ErrName):
		data.Error = "person_name_required"
	default:
		s.fail(w, err)
		return
	}
	s.render(w, r, data, status)
}

func (s *server) calendar(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "calendar"}
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	start := today.MonthStart()
	if month := r.URL.Query().Get("month"); month != "" {
		start, err = civildate.Parse(month + "-01")
		if err != nil {
			data.Error = "invalid_month"
			s.render(w, r, data, http.StatusBadRequest)
			return
		}
	}
	data.Month = start.String()[:7]
	if start.Year() > 1 || start.Month() > 1 {
		data.PreviousMonth = start.AddDays(-1).String()[:7]
	}
	end := start
	for end.Month() == start.Month() && end.Year() == start.Year() {
		data.Days = append(data.Days, calendarDay{Date: end.String(), Number: end.Day(), Today: end.Compare(today) == 0})
		end = end.AddDays(1)
	}
	if end.Year() <= 9999 {
		data.NextMonth = end.String()[:7]
	}
	monthRange, err := occupancy.Stay(start, end)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.bookingChoices(r.Context(), &data); err != nil {
		s.fail(w, err)
		return
	}
	values, err := booking.List(r.Context(), s.database, booking.Filter{Status: booking.StatusConfirmed})
	if err != nil {
		s.fail(w, err)
		return
	}
	type occupied struct {
		row      bookingRow
		interval occupancy.Range
	}
	byUnit := make(map[int64][]occupied)
	for _, row := range bookingRows(values, data) {
		checkIn, err := civildate.Parse(row.CheckIn)
		if err != nil {
			s.fail(w, err)
			return
		}
		checkOut, err := civildate.Parse(row.CheckOut)
		if err != nil {
			s.fail(w, err)
			return
		}
		interval, err := occupancy.Stay(checkIn, checkOut)
		if err != nil {
			s.fail(w, err)
			return
		}
		if interval.Overlaps(monthRange) {
			byUnit[row.UnitID] = append(byUnit[row.UnitID], occupied{row, interval})
		}
	}
	for unitID, items := range byUnit {
		for i := range items {
			for j := i + 1; j < len(items); j++ {
				if items[i].interval.Overlaps(items[j].interval) {
					items[i].row.Conflict = true
					items[j].row.Conflict = true
				}
			}
		}
		byUnit[unitID] = items
	}
	properties, err := property.List(r.Context(), s.database, false)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, p := range properties {
		group := calendarProperty{Name: p.Name}
		units, err := property.ListUnits(r.Context(), s.database, p.ID, false)
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, u := range units {
			row := calendarUnit{ID: u.ID, Name: u.Name}
			for i := 0; i < len(data.Days); {
				day := start.AddDays(i)
				dayRange, err := occupancy.Stay(day, day.AddDays(1))
				if err != nil {
					s.fail(w, err)
					return
				}
				cell := calendarCell{calendarDay: data.Days[i], Span: 1}
				for _, b := range byUnit[u.ID] {
					if b.interval.Overlaps(dayRange) {
						cell.Bookings = append(cell.Bookings, b.row)
					}
				}
				cell.Conflict = len(cell.Bookings) > 1
				// Merge only adjacent days with the same occupants, so overlapping imports
				// all remain visible and a conflict never hides the second booking.
				for len(cell.Bookings) > 0 && i+cell.Span < len(data.Days) {
					next := day.AddDays(cell.Span)
					nextRange, err := occupancy.Stay(next, next.AddDays(1))
					if err != nil {
						s.fail(w, err)
						return
					}
					var nextBookings []bookingRow
					for _, b := range byUnit[u.ID] {
						if b.interval.Overlaps(nextRange) {
							nextBookings = append(nextBookings, b.row)
						}
					}
					same := len(nextBookings) == len(cell.Bookings)
					if same {
						for j := range nextBookings {
							if nextBookings[j].ID != cell.Bookings[j].ID {
								same = false
								break
							}
						}
					}
					if !same {
						break
					}
					cell.Span++
				}
				row.Cells = append(row.Cells, cell)
				i += cell.Span
			}
			group.Units = append(group.Units, row)
		}
		data.CalendarProperties = append(data.CalendarProperties, group)
	}
	s.render(w, r, data, http.StatusOK)
}
