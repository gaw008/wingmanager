package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/web"
)

func assertResponse(t *testing.T, a app, cookie *http.Cookie, method, path string, form url.Values, status int, contains ...string) string {
	t.Helper()
	response := a.request(method, path, form, cookie)
	if response.Code != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.Code, status, response.Body)
	}
	for _, text := range contains {
		if !strings.Contains(response.Body.String(), text) {
			t.Fatalf("%s %s: missing %q in %s", method, path, text, response.Body)
		}
	}
	return response.Body.String()
}

func TestM2PagesAndActionsRequireSession(t *testing.T) {
	a := newApp(t)
	a.setup(t)
	for _, route := range []struct{ method, path string }{
		{"GET", "/properties"}, {"GET", "/properties/new"}, {"POST", "/properties/new"},
		{"GET", "/properties/1"}, {"GET", "/properties/1/edit"}, {"POST", "/properties/1/edit"},
		{"POST", "/properties/1/archive"}, {"POST", "/properties/1/restore"}, {"POST", "/properties/1/units"},
		{"POST", "/units/1/rename"}, {"POST", "/units/1/archive"}, {"POST", "/units/1/restore"}, {"POST", "/units/1/token"},
		{"GET", "/people"}, {"GET", "/people/new"}, {"POST", "/people/new"}, {"GET", "/people/1/edit"}, {"POST", "/people/1/edit"},
	} {
		for _, cookie := range []*http.Cookie{nil, {Name: "wingmanager_session", Value: "forged"}} {
			response := a.request(route.method, route.path, nil, cookie)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
				t.Fatalf("unprotected route: %+v status=%d", route, response.Code)
			}
		}
	}
	properties, err := property.List(t.Context(), a.database, true)
	if err != nil || len(properties) != 0 {
		t.Fatalf("unauthenticated write: %v %v", properties, err)
	}
	people, err := person.List(t.Context(), a.database, "")
	if err != nil || len(people) != 0 {
		t.Fatalf("unauthenticated write: %v %v", people, err)
	}
}

