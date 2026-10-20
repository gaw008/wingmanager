// Package task owns tasks and transactional cleaning-task maintenance.
package task

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
)

const (
	KindCleaning    = "cleaning"
	KindMaintenance = "maintenance"
	KindOther       = "other"
	StatusOpen      = "open"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
)

func Kinds() []string    { return []string{KindCleaning, KindMaintenance, KindOther} }
func Statuses() []string { return []string{StatusOpen, StatusDone, StatusCancelled} }

// Task is work for a property. Zero UnitID or BookingID means no association;
// empty DueOn or DoneAt means no due date or completion timestamp, respectively.
// DueOn is a canonical YYYY-MM-DD date.
type Task struct {
	ID, PropertyID, UnitID, BookingID int64
	Kind, Title, DueOn, Status        string
	Assignee, Notes, DoneAt           string
}

// EnsureCleaning creates or reopens the booking's cleaning task, moving unfinished
// work to its current unit and checkout date. Unconfirmed stays keep cancelled
// work; completed work is immutable.
func EnsureCleaning(ctx context.Context, tx *sql.Tx, bookingID int64, confirmed bool) error {
	status := StatusCancelled
	if confirmed {
		status = StatusOpen
	}
	return New(tx).ensureCleaning(ctx, ensureCleaningParams{Status: status, BookingID: bookingID})
}

// CancelCleaning cancels open work only; completed work remains historical fact.
func CancelCleaning(ctx context.Context, tx *sql.Tx, bookingID int64) error {
	return New(tx).cancelCleaning(ctx, cancelCleaningParams{Status: StatusCancelled, BookingID: sql.NullInt64{Int64: bookingID, Valid: true}})
}

// CleaningForBooking returns nil when a booking has no cleaning task.
func CleaningForBooking(ctx context.Context, database *sql.DB, bookingID int64) (*Task, error) {
	value, err := New(database).cleaningForBooking(ctx, sql.NullInt64{Int64: bookingID, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := taskValue(value)
	return &result, nil
}

var (
	ErrInvalid  = errors.New("invalid task details or filter")
	ErrNotFound = errors.New("task not found")
	ErrCleaning = errors.New("cleaning task dates, title and cancellation are controlled by its booking")
)

const (
	DueOverdue      = "overdue"
	DueWeek         = "week"
	DueThroughToday = "through_today"
)

// Details creates manual maintenance or other work; cleaning belongs to bookings.
type Details struct {
	PropertyID, UnitID                  int64
	Kind, Title, DueOn, Assignee, Notes string
}

// Edit changes owner-controlled fields. Cleaning title and due date must remain unchanged.
type Edit struct{ Title, DueOn, Assignee, Notes string }

// Filter combines optional filters. Due requires Today; week means Monday through
// Sunday of Today's calendar week. Empty Due includes undated tasks.
type Filter struct {
	PropertyID        int64
	Status, Kind, Due string
	Today             civildate.Date
}

func taskValue(value taskRecord) Task {
	return Task{ID: value.ID, PropertyID: value.PropertyID, UnitID: value.UnitID.Int64,
		BookingID: value.BookingID.Int64, Kind: value.Kind, Title: value.Title, DueOn: value.DueOn.String,
		Status: value.Status, Assignee: value.Assignee, Notes: value.Notes, DoneAt: value.DoneAt.String}
}

// Get returns a task, including completed and cancelled work.
func Get(ctx context.Context, database *sql.DB, id int64) (Task, error) {
	value, err := New(database).getTask(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return taskValue(value), err
}

// List reads tasks in due-date order, with undated work last.
func List(ctx context.Context, database *sql.DB, filter Filter) ([]Task, error) {
	if filter.PropertyID < 0 || (filter.Kind != "" && !slices.Contains(Kinds(), filter.Kind)) || (filter.Status != "" && !slices.Contains(Statuses(), filter.Status)) {
		return nil, ErrInvalid
	}
	var from, until string
	if filter.Due != "" {
		if filter.Today.IsZero() {
			return nil, ErrInvalid
		}
		switch filter.Due {
		case DueOverdue:
			until = filter.Today.String()
		case DueThroughToday:
			until = filter.Today.AddDays(1).String()
		case DueWeek:
			weekday := int(filter.Today.MidnightUTC().Weekday())
			if weekday == int(time.Sunday) {
				weekday = 7
			}
			start := filter.Today.AddDays(1 - weekday)
			from, until = start.String(), start.AddDays(7).String()
		default:
			return nil, ErrInvalid
		}
	}
	values, err := New(database).listTasks(ctx, listTasksParams{PropertyID: filter.PropertyID, Kind: filter.Kind, Status: filter.Status, DueFrom: from, DueUntil: until})
	if err != nil {
		return nil, err
	}
	result := make([]Task, len(values))
	for i, value := range values {
		result[i] = taskValue(value)
	}
	return result, nil
}

// Create creates open maintenance or other work. An optional unit must belong to
// the property; neither the task's kind nor its associations change after creation.
func Create(ctx context.Context, database *sql.DB, value Details) (int64, error) {
	if value.Kind == KindCleaning {
		return 0, ErrCleaning
	}
	value.Title = strings.TrimSpace(value.Title)
	if value.PropertyID <= 0 || value.UnitID < 0 || !slices.Contains(Kinds(), value.Kind) || value.Title == "" || !validDue(value.DueOn) {
		return 0, ErrInvalid
	}
	id, err := New(database).createTask(ctx, createTaskParams{PropertyID: value.PropertyID, UnitID: sql.NullInt64{Int64: value.UnitID, Valid: value.UnitID != 0}, Kind: value.Kind, Title: value.Title, DueOn: sql.NullString{String: value.DueOn, Valid: value.DueOn != ""}, Assignee: value.Assignee, Notes: value.Notes})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInvalid
	}
	return id, err
}

// Update preserves lifecycle and associations. The IMMEDIATE transaction keeps
// a booking's concurrent cleaning update from invalidating the field restrictions.
func Update(ctx context.Context, database *sql.DB, id int64, value Edit) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	old, err := q.getTask(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	value.Title = strings.TrimSpace(value.Title)
	if old.Kind == KindCleaning && (value.Title != old.Title || value.DueOn != old.DueOn.String) {
		return ErrCleaning
	}
	if (old.Kind != KindCleaning && value.Title == "") || !validDue(value.DueOn) {
		return ErrInvalid
	}
	err = q.updateTask(ctx, updateTaskParams{ID: id, Title: value.Title, DueOn: sql.NullString{String: value.DueOn, Valid: value.DueOn != ""}, Assignee: value.Assignee, Notes: value.Notes})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SetStatus marks work done, reopens it or cancels manual work. Repeated completion
// preserves done_at; reopening or cancelling clears it.
func SetStatus(ctx context.Context, database *sql.DB, id int64, status string) error {
	if !slices.Contains(Statuses(), status) {
		return ErrInvalid
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	old, err := q.getTask(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if old.Kind == KindCleaning && status == StatusCancelled {
		return ErrCleaning
	}
	if err := q.setTaskStatus(ctx, setTaskStatusParams{ID: id, Status: status}); err != nil {
		return err
	}
	return tx.Commit()
}

func validDue(value string) bool {
	if value == "" {
		return true
	}
	_, err := civildate.Parse(value)
	return err == nil
}
