package csvio

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

func TestAChainFileReadsEachPathAndWhereItBreaks(t *testing.T) {
	need := &requirement.Ref{Key: "CHK-R1", Title: "Stop within 40 m"}
	system := &requirement.Ref{Key: "CHK-R2", Title: "Brakes engage within 100 ms"}
	page := &trace.MatrixPage{ID: uuid.MustParse("0195f000-0000-7000-8000-00000000b001"), SpaceKey: "ENG"}
	c := &trace.Chains{
		Levels: []trace.ModelLevel{{Name: "Need"}, {Name: "System"}, {Name: "Issue"}},
		Roots:  []requirement.Ref{*need},
		Lines: []trace.ChainLine{
			{Nodes: []trace.ChainNode{{Requirement: need}, {Requirement: system, Suspect: true}, {IssueKey: "WEB-1"}}},
			{Nodes: []trace.ChainNode{{Requirement: need}, {Page: page}}, Broken: true},
		},
		Total: 2,
	}
	var b strings.Builder
	if err := WriteChains(&b, c); err != nil {
		t.Fatal(err)
	}
	want := "Need,System,Issue,Breaks after\n" +
		"CHK-R1 Stop within 40 m,CHK-R2 Brakes engage within 100 ms (suspect),WEB-1,\n" +
		"CHK-R1 Stop within 40 m,ENG/0195f000-0000-7000-8000-00000000b001,,System\n" +
		"This file holds the chains of the first 1 of the 2 requirements the filter keeps and at most 20000 lines. Narrow the filter to export the rest.\n"
	if b.String() != want {
		t.Errorf("wrote\n%s\nwant\n%s", b.String(), want)
	}
}
