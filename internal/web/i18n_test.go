package web

import (
	"github.com/gaw008/wingmanager/internal/settings"
	"testing"
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
}
