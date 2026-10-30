package auth

import "testing"

func TestKnownTimezone(t *testing.T) {
	for zone, want := range map[string]bool{
		"":                    true,
		"Europe/Berlin":       true,
		"America/Los_Angeles": true,
		"UTC":                 true,
		"Mars/Olympus":        false,
		"Local":               false,
		" Europe/Berlin":      false,
		"europe/berlin":       false,
	} {
		if got := KnownTimezone(zone); got != want {
			t.Errorf("KnownTimezone(%q) = %v, want %v", zone, got, want)
		}
	}
}
