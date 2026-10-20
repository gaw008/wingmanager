package web

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/document"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func uploadRequest(t *testing.T, path, filename, declaredMIME string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename})},
		"Content-Type":        {declaredMIME},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestM7DocumentHTTPAttachmentOwnersAndDelete(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			dataDir := t.TempDir()
			handler, err := newHandler(a.database, time.Now, ical.NewService(a.database, nil, time.Now), Config{DataDir: dataDir})
			if err != nil {
				t.Fatal(err)
			}
			a.handler = handler
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			personID, err := person.Create(t.Context(), a.database, person.Details{Name: "Document tenant"})
			if err != nil {
				t.Fatal(err)
			}
			leaseID, err := lease.Create(t.Context(), a.database, lease.Details{UnitID: a.unitID, StartsOn: "2026-01-01", EndsOn: "2026-12-31", RentCents: 100000, DueDay: 1})
			if err != nil {
				t.Fatal(err)
			}
			entryID, err := ledger.RecordProperty(t.Context(), a.database, unit.PropertyID, 0, ledger.Details{Kind: ledger.KindExpense, Category: ledger.CategoryRepairs, AmountCents: 3210, OccurredOn: "2026-10-01", Memo: "Receipt expense"})
			if err != nil {
				t.Fatal(err)
			}
			store := document.NewStore(a.database, dataDir)
			for _, target := range []struct {
				path  string
				owner document.Owner
			}{
				{fmt.Sprintf("/properties/%d", unit.PropertyID), document.Owner{PropertyID: unit.PropertyID}},
				{fmt.Sprintf("/units/%d", unit.ID), document.Owner{UnitID: unit.ID}},
				{fmt.Sprintf("/leases/%d", leaseID), document.Owner{LeaseID: leaseID}},
				{fmt.Sprintf("/people/%d", personID), document.Owner{PersonID: personID}},
				{fmt.Sprintf("/money/%d", entryID), document.Owner{LedgerEntryID: entryID}},
			} {
				filename := "租约 <script>.html"
				content := []byte("<!DOCTYPE html><html><script>alert('receipt')</script></html>\n")
				upload := uploadRequest(t, target.path+"/documents", filename, "image/png", content)
				upload.AddCookie(a.cookie)
				response := httptest.NewRecorder()
				a.handler.ServeHTTP(response, upload)
				if response.Code != 303 || response.Header().Get("Location") != target.path {
					t.Fatalf("upload %s: %d %s", target.path, response.Code, response.Body)
				}
				docs, err := store.List(t.Context(), target.owner)
				if err != nil || len(docs) != 1 {
					t.Fatalf("documents=%v err=%v", docs, err)
				}
				value := docs[0]
				downloadPath := fmt.Sprintf("/documents/%d", value.ID)
				a.request(t, "GET", target.path, nil, 200, catalogs[locale]["documents"], `enctype="multipart/form-data"`, target.path+"/documents", downloadPath, "租约 &lt;script&gt;.html")
				request := httptest.NewRequest("GET", downloadPath, nil)
				request.AddCookie(a.cookie)
				response = httptest.NewRecorder()
				a.handler.ServeHTTP(response, request)
				disposition, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
				if err != nil || disposition != "attachment" || params["filename"] != filename || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Type") != "text/html; charset=utf-8" || response.Code != 200 || !bytes.Equal(content, response.Body.Bytes()) {
					t.Fatalf("download: %d %+v %q %v", response.Code, response.Header(), response.Body.Bytes(), err)
				}
				if raw, err := os.ReadFile(filepath.Join(dataDir, "files", fmt.Sprint(value.ID))); err != nil || !bytes.Equal(raw, content) {
					t.Fatalf("disk file=%q err=%v", raw, err)
				}
				a.request(t, "POST", downloadPath+"/delete", nil, 303)
				if _, err := os.Stat(filepath.Join(dataDir, "files", fmt.Sprint(value.ID))); !os.IsNotExist(err) {
					t.Fatalf("deleted disk file remains: %v", err)
				}
				a.request(t, "GET", downloadPath, nil, 404)
				a.request(t, "GET", target.path, nil, 200, catalogs[locale]["no_documents"])
			}
			a.request(t, "GET", "/people", nil, 200, fmt.Sprintf(`/people/%d`, personID))
			a.request(t, "GET", fmt.Sprintf("/properties/%d", unit.PropertyID), nil, 200, fmt.Sprintf(`/units/%d`, unit.ID))
			a.request(t, "GET", "/money?year=2026", nil, 200, fmt.Sprintf(`/money/%d`, entryID))
		})
	}
}

