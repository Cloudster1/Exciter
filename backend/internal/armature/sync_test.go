package armature

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestALinkTitleNamesKeyTitleAndRelationWithinArmaturesBound(t *testing.T) {
	if got := LinkTitle("CHK-R42", " Brakes engage within 100 ms ", Implements); got != "CHK-R42 Brakes engage within 100 ms (implements)" {
		t.Errorf("a short title is %q", got)
	}
	long := LinkTitle("CHK-R42", strings.Repeat("Ü", 300), Verifies)
	if n := utf8.RuneCountInString(long); n != MaxLinkTitleLength {
		t.Errorf("a long title has %d characters, want %d", n, MaxLinkTitleLength)
	}
	if !strings.HasPrefix(long, "CHK-R42 ÜÜ") || !strings.HasSuffix(long, "Ü... (verifies)") {
		t.Errorf("a long title loses its key or its relation: %q", long)
	}
	exact := LinkTitle("A-R1", strings.Repeat("x", MaxLinkTitleLength-len("A-R1  (relates)")), Relates)
	if utf8.RuneCountInString(exact) != MaxLinkTitleLength || strings.Contains(exact, "...") {
		t.Errorf("a title that fits exactly was cut: %q", exact)
	}
}

func TestARequirementWantsOneLinkPerIssueTitledWithItsFirstRelation(t *testing.T) {
	id := uuid.New()
	req := RequirementState{ID: id, Found: true, Key: "CHK-R7", Title: "Log every login"}
	links := []LinkedIssue{
		{Key: "CP-1", Relation: Relates}, {Key: "CP-1", Relation: Verifies},
		{Key: "CP-2", Relation: Implements},
		{Key: "CP-3", Relation: Implements, Broken: true},
		{Key: "CP-4", Relation: Implements, Detached: true},
	}
	want := WantedLinks("https://exciter.example/", req, links)
	if len(want) != 2 {
		t.Fatalf("wanted %v, want CP-1 and CP-2 alone", want)
	}
	if got := want["CP-1"]; got.Title != "CHK-R7 Log every login (verifies)" || got.URL != "https://exciter.example/r/"+id.String() {
		t.Errorf("CP-1 wants %+v", got)
	}
	if got := want["CP-2"].Title; got != "CHK-R7 Log every login (implements)" {
		t.Errorf("CP-2 wants %q", got)
	}
	for name, state := range map[string]RequirementState{"trashed": {ID: id, Found: true, Trashed: true}, "purged": {ID: id}} {
		if got := WantedLinks("https://exciter.example", state, links); len(got) != 0 {
			t.Errorf("a %s requirement wants %v", name, got)
		}
	}
}

func TestPlanLinksChangesOnlyWhatDiffers(t *testing.T) {
	remote := uuid.New()
	want := map[string]WantedLink{
		"CP-1": {URL: "u", Title: "kept"},
		"CP-2": {URL: "u", Title: "renamed"},
		"CP-3": {URL: "u", Title: "failed before"},
		"CP-4": {URL: "u", Title: "new"},
	}
	have := []LinkRecord{
		{Key: "CP-1", RemoteID: &remote, URL: "u", Title: "kept", State: SyncSynced},
		{Key: "CP-2", RemoteID: &remote, URL: "u", Title: "old", State: SyncSynced},
		{Key: "CP-3", URL: "u", Title: "failed before", State: SyncFailed},
		{Key: "CP-9", RemoteID: &remote, URL: "u", Title: "gone", State: SyncSynced},
	}
	var got []string
	for _, step := range PlanLinks(want, have) {
		what := "put"
		if step.Want == nil {
			what = "take off"
		}
		got = append(got, step.Key+" "+what)
	}
	if strings.Join(got, ", ") != "CP-2 put, CP-3 put, CP-4 put, CP-9 take off" {
		t.Errorf("the plan is %v", got)
	}
	if steps := PlanLinks(map[string]WantedLink{"CP-1": {URL: "u", Title: "kept"}}, have[:1]); len(steps) != 0 {
		t.Errorf("a synced link is sent again: %v", steps)
	}
}
