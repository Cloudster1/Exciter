package webhook

import (
	"strconv"
	"testing"
)

func TestExciterReadsItsVersionAndOneFromBeforeVersions(t *testing.T) {
	for v, want := range map[int]bool{0: true, Version: true, Version + 1: false, -1: false} {
		if got := Reads(v); got != want {
			t.Errorf("Reads(%d) = %t, want %t", v, got, want)
		}
	}
	for h, want := range map[string]bool{"": true, strconv.Itoa(Version): true, strconv.Itoa(Version + 1): false, "0": false, "v1": false, "1.0": false} {
		if got := ReadsHeader(h); got != want {
			t.Errorf("ReadsHeader(%q) = %t, want %t", h, got, want)
		}
	}
}
