package stator

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestALinkTitleNamesKeyTitleAndRelationWithinStatorsBound(t *testing.T) {
	if got := LinkTitle("CHK-R42", " Brakes engage within 100 ms ", Specifies); got != "CHK-R42 Brakes engage within 100 ms (specifies)" {
		t.Errorf("a short title is %q", got)
	}
	long := LinkTitle("CHK-R42", strings.Repeat("Ü", 300), Describes)
	if n := utf8.RuneCountInString(long); n != MaxLinkTitleLength {
		t.Errorf("a long title has %d characters, want %d", n, MaxLinkTitleLength)
	}
	if !strings.HasPrefix(long, "CHK-R42 ÜÜ") || !strings.HasSuffix(long, "Ü... (describes)") {
		t.Errorf("a long title loses its key or its relation: %q", long)
	}
	exact := LinkTitle("A-R1", strings.Repeat("x", MaxLinkTitleLength-len("A-R1  (relates)")), Relates)
	if utf8.RuneCountInString(exact) != MaxLinkTitleLength || strings.Contains(exact, "...") {
		t.Errorf("a title that fits exactly was cut: %q", exact)
	}
}

func TestARequirementWantsOneLinkPerPageTitledWithItsFirstRelation(t *testing.T) {
	id := uuid.New()
	one, two, broken, detached := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	req := RequirementState{ID: id, Found: true, Key: "CHK-R7", Title: "Log every login"}
	links := []LinkedPage{
		{PageID: one, Relation: Relates}, {PageID: one, Relation: Describes},
		{PageID: two, Relation: Specifies},
		{PageID: broken, Relation: Specifies, Broken: true},
		{PageID: detached, Relation: Specifies, Detached: true},
	}
	want := WantedLinks("https://exciter.example/", req, links)
	if len(want) != 2 {
		t.Fatalf("wanted %v, want two pages alone", want)
	}
	if got := want[one]; got.Title != "CHK-R7 Log every login (describes)" || got.URL != "https://exciter.example/r/"+id.String() {
		t.Errorf("the first page wants %+v", got)
	}
	if got := want[two].Title; got != "CHK-R7 Log every login (specifies)" {
		t.Errorf("the second page wants %q", got)
	}
	for name, state := range map[string]RequirementState{"trashed": {ID: id, Found: true, Trashed: true}, "purged": {ID: id}} {
		if got := WantedLinks("https://exciter.example", state, links); len(got) != 0 {
			t.Errorf("a %s requirement wants %v", name, got)
		}
	}
}

func TestPlanLinksChangesOnlyWhatDiffers(t *testing.T) {
	remote := uuid.New()
	page := func(n byte) uuid.UUID { return uuid.UUID{n} }
	want := map[uuid.UUID]WantedLink{
		page(1): {URL: "u", Title: "kept"},
		page(2): {URL: "u", Title: "renamed"},
		page(3): {URL: "u", Title: "failed before"},
		page(4): {URL: "u", Title: "new"},
	}
	have := []LinkRecord{
		{PageID: page(1), RemoteID: &remote, URL: "u", Title: "kept", State: SyncSynced},
		{PageID: page(2), RemoteID: &remote, URL: "u", Title: "old", State: SyncSynced},
		{PageID: page(3), URL: "u", Title: "failed before", State: SyncFailed},
		{PageID: page(9), RemoteID: &remote, URL: "u", Title: "gone", State: SyncSynced},
	}
	var got []string
	for _, step := range PlanLinks(want, have) {
		what := "put"
		if step.Want == nil {
			what = "take off"
		}
		got = append(got, string('0'+step.PageID[0])+" "+what)
	}
	if strings.Join(got, ", ") != "2 put, 3 put, 4 put, 9 take off" {
		t.Errorf("the plan is %v", got)
	}
	if steps := PlanLinks(map[uuid.UUID]WantedLink{page(1): {URL: "u", Title: "kept"}}, have[:1]); len(steps) != 0 {
		t.Errorf("a synced link is sent again: %v", steps)
	}
}
