package label

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeMakesOneLowerCaseWord(t *testing.T) {
	for raw, want := range map[string]string{
		"backend":               "backend",
		"  Backend ":            "backend",
		"Release Notes":         "release-notes",
		"release \t  notes":     "release-notes",
		"v1.2":                  "v1.2",
		"team_a-b.c":            "team_a-b.c",
		"Übersicht":             "übersicht",
		"2026":                  "2026",
		strings.Repeat("a", 40): strings.Repeat("a", 40),
		strings.Repeat("ä", 40): strings.Repeat("ä", 40),
	} {
		got, err := Normalize(raw)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestNormalizeRefusesWhatIsNoLabel(t *testing.T) {
	for _, raw := range []string{"", "   ", "-lead", "_lead", ".", "..", "a/b", "a?b", "a#b", "a%20", "semi;colon", "quote\"", "é", "emoji😀", "tab\x00"} {
		if _, err := Normalize(raw); !errors.Is(err, ErrBadName) {
			t.Errorf("Normalize(%q) = %v, want ErrBadName", raw, err)
		}
	}
	if _, err := Normalize(strings.Repeat("a", MaxNameLength+1)); !errors.Is(err, ErrTooLong) {
		t.Errorf("a name past %d characters is %v", MaxNameLength, err)
	}
}

// What a person types so far is matched as a prefix, literally.
func TestLikePrefixTakesTheTypedTextLiterally(t *testing.T) {
	for typed, want := range map[string]string{
		"":          "%",
		"Rel":       "rel%",
		"release n": "release-n%",
		"release ":  "release-%",
		"a_b%c":     `a\_b\%c%`,
		`back\`:     `back\\%`,
	} {
		if got := likePrefix(typedPrefix(typed)); got != want {
			t.Errorf("likePrefix(typedPrefix(%q)) = %q, want %q", typed, got, want)
		}
	}
}
