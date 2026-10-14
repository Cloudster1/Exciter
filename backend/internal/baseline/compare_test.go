package baseline

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func TestClassifySaysHowEachRequirementDiffers(t *testing.T) {
	section := uuid.New()
	draft := &requirement.StatusRef{ID: uuid.New(), Name: "Draft", Category: "draft"}
	review := &requirement.StatusRef{ID: uuid.New(), Name: "In review", Category: "review"}
	at := func(id uuid.UUID, number, revision int, parent *uuid.UUID, rank string, status *requirement.StatusRef, path ...int32) held {
		return held{ID: id, Number: number, Revision: revision, Title: "T", Path: path, ParentID: parent, Rank: rank, Status: status}
	}
	same, revised, moved, transitioned, gone, added := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	from := []held{
		at(section, 1, 1, nil, "a", nil, 1),
		at(same, 2, 1, &section, "a", draft, 1, 1),
		at(revised, 3, 1, &section, "b", draft, 1, 2),
		at(moved, 4, 1, &section, "c", draft, 1, 3),
		at(transitioned, 5, 1, nil, "b", draft, 2),
		at(gone, 6, 2, nil, "c", nil, 3),
	}
	to := []held{
		at(section, 1, 1, nil, "a", nil, 1),
		at(moved, 4, 1, nil, "0", draft, 1),
		at(same, 2, 1, &section, "a", draft, 2, 1),
		at(revised, 3, 2, &section, "b", draft, 2, 2),
		at(added, 7, 1, &section, "c", nil, 2, 3),
		at(transitioned, 5, 1, nil, "b", review, 3),
	}
	got := Classify("CHK", from, to)
	type summary struct {
		Key                           string
		Change                        Change
		Revised, Moved, StatusChanged bool
		FromRevision, ToRevision      int
		FromOutline, ToOutline        string
	}
	var sums []summary
	for _, c := range got {
		s := summary{Key: c.Key, Change: c.Change, Revised: c.Revised, Moved: c.Moved, StatusChanged: c.StatusChanged}
		if c.From != nil {
			s.FromRevision, s.FromOutline = c.From.Revision, c.From.Outline
		}
		if c.To != nil {
			s.ToRevision, s.ToOutline = c.To.Revision, c.To.Outline
		}
		sums = append(sums, s)
	}
	want := []summary{
		{Key: "CHK-R4", Change: Changed, Moved: true, FromRevision: 1, ToRevision: 1, FromOutline: "1.3", ToOutline: "1"},
		{Key: "CHK-R3", Change: Changed, Revised: true, FromRevision: 1, ToRevision: 2, FromOutline: "1.2", ToOutline: "2.2"},
		{Key: "CHK-R7", Change: Added, ToRevision: 1, ToOutline: "2.3"},
		{Key: "CHK-R5", Change: Changed, StatusChanged: true, FromRevision: 1, ToRevision: 1, FromOutline: "2", ToOutline: "3"},
		{Key: "CHK-R6", Change: Removed, FromRevision: 2, FromOutline: "3"},
	}
	if !reflect.DeepEqual(sums, want) {
		t.Errorf("Classify =\n%+v\nwant\n%+v", sums, want)
	}
}

func TestDiffLinksAndExternal(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	derives, verifies := uuid.New(), uuid.New()
	keys := map[uuid.UUID]string{a: "CHK-R1", b: "CHK-R2", c: "CHK-R3"}
	keyOf := func(id uuid.UUID) string { return keys[id] }
	from := []heldLink{{derives, a, b, "Derives"}, {verifies, c, a, "Verifies"}}
	to := []heldLink{{derives, a, b, "Derives"}, {derives, c, b, "Derives"}}
	want := []LinkChange{
		{Change: Removed, TypeName: "Verifies", SourceKey: "CHK-R3", TargetKey: "CHK-R1"},
		{Change: Added, TypeName: "Derives", SourceKey: "CHK-R3", TargetKey: "CHK-R2"},
	}
	if got := DiffLinks(from, to, keyOf); !reflect.DeepEqual(got, want) {
		t.Errorf("DiffLinks = %+v, want %+v", got, want)
	}
	if got := DiffLinks(from, from, keyOf); len(got) != 0 {
		t.Errorf("a side against itself differs by %+v", got)
	}
	ext := DiffExternal([]heldExternal{{a, "CP-1", "implements"}}, []heldExternal{{a, "CP-1", "verifies"}}, keyOf)
	wantExt := []ExternalChange{
		{Change: Removed, RequirementKey: "CHK-R1", IssueKey: "CP-1", Relation: "implements"},
		{Change: Added, RequirementKey: "CHK-R1", IssueKey: "CP-1", Relation: "verifies"},
	}
	if !reflect.DeepEqual(ext, wantExt) {
		t.Errorf("DiffExternal = %+v, want %+v", ext, wantExt)
	}
}
