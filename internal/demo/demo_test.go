package demo

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/web"
)

func testDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "wingmanager.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	return database, dir
}

func assertSamples(t *testing.T, database *sql.DB, today civildate.Date) {
	t.Helper()
	ctx := t.Context()
	properties, err := property.List(ctx, database, false)
	if err != nil || len(properties) != 2 {
		t.Fatalf("properties = %+v, %v", properties, err)
	}
	bookings, err := booking.List(ctx, database, booking.Filter{})
	if err != nil || len(bookings) != 3 {
		t.Fatalf("bookings = %+v, %v", bookings, err)
	}
	imported := 0
	for _, value := range bookings {
		if value.Imported {
			imported++
			if value.FeedID == 0 || value.ExternalUID == "" || value.Channel != booking.ChannelAirbnb {
				t.Fatalf("imported booking lacks source: %+v", value)
			}
		}
	}
	if imported != 1 {
		t.Fatalf("imported booking count = %d", imported)
	}
	turnovers, err := booking.Turnovers(ctx, database, today)
	if err != nil || len(turnovers.CheckIns) != 1 || len(turnovers.CheckOuts) != 1 || len(turnovers.UpcomingCheckIns) != 1 {
		t.Fatalf("turnovers = %+v, %v", turnovers, err)
	}
	leases, err := lease.List(ctx, database, lease.StatusActive)
	if err != nil || len(leases) != 1 {
		t.Fatalf("active leases = %+v, %v", leases, err)
	}
	overdue, err := ledger.Overdue(ctx, database, today)
	if err != nil || len(overdue) != 1 || overdue[0].DaysOverdue <= 0 || overdue[0].TenantNames != "Demo overdue tenant" {
		t.Fatalf("overdue = %+v, %v", overdue, err)
	}
	report, err := ledger.ReportToDate(ctx, database, today)
	if err != nil || report.IncomeCents != 75000 || report.ExpenseCents != 12500 || report.NetCents != 62500 {
		t.Fatalf("YTD = %+v, %v", report.Totals, err)
	}
}

func TestSeedPopulatesTodayPageAndRefusesRepeat(t *testing.T) {
	database, dir := testDB(t)
	ctx := t.Context()
	credentials, err := Seed(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	today, err := settings.Today(ctx, database, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	assertSamples(t, database, today)
	service := auth.NewService(database)
	token, _, err := service.Login(ctx, credentials.Email, credentials.Password)
	if err != nil {
		t.Fatalf("printed credentials cannot log in: %v", err)
	}
	handler, err := web.New(database, ical.NewService(database, nil, time.Now), web.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "wingmanager_session", Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Today status = %d: %s", response.Code, response.Body.String())
	}
	for _, want := range []string{"Demo departing guest", "Demo overdue tenant", `id="ytd-net">$625`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("Today missing %q: %s", want, response.Body.String())
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/?tab=in", nil)
	request.AddCookie(&http.Cookie{Name: "wingmanager_session", Value: token})
	arrivals := httptest.NewRecorder()
	handler.ServeHTTP(arrivals, request)
	if arrivals.Code != http.StatusOK || !strings.Contains(arrivals.Body.String(), "Demo arriving guest") || strings.Contains(arrivals.Body.String(), "Demo departing guest") {
		t.Fatalf("Today arrivals tab: %d %s", arrivals.Code, arrivals.Body)
	}
	if _, err := Seed(ctx, database); !errors.Is(err, auth.ErrSetupComplete) {
		t.Fatalf("repeat seed = %v", err)
	}
	assertSamples(t, database, today)
}

func TestSeedYearBoundaryKeepsOverdueAndCurrentYearCash(t *testing.T) {
	for _, date := range []string{"2027-01-01", "2026-12-31", "2028-02-29"} {
		t.Run(date, func(t *testing.T) {
			database, _ := testDB(t)
			today, err := civildate.Parse(date)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := seed(t.Context(), database, today); err != nil {
				t.Fatal(err)
			}
			assertSamples(t, database, today)
		})
	}
}

func TestSeedRefusesExistingOwnerWithoutAddingSamples(t *testing.T) {
	database, _ := testDB(t)
	if err := auth.NewService(database).Setup(t.Context(), "owner@example.com", "owner-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(t.Context(), database); !errors.Is(err, auth.ErrSetupComplete) {
		t.Fatalf("seed existing owner = %v", err)
	}
	properties, err := property.List(t.Context(), database, false)
	if err != nil || len(properties) != 0 {
		t.Fatalf("existing instance changed: %+v, %v", properties, err)
	}
}

func TestConcurrentSeedCreatesOneDemo(t *testing.T) {
	database, _ := testDB(t)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := Seed(t.Context(), database)
			results <- err
		}()
	}
	var successes, refusals int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, auth.ErrSetupComplete):
			refusals++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatalf("successes = %d, refusals = %d", successes, refusals)
	}
	today, err := settings.Today(t.Context(), database, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	assertSamples(t, database, today)
}

func TestSeedFailureReturnsCredentialsAndStopsFurtherWrites(t *testing.T) {
	database, _ := testDB(t)
	if _, err := database.ExecContext(t.Context(), `CREATE TRIGGER fail_demo_properties BEFORE INSERT ON properties BEGIN SELECT RAISE(ABORT, 'demo write failure'); END`); err != nil {
		t.Fatal(err)
	}
	credentials, err := Seed(t.Context(), database)
	if err == nil || !strings.Contains(err.Error(), "demo write failure") {
		t.Fatalf("seed failure = %v", err)
	}
	if _, _, err := auth.NewService(database).Login(t.Context(), credentials.Email, credentials.Password); err != nil {
		t.Fatalf("partial failure lost owner credentials: %v", err)
	}
	properties, err := property.List(t.Context(), database, false)
	if err != nil || len(properties) != 0 {
		t.Fatalf("failed property left data: %+v, %v", properties, err)
	}
	if _, err := Seed(t.Context(), database); !errors.Is(err, auth.ErrSetupComplete) {
		t.Fatalf("partial seed rerun = %v", err)
	}
}
