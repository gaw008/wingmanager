package web

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gaw008/wingmanager/internal/settings"
)

type navigationItem struct {
	URL, Key, Icon string
	Current        bool
}

func navigation(page string) []navigationItem {
	current := page
	switch {
	case strings.Contains(page, "booking"):
		current = "bookings"
	case strings.Contains(page, "lease") || page == "overdue":
		current = "leases"
	case strings.Contains(page, "money") || page == "reports" || page == "add_expense" || page == "add_income":
		current = "money"
	}
	items := []navigationItem{{URL: "/", Key: "today", Icon: "sun"}, {URL: "/calendar", Key: "calendar", Icon: "cal"}, {URL: "/bookings", Key: "nav_bookings", Icon: "bed"}, {URL: "/leases", Key: "nav_leases", Icon: "key"}, {URL: "/money", Key: "nav_money", Icon: "wallet"}}
	pages := []string{"today", "calendar", "bookings", "leases", "money"}
	for i := range items {
		items[i].Current = pages[i] == current
	}
	return items
}

func accountInitial(name, locale string) string {
	if name == "" {
		return catalogs[locale]["landlord_initial"]
	}
	initial, _ := utf8.DecodeRuneInString(name)
	return string(initial)
}

// redirectView preserves filters while selecting the replacement view.
func redirectView(path, key, value string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		query.Set(key, value)
		http.Redirect(w, r, path+"?"+query.Encode(), http.StatusSeeOther)
	}
}

func init() {
	values := map[string][2]string{
		"primary_navigation": {"主导航", "Main navigation"},
		"mobile_navigation":  {"主导航（手机）", "Mobile navigation"},
		"account_menu":       {"设置、房产、联系人", "Account, properties and contacts"},
		"properties_units":   {"房产与单元", "Properties and units"},
		"nav_bookings":       {"短租", "Bookings"},
		"nav_money":          {"账本", "Money"},
		"nav_leases":         {"长租", "Leases"},
		"landlord_initial":   {"房", "L"},
		"close":              {"关闭", "Close"},
	}
	for key, value := range values {
		catalogs[settings.LocaleChinese][key] = value[0]
		catalogs[settings.LocaleEnglish][key] = value[1]
	}
}
