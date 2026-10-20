package web

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/property"
)

type feedPage struct {
	Feeds        map[int64][]ical.CalendarFeed
	ExportURLs   map[int64]string
	FeedChannels []string
}

func (s *server) registerFeedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ical/{file}", s.exportCalendar)
	mux.HandleFunc("POST /units/{id}/feeds", s.addFeed)
	for _, action := range []string{"edit", "remove", "sync"} {
		mux.HandleFunc("POST /feeds/{id}/"+action, func(w http.ResponseWriter, r *http.Request) { s.changeFeed(w, r, action) })
	}
}

func (s *server) propertyFeeds(r *http.Request, data *pageData) error {
	feeds, err := ical.List(r.Context(), s.database)
	if err != nil {
		return err
	}
	data.FeedChannels = ical.Channels()
	data.Feeds = make(map[int64][]ical.CalendarFeed)
	data.ExportURLs = make(map[int64]string)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	for _, unit := range data.Units {
		data.ExportURLs[unit.ID] = scheme + "://" + r.Host + "/ical/" + unit.IcalExportToken + ".ics"
	}
	for _, feed := range feeds {
		data.Feeds[feed.UnitID] = append(data.Feeds[feed.UnitID], feed)
	}
	return nil
}

func (s *server) addFeed(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	unit, err := property.GetUnit(r.Context(), s.database, id)
	if err != nil {
		s.renderDomainResult(w, r, pageData{}, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderPropertyDetail(w, r, unit.PropertyID, pageData{Error: "invalid_form"}, nil)
		return
	}
	_, err = ical.Add(r.Context(), s.database, id, r.PostForm.Get("channel"), r.PostForm.Get("url"))
	s.feedResult(w, r, unit.PropertyID, err)
}

func (s *server) changeFeed(w http.ResponseWriter, r *http.Request, action string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	feed, err := ical.Get(r.Context(), s.database, id)
	if errors.Is(err, ical.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	unit, err := property.GetUnit(r.Context(), s.database, feed.UnitID)
	if err != nil {
		s.renderDomainResult(w, r, pageData{}, err)
		return
	}
	switch action {
	case "edit":
		if err := r.ParseForm(); err != nil {
			s.renderPropertyDetail(w, r, unit.PropertyID, pageData{Error: "invalid_form"}, nil)
			return
		}
		err = ical.Edit(r.Context(), s.database, id, r.PostForm.Get("channel"), r.PostForm.Get("url"))
	case "remove":
		err = ical.Remove(r.Context(), s.database, id)
	case "sync":
		err = s.ical.Sync(r.Context(), id)
		if err != nil {
			log.Printf("sync feed %d: %v", id, err)
		}
		// Sync failures and warnings are persisted on the feed and shown after redirect.
		http.Redirect(w, r, fmt.Sprintf("/properties/%d", unit.PropertyID), http.StatusSeeOther)
		return
	}
	s.feedResult(w, r, unit.PropertyID, err)
}

func (s *server) feedResult(w http.ResponseWriter, r *http.Request, propertyID int64, err error) {
	if err != nil {
		if errors.Is(err, ical.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		data := pageData{}
		switch {
		case errors.Is(err, ical.ErrInvalid):
			data.Error = "invalid_feed"
		case errors.Is(err, ical.ErrDuplicate):
			data.Error = "duplicate_feed"
		default:
			s.fail(w, err)
			return
		}
		s.renderPropertyDetail(w, r, propertyID, data, nil)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/properties/%d", propertyID), http.StatusSeeOther)
}

func (s *server) exportCalendar(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if !strings.HasSuffix(file, ".ics") {
		http.NotFound(w, r)
		return
	}
	var body bytes.Buffer
	err := ical.Export(r.Context(), s.database, strings.TrimSuffix(file, ".ics"), s.now, &body)
	if errors.Is(err, ical.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := body.WriteTo(w); err != nil {
		log.Printf("write calendar: %v", err)
	}
}
