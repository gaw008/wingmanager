package web

import (
	"testing"

	"github.com/gaw008/wingmanager/internal/settings"
)

func TestCatalogKeysIdentical(t *testing.T) {
	chinese := catalogs[settings.LocaleChinese]
	english := catalogs[settings.LocaleEnglish]
	if len(catalogs) != len(settings.Locales()) || len(chinese) == 0 || len(chinese) != len(english) {
		t.Fatal("catalog sizes differ or are empty")
	}
	for key, value := range chinese {
		if value == "" || english[key] == "" {
			t.Errorf("missing translation for %q", key)
		}
	}
	for key := range english {
		if _, ok := chinese[key]; !ok {
			t.Errorf("Chinese catalog missing %q", key)
		}
	}
	chineseCounts := countCatalogs[settings.LocaleChinese]
	englishCounts := countCatalogs[settings.LocaleEnglish]
	if len(countCatalogs) != len(settings.Locales()) || len(chineseCounts) == 0 || len(chineseCounts) != len(englishCounts) {
		t.Fatal("count catalog sizes differ or are empty")
	}
	for key, message := range chineseCounts {
		if message.other == "" || message.one != "" {
			t.Errorf("Chinese count translation %q must have a single other form", key)
		}
		if englishCounts[key].one == "" || englishCounts[key].other == "" {
			t.Errorf("English count translation %q requires both one and other forms", key)
		}
		for locale, catalog := range catalogs {
			if _, ok := catalog[key]; ok {
				t.Errorf("duplicate regular and count translation %s/%s", locale, key)
			}
		}
	}
	for key := range englishCounts {
		if _, ok := chineseCounts[key]; !ok {
			t.Errorf("Chinese count catalog missing %q", key)
		}
	}
}