func TestM2HTTPHappyPathsBothLanguages(t *testing.T) {
	for _, test := range []struct{ locale, defaultUnit, createProperty, createPerson, lastActive, duplicate string }{
		{"zh-CN", "整套", "新建房产", "新建联系人", "每个房产必须至少保留一个活跃单元", "此房产中已有同名单元"},
		{"en", "Whole home", "Create property", "Create person", "Each property must retain at least one active unit", "This property already has a unit with this name"},
	} {
		t.Run(test.locale, func(t *testing.T) {
			a := newApp(t)
			a.setup(t)
			cookie := a.login(t)
			value, err := settings.Get(t.Context(), a.database)
			if err != nil {
				t.Fatal(err)
			}
			value.Locale = test.locale
			if err := settings.Save(t.Context(), a.database, value); err != nil {
				t.Fatal(err)
			}
			assertResponse(t, a, cookie, "GET", "/", nil, 200, `href="/properties"`, `href="/people"`)
			assertResponse(t, a, cookie, "GET", "/properties", nil, 200, `href="/properties/new"`)
			assertResponse(t, a, cookie, "GET", "/properties/new", nil, 200, test.createProperty, `name="first_unit_name" value="`+test.defaultUnit+`"`)
			form := url.Values{"name": {" Beach house "}, "address": {"1 Ocean Ave"}, "notes": {"Sunny"}, "first_unit_name": {test.defaultUnit}}
			response := a.request("POST", "/properties/new", form, cookie)
			if response.Code != 303 {
				t.Fatalf("create property: %d %s", response.Code, response.Body)
			}
			detail := response.Header().Get("Location")
			properties, err := property.List(t.Context(), a.database, false)
			if err != nil || len(properties) != 1 {
				t.Fatalf("properties=%v err=%v", properties, err)
			}
			p := properties[0]
			if p.Name != "Beach house" || p.Address != "1 Ocean Ave" || p.Notes != "Sunny" {
				t.Fatalf("property=%+v", p)
			}
			units, err := property.ListUnits(t.Context(), a.database, p.ID, true)
			if err != nil || len(units) != 1 || units[0].IcalExportToken == "" {
				t.Fatalf("units=%v err=%v", units, err)
			}
			first := units[0]
			unitPath := fmt.Sprintf("/units/%d", first.ID)
			assertResponse(t, a, cookie, "GET", detail, nil, 200, "Beach house", test.defaultUnit, first.IcalExportToken)
			assertResponse(t, a, cookie, "POST", unitPath+"/archive", nil, 400, test.lastActive)
			assertResponse(t, a, cookie, "POST", detail+"/units", url.Values{"name": {"Guest suite"}}, 303)
			units, err = property.ListUnits(t.Context(), a.database, p.ID, true)
			if err != nil || len(units) != 2 {
				t.Fatalf("units=%v err=%v", units, err)
			}
			var second property.Unit
			for _, unit := range units {
				if unit.ID != first.ID {
					second = unit
				}
			}
			secondPath := fmt.Sprintf("/units/%d", second.ID)
			assertResponse(t, a, cookie, "POST", detail+"/units", url.Values{"name": {" Guest suite "}}, 400, test.duplicate)
			assertResponse(t, a, cookie, "POST", secondPath+"/rename", url.Values{"name": {test.defaultUnit}}, 400, test.duplicate)
			assertResponse(t, a, cookie, "POST", secondPath+"/rename", url.Values{"name": {" Garden suite "}}, 303)
			assertResponse(t, a, cookie, "POST", secondPath+"/archive", nil, 303)
			body := assertResponse(t, a, cookie, "GET", detail, nil, 200)
			if strings.Contains(body, "Garden suite") {
				t.Fatal("archived unit visible by default")
			}
			assertResponse(t, a, cookie, "GET", detail+"?archived=1", nil, 200, "Garden suite", secondPath+"/restore?archived=1")
			assertResponse(t, a, cookie, "POST", secondPath+"/restore?archived=1", nil, 303)
			assertResponse(t, a, cookie, "GET", detail, nil, 200, "Garden suite")
			assertResponse(t, a, cookie, "POST", unitPath+"/token", nil, 303)
			regenerated, err := property.GetUnit(t.Context(), a.database, first.ID)
			if err != nil || regenerated.IcalExportToken == first.IcalExportToken {
				t.Fatal("token unchanged", err)
			}
			body = assertResponse(t, a, cookie, "GET", detail, nil, 200, regenerated.IcalExportToken)
			if strings.Contains(body, first.IcalExportToken) {
				t.Fatal("old token still rendered")
			}
			assertResponse(t, a, cookie, "GET", detail+"/edit", nil, 200, `value="Beach house"`, `value="1 Ocean Ave"`, "Sunny")
			form.Set("name", " Updated house ")
			form.Set("address", "2 Ocean Ave")
			form.Set("notes", "Updated notes")
			assertResponse(t, a, cookie, "POST", detail+"/edit", form, 303)
			assertResponse(t, a, cookie, "GET", detail, nil, 200, "Updated house", "2 Ocean Ave", "Updated notes")
			assertResponse(t, a, cookie, "POST", detail+"/archive", nil, 303)
			body = assertResponse(t, a, cookie, "GET", "/properties", nil, 200)
			if strings.Contains(body, "Updated house") {
				t.Fatal("archived property visible by default")
			}
			assertResponse(t, a, cookie, "GET", "/properties?archived=1", nil, 200, "Updated house")
			assertResponse(t, a, cookie, "GET", detail, nil, 200, detail+"/restore")
			assertResponse(t, a, cookie, "POST", detail+"/restore", nil, 303)
			assertResponse(t, a, cookie, "GET", "/properties", nil, 200, "Updated house")
			assertResponse(t, a, cookie, "GET", "/people", nil, 200, `href="/people/new"`)
			assertResponse(t, a, cookie, "GET", "/people/new", nil, 200, test.createPerson)
			contact := url.Values{"name": {" Alice "}, "email": {"alice@example.com"}, "phone": {"123"}, "notes": {"Guest"}}
			assertResponse(t, a, cookie, "POST", "/people/new", contact, 303)
			people, err := person.List(t.Context(), a.database, "")
			if err != nil || len(people) != 1 {
				t.Fatalf("people=%v err=%v", people, err)
			}
			edit := fmt.Sprintf("/people/%d/edit", people[0].ID)
			assertResponse(t, a, cookie, "GET", edit, nil, 200, `value="Alice"`, `value="alice@example.com"`, `value="123"`, "Guest")
			contact.Set("name", " Alice Wong ")
			contact.Set("email", "wong@example.com")
			contact.Set("phone", "456")
			contact.Set("notes", "Updated contact")
			assertResponse(t, a, cookie, "POST", edit, contact, 303)
			assertResponse(t, a, cookie, "GET", "/people?q=wOnG", nil, 200, "Alice Wong", "wong@example.com", "456", "Updated contact")
			body = assertResponse(t, a, cookie, "GET", "/people?q=missing", nil, 200)
			if strings.Contains(body, "Alice Wong") {
				t.Fatal("search ignored")
			}
			// Database-backed state remains available after constructing a fresh handler.
			rebuilt, err := web.New(a.database, ical.NewService(a.database, nil, time.Now))
			if err != nil {
				t.Fatal(err)
			}
			a.handler = rebuilt
			assertResponse(t, a, cookie, "GET", detail, nil, 200, "Updated house", regenerated.IcalExportToken)
			assertResponse(t, a, cookie, "GET", edit, nil, 200, `value="Alice Wong"`)
		})
	}
}

