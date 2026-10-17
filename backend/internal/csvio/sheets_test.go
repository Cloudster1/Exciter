package csvio

import (
	"testing"

	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/xlsx"
)

func TestAMatrixSheetColoursLinksAndGaps(t *testing.T) {
	m := &trace.Matrix{
		Rows:        []trace.MatrixRow{{Ref: requirement.Ref{Key: "CHK-R1", Title: "Brakes"}}, {Ref: requirement.Ref{Key: "CHK-R2", Title: "Horn"}}},
		Columns:     []trace.MatrixColumn{{IssueKey: "WEB-1"}, {IssueKey: "WEB-2"}},
		Cells:       []trace.MatrixCell{{Row: 0, Column: 0, Links: []trace.MatrixLink{{Name: "implements"}, {Name: "verifies", Suspect: true}}}},
		RowLinks:    []int{2, 0},
		ColumnLinks: []int{2, 0},
		Total:       2,
	}
	s := MatrixSheet("CHK matrix", m)
	if s.FreezeRows != 2 || s.FreezeColumns != 2 || !s.Filter {
		t.Errorf("the sheet freezes %d rows and %d columns, filter %v", s.FreezeRows, s.FreezeColumns, s.Filter)
	}
	if c := s.Rows[2][3]; c.Text != "implements; verifies (suspect)" || c.Style != xlsx.Suspect {
		t.Errorf("R1's cell reads %+v", c)
	}
	if c := s.Rows[3][2]; c.Count == nil || *c.Count != 0 || c.Style != xlsx.Gap {
		t.Errorf("R2's count reads %+v, a gap", c)
	}
	if c := s.Rows[1][4]; c.Style != xlsx.Gap {
		t.Errorf("WEB-2's count reads %+v, a gap", c)
	}
}

func TestAChainSheetMarksWhereALineBreaks(t *testing.T) {
	need := &requirement.Ref{Key: "CHK-R1", Title: "Stop"}
	c := &trace.Chains{
		Levels: []trace.ModelLevel{{Name: "Need"}, {Name: "System"}},
		Roots:  []requirement.Ref{*need},
		Lines:  []trace.ChainLine{{Nodes: []trace.ChainNode{{Requirement: need}}, Broken: true}},
		Total:  1,
	}
	s := ChainsSheet("CHK chains", c)
	if s.Rows[1][1].Style != xlsx.Break || s.Rows[1][2].Text != "Need" || len(s.Rows) != 2 {
		t.Errorf("the broken line reads %+v", s.Rows)
	}
}

func TestABaselineSheetColoursChanges(t *testing.T) {
	yes := true
	m := &baseline.Matrix{
		Rows:    []requirement.Ref{{Key: "CHK-R1", Title: "Brakes"}},
		Columns: []trace.MatrixColumn{{IssueKey: "WEB-1"}},
		Cells:   []baseline.MatrixCell{{Row: 0, Column: 0, Links: []baseline.MatrixLink{{Name: "implements", Suspect: &yes, Change: baseline.Removed}}}},
		Total:   1,
	}
	if c := BaselineMatrixSheet("Baseline matrix", m).Rows[1][2]; c.Text != "implements (suspect) (removed)" || c.Style != xlsx.Removed {
		t.Errorf("the removed link reads %+v", c)
	}
}
