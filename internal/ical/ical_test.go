package ical

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goical "github.com/emersion/go-ical"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/task"
)

func clock() time.Time { return time.Date(2026, 10, 20, 1, 30, 0, 0, time.UTC) }

func setup(t *testing.T) (*sql.DB, int64, *Service) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "ical.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	id, err := property.Create(t.Context(), database, property.Details{Name: "Test home"}, "Whole home")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, id, false)
	if err != nil || len(units) != 1 {
		t.Fatalf("units=%v err=%v", units, err)
	}
	return database, units[0].ID, NewService(database, nil, clock)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func feedServer(t *testing.T, body *[]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		if _, err := w.Write(*body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func add(t *testing.T, database *sql.DB, unit int64, channel, url string) int64 {
	t.Helper()
	id, err := Add(t.Context(), database, unit, channel, url)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func syncOK(t *testing.T, s *Service, id int64) {
	t.Helper()
	if err := s.Sync(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}
func bookings(t *testing.T, database *sql.DB) []booking.Booking {
	t.Helper()
	values, err := booking.List(t.Context(), database, booking.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return values
}
func cleaning(t *testing.T, database *sql.DB, id int64) *task.Task {
	t.Helper()
	value, err := task.CleaningForBooking(t.Context(), database, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func readFeed(t *testing.T, database *sql.DB, id int64) CalendarFeed {
	t.Helper()
	f, err := Get(t.Context(), database, id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func requireCount(t *testing.T, database *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := database.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s rows=%d want=%d", table, got, want)
	}
}

func TestNormalImportAndIdenticalSync(t *testing.T) {
	for _, sample := range []struct {
		file, channel string
		wantKinds     []string
		from, to      string
	}{
		{"airbnb.ics", booking.ChannelAirbnb, []string{booking.KindStay, booking.KindStay, booking.KindBlock}, "2026-10-01", "2026-10-03"},
		{"vrbo.ics", booking.ChannelVRBO, []string{booking.KindStay, booking.KindBlock}, "2026-11-05", "2026-11-07"},
		{"booking.ics", booking.ChannelBooking, []string{booking.KindStay}, "2026-11-12", "2026-11-14"},
		{"dates.ics", booking.ChannelOther, []string{booking.KindStay, booking.KindBlock, booking.KindBlock, booking.KindBlock}, "2026-11-01", "2026-11-02"},
	} {
		t.Run(sample.file, func(t *testing.T) {
			database, unit, s := setup(t)
			body := fixture(t, sample.file)
			server := feedServer(t, &body)
			id := add(t, database, unit, sample.channel, server.URL)
			syncOK(t, s, id)
			before := bookings(t, database)
			if len(before) != len(sample.wantKinds) {
				t.Fatalf("bookings=%+v", before)
			}
			tasks := map[int64]*task.Task{}
			for i, b := range before {
				if b.Kind != sample.wantKinds[i] || !b.Imported || b.Channel != sample.channel || b.Status != booking.StatusConfirmed {
					t.Fatalf("booking=%+v", b)
				}
				tasks[b.ID] = cleaning(t, database, b.ID)
				if b.Kind == booking.KindStay && (tasks[b.ID] == nil || tasks[b.ID].DueOn.String != b.CheckOut || tasks[b.ID].Title != "") {
					t.Fatalf("cleaning=%+v", tasks[b.ID])
				}
				if b.Kind == booking.KindBlock && tasks[b.ID] != nil {
					t.Fatal("block has a task")
				}
			}
			if before[0].CheckIn != sample.from || before[0].CheckOut != sample.to {
				t.Fatalf("dates=%+v", before[0])
			}
			if sample.file == "booking.ics" && before[0].ExternalUID != "booking-confirmed-long-identifier@booking.example" {
				t.Fatal("folded UID not decoded")
			}
			if sample.file == "dates.ics" {
				for i, want := range []string{"2026-11-02", "2026-11-03", "2026-11-07", "2026-11-11"} {
					if before[i].CheckOut != want {
						t.Fatalf("date event %+v", before[i])
					}
				}
			}
			// These triggers distinguish a true no-op from a rewrite within the same
			// SQLite timestamp second, without sleeping or mocking the database clock.
			if _, err := database.Exec(`CREATE TRIGGER no_booking_rewrite BEFORE UPDATE ON bookings BEGIN SELECT RAISE(ABORT,'identical booking rewritten'); END;
   CREATE TRIGGER no_task_rewrite BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'identical task rewritten'); END;`); err != nil {
				t.Fatal(err)
			}
			syncOK(t, s, id)
			after := bookings(t, database)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("identical sync changed bookings: before=%+v after=%+v", before, after)
			}
			for _, b := range after {
				if !reflect.DeepEqual(tasks[b.ID], cleaning(t, database, b.ID)) {
					t.Fatal("identical sync changed tasks")
				}
			}
			if f := readFeed(t, database, id); f.SyncGeneration != 2 || !f.LastSuccessAt.Valid || f.LastError.Valid {
				t.Fatalf("feed=%+v", f)
			}
		})
	}
}

func TestDisappearanceDateChangeReappearanceAndOwnerFields(t *testing.T) {
	database, unit, s := setup(t)
	initial := fixture(t, "airbnb.ics")
	body := initial
	server := feedServer(t, &body)
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	syncOK(t, s, id)
	before := bookings(t, database)
	past, future := before[0], before[1]
	guest, err := person.Create(t.Context(), database, person.Details{Name: "Private guest"})
	if err != nil {
		t.Fatal(err)
	}
	if err := booking.UpdateImported(t.Context(), database, future.ID, booking.OwnerDetails{GuestID: guest, Notes: "Owner-only notes"}); err != nil {
		t.Fatal(err)
	}
	changed := bytes.ReplaceAll(initial, []byte("20261104"), []byte("20261105"))
	body = changed
	syncOK(t, s, id)
	updated := bookings(t, database)[1]
	if updated.CheckOut != "2026-11-05" || updated.GuestID != guest || updated.Notes != "Owner-only notes" || cleaning(t, database, future.ID).DueOn.String != "2026-11-05" {
		t.Fatalf("updated=%+v", updated)
	}
	// Retain one block, so disappearance is distinct from the empty-feed guard.
	re := regexp.MustCompile(`(?s)BEGIN:VEVENT\nUID:(past-stay|future-stay).*?END:VEVENT\n`)
	body = re.ReplaceAll(initial, nil)
	syncOK(t, s, id)
	after := bookings(t, database)
	if after[0] != past || after[1].Status != booking.StatusCancelled || cleaning(t, database, future.ID).Status != task.StatusCancelled {
		t.Fatalf("after removal=%+v", after)
	}
	body = initial
	syncOK(t, s, id)
	restored := bookings(t, database)[1]
	if restored.ID != future.ID || restored.Status != booking.StatusConfirmed || restored.GuestID != guest || restored.Notes != "Owner-only notes" || cleaning(t, database, future.ID).Status != task.StatusOpen || cleaning(t, database, future.ID).DueOn.String != future.CheckOut {
		t.Fatalf("restored=%+v", restored)
	}
	requireCount(t, database, "tasks", 2)
	// A malformed event must invalidate the whole snapshot before any writes.
	before = bookings(t, database)
	body = fixture(t, "malformed.ics")
	if err := s.Sync(t.Context(), id); err == nil {
		t.Fatal("malformed snapshot accepted")
	}
	if !reflect.DeepEqual(before, bookings(t, database)) {
		t.Fatal("malformed snapshot partially applied")
	}
}

func TestScenarioOneAndSizeLimitPreserveBookings(t *testing.T) {
	for _, kind := range []string{"empty calendar", "HTML 200", "HTTP 500", "timeout", "oversize", "empty body", "malformed event"} {
		t.Run(kind, func(t *testing.T) {
			database, unit, s := setup(t)
			body := fixture(t, "airbnb.ics")
			normal := feedServer(t, &body)
			id := add(t, database, unit, booking.ChannelAirbnb, normal.URL)
			syncOK(t, s, id)
			before := bookings(t, database)
			oldTask := cleaning(t, database, before[1].ID)
			bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "empty calendar":
					_, _ = w.Write(fixture(t, "empty.ics"))
				case "HTML 200":
					_, _ = w.Write(fixture(t, "error.html"))
				case "HTTP 500":
					w.WriteHeader(500)
					_, _ = w.Write(fixture(t, "error.html"))
				case "timeout":
					<-r.Context().Done()
				case "oversize":
					_, _ = w.Write(bytes.Repeat([]byte(" "), MaxFeedBytes+1))
				case "malformed event":
					_, _ = w.Write(fixture(t, "malformed.ics"))
				}
			}))
			defer bad.Close()
			// Feed editing is itself versioned; capture its generation before the failed sync.
			if err := Edit(t.Context(), database, id, booking.ChannelAirbnb, bad.URL); err != nil {
				t.Fatal(err)
			}
			prior := readFeed(t, database, id)
			if kind == "timeout" {
				s.client = &http.Client{Timeout: 20 * time.Millisecond}
			}
			err := s.Sync(t.Context(), id)
			if err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			if kind == "empty calendar" && !errors.Is(err, ErrEmptyFeed) {
				t.Fatalf("warning=%v", err)
			}
			if !reflect.DeepEqual(before, bookings(t, database)) || !reflect.DeepEqual(oldTask, cleaning(t, database, before[1].ID)) {
				t.Fatal("failure changed occupancy or tasks")
			}
			f := readFeed(t, database, id)
			if f.SyncGeneration != prior.SyncGeneration || !f.LastError.Valid || !f.LastAttemptAt.Valid || f.LastSuccessAt != prior.LastSuccessAt {
				t.Fatalf("failure status=%+v", f)
			}
		})
	}
}

func TestPartialApplyRollsBackAndRecordsFailure(t *testing.T) {
	database, unit, s := setup(t)
	body := fixture(t, "airbnb.ics")
	server := feedServer(t, &body)
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	// The first event and its task are written before this trigger rejects event two.
	if _, err := database.Exec(`CREATE TRIGGER fail_second_task BEFORE INSERT ON tasks WHEN NEW.due_on = '2026-11-04' BEGIN SELECT RAISE(ABORT,'forced task failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(t.Context(), id); err == nil {
		t.Fatal("forced failure accepted")
	}
	requireCount(t, database, "bookings", 0)
	requireCount(t, database, "tasks", 0)
	f := readFeed(t, database, id)
	if f.SyncGeneration != 0 || f.LastSuccessAt.Valid || !f.LastError.Valid {
		t.Fatalf("feed=%+v", f)
	}
}

func TestStaleSnapshotAfterEditIsDiscarded(t *testing.T) {
	database, unit, s := setup(t)
	initial := fixture(t, "airbnb.ics")
	changed := bytes.ReplaceAll(initial, []byte("20261104"), []byte("20261105"))
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write(initial)
			return
		}
		close(entered)
		<-release
		_, _ = w.Write(changed)
	}))
	defer server.Close()
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	syncOK(t, s, id)
	before := bookings(t, database)
	oldTask := cleaning(t, database, before[1].ID)
	result := make(chan error, 1)
	go func() { result <- s.Sync(t.Context(), id) }()
	<-entered
	if err := Edit(t.Context(), database, id, booking.ChannelVRBO, server.URL); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, ErrStale) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, bookings(t, database)) || !reflect.DeepEqual(oldTask, cleaning(t, database, before[1].ID)) {
		t.Fatal("stale snapshot changed bookings or tasks")
	}
	if f := readFeed(t, database, id); f.SyncGeneration != 2 || f.LastError.Valid || f.LastAttemptAt.Valid {
		t.Fatalf("stale result overwrote edited status: %+v", f)
	}
}

// joinedContext observes a follower entering the single-flight wait without sleeps.
type joinedContext struct {
	context.Context
	joined chan struct{}
}

func (c joinedContext) Done() <-chan struct{} { close(c.joined); return c.Context.Done() }

func TestConcurrentSyncReusesSingleFlight(t *testing.T) {
	database, unit, s := setup(t)
	body := fixture(t, "airbnb.ics")
	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		_, _ = w.Write(body)
	}))
	defer server.Close()
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	results := make(chan error, 9)
	go func() { results <- s.Sync(t.Context(), id) }()
	<-entered
	for range 8 {
		joined := make(chan struct{})
		go func() { results <- s.Sync(joinedContext{t.Context(), joined}, id) }()
		<-joined
	}
	close(release)
	for range 9 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 || readFeed(t, database, id).SyncGeneration != 1 {
		t.Fatal("concurrent sync did not reuse flight")
	}
	requireCount(t, database, "bookings", 3)
	requireCount(t, database, "tasks", 2)
}

func TestFeedRemovalTransfersOwnershipAtomically(t *testing.T) {
	database, unit, s := setup(t)
	body := fixture(t, "airbnb.ics")
	server := feedServer(t, &body)
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	syncOK(t, s, id)
	before := bookings(t, database)
	oldTask := cleaning(t, database, before[1].ID)
	if _, err := database.Exec(`CREATE TRIGGER reject_remove BEFORE DELETE ON calendar_feeds BEGIN SELECT RAISE(ABORT,'forced removal failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := Remove(t.Context(), database, id); err == nil {
		t.Fatal("forced removal succeeded")
	}
	if !reflect.DeepEqual(before, bookings(t, database)) {
		t.Fatal("failed removal detached bookings")
	}
	if _, err := database.Exec(`DROP TRIGGER reject_remove`); err != nil {
		t.Fatal(err)
	}
	if err := Remove(t.Context(), database, id); err != nil {
		t.Fatal(err)
	}
	for _, b := range bookings(t, database) {
		if b.Imported || b.FeedID != 0 || b.ExternalUID != "" {
			t.Fatalf("not manual: %+v", b)
		}
	}
	if !reflect.DeepEqual(oldTask, cleaning(t, database, before[1].ID)) {
		t.Fatal("removal changed tasks")
	}
	if _, err := Get(t.Context(), database, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed feed: %v", err)
	}
	requireCount(t, database, "bookings", 3)
}

func TestExportPrivacyDatesAndStableUIDs(t *testing.T) {
	database, unit, s := setup(t)
	body := fixture(t, "airbnb.ics")
	server := feedServer(t, &body)
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	syncOK(t, s, id)
	guest, err := person.Create(t.Context(), database, person.Details{Name: "Never export this guest"})
	if err != nil {
		t.Fatal(err)
	}
	bID, err := booking.Create(t.Context(), database, booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-10-18", CheckOut: "2026-10-19", GuestID: guest, Notes: "Private notes"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := booking.Create(t.Context(), database, booking.Details{UnitID: unit, Kind: booking.KindBlock, Channel: booking.ChannelDirect, CheckIn: "2026-11-20", CheckOut: "2026-11-21"})
	if err != nil {
		t.Fatal(err)
	}
	if err := booking.SetCancelled(t.Context(), database, cancelled, true); err != nil {
		t.Fatal(err)
	}
	u, err := property.GetUnit(t.Context(), database, unit)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Export(t.Context(), database, u.IcalExportToken, clock, &output); err != nil {
		t.Fatal(err)
	}
	raw := output.String()
	if strings.Contains(raw, "Never export") || strings.Contains(raw, "Private") || strings.Contains(raw, "DESCRIPTION") {
		t.Fatalf("privacy leak: %s", raw)
	}
	calendar, err := goical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	if len(calendar.Events()) != 3 {
		t.Fatalf("export=%s", raw)
	} // Includes checkout today in Los Angeles (Oct 19).
	found := false
	for _, event := range calendar.Events() {
		summary, err := event.Props.Text(goical.PropSummary)
		if err != nil || summary != "Not available" {
			t.Fatalf("summary=%q err=%v", summary, err)
		}
		uid, _ := event.Props.Text(goical.PropUID)
		if uid == fmt.Sprintf("booking-%d@wingmanager", bID) {
			found = true
		}
		if event.Props.Get(goical.PropDateTimeStart).ValueType() != goical.ValueDate {
			t.Fatal("not an all-day event")
		}
	}
	if !found {
		t.Fatal("checkout today was excluded")
	}
	token, err := property.RegenerateToken(t.Context(), database, unit)
	if err != nil {
		t.Fatal(err)
	}
	if err := Export(t.Context(), database, u.IcalExportToken, clock, io.Discard); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token err=%v", err)
	}
	if err := Export(t.Context(), database, token, clock, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestImportedEditingCannotChangeOccupancy(t *testing.T) {
	database, unit, s := setup(t)
	body := fixture(t, "airbnb.ics")
	server := feedServer(t, &body)
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	syncOK(t, s, id)
	b := bookings(t, database)[1]
	if err := booking.Update(t.Context(), database, b.ID, booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-12-01", CheckOut: "2026-12-02"}); !errors.Is(err, booking.ErrImported) {
		t.Fatalf("date edit=%v", err)
	}
	if err := booking.SetCancelled(t.Context(), database, b.ID, true); !errors.Is(err, booking.ErrImported) {
		t.Fatalf("cancel=%v", err)
	}
	if err := booking.UpdateImported(t.Context(), database, b.ID, booking.OwnerDetails{NewGuest: &person.Details{Name: "Owner's guest"}, Notes: "Saved locally"}); err != nil {
		t.Fatal(err)
	}
	after, err := booking.Get(t.Context(), database, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CheckIn != b.CheckIn || after.CheckOut != b.CheckOut || after.Status != b.Status || after.GuestID == 0 || after.Notes != "Saved locally" {
		t.Fatalf("after=%+v", after)
	}
}

func TestClassification(t *testing.T) {
	for _, test := range []struct{ channel, summary, want string }{
		{booking.ChannelAirbnb, "Reserved", booking.KindStay}, {booking.ChannelAirbnb, "Airbnb (Not available)", booking.KindBlock},
		{booking.ChannelVRBO, "Blocked", booking.KindBlock}, {booking.ChannelVRBO, "Reserved", booking.KindStay}, {booking.ChannelVRBO, "Some reservation", booking.KindStay},
		{booking.ChannelBooking, "Blocked", booking.KindStay}, {booking.ChannelBooking, "Not available", booking.KindStay},
		{booking.ChannelOther, "bLoCk for maintenance", booking.KindBlock}, {booking.ChannelOther, "NOT AVAILABLE", booking.KindBlock}, {booking.ChannelOther, "Unavailable", booking.KindBlock}, {booking.ChannelOther, "Closed", booking.KindBlock}, {booking.ChannelOther, "Reserved", booking.KindStay},
	} {
		if got := classify(test.channel, test.summary); got != test.want {
			t.Fatalf("%s %q: %s want %s", test.channel, test.summary, got, test.want)
		}
	}
}

func TestFeedChannelsMatchDatabase(t *testing.T) {
	database, _, _ := setup(t)
	var schema string
	if err := database.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='calendar_feeds'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	values := regexp.MustCompile(`channel IN \(([^)]*)\)`).FindStringSubmatch(schema)
	if len(values) != 2 {
		t.Fatal(schema)
	}
	var channels []string
	for _, value := range strings.Split(values[1], ",") {
		channels = append(channels, strings.Trim(strings.TrimSpace(value), "'"))
	}
	if !slices.Equal(channels, Channels()) {
		t.Fatalf("schema=%v Go=%v", channels, Channels())
	}
}

func TestImportedCompletedCleaningRemainsUnchanged(t *testing.T) {
	database, unit, s := setup(t)
	initial := fixture(t, "airbnb.ics")
	body := initial
	server := feedServer(t, &body)
	id := add(t, database, unit, booking.ChannelAirbnb, server.URL)
	syncOK(t, s, id)
	future := bookings(t, database)[1]
	if _, err := database.Exec(`UPDATE tasks SET status='done',done_at='2026-11-04T12:00:00Z' WHERE booking_id=?`, future.ID); err != nil {
		t.Fatal(err)
	}
	done := cleaning(t, database, future.ID)
	body = bytes.ReplaceAll(initial, []byte("20261104"), []byte("20261105"))
	syncOK(t, s, id)
	body = regexp.MustCompile(`(?s)BEGIN:VEVENT\nUID:future-stay.*?END:VEVENT\n`).ReplaceAll(initial, nil)
	syncOK(t, s, id)
	body = initial
	syncOK(t, s, id)
	if !reflect.DeepEqual(done, cleaning(t, database, future.ID)) {
		t.Fatal("import changed completed cleaning")
	}
}
