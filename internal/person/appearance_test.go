package person

import "testing"

func TestAppearance(t *testing.T) {
	for name, want := range map[string]string{"Emily Chen": "EC", "王小明": "王", "single": "S", "  mia  lopez  ": "ML", "": "?"} {
		initials, color := Appearance(name)
		if initials != want {
			t.Errorf("Appearance(%q) initials = %q, want %q", name, initials, want)
		}
		_, again := Appearance(name)
		if color != again || len(color) != 7 || color[0] != '#' {
			t.Errorf("Appearance(%q) color is not stable CSS: %q, %q", name, color, again)
		}
	}
}
