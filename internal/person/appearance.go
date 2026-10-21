package person

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// Appearance returns stable avatar initials and a pastel color for a name.
// Han names use the first character; other names use the first and last initials.
func Appearance(name string) (initials string, color string) {
	name = strings.TrimSpace(name)
	colors := [...]string{"#f6c98b", "#f3a6b4", "#9cc7f2", "#c9b6f2", "#a7d8de", "#f2b8a2"}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	color = colors[hash.Sum32()%uint32(len(colors))]
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?", color
	}
	first := []rune(parts[0])[0]
	initials = string(unicode.ToUpper(first))
	if len(parts) > 1 && !unicode.Is(unicode.Han, first) {
		initials += string(unicode.ToUpper([]rune(parts[len(parts)-1])[0]))
	}
	return initials, color
}
