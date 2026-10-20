package web

import (
	"net/http"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/occupancy"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

type todayPage struct {
	TodayDate       string
	Turnovers       booking.TurnoverSummary
	OverdueTenants  []ledger.OverdueLease
	DueTasks        []task.Task
	ProblemFeeds    []ical.ProblemFeed
	FutureConflicts []occupancy.Conflict
	YTD             ledger.YearReport
}

func (s *server) today(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "today"}
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.TodayDate = today.String()
	data.Turnovers, err = booking.Turnovers(r.Context(), s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.OverdueTenants, err = ledger.Overdue(r.Context(), s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.DueTasks, err = task.List(r.Context(), s.database, task.Filter{Status: task.StatusOpen, Due: task.DueThroughToday, Today: today})
	if err != nil {
		s.fail(w, err)
		return
	}
	data.ProblemFeeds, err = ical.ProblemFeeds(r.Context(), s.database, s.now())
	if err != nil {
		s.fail(w, err)
		return
	}
	data.FutureConflicts, err = occupancy.FutureConflicts(r.Context(), s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.YTD, err = ledger.ReportToDate(r.Context(), s.database, today)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, data, http.StatusOK)
}
