package web

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/gaw008/wingmanager/internal/document"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

// Multipart framing is separate from the domain's per-file size limit.
const multipartOverheadBytes = 64 << 10

var errDocumentForm = errors.New("invalid document form")

type documentPanel struct {
	Locale, UploadPath string
	Documents          []document.Document
}

type documentPage struct {
	DocumentPanel documentPanel
	DocumentBack  string
	Unit          property.Unit
}

func (s *server) registerDocumentRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"properties", "units", "leases", "people", "money"} {
		mux.HandleFunc("POST /"+kind+"/{id}/documents", func(w http.ResponseWriter, r *http.Request) {
			s.uploadDocument(w, r, kind)
		})
	}
	mux.HandleFunc("GET /documents/{id}", s.downloadDocument)
	mux.HandleFunc("POST /documents/{id}/delete", s.deleteDocument)
	for _, kind := range []string{"units", "people", "money"} {
		mux.HandleFunc("GET /"+kind+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			s.documentOwnerDetail(w, r, kind)
		})
	}
}

// documentOwner resolves an existing owner through its domain, including archived
// records. No path or owner identifier supplied by the upload becomes a disk path.
func (s *server) documentOwner(r *http.Request, kind string, id int64) (document.Owner, error) {
	var owner document.Owner
	var err error
	switch kind {
	case "properties":
		_, err = property.Get(r.Context(), s.database, id)
		owner.PropertyID = id
	case "units":
		_, err = property.GetUnit(r.Context(), s.database, id)
		owner.UnitID = id
	case "leases":
		_, err = lease.Get(r.Context(), s.database, id)
		owner.LeaseID = id
	case "people":
		_, err = person.Get(r.Context(), s.database, id)
		owner.PersonID = id
	case "money":
		_, err = ledger.Get(r.Context(), s.database, id)
		owner.LedgerEntryID = id
	}
	return owner, err
}

func ownerPath(o document.Owner) string {
	switch {
	case o.PropertyID != 0:
		return fmt.Sprintf("/properties/%d", o.PropertyID)
	case o.UnitID != 0:
		return fmt.Sprintf("/units/%d", o.UnitID)
	case o.LeaseID != 0:
		return fmt.Sprintf("/leases/%d", o.LeaseID)
	case o.PersonID != 0:
		return fmt.Sprintf("/people/%d", o.PersonID)
	default:
		return fmt.Sprintf("/money/%d", o.LedgerEntryID)
	}
}

func (s *server) loadDocumentPanel(r *http.Request, data *pageData) error {
	var owner document.Owner
	switch data.Page {
	case "property_detail":
		owner.PropertyID = data.Property.ID
	case "unit_detail":
		owner.UnitID = data.Unit.ID
	case "lease_detail":
		owner.LeaseID = data.Lease.ID
	case "person_detail", "edit_person":
		owner.PersonID = data.Person.ID
	case "money_detail":
		owner.LedgerEntryID = data.MoneyEntry.ID
	default:
		return nil
	}
	values, err := s.documents.List(r.Context(), owner)
	data.DocumentPanel = documentPanel{Locale: data.Settings.Locale, UploadPath: ownerPath(owner) + "/documents", Documents: values}
	return err
}

func (s *server) documentOwnerDetail(w http.ResponseWriter, r *http.Request, kind string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data := pageData{}
	switch kind {
	case "units":
		data.Page = "unit_detail"
		data.Unit, err = property.GetUnit(r.Context(), s.database, id)
		if err == nil {
			data.Property, err = property.Get(r.Context(), s.database, data.Unit.PropertyID)
		}
	case "people":
		data.Page = "person_detail"
		data.Person, err = person.Get(r.Context(), s.database, id)
	default:
		data.Page = "money_detail"
		data.MoneyEntry, err = ledger.Get(r.Context(), s.database, id)
		if err == nil {
			data.Property, err = property.Get(r.Context(), s.database, data.MoneyEntry.PropertyID)
		}
	}
	if err != nil {
		s.documentResult(w, r, err, "")
		return
	}
	s.render(w, r, data, http.StatusOK)
}

func (s *server) uploadDocument(w http.ResponseWriter, r *http.Request, kind string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	owner, err := s.documentOwner(r, kind, id)
	if err != nil {
		s.documentResult(w, r, err, "")
		return
	}
	back := ownerPath(owner)
	r.Body = http.MaxBytesReader(w, r.Body, document.MaxSizeBytes+multipartOverheadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		s.documentResult(w, r, errDocumentForm, back)
		return
	}
	for {
		part, err := reader.NextPart()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if !errors.As(err, &tooLarge) {
				err = errDocumentForm
			}
			s.documentResult(w, r, err, back)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			if err := part.Close(); err != nil {
				s.documentResult(w, r, err, back)
				return
			}
			continue
		}
		_, err = s.documents.Upload(r.Context(), owner, part.FileName(), part)
		if err != nil {
			s.documentResult(w, r, err, back)
			return
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
}

func (s *server) downloadDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	value, file, err := s.documents.Open(r.Context(), id)
	if err != nil {
		s.documentResult(w, r, err, "")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": value.Filename}))
	w.Header().Set("Content-Type", value.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, value.Filename, info.ModTime(), file)
}

func (s *server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	value, err := s.documents.Get(r.Context(), id)
	if err == nil {
		err = s.documents.Delete(r.Context(), id)
	}
	if err != nil {
		s.documentResult(w, r, err, "")
		return
	}
	http.Redirect(w, r, ownerPath(value.Owner), http.StatusSeeOther)
}

func (s *server) documentResult(w http.ResponseWriter, r *http.Request, err error, back string) {
	status, key := http.StatusBadRequest, "invalid_document"
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, document.ErrNotFound), errors.Is(err, property.ErrNotFound), errors.Is(err, person.ErrNotFound), errors.Is(err, lease.ErrNotFound), errors.Is(err, ledger.ErrNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, document.ErrTooLarge), errors.As(err, &tooLarge):
		status, key = http.StatusRequestEntityTooLarge, "document_too_large"
	case errors.Is(err, document.ErrInvalidOwner), errors.Is(err, errDocumentForm), errors.Is(err, io.ErrUnexpectedEOF):
	default:
		s.fail(w, err)
		return
	}
	s.render(w, r, pageData{Page: "document_error", Error: key, documentPage: documentPage{DocumentBack: back}}, status)
}
