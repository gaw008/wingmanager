// Package ical owns calendar feeds and coordinates atomic snapshot imports.
package ical

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/settings"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	// FetchTimeout bounds a complete calendar request, including reading its body.
	FetchTimeout = 30 * time.Second
	// MaxFeedBytes allows large rental calendars while bounding response memory.
	MaxFeedBytes = 4 << 20
)

var (
	ErrDuplicate = errors.New("calendar feed URL already exists for this unit")
	ErrInvalid   = errors.New("invalid calendar feed: choose a channel and an HTTP(S) URL")
	ErrNotFound  = errors.New("calendar feed not found")
	ErrStale     = errors.New("calendar snapshot discarded: feed generation changed")
	ErrEmptyFeed = errors.New("warning: empty calendar; future confirmed bookings preserved")
)

// CalendarFeed is an external calendar source. Empty sync timestamps mean no
// attempt or success yet; an empty LastError means no recorded error.
type CalendarFeed struct {
	ID, UnitID, SyncGeneration              int64
	Channel, URL                            string
	LastAttemptAt, LastSuccessAt, LastError string
}

// Channels reuses the booking channel vocabulary, excluding manual-only direct.
func Channels() []string {
	return slices.DeleteFunc(booking.Channels(), func(channel string) bool {
		return channel == booking.ChannelDirect
	})
}

// Feed details use a fixed unit. Editing channel or URL invalidates in-flight
// snapshots but retains all imported history until the next successful sync.
func Add(ctx context.Context, database *sql.DB, unitID int64, channel, rawURL string) (int64, error) {
	if err := validateFeed(channel, rawURL); err != nil {
		return 0, err
	}
	id, err := New(database).addFeed(ctx, addFeedParams{UnitID: unitID, Channel: channel, Url: rawURL})
	return id, feedWriteError(err)
}

func Edit(ctx context.Context, database *sql.DB, id int64, channel, rawURL string) error {
	if err := validateFeed(channel, rawURL); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := New(tx).getFeed(ctx, id); err != nil {
		return feedError(err)
	}
	if err := New(tx).editFeed(ctx, editFeedParams{ID: id, Channel: channel, Url: rawURL}); err != nil {
		return feedWriteError(err)
	}
	return tx.Commit()
}

// Remove atomically transfers bookings to the owner, then removes their feed.
func Remove(ctx context.Context, database *sql.DB, id int64) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := New(tx).getFeed(ctx, id); err != nil {
		return feedError(err)
	}
	if err := booking.DetachFeed(ctx, tx, id); err != nil {
		return err
	}
	if err := New(tx).removeFeed(ctx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func Get(ctx context.Context, database *sql.DB, id int64) (CalendarFeed, error) {
	value, err := New(database).getFeed(ctx, id)
	return fromRecord(value), feedError(err)
}

func List(ctx context.Context, database *sql.DB) ([]CalendarFeed, error) {
	records, err := New(database).listFeeds(ctx)
	if err != nil {
		return nil, err
	}
	var feeds []CalendarFeed
	for _, record := range records {
		feeds = append(feeds, fromRecord(record))
	}
	return feeds, nil
}

func fromRecord(value feedRecord) CalendarFeed {
	return CalendarFeed{
		ID: value.ID, UnitID: value.UnitID, Channel: value.Channel, URL: value.Url,
		SyncGeneration: value.SyncGeneration, LastAttemptAt: value.LastAttemptAt.String,
		LastSuccessAt: value.LastSuccessAt.String, LastError: value.LastError.String,
	}
}

func validateFeed(channel, rawURL string) error {
	value, err := url.Parse(rawURL)
	if err != nil || value.Host == "" || (value.Scheme != "http" && value.Scheme != "https") || !slices.Contains(Channels(), channel) {
		return ErrInvalid
	}
	return nil
}

func feedError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type flight struct {
	done chan struct{}
	err  error
}

// Service is shared by the scheduler and HTTP handlers. Concurrent Sync callers
// for one feed reuse the in-flight result; different feeds have independent flights.
type Service struct {
	database *sql.DB
	client   *http.Client
	now      func() time.Time
	mu       sync.Mutex
	flights  map[int64]*flight
}

// NewService defaults to FetchTimeout for headers and body reads. A supplied
// client controls its own timeout. The clock determines the current local date.
func NewService(database *sql.DB, client *http.Client, now func() time.Time) *Service {
	if client == nil {
		client = &http.Client{Timeout: FetchTimeout}
	}
	return &Service{database: database, client: client, now: now, flights: make(map[int64]*flight)}
}

// Sync fetches outside the write transaction, then applies one complete snapshot.
// A waiting caller can cancel its wait without cancelling the leader's request.
func (s *Service) Sync(ctx context.Context, id int64) error {
	s.mu.Lock()
	if f, ok := s.flights[id]; ok {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.done:
			return f.err
		}
	}
	f := &flight{done: make(chan struct{})}
	s.flights[id] = f
	s.mu.Unlock()
	err := s.sync(ctx, id)
	s.mu.Lock()
	f.err = err
	delete(s.flights, id)
	close(f.done)
	s.mu.Unlock()
	return err
}

func (s *Service) sync(ctx context.Context, id int64) error {
	feed, err := Get(ctx, s.database, id)
	if err != nil {
		return err
	}
	attempted := s.now().UTC().Format(time.RFC3339Nano)
	err = s.fetchAndApply(ctx, feed, attempted)
	if err == nil {
		return nil
	}
	// Failures are recorded after rollback, including a cancelled network request.
	// The generation predicate prevents old failures overwriting a newer sync/edit.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FetchTimeout)
	defer cancel()
	recordErr := New(s.database).syncFailed(recordCtx, syncFailedParams{ID: id, SyncGeneration: feed.SyncGeneration, LastAttemptAt: sql.NullString{String: attempted, Valid: true}, LastError: sql.NullString{String: err.Error(), Valid: true}})
	return errors.Join(err, recordErr)
}

func (s *Service) fetchAndApply(ctx context.Context, feed CalendarFeed, attempted string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("calendar HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxFeedBytes+1))
	if err != nil {
		return err
	}
	if len(body) > MaxFeedBytes {
		return fmt.Errorf("calendar exceeds %d bytes", MaxFeedBytes)
	}
	value, err := settings.Get(ctx, s.database)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(value.Timezone)
	if err != nil {
		return err
	}
	events, err := parse(bytes.NewReader(body), feed.Channel, location)
	if err != nil {
		return err
	}
	today, err := settings.Today(ctx, s.database, s.now)
	if err != nil {
		return err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	current, err := q.getFeed(ctx, feed.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return err
	}
	if current.SyncGeneration != feed.SyncGeneration {
		return ErrStale
	}
	if len(events) == 0 {
		count, err := q.futureConfirmedCount(ctx, futureConfirmedCountParams{FeedID: sql.NullInt64{Int64: feed.ID, Valid: true}, CheckOut: today.String()})
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrEmptyFeed
		}
	}
	if err := booking.ApplySnapshot(ctx, tx, feed.ID, feed.UnitID, feed.Channel, events, today); err != nil {
		return err
	}
	if err := q.syncSucceeded(ctx, syncSucceededParams{ID: feed.ID, LastAttemptAt: sql.NullString{String: attempted, Valid: true}, LastSuccessAt: sql.NullString{String: s.now().UTC().Format(time.RFC3339Nano), Valid: true}}); err != nil {
		return err
	}
	return tx.Commit()
}

func feedWriteError(err error) error {
	var constraint *sqlite.Error
	if errors.As(err, &constraint) && constraint.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return ErrDuplicate
	}
	return err
}