func TestM2ValidationAndMissingRecordsHTTP(t *testing.T) {
	a := newApp(t)
	a.setup(t)
	cookie := a.login(t)
	assertResponse(t, a, cookie, "POST", "/properties/new", url.Values{"name": {" "}, "address": {"Keep address"}, "first_unit_name": {"整套"}}, 400, "请填写房产名称", `value="Keep address"`)
	assertResponse(t, a, cookie, "POST", "/properties/new", url.Values{"name": {"Keep name"}, "first_unit_name": {" "}}, 400, "请填写单元名称", `value="Keep name"`)
	id, err := property.Create(t.Context(), a.database, property.Details{Name: "Home"}, "Whole home")
	if err != nil {
		t.Fatal(err)
	}
	detail := fmt.Sprintf("/properties/%d", id)
	assertResponse(t, a, cookie, "POST", detail+"/edit", url.Values{"name": {" "}, "notes": {"Keep notes"}}, 400, "请填写房产名称", "Keep notes")
	assertResponse(t, a, cookie, "POST", detail+"/units", url.Values{"name": {" "}}, 400, "请填写单元名称")
	units, err := property.ListUnits(t.Context(), a.database, id, false)
	if err != nil {
		t.Fatal(err)
	}
	assertResponse(t, a, cookie, "POST", fmt.Sprintf("/units/%d/rename", units[0].ID), url.Values{"name": {" "}}, 400, "请填写单元名称")
	assertResponse(t, a, cookie, "POST", "/people/new", url.Values{"name": {" "}, "phone": {"123"}}, 400, "请填写联系人姓名", `value="123"`)
	personID, err := person.Create(t.Context(), a.database, person.Details{Name: "Person"})
	if err != nil {
		t.Fatal(err)
	}
	assertResponse(t, a, cookie, "POST", fmt.Sprintf("/people/%d/edit", personID), url.Values{"name": {" "}}, 400, "请填写联系人姓名")
	for _, path := range []string{"/properties/999", "/properties/no-number", "/properties/999/edit", "/people/999/edit"} {
		assertResponse(t, a, cookie, "GET", path, nil, 404, "未找到此页面或记录")
	}
	for _, path := range []string{"/properties/999/edit", "/properties/999/units", "/properties/999/archive", "/properties/999/restore", "/units/999/rename", "/units/999/archive", "/units/999/restore", "/units/999/token", "/people/999/edit"} {
		assertResponse(t, a, cookie, "POST", path, url.Values{"name": {"Missing"}}, 404, "未找到此页面或记录")
	}
	for _, route := range []struct {
		path   string
		status int
	}{
		{detail, 405},
		{fmt.Sprintf("/units/%d", units[0].ID), 404},
		{fmt.Sprintf("/people/%d", personID), 404},
	} {
		assertResponse(t, a, cookie, "DELETE", route.path, nil, route.status)
	}
	if _, err := property.Get(t.Context(), a.database, id); err != nil {
		t.Fatal(err)
	}
	if _, err := property.GetUnit(t.Context(), a.database, units[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := person.Get(t.Context(), a.database, personID); err != nil {
		t.Fatal(err)
	}
}

func TestM2EscapesContactAndPropertyContent(t *testing.T) {
	a := newApp(t)
	a.setup(t)
	cookie := a.login(t)
	payload := `<script>alert("x")</script>`
	assertResponse(t, a, cookie, "POST", "/properties/new", url.Values{"name": {payload}, "address": {payload}, "notes": {payload}, "first_unit_name": {payload}}, 303)
	assertResponse(t, a, cookie, "POST", "/people/new", url.Values{"name": {payload}, "notes": {payload}}, 303)
	for _, path := range []string{"/properties", "/properties/1", "/properties/1/edit", "/people", "/people/1/edit"} {
		body := assertResponse(t, a, cookie, "GET", path, nil, 200, "&lt;script&gt;")
		if strings.Contains(body, payload) {
			t.Fatalf("unescaped content on %s", path)
		}
	}
}
