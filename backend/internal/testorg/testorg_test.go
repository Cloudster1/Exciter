package testorg

import (
	"regexp"
	"strings"
	"testing"
)

// The org table's own check on a slug, with the suffix Create adds.
var slugShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

func TestAnyLabelMakesASlugTheTableAccepts(t *testing.T) {
	for label, want := range map[string]string{
		"themes":                            "themes",
		"Themes Spec.ts":                    "themes-spec-ts",
		"  --projects  and   specs--  ":     "projects-and-specs",
		"":                                  defaultLabel,
		"???":                               defaultLabel,
		"\u00dcberschrift":                  "berschrift",
		strings.Repeat("abcdefghij", 5):     "abcdefghijabcdefghijabcd",
		"a very long label - with - dashes": "a-very-long-label-with-d",
	} {
		got := slugPart(label)
		if got != want {
			t.Errorf("slugPart(%q) = %q, want %q", label, got, want)
		}
		if slug := got + "-0123abcd"; !slugShape.MatchString(slug) {
			t.Errorf("%q makes %q, which the org table refuses", label, slug)
		}
	}
}
