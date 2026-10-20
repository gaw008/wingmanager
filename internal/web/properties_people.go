package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func (s *server) registerPropertyAndPersonRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /properties", s.listProperties)
	mux.HandleFunc("GET /properties/new", s.editProperty)
	mux.HandleFunc("POST /properties/new", s.editProperty)
	mux.HandleFunc("GET /properties/{id}", s.propertyDetail)
	mux.HandleFunc("GET /properties/{id}/edit", s.editProperty)
	mux.HandleFunc("POST /properties/{id}/edit", s.editProperty)
	mux.HandleFunc("POST /properties/{id}/units", s.addUnit)
	for _, action := range []string{"archive", "restore"} {
		mux.HandleFunc("POST /properties/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			s.archiveProperty(w, r, action == "archive")
		})
	}
	for _, action := range []string{"rename", "archive", "restore", "token"} {
		mux.HandleFunc("POST /units/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			s.changeUnit(w, r, action)
		})
	}
	mux.HandleFunc("GET /people", s.listPeople)
	mux.HandleFunc("GET /people/new", s.editPerson)
	mux.HandleFunc("POST /people/new", s.editPerson)
	mux.HandleFunc("GET /people/{id}/edit", s.editPerson)
	mux.HandleFunc("POST /people/{id}/edit", s.editPerson)
}

func (s *server) listProperties(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "properties", ShowArchived: r.URL.Query().Get("archived") == "1"}
	values, err := property.List(r.Context(), s.database, data.ShowArchived)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.Properties = values
	s.render(w, r, data, http.StatusOK)
}

func (s *server) propertyDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.renderPropertyDetail(w, r, id, pageData{}, nil)
}

func (s *server) renderPropertyDetail(w http.ResponseWriter, r *http.Request, id int64, data pageData, actionErr error) {
	value, err := property.Get(r.Context(), s.database, id)
	if err != nil {
		s.renderDomainResult(w, r, data, err)
		return
	}
	data.Page = "property_detail"
	data.Property = value
	data.ShowArchived = r.URL.Query().Get("archived") == "1"
	data.Units, err = property.ListUnits(r.Context(), s.database, id, data.ShowArchived)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.propertyFeeds(r, &data); err != nil {
		s.fail(w, err)
		return
	}
	s.renderDomainResult(w, r, data, actionErr)
}

func (s *server) editProperty(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "new_property"}
	var id int64
	if r.PathValue("id") != "" {
		data.Page = "edit_property"
		var err error
		id, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			s.notFound(w, r)
			return
		}
		data.Property, err = property.Get(r.Context(), s.database, id)
		if err != nil {
			s.renderDomainResult(w, r, data, err)
			return
		}
	}
	if r.Method == http.MethodGet {
		s.render(w, r, data, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		data.Error = "invalid_form"
		s.render(w, r, data, http.StatusBadRequest)
		return
	}
	value := property.Details{Name: r.PostForm.Get("name"), Address: r.PostForm.Get("address"), Notes: r.PostForm.Get("notes")}
	data.Property.Name, data.Property.Address, data.Property.Notes = value.Name, value.Address, value.Notes
	data.FirstUnitName = r.PostForm.Get("first_unit_name")
	var err error
	if data.Page == "new_property" {
		id, err = property.Create(r.Context(), s.database, value, data.FirstUnitName)
	} else {
		err = property.Update(r.Context(), s.database, id, value)
	}
	if err != nil {
		s.renderDomainResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/properties/%d", id), http.StatusSeeOther)
}

func (s *server) archiveProperty(w http.ResponseWriter, r *http.Request, archived bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := property.SetArchived(r.Context(), s.database, id, archived); err != nil {
		s.renderDomainResult(w, r, pageData{}, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/properties/%d", id), http.StatusSeeOther)
}

func (s *server) addUnit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	// Resolve the parent before writing, so an unknown property is a 404, not a FK error.
	if _, err := property.Get(r.Context(), s.database, id); err != nil {
		s.renderDomainResult(w, r, pageData{}, err)
		return
	}
	data := pageData{}
	if err := r.ParseForm(); err != nil {
		data.Error = "invalid_form"
		s.renderPropertyDetail(w, r, id, data, nil)
		return
	}
	data.UnitName = r.PostForm.Get("name")
	if _, err := property.AddUnit(r.Context(), s.database, id, data.UnitName); err != nil {
		s.renderPropertyDetail(w, r, id, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/properties/%d", id), http.StatusSeeOther)
}

func (s *server) changeUnit(w http.ResponseWriter, r *http.Request, action string) {
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
	switch action {
	case "rename":
		if err := r.ParseForm(); err != nil {
			s.renderPropertyDetail(w, r, unit.PropertyID, pageData{Error: "invalid_form"}, nil)
			return
		}
		err = property.RenameUnit(r.Context(), s.database, id, r.PostForm.Get("name"))
	case "archive", "restore":
		err = property.SetUnitArchived(r.Context(), s.database, id, action == "archive")
	case "token":
		_, err = property.RegenerateToken(r.Context(), s.database, id)
	}
	if err != nil {
		s.renderPropertyDetail(w, r, unit.PropertyID, pageData{}, err)
		return
	}
	path := fmt.Sprintf("/properties/%d", unit.PropertyID)
	if r.URL.Query().Get("archived") == "1" {
		path += "?archived=1"
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func (s *server) listPeople(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "people", Search: r.URL.Query().Get("q")}
	values, err := person.List(r.Context(), s.database, data.Search)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.People = values
	s.render(w, r, data, http.StatusOK)
}

func (s *server) editPerson(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "new_person"}
	var id int64
	if r.PathValue("id") != "" {
		data.Page = "edit_person"
		var err error
		id, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			s.notFound(w, r)
			return
		}
		data.Person, err = person.Get(r.Context(), s.database, id)
		if err != nil {
			s.renderDomainResult(w, r, data, err)
			return
		}
	}
	if r.Method == http.MethodGet {
		s.render(w, r, data, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		data.Error = "invalid_form"
		s.render(w, r, data, http.StatusBadRequest)
		return
	}
	value := person.Details{Name: r.PostForm.Get("name"), Email: r.PostForm.Get("email"), Phone: r.PostForm.Get("phone"), Notes: r.PostForm.Get("notes")}
	data.Person.Name, data.Person.Email, data.Person.Phone, data.Person.Notes = value.Name, value.Email, value.Phone, value.Notes
	var err error
	if data.Page == "new_person" {
		_, err = person.Create(r.Context(), s.database, value)
	} else {
		err = person.Update(r.Context(), s.database, id, value)
	}
	if err != nil {
		s.renderDomainResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, "/people", http.StatusSeeOther)
}

func (s *server) renderDomainResult(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	status := http.StatusBadRequest
	switch {
	case err == nil:
		status = http.StatusOK
		if data.Error != "" {
			status = http.StatusBadRequest
		}
	case errors.Is(err, property.ErrNotFound), errors.Is(err, person.ErrNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, property.ErrName):
		data.Error = "property_name_required"
	case errors.Is(err, property.ErrUnitName):
		data.Error = "unit_name_required"
	case errors.Is(err, property.ErrDuplicateUnitName):
		data.Error = "duplicate_unit_name"
	case errors.Is(err, property.ErrLastActiveUnit):
		data.Error = "last_active_unit"
	case errors.Is(err, person.ErrName):
		data.Error = "person_name_required"
	default:
		s.fail(w, err)
		return
	}
	s.render(w, r, data, status)
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, pageData{Page: "not_found"}, http.StatusNotFound)
}
