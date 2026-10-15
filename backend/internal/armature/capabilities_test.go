package armature

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// An Armature not asked yet offers everything, so upgrading Exciter alone switches nothing off; once asked, a name
// counts only as it is spelled, so a capability@2 does not stand in for the @1 a feature was built against.
func TestWhatArmatureOffersIsReadByExactName(t *testing.T) {
	if missing := Offered(nil).Missing(); len(missing) != 0 {
		t.Errorf("an Armature not asked yet lacks %v", missing)
	}
	all := Offered(Needs())
	if missing := all.Missing(); len(missing) != 0 {
		t.Errorf("an Armature offering every need lacks %v", missing)
	}
	newer := Offered{CapabilityIssues, CapabilityWebhooks, CapabilityThemes, "remote-links@2"}
	lacks := newer.Lacks(CapabilityRemoteLinks)
	if lacks == nil || !errors.Is(lacks, ErrLacks) {
		t.Fatalf("remote-links@2 stood in for %s", CapabilityRemoteLinks)
	}
	if !strings.Contains(lacks.Message, CapabilityRemoteLinks) || !strings.Contains(lacks.Message, "Update Armature") {
		t.Errorf("the sentence names neither the capability nor what to do: %q", lacks.Message)
	}
	got := []string{}
	for _, m := range (Offered{}).Missing() {
		got = append(got, m.Capability)
	}
	if !slices.Equal(got, Needs()) {
		t.Errorf("an Armature offering nothing lacks %v, want every need %v", got, Needs())
	}
}
