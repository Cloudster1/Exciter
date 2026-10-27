package trace

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// matrixRule is one refusal of checkMatrix as the contract states it: the field it names when it applies.
type matrixRule struct {
	field   string
	applies func(q MatrixQuery) bool
}

// isEvidence says the columns are tests or code, which a repository narrows and no relation does.
func isEvidence(c MatrixColumns) bool { return c == ColumnsTests || c == ColumnsCode }

// matrixRules are the refusals in the order a matrix query meets them, so a query breaking two names the first.
var matrixRules = []matrixRule{
	{"columns", func(q MatrixQuery) bool { return !slices.Contains(MatrixColumnKinds, q.Columns) }},
	{"direction", func(q MatrixQuery) bool { return !slices.Contains(MatrixDirections, q.Direction) }},
	{"relation", func(q MatrixQuery) bool { return q.Columns == ColumnsRequirements && len(q.Relations) > 0 }},
	{"linkType", func(q MatrixQuery) bool { return q.Columns != ColumnsRequirements && len(q.LinkTypes) > 0 }},
	{"columnRepository", func(q MatrixQuery) bool { return !isEvidence(q.Columns) && q.ColumnRepository != "" }},
	{"relation", func(q MatrixQuery) bool { return isEvidence(q.Columns) && len(q.Relations) > 0 }},
	{"columnQuery", func(q MatrixQuery) bool { return q.Columns != ColumnsRequirements && q.ColumnFilter != nil }},
	{"gaps", func(q MatrixQuery) bool { return q.Gaps != "" && !slices.Contains(GapKinds, q.Gaps) }},
	{"gaps", func(q MatrixQuery) bool {
		return (q.Gaps == GapsColumns || q.Gaps == GapsBoth) && q.ColumnFilter == nil
	}},
}

// Every combination of what a matrix query names is refused on the field of the first rule it breaks, or answered
// with its defaults filled in.
func TestEveryMatrixQueryIsRefusedOnItsFieldOrFilledIn(t *testing.T) {
	columns := append([]MatrixColumns{"", "everything"}, MatrixColumnKinds...)
	directions := append([]MatrixDirection{"", "sideways"}, MatrixDirections...)
	gaps := append([]Gaps{"", "sideways"}, GapKinds...)
	tried := 0
	for _, c := range columns {
		for _, d := range directions {
			for _, g := range gaps {
				for _, relation := range []bool{false, true} {
					for _, linkType := range []bool{false, true} {
						for _, repository := range []bool{false, true} {
							for _, filtered := range []bool{false, true} {
								q := MatrixQuery{Columns: c, Direction: d, Gaps: g}
								if relation {
									q.Relations = []string{"verifies"}
								}
								if linkType {
									q.LinkTypes = []string{"derives"}
								}
								if repository {
									q.ColumnRepository = "brakes"
								}
								if filtered {
									q.ColumnFilter = &search.Filter{}
								}
								checkOne(t, q)
								tried++
							}
						}
					}
				}
			}
		}
	}
	if want := len(columns) * len(directions) * len(gaps) * 16; tried != want {
		t.Errorf("tried %d combinations, want %d", tried, want)
	}
}

// checkOne holds one query to the rules: the first that applies names the field, and none filling the defaults.
func checkOne(t *testing.T, q MatrixQuery) {
	t.Helper()
	filled := q
	if filled.Columns == "" {
		filled.Columns = ColumnsRequirements
	}
	if filled.Direction == "" {
		filled.Direction = DirectionBoth
	}
	wantField := ""
	for _, rule := range matrixRules {
		if rule.applies(filled) {
			wantField = rule.field
			break
		}
	}
	got := q
	err := checkMatrix(&got)
	name := fmt.Sprintf("columns %q, direction %q, gaps %q, relations %v, link types %v, repository %q, column filter %v",
		q.Columns, q.Direction, q.Gaps, q.Relations, q.LinkTypes, q.ColumnRepository, q.ColumnFilter != nil)
	var field *FieldError
	switch {
	case wantField == "" && err != nil:
		t.Errorf("%s: refused with %v, want it answered", name, err)
	case wantField != "" && !errors.As(err, &field):
		t.Errorf("%s: answered %v, want a refusal on %s", name, err, wantField)
	case wantField != "" && field.Field != wantField:
		t.Errorf("%s: refused on %s, want %s", name, field.Field, wantField)
	case wantField != "" && (field.Message == "" || field.Message[len(field.Message)-1] != '.'):
		t.Errorf("%s: the refusal %q is no sentence", name, field.Message)
	case wantField == "" && (got.Columns != filled.Columns || got.Direction != filled.Direction):
		t.Errorf("%s: filled in as columns %q and direction %q, want %q and %q", name, got.Columns, got.Direction, filled.Columns, filled.Direction)
	}
}

