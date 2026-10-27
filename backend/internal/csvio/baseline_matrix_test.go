package csvio

import (
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

func TestASigningNoteSaysWhereTheSigningStands(t *testing.T) {
	for _, c := range []struct {
		name    string
		signing *baseline.Signing
		want    string
	}{
		{"nobody signs the project's baselines", nil, ""},
		{"nobody signed it yet", &baseline.Signing{Required: 2}, ""},
		{"one of two signed it", &baseline.Signing{Required: 2, Signatures: 1}, "This baseline is being signed: 1 of 2 signatures so far. It never changes."},
		{"both signed it", &baseline.Signing{Required: 2, Signatures: 2, Signed: true}, "This baseline is signed, with 2 of 2 signatures, and never changes."},
	} {
		if got := SigningNote(c.signing); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Each link is named with its suspect mark and, compared, how it changed; every cell is text a spreadsheet never
// takes for a formula; the file says when it holds only part of the baseline and where its signing stands.
func TestABaselineMatrixFileMarksEachLinkAsItChanged(t *testing.T) {
	suspect, clear := true, false
	m := &baseline.Matrix{
		Signing: &baseline.Signing{Required: 2, Signatures: 1},
		Rows:    []requirement.Ref{{Key: "CHK-R1", Title: "=Brakes"}, {Key: "CHK-R2", Title: "+Lights"}},
		Columns: []trace.MatrixColumn{{Requirement: &requirement.Ref{Key: "CHK-R3"}}, {IssueKey: "@WEB-1"}},
		Cells: []baseline.MatrixCell{
			{Row: 0, Column: 0, Links: []baseline.MatrixLink{
				{Name: "Derives", Suspect: &suspect, Change: baseline.Changed},
				{Name: "Verifies", Suspect: &clear, Change: baseline.Added},
			}},
			{Row: 1, Column: 1, Links: []baseline.MatrixLink{{Name: "-implements", Change: baseline.Removed}}},
			{Row: 1, Column: 0, Links: []baseline.MatrixLink{{Name: "Refines", Suspect: &suspect}}},
		},
		Total: 3,
	}
	var b strings.Builder
	if err := WriteBaselineMatrix(&b, m); err != nil {
		t.Fatal(err)
	}
	want := "Key,Title,CHK-R3,'@WEB-1\n" +
		"CHK-R1,'=Brakes,Derives (suspect) (changed); Verifies (added),\n" +
		"CHK-R2,'+Lights,Refines (suspect),'-implements (removed)\n" +
		"This file holds the first 2 of the 3 requirements the baseline holds.\n" +
		"This baseline is being signed: 1 of 2 signatures so far. It never changes.\n"
	if b.String() != want {
		t.Errorf("wrote\n%s\nwant\n%s", b.String(), want)
	}
}

func TestABaselineMatrixFileOfAWholeUnsignedBaselineEndsWithItsRows(t *testing.T) {
	m := &baseline.Matrix{
		Signing: &baseline.Signing{Required: 1},
		Rows:    []requirement.Ref{{Key: "CHK-R1", Title: "Brakes"}},
		Columns: []trace.MatrixColumn{{IssueKey: "WEB-1"}},
		Total:   1,
	}
	var b strings.Builder
	if err := WriteBaselineMatrix(&b, m); err != nil {
		t.Fatal(err)
	}
	if want := "Key,Title,WEB-1\nCHK-R1,Brakes,\n"; b.String() != want {
		t.Errorf("wrote\n%s\nwant\n%s", b.String(), want)
	}
}
