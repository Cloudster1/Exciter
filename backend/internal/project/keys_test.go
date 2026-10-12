package project

import "testing"

func TestKeysAreTwoToTenCapitalsAndDigitsLedByALetter(t *testing.T) {
	for key, want := range map[string]bool{
		"CHK": true, "SYS2": true, "AB": true, "ABCDEFGHIJ": true,
		"A": false, "ABCDEFGHIJK": false, "2SYS": false, "chk": false, "CH-K": false, "": false, "ÄBC": false,
	} {
		if got := ValidKey(key); got != want {
			t.Errorf("ValidKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestAKeyTypedIsTrimmedAndUpperCased(t *testing.T) {
	if got := NormalizeKey("  chk2 "); got != "CHK2" {
		t.Errorf("NormalizeKey = %q, want CHK2", got)
	}
}

func TestAKeyIsSuggestedAsAPersonWould(t *testing.T) {
	for name, want := range map[string]string{
		"Checkout service":              "CS",
		"Checkout":                      "CHECKOUT",
		"Brake-by-wire controller unit": "BBWCU",
		"Telemetry and diagnostics":     "TAD",
		"Q":                             "QQ",
		"A very long name with eleven words in it for sure ok": "AVLNWEWIIF",
		"Übersicht":            "BERSICHT",
		"2024 roadmap":         "",
		"!!!":                  "",
		"Supercalifragilistic": "SUPERCALIF",
	} {
		got := SuggestKey(name)
		if got != want {
			t.Errorf("SuggestKey(%q) = %q, want %q", name, got, want)
		}
		if got != "" && !ValidKey(got) {
			t.Errorf("SuggestKey(%q) = %q, which is not a valid key", name, got)
		}
	}
}

func TestAKeyTakenIsNumberedWithinTenCharacters(t *testing.T) {
	for _, c := range []struct {
		base string
		n    int
		want string
	}{
		{"CHK", 1, "CHK"}, {"CHK", 2, "CHK2"}, {"ABCDEFGHIJ", 2, "ABCDEFGHI2"}, {"ABCDEFGHIJ", 12, "ABCDEFGH12"},
	} {
		if got := numbered(c.base, c.n); got != c.want || !ValidKey(got) {
			t.Errorf("numbered(%q, %d) = %q, want %q", c.base, c.n, got, c.want)
		}
	}
}
