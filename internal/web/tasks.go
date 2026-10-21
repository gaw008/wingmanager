package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

type taskRow struct {
	task.Task
	PropertyName, UnitName string
}

type taskPage struct {
	Task                    task.Task
	TaskRows                []taskRow
	TaskFilter              task.Filter
	TaskKinds, TaskStatuses []string
	TaskTabs                []navigationItem
}

func (s *server) registerTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tasks", s.listTasks)
	mux.HandleFunc("GET /tasks/new", s.editTask)
	mux.HandleFunc("POST /tasks/new", s.editTask)
	mux.HandleFunc("GET /tasks/{id}", s.editTask)
	mux.HandleFunc("POST /tasks/{id}", s.editTask)
	mux.HandleFunc("POST /tasks/{id}/status", s.taskStatus)
}

func (s *server) taskChoices(r *http.Request, data *pageData) error {
	var err error
	data.Properties, err = property.List(r.Context(), s.database, true)
	if err != nil {
		return err
	}
	data.TaskKinds, data.TaskStatuses = task.Kinds(), task.Statuses()
	return s.contactAndUnitChoices(r.Context(), data)
}

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "tasks"}
	if err := s.taskChoices(r, &data); err != nil {
		s.fail(w, err)
		return
	}
	query := r.URL.Query()
	filter := task.Filter{Status: query.Get("status"), Kind: query.Get("kind"), Due: query.Get("due")}
	var err error
	if raw := query.Get("property"); raw != "" {
		filter.PropertyID, err = strconv.ParseInt(raw, 10, 64)
	}
	if err != nil {
		s.taskResult(w, r, data, task.ErrInvalid)
		return
	}
	filter.Today, err = settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.TaskFilter = filter
	for _, tab := range []struct{ key, status, due string }{
		{"task_all", "", ""}, {"task_open", task.StatusOpen, ""},
		{"task_overdue", task.StatusOpen, task.DueOverdue},
		{"task_done", task.StatusDone, ""}, {"task_cancelled", task.StatusCancelled, ""},
	} {
		params := r.URL.Query()
		params.Set("status", tab.status)
		params.Set("due", tab.due)
		selected := filter.Status == tab.status && filter.Due == tab.due
		data.TaskTabs = append(data.TaskTabs, navigationItem{URL: "/tasks?" + params.Encode(), Key: tab.key, Current: selected})
	}
	values, err := task.List(r.Context(), s.database, filter)
	if err != nil {
		s.taskResult(w, r, data, err)
		return
	}
	properties := make(map[int64]string, len(data.Properties))
	units := make(map[int64]string, len(data.UnitChoices))
	for _, p := range data.Properties {
		properties[p.ID] = p.Name
	}
	for _, u := range data.UnitChoices {
		units[u.ID] = u.Label
	}
	for _, value := range values {
		data.TaskRows = append(data.TaskRows, taskRow{Task: value, PropertyName: properties[value.PropertyID], UnitName: units[value.UnitID]})
	}
	s.render(w, r, data, http.StatusOK)
}

func (s *server) editTask(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "new_task"}
	if err := s.taskChoices(r, &data); err != nil {
		s.fail(w, err)
		return
	}
	data.Task.Kind = task.KindMaintenance
	if raw := r.PathValue("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.notFound(w, r)
			return
		}
		data.Task, err = task.Get(r.Context(), s.database, id)
		if err != nil {
			s.taskResult(w, r, data, err)
			return
		}
		data.Page = "edit_task"
	}
	if r.Method == http.MethodGet {
		s.render(w, r, data, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.taskResult(w, r, data, task.ErrInvalid)
		return
	}
	value := &data.Task
	value.Title, value.DueOn, value.Assignee, value.Notes = r.PostForm.Get("title"), r.PostForm.Get("due_on"), r.PostForm.Get("assignee"), r.PostForm.Get("notes")
	var err error
	if value.ID == 0 {
		value.Kind = r.PostForm.Get("kind")
		value.PropertyID, err = strconv.ParseInt(r.PostForm.Get("property_id"), 10, 64)
		if err == nil && r.PostForm.Get("unit_id") != "" {
			value.UnitID, err = strconv.ParseInt(r.PostForm.Get("unit_id"), 10, 64)
		}
		if err != nil {
			s.taskResult(w, r, data, task.ErrInvalid)
			return
		}
		value.ID, err = task.Create(r.Context(), s.database, task.Details{PropertyID: value.PropertyID, UnitID: value.UnitID, Kind: value.Kind, Title: value.Title, DueOn: value.DueOn, Assignee: value.Assignee, Notes: value.Notes})
	} else {
		err = task.Update(r.Context(), s.database, value.ID, task.Edit{Title: value.Title, DueOn: value.DueOn, Assignee: value.Assignee, Notes: value.Notes})
	}
	if err != nil {
		s.taskResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", value.ID), http.StatusSeeOther)
}

func (s *server) taskStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data := pageData{Page: "edit_task"}
	if err := s.taskChoices(r, &data); err != nil {
		s.fail(w, err)
		return
	}
	data.Task, err = task.Get(r.Context(), s.database, id)
	if err != nil {
		s.taskResult(w, r, data, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		s.taskResult(w, r, data, task.ErrInvalid)
		return
	}
	if err = task.SetStatus(r.Context(), s.database, id, r.PostForm.Get("status")); err != nil {
		s.taskResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", id), http.StatusSeeOther)
}

func (s *server) taskResult(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	switch {
	case errors.Is(err, task.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, task.ErrCleaning):
		data.Error = "cleaning_task_restricted"
		s.render(w, r, data, http.StatusBadRequest)
	case errors.Is(err, task.ErrInvalid):
		data.Error = "invalid_task"
		s.render(w, r, data, http.StatusBadRequest)
	default:
		s.fail(w, err)
	}
}
