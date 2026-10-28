package docx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

func aRiskReport() *risk.Report {
	n := func(v int) *int { return &v }
	mitigated, unmitigated := requirement.Mitigated, requirement.Unmitigated
	high := requirement.RiskLevel{Name: "High", Colour: "#dc2626"}
	low := requirement.RiskLevel{Name: "Low", Colour: "#16a34a", Acceptable: true}
	return &risk.Report{
		ProjectKey: "CHK", ProjectName: "Brakes",
		Scheme:  risk.Scheme{Method: risk.MethodMatrix},
		Levels:  []risk.LevelCount{{Level: low, Effective: 1}, {Level: high, Before: 1}},
		Unrated: 1,
		Risks: []risk.Row{
			{
				Risk: risk.RiskRef{Key: "CHK-R2", Title: "Brakes fail on ice", Type: requirement.TypeRef{Name: "Hazard"}},
				Rating: requirement.RiskView{
					Rating:    requirement.Rating{Before: requirement.Side{Severity: n(4), Probability: n(3)}, After: requirement.Side{Severity: n(1), Probability: n(1)}},
					BeforeRPN: n(12), AfterRPN: n(1), BeforeLevel: &high, AfterLevel: &low, State: &mitigated, Level: &low,
				},
				Mitigations: []risk.Mitigation{
					{Source: risk.SourceRef{Key: "CHK-R1", Title: "Stop on ice"}, Implemented: trace.LevelNA, Verified: trace.LevelFull, InEffect: true},
					{Source: risk.SourceRef{Hidden: true}, Implemented: trace.LevelNA, Verified: trace.LevelNA, InEffect: true},
				},
			},
			{
				Risk:   risk.RiskRef{Key: "CHK-R3", Title: "Brakes squeal", Type: requirement.TypeRef{Name: "Hazard"}},
				Rating: requirement.RiskView{State: &unmitigated},
			},
		},
	}
}

func TestTheRiskReportWritesLevelsRisksAndHiddenMitigations(t *testing.T) {
	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	body, err := RiskReportBytesWith(context.Background(), aRiskReport(), "Ada", at, "en", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := unzip(t, body)["word/document.xml"]
	for _, wanted := range []string{
		"Risk report", "Brakes (CHK)", "Levels by a matrix of severity and probability", "2 risks", "not rated yet.",
		"CHK-R2 Brakes fail on ice", "Hazard, mitigated, stands at Low", "CHK-R1 Stop on ice", "A requirement you may not view",
		"CHK-R3 Brakes squeal", "Hazard, unmitigated", "Nothing mitigates it.",
	} {
		if !strings.Contains(doc, wanted) {
			t.Errorf("the report lacks %q", wanted)
		}
	}
	// An unrated risk reads a dash in every cell of its rating rather than a level.
	squeal := doc[strings.Index(doc, "CHK-R3 Brakes squeal"):]
	if strings.Contains(squeal, ">High<") || strings.Contains(squeal, ">Low<") || !strings.Contains(squeal, ">-<") {
		t.Error("the unrated risk reads a level")
	}
	if got := RiskReportFileName(aRiskReport(), at, "docx"); got != "CHK-risk-report-2026-10-28.docx" {
		t.Errorf("the report is named %s", got)
	}
}

func TestTheRiskReportSpeaksItsReadersLanguage(t *testing.T) {
	body, err := RiskReportBytesWith(context.Background(), aRiskReport(), "Ada", time.Now(), "de", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := unzip(t, body)["word/document.xml"]
	for _, wanted := range []string{"Risikobericht", "Eine Anforderung, die Sie nicht sehen dürfen", "Nichts mindert es."} {
		if !strings.Contains(doc, wanted) {
			t.Errorf("the German report lacks %q", wanted)
		}
	}
}