// The few combinations the issue names, said once more by hand so a change to the rules above cannot hide them.
func TestTheMatrixRefusalsTheContractNames(t *testing.T) {
	for _, c := range []struct {
		q     MatrixQuery
		field string
	}{
		{MatrixQuery{Columns: ColumnsTests, Relations: []string{"verifies"}}, "relation"},
		{MatrixQuery{Columns: ColumnsCode, Relations: []string{"implements"}}, "relation"},
		{MatrixQuery{Gaps: GapsBoth}, "gaps"},
		{MatrixQuery{Columns: ColumnsPages, ColumnRepository: "x"}, "columnRepository"},
		{MatrixQuery{Columns: ColumnsIssues, ColumnFilter: &search.Filter{}}, "columnQuery"},
		{MatrixQuery{Columns: ColumnsRequirements, Relations: []string{"implements"}}, "relation"},
	} {
		var field *FieldError
		if err := checkMatrix(&c.q); !errors.As(err, &field) || field.Field != c.field {
			t.Errorf("%+v: answered %v, want a refusal on %s", c.q, err, c.field)
		}
	}
	for _, q := range []MatrixQuery{
		{Columns: ColumnsTests, ColumnRepository: "brakes"},
		{Columns: ColumnsCode, ColumnRepository: "brakes"},
		{Columns: ColumnsPages, Relations: []string{"specifies"}},
		{ColumnFilter: &search.Filter{}, Gaps: GapsBoth},
	} {
		if err := checkMatrix(&q); err != nil {
			t.Errorf("%+v: refused with %v", q, err)
		}
	}
}

// wideLayout is one row linked to n requirement columns, as requirementLinks would read it.
func wideLayout(n int) (*laid, []search.Kept) {
	row := requirement.Ref{ID: uuid.New(), Key: "WID-R1"}
	l := &laid{}
	for i := range n {
		l.columns = append(l.columns, MatrixColumn{Requirement: &requirement.Ref{ID: uuid.New(), Key: fmt.Sprintf("WID-R%d", i+2)}})
		l.links = append(l.links, laidLink{row: row.ID, column: i, link: MatrixLink{Kind: KindTrace, Name: "Derives"}})
	}
	return l, []search.Kept{{Ref: row}}
}

// A matrix asked for whole refuses more columns than MatrixMaxColumns, and a window of the columns still answers.
func TestAWholeMatrixPastTheWidestIsRefusedAndAWindowIsNot(t *testing.T) {
	l, kept := wideLayout(MatrixMaxColumns + 1)
	var wide *TooWideError
	if err := l.into(&Matrix{}, kept, true, MatrixQuery{Limit: 1}); !errors.As(err, &wide) || wide.Columns != MatrixMaxColumns+1 {
		t.Fatalf("a whole matrix one column too wide answered %v", err)
	}
	if msg := wide.Error(); msg[len(msg)-1] != '.' {
		t.Errorf("the refusal %q is no sentence", msg)
	}

	m := &Matrix{}
	if err := l.into(m, kept, true, MatrixQuery{Limit: 1, ColumnLimit: MatrixColumnDefaultLimit, ColumnOffset: MatrixMaxColumns}); err != nil {
		t.Fatalf("a window of the wide matrix was refused: %v", err)
	}
	if m.ColumnTotal != MatrixMaxColumns+1 || len(m.Columns) != 1 || len(m.Cells) != 1 || m.Cells[0].Column != 0 {
		t.Errorf("the last window reads %d of %d columns and cells %v", len(m.Columns), m.ColumnTotal, m.Cells)
	}

	l, kept = wideLayout(MatrixMaxColumns)
	m = &Matrix{}
	if err := l.into(m, kept, true, MatrixQuery{Limit: 1}); err != nil || len(m.Columns) != MatrixMaxColumns || m.ColumnLimit != MatrixMaxColumns {
		t.Errorf("a whole matrix exactly as wide as allowed answered %d columns, limit %d: %v", len(m.Columns), m.ColumnLimit, err)
	}
}
