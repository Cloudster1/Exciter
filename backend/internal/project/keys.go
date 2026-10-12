package project

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// keyShape matches the database constraint: two to ten characters, starting
// with a letter, upper case letters and digits only, as Armature's project keys.
var keyShape = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// ValidKey reports whether a key is usable.
func ValidKey(key string) bool { return keyShape.MatchString(key) }

// NormalizeKey upper-cases and trims a key somebody typed or put in an address.
func NormalizeKey(key string) string { return strings.ToUpper(strings.TrimSpace(key)) }

// SuggestKey derives a key from a name the way a person would: initials for
// several words, the leading letters of one. Empty when nothing usable is left.
func SuggestKey(name string) string {
	words := strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	clean := make([]string, 0, len(words))
	for _, w := range words {
		var b strings.Builder
		for _, r := range w {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			clean = append(clean, b.String())
		}
	}
	if len(clean) == 0 {
		return ""
	}
	key := clean[0]
	if len(clean) > 1 {
		var b strings.Builder
		for _, w := range clean {
			b.WriteByte(w[0])
		}
		key = b.String()
	}
	if len(key) > MaxKeyLength {
		key = key[:MaxKeyLength]
	}
	if key[0] < 'A' || key[0] > 'Z' {
		return ""
	}
	if len(key) == 1 {
		key += key
	}
	return key
}

// numbered is the nth try at a free key from base: the base itself, then the
// base with 2, 3 and so on, shortened so the whole stays a key.
func numbered(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := strconv.Itoa(n)
	if len(base)+len(suffix) > MaxKeyLength {
		base = base[:MaxKeyLength-len(suffix)]
	}
	return base + suffix
}
