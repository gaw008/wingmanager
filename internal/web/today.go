package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

type reservationCard struct {
	booking.Reservation
	Status, StatusClass, GuestLabel, Initials, Color string
}
type reservationTab struct {
	Key, Label string
	Count      int
	Selected   bool
}
type currentLeaseCard struct {
	lease.CurrentLease
	ledger.Summary
}
type todayPage struct {
	TodayDate, Greeting, TodayHeading, ReservationTab string
	Reservations                                      booking.Reservations
	ReservationCards                                  []reservationCard
	ReservationTabs                                   []reservationTab
	OverdueTotals                                     ledger.OverdueSummary
	DueTasks                                          []task.Task
	DueTaskLabels                                     map[int64]string
	ProblemFeeds                                      []ical.ProblemFeed
	YTD                                               ledger.YearReport
	CurrentLeases                                     []currentLeaseCard
	VacantUnits                                       []property.VacantUnit
	AttentionCount                                    int
}

func (s *server) today(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "today"}
	ctx := r.Context()
	value, err := settings.Get(ctx, s.database)
	if err != nil {
		s.fail(w, err)
		return
	}
	location, err := time.LoadLocation(value.Timezone)
	if err != nil {
		s.fail(w, err)
		return
	}
	now := s.now()
	local := now.In(location)
	today, err := settings.Today(ctx, s.database, func() time.Time { return now })
	if err != nil {
		s.fail(w, err)
		return
	}
	data.TodayDate = today.String()
	greeting := "greeting_evening"
	if local.Hour() < 12 {
		greeting = "greeting_morning"
	} else if local.Hour() < 18 {
		greeting = "greeting_afternoon"
	}
	name := value.LandlordName
	if name == "" {
		name = catalogs[value.Locale]["landlord"]
	}
	data.Greeting = fmt.Sprintf(catalogs[value.Locale][greeting], name)
	data.Reservations, err = booking.TodayReservations(ctx, s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	dateLabel, err := displayDate(value.Locale, today.Year(), today.String())
	if err != nil {
		s.fail(w, err)
		return
	}
	if value.Locale == settings.LocaleChinese {
		dateLabel += fmt.Sprintf(" 星期%s", []string{"日", "一", "二", "三", "四", "五", "六"}[local.Weekday()])
	} else {
		dateLabel += local.Format(", Monday")
	}

	data.TodayHeading = fmt.Sprintf(catalogs[value.Locale]["today_summary"], dateLabel, data.Reservations.CheckInCount, data.Reservations.CheckOutCount)
	selected := r.URL.Query().Get("tab")
	switch selected {
	case "out", "in", "stay", "soon":
	default:
		selected = "out"
	}
	data.ReservationTab = selected
	tabs := []struct {
		key, label string
		list       []booking.Reservation
		count      int
	}{
		{"out", "today_check_outs", data.Reservations.CheckOuts, data.Reservations.CheckOutCount},
		{"in", "today_check_ins", data.Reservations.CheckIns, data.Reservations.CheckInCount},
		{"stay", "staying_now", data.Reservations.Staying, data.Reservations.StayingCount},
		{"soon", "coming_soon", data.Reservations.Upcoming, data.Reservations.UpcomingCount},
	}
	for _, tab := range tabs {
		data.ReservationTabs = append(data.ReservationTabs, reservationTab{Key: tab.key, Label: catalogs[value.Locale][tab.label], Count: tab.count, Selected: tab.key == selected})
		if tab.key != selected {
			continue
		}
		for _, stay := range tab.list {
			card := reservationCard{Reservation: stay, Status: catalogs[value.Locale][tab.label]}
			switch selected {
			case "out", "in":
				card.StatusClass = selected
			case "soon":
				key := "arrives_in_days"
				if stay.Departing {
					key = "departs_in_days"
				}
				card.Status = fmt.Sprintf(catalogs[value.Locale][key], stay.EventDays)
			}
			card.GuestLabel = stay.GuestName
			if card.GuestLabel == "" {
				card.GuestLabel = catalogs[value.Locale]["unnamed_guest"]
			}
			card.Initials, card.Color = person.Appearance(card.GuestLabel)
			data.ReservationCards = append(data.ReservationCards, card)
		}
	}
	data.OverdueTotals, err = ledger.OverdueTotals(ctx, s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.DueTasks, err = task.List(ctx, s.database, task.Filter{Status: task.StatusOpen, Due: task.DueThroughToday, Today: today})
	if err != nil {
		s.fail(w, err)
		return
	}
	data.DueTaskLabels = make(map[int64]string)
	for _, due := range data.DueTasks {
		if due.Kind != task.KindCleaning {
			continue
		}
		p, err := property.Get(ctx, s.database, due.PropertyID)
		if err != nil {
			s.fail(w, err)
			return
		}
		label := p.Name
		if due.UnitID != 0 {
			u, err := property.GetUnit(ctx, s.database, due.UnitID)
			if err != nil {
				s.fail(w, err)
				return
			}
			label += " · " + u.Name
		}
		data.DueTaskLabels[due.ID] = label
	}
	data.ProblemFeeds, err = ical.ProblemFeeds(ctx, s.database, now)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.AttentionCount = data.OverdueTotals.Count + len(data.DueTasks) + len(data.ProblemFeeds)
	data.YTD, err = ledger.ReportToDate(ctx, s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	current, err := lease.Current(ctx, s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, currentLease := range current {
		balance, err := ledger.Balance(ctx, s.database, currentLease.ID, today)
		if err != nil {
			s.fail(w, err)
			return
		}
		data.CurrentLeases = append(data.CurrentLeases, currentLeaseCard{CurrentLease: currentLease, Summary: balance})
	}
	data.VacantUnits, err = property.Vacant(ctx, s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, data, http.StatusOK)
}
