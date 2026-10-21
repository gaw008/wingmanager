package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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
	State                booking.ViewState
	Initials             string
}

// bookingPage holds the booking forms and the calendar derived from existing records.
type bookingPage struct {
	Booking                    booking.Booking
	BookingReadOnly            bool
	BookingDetail              bookingRow
	BookingTab, BookingSection string
	BookingTabs                []reservationTab
	Cleaning                   *task.Task
	SourceURL                  string
	NewGuest                   person.Details
	BookingRows, Conflicts     []bookingRow
	UnitChoices                []unitChoice
	Kinds, Statuses, Channels  []string
	UnitFilter                 int64
	PeriodFilter, StatusFilter string
	Calendar                   occupancy.CalendarView
	CalendarPropertyFilter     int64
	CalendarMonthLabel         string
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

func (s *server) contactAndUnitChoices(ctx context.Context, data *pageData) error {
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
	s.renderBookingList(w, r, pageData{})
}

func (s *server) renderBookingList(w http.ResponseWriter, r *http.Request, data pageData) {
	data.Page = "bookings"
	data.Statuses = booking.Statuses()
	if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
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

	data.BookingSection = query.Get("section")
	data.BookingTab = query.Get("tab")
	if data.BookingTab == "" && data.PeriodFilter == "" && data.StatusFilter == "" {
		data.BookingTab = booking.ViewUpcoming
	}
	for _, tab := range []struct{ key, label string }{{booking.ViewUpcoming, "coming_soon"}, {booking.ViewInHouse, "staying_now"}, {booking.ViewEnded, "booking_ended"}, {booking.ViewCancelled, "cancelled"}} {
		data.BookingTabs = append(data.BookingTabs, reservationTab{Key: tab.key, Label: tab.label, Selected: tab.key == data.BookingTab})
	}
	values, err := booking.List(r.Context(), s.database, booking.Filter{
		UnitID: data.UnitFilter, Status: data.StatusFilter, Period: data.PeriodFilter, View: data.BookingTab, Today: today,
	})
	if err != nil {
		s.bookingResult(w, r, data, err)
		return
	}
	data.BookingRows = bookingRows(values, data)
	for i := range data.BookingRows {
		row := &data.BookingRows[i]
		row.State, err = row.Booking.State(today)
		if err != nil {
			s.fail(w, err)
			return
		}
		row.Initials, _ = person.Appearance(row.GuestName)
	}
	properties, err := property.List(r.Context(), s.database, true)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, p := range properties {
		units, err := property.ListUnits(r.Context(), s.database, p.ID, true)
		if err != nil {
			s.fail(w, err)
			return
		}
		data.Units = append(data.Units, units...)
	}
	if err := s.propertyFeeds(r, &data); err != nil {
		s.fail(w, err)
		return
	}
	status := http.StatusOK
	if data.Error != "" {
		status = http.StatusBadRequest
	}
	s.render(w, r, data, status)
}

func (s *server) editBooking(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "new_booking"}
	data.Kinds, data.Statuses, data.Channels = booking.Kinds(), booking.Statuses(), booking.Channels()
	if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
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
		data.BookingReadOnly = r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, "/edit")
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
			data.SourceURL = feed.URL
		}
	} else {
		data.Booking.Kind, data.Booking.Channel, data.Booking.Status = booking.KindStay, booking.ChannelDirect, booking.StatusConfirmed
	}
	if data.BookingReadOnly {
		today, err := settings.Today(r.Context(), s.database, s.now)
		if err != nil {
			s.fail(w, err)
			return
		}
		data.BookingDetail = bookingRows([]booking.Booking{data.Booking}, data)[0]
		data.BookingDetail.State, err = data.Booking.State(today)
		if err != nil {
			s.fail(w, err)
			return
		}
		data.BookingDetail.Initials, _ = person.Appearance(data.BookingDetail.GuestName)
		data.BookingDetail.Conflict, err = booking.HasConflict(r.Context(), s.database, data.Booking)
		if err != nil {
			s.fail(w, err)
			return
		}
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
	data.Kinds, data.Statuses, data.Channels = booking.Kinds(), booking.Statuses(), booking.Channels()
	if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
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
		if len(conflict.LeaseIDs) > 0 {
			data.Error = "booking_lease_conflict"
		}
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
	month := today.MonthStart()
	if raw := r.URL.Query().Get("month"); raw != "" {
		month, err = civildate.Parse(raw + "-01")
		if err != nil {
			data.Error = "invalid_month"
			s.render(w, r, data, http.StatusBadRequest)
			return
		}
	}
	if raw := r.URL.Query().Get("property"); raw != "" {
		data.CalendarPropertyFilter, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || data.CalendarPropertyFilter <= 0 {
			data.Error = "invalid_form"
			s.render(w, r, data, http.StatusBadRequest)
			return
		}
	}
	data.Properties, err = property.List(r.Context(), s.database, false)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.Calendar, err = occupancy.Calendar(r.Context(), s.database, month, today, data.CalendarPropertyFilter)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Formatting is presentation only; all positions and occupancy rules are in occupancy.
	config, err := settings.Get(r.Context(), s.database)
	if err != nil {
		s.fail(w, err)
		return
	}
	if config.Locale == settings.LocaleChinese {
		data.CalendarMonthLabel = fmt.Sprintf("%d 年 %d 月", month.Year(), month.Month())
	} else {
		data.CalendarMonthLabel = month.MidnightUTC().Format("January 2006")
	}
	s.render(w, r, data, http.StatusOK)
}