func TestM7DocumentsHTTPRequiresSessionAndCrossOrigin(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	for _, route := range []struct{ method, path string }{
		{"POST", "/properties/1/documents"}, {"POST", "/units/1/documents"}, {"POST", "/leases/1/documents"}, {"POST", "/people/1/documents"}, {"POST", "/money/1/documents"},
		{"GET", "/documents/1"}, {"POST", "/documents/1/delete"}, {"GET", "/units/1"}, {"GET", "/people/1"}, {"GET", "/money/1"},
	} {
		for _, cookie := range []*http.Cookie{nil, {Name: sessionCookie, Value: "forged"}} {
			request := httptest.NewRequest(route.method, route.path, nil)
			if cookie != nil {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != 303 || response.Header().Get("Location") != "/login" {
				t.Fatalf("unprotected %v: %d", route, response.Code)
			}
		}
		if route.method == "POST" {
			request := httptest.NewRequest(route.method, route.path, nil)
			request.AddCookie(a.cookie)
			request.Header.Set("Sec-Fetch-Site", "cross-site")
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != 403 {
				t.Fatalf("cross-origin write allowed: %v", route)
			}
		}
	}
	var count int
	if err := a.database.QueryRow(`SELECT count(*) FROM documents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unauthorized rows=%d err=%v", count, err)
	}
}

func TestM7DocumentsHTTPSizeAndInvalidRequests(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	request := uploadRequest(t, "/units/1/documents", "large.bin", "application/octet-stream", bytes.Repeat([]byte{'x'}, int(document.MaxSizeBytes)+1))
	request.AddCookie(a.cookie)
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	if response.Code != 413 || !strings.Contains(response.Body.String(), catalogs[settings.LocaleEnglish]["document_too_large"]) {
		t.Fatalf("oversize status=%d body=%s", response.Code, response.Body)
	}
	for _, path := range []string{"/units/9999/documents", "/properties/9999/documents", "/people/9999/documents", "/leases/9999/documents", "/money/9999/documents"} {
		r := uploadRequest(t, path, "file.txt", "text/plain", []byte("test"))
		r.AddCookie(a.cookie)
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, r)
		if response.Code != 404 {
			t.Fatalf("unknown owner %s: %d", path, response.Code)
		}
	}
	a.request(t, "POST", "/units/1/documents", nil, 400, catalogs[settings.LocaleEnglish]["invalid_document"])
	truncated := uploadRequest(t, "/units/1/documents", "truncated.txt", "text/plain", []byte("receipt"))
	body, err := io.ReadAll(truncated.Body)
	if err != nil {
		t.Fatal(err)
	}
	truncated.Body = io.NopCloser(bytes.NewReader(body[:len(body)-12]))
	truncated.AddCookie(a.cookie)
	response = httptest.NewRecorder()
	a.handler.ServeHTTP(response, truncated)
	if response.Code != 400 {
		t.Fatalf("truncated multipart: %d %s", response.Code, response.Body)
	}
	a.request(t, "POST", "/documents/9999/delete", nil, 404)
	var count int
	if err := a.database.QueryRow(`SELECT count(*) FROM documents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid uploads created rows=%d err=%v", count, err)
	}
}
