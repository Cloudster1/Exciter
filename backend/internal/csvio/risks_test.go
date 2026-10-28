package csvio

import (
	"slices"
	"strconv"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	risks "github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/xlsx"
)

func number(v int) *int { return &v }

// cellTexts reads a row of the sheet as text, a number as its digits.
func cellTexts(row []xlsx.Cell) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = c.Text
		if c.Count != nil {
			out[i] = strconv.Itoa(*c.Count)
		}
	}
	return out
}

func TestTheRisksSheetLaysOutRatingsAndMitigations(t *testing.T) {
	mitigated := requirement.Mitigated
	unmitigated := requirement.Unmitigated
	high := &requirement.RiskLevel{Name: "High", Colour: "#dc2626"}
	low := &requirement.RiskLevel{Name: "Low", Colour: "#16a34a", Acceptable: true}
	rows := []risks.Row{
		{
			Risk: risks.RiskRef{Key: "CHK-R2", Title: "Brakes fail on ice", Type: requirement.TypeRef{Name: "Hazard"}},
			Rating: requirement.RiskView{
				Rating: requirement.Rating{Before: requirement.Side{Severity: number(4), Probability: number(3)},
					After: requirement.Side{Severity: number(1), Probability: number(1)}},
				BeforeRPN: number(12), AfterRPN: number(1), BeforeLevel: high, AfterLevel: low, State: &mitigated, Level: low,
			},
			Mitigations: []risks.Mitigation{
				{Source: risks.SourceRef{Key: "CHK-R1", Title: "Stop on ice"}, Implemented: trace.LevelNA, Verified: trace.LevelFull, InEffect: true},
				{Source: risks.SourceRef{Hidden: true}, Implemented: trace.LevelNA, Verified: trace.LevelNA, InEffect: true},
			},
		},
		{
			Risk:   risks.RiskRef{Key: "CHK-R3", Title: "Brakes squeal", Type: requirement.TypeRef{Name: "Hazard"}},
			Rating: requirement.RiskView{State: &unmitigated},
		},
	}
	sheet := RisksSheet("CHK risks", rows, nil, nil)
	if len(sheet.Rows) != 4 {
		t.Fatalf("a header, two rows for two mitigations and one for none, got %d rows", len(sheet.Rows))
	}
	header := cellTexts(sheet.Rows[0])
	at := func(name string) int {
		i := slices.Index(header, name)
		if i < 0 {
			t.Fatalf("the header lacks %s: %v", name, header)
		}
		return i
	}

	t.Run("a level is filled in its colour", func(t *testing.T) {
		first := sheet.Rows[1]
		if c := first[at("Level")]; c.Text != "High" || c.Fill != "DC2626" {
			t.Errorf("the level before reads %+v", c)
		}
		if c := first[at("Level in effect")]; c.Text != "Low" || c.Fill != "16A34A" {
			t.Errorf("the level in effect reads %+v", c)
		}
		if got := cellTexts(first)[at("Mitigated by")]; got != "CHK-R1 Stop on ice" {
			t.Errorf("the first mitigation reads %q", got)
		}
	})

	t.Run("a hidden mitigation is named as hidden, with its coverage", func(t *testing.T) {
		second := cellTexts(sheet.Rows[2])
		if second[at("Key")] != "" || second[at("Mitigated by")] != HiddenMitigation || second[at("Verified")] != "na" || second[at("In effect")] != "true" {
			t.Errorf("the hidden mitigation's row reads %v", second)
		}
	})

	t.Run("an unrated risk has empty ratings and no level", func(t *testing.T) {
		third := sheet.Rows[3]
		texts := cellTexts(third)
		for _, name := range []string{"Severity", "Probability", "RPN", "Level", "Mitigated by", "Level in effect"} {
			if c := third[at(name)]; c.Text != "" || c.Count != nil || c.Fill != "" {
				t.Errorf("an unrated risk's %s reads %+v", name, c)
			}
		}
		if texts[at("Key")] != "CHK-R3" || texts[at("State")] != "unmitigated" {
			t.Errorf("the unrated risk reads %v", texts)
		}
	})
}
