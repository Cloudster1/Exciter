package csvio

import (
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

func TestAMatrixFileNamesTheLinksOfEachCell(t *testing.T) {
	m := &trace.Matrix{
		Rows:    []trace.MatrixRow{{Ref: requirement.Ref{Key: "CHK-R1", Title: "=Brakes"}}, {Ref: requirement.Ref{Key: "CHK-R2", Title: "Lights"}}},
		Columns: []trace.MatrixColumn{{IssueKey: "WEB-1"}, {IssueKey: "WEB-2"}},
		Cells: []trace.MatrixCell{
			{Row: 0, Column: 1, Links: []trace.MatrixLink{{Name: "implements"}, {Name: "verifies", Suspect: true}}},
			{Row: 1, Column: 0, Links: []trace.MatrixLink{{Name: "relates"}}},
		},
		Total: 3,
	}
	var b strings.Builder
	if err := WriteMatrix(&b, m); err != nil {
		t.Fatal(err)
	}
	want := "Key,Title,WEB-1,WEB-2\n" +
		"CHK-R1,'=Brakes,,implements; verifies (suspect)\n" +
		"CHK-R2,Lights,relates,\n" +
		"This file holds the first 2 of the 3 requirements the filter keeps. Narrow the filter to export the rest.\n"
	if b.String() != want {
		t.Errorf("wrote\n%s\nwant\n%s", b.String(), want)
	}
}
