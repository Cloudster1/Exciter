package webhook

import (
	"strconv"
	"testing"
)

func TestExciterReadsItsVersionsAndAHeaderFromBeforeVersions(t *testing.T) {
	unknown := Versions[len(Versions)-1] + 1
	for h, want := range map[string]bool{"": true, strconv.Itoa(Version): true, strconv.Itoa(VersionCloudEvents): true, strconv.Itoa(unknown): false,
		"0": false, "v1": false, "1.0": false, " 2": false} {
		if got := ReadsHeader(h); got != want {
			t.Errorf("ReadsHeader(%q) = %t, want %t", h, got, want)
		}
	}
	if got := VersionList(); got != "1 or 2" {
		t.Errorf("the versions read as %q in a sentence", got)
	}
}
