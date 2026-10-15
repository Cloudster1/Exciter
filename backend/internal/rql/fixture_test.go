package rql

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
)

// Friday 4 September 2026, mid-morning, in UTC.
var testNow = time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)

var me = uuid.MustParse("00000000-0000-0000-0000-00000000a11a")

// The ids of the sample project's attributes, options and link types, by what they are.
var (
	riskID     = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	lowID      = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	highID     = uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
	pointsID   = uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
	dueID      = uuid.MustParse("00000000-0000-0000-0000-0000000000a3")
	reviewerID = uuid.MustParse("00000000-0000-0000-0000-0000000000a4")
	safetyID   = uuid.MustParse("00000000-0000-0000-0000-0000000000a5")
	notesID    = uuid.MustParse("00000000-0000-0000-0000-0000000000a6")
	detailID   = uuid.MustParse("00000000-0000-0000-0000-0000000000a7")
	tagsID     = uuid.MustParse("00000000-0000-0000-0000-0000000000a8")
	derivesID  = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
	verifiesID = uuid.MustParse("00000000-0000-0000-0000-0000000000c2")
	relatesID  = uuid.MustParse("00000000-0000-0000-0000-0000000000c3")
	statusID   = uuid.MustParse("00000000-0000-0000-0000-0000000000c4")
)

// sample is a project with an attribute of every kind and the seeded link types, one of them named like a field.
func sample() *Catalog {
	return NewCatalog([]attribute.Def{
		{ID: riskID, Name: "Risk", Kind: attribute.Select, Options: []attribute.Option{{ID: lowID, Name: "Low"}, {ID: highID, Name: "High"}}},
		{ID: pointsID, Name: "Story points", Kind: attribute.Number},
		{ID: dueID, Name: "Due", Kind: attribute.Date},
		{ID: reviewerID, Name: "Reviewer", Kind: attribute.User},
		{ID: safetyID, Name: "Safety", Kind: attribute.Boolean},
		{ID: notesID, Name: "Notes", Kind: attribute.Text},
		{ID: detailID, Name: "Detail", Kind: attribute.RichText},
		{ID: tagsID, Name: "Tags", Kind: attribute.MultiSelect, Options: []attribute.Option{{ID: lowID, Name: "ui"}}},
	}, []LinkType{
		{ID: derivesID, Key: "derives", Forward: "derives from", Backward: "is derived into"},
		{ID: verifiesID, Key: "verifies", Forward: "verifies", Backward: "is verified by"},
		{ID: relatesID, Key: "relates", Forward: "relates to", Backward: "relates to"},
		{ID: statusID, Key: "gates", Forward: "status", Backward: "is gated by"},
	})
}

func compile(t *testing.T, text string) *Compiled {
	t.Helper()
	q, err := Parse(text)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	c, err := q.Compile(sample(), Env{UserID: me, Now: testNow})
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return c
}
