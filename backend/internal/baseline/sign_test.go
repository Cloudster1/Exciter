package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
)

// The hash is of the form the contract gives, whatever order the rows come in, so the database's guard and the
// service agree on it.
func TestContentHashIsOfTheAgreedForm(t *testing.T) {
	id := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	projectID := uuid.MustParse("01900000-0000-7000-8000-000000000002")
	a := uuid.MustParse("01900000-0000-7000-8000-00000000000a")
	b := uuid.MustParse("01900000-0000-7000-8000-00000000000b")
	linkType := uuid.MustParse("01900000-0000-7000-8000-0000000000f0")
	page := uuid.MustParse("01900000-0000-7000-8000-0000000000e0")
	c := Content{BaselineID: id, ProjectID: projectID,
		Items: []ContentItem{
			{RequirementID: b, Revision: 1, Path: []int32{1, 1}},
			{RequirementID: a, Revision: 3, Hash: "abc", Path: []int32{1}},
		},
		Links: []ContentLink{{TypeID: linkType, SourceID: b, TargetID: a}},
		Pages: []uuid.UUID{page},
	}
	text := "baseline " + id.String() + " " + projectID.String() + " -\n" +
		"item " + a.String() + " 3 abc 1\n" +
		"item " + b.String() + " 1 - 1.1\n" +
		"link " + linkType.String() + " " + b.String() + " " + a.String() + "\n" +
		"page " + page.String() + "\n"
	sum := sha256.Sum256([]byte(text))
	if got, want := ContentHash(c), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("ContentHash = %s, want %s of\n%s", got, want, text)
	}
	c.Items[0], c.Items[1] = c.Items[1], c.Items[0]
	if ContentHash(c) != hex.EncodeToString(sum[:]) {
		t.Error("the hash depends on the order the rows were read in")
	}
	root := a
	c.RootID = &root
	if ContentHash(c) == hex.EncodeToString(sum[:]) {
		t.Error("a baseline of a section hashes as one of the whole project")
	}
}

// A section deeper in the outline sorts after its parent and before the parent's next sibling, as integer[] does.
func TestContentHashOrdersItemsByOutline(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	one := Content{Items: []ContentItem{{RequirementID: ids[0], Path: []int32{2}}, {RequirementID: ids[1], Path: []int32{1, 9}},
		{RequirementID: ids[2], Path: []int32{1}}}}
	other := Content{Items: []ContentItem{{RequirementID: ids[2], Path: []int32{1}}, {RequirementID: ids[1], Path: []int32{1, 9}},
		{RequirementID: ids[0], Path: []int32{2}}}}
	if ContentHash(one) != ContentHash(other) {
		t.Error("the same items in another order hash differently")
	}
}
