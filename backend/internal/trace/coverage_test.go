package trace

import (
	"slices"
	"testing"
)

func TestARequirementIsJudgedAsTheContractsTableSays(t *testing.T) {
	all := Asks{Decomposed: true, Implemented: true, Verified: true}
	cases := []struct {
		name    string
		asks    Asks
		counted Counted
		want    [3]Level
	}{
		{"nothing asked", Asks{}, Counted{Decomposing: 1}, [3]Level{LevelNA, LevelNA, LevelNA}},
		{"no links", all, Counted{}, [3]Level{LevelNone, LevelNone, LevelNone}},
		{"all whole", all, Counted{Decomposing: 2, Implementing: 2, ImplementingDone: 1, VerifyingLinks: 1, VerifyingIssues: 1, VerifyingIssuesDone: 1},
			[3]Level{LevelFull, LevelFull, LevelFull}},
		{"a suspect link", all, Counted{Decomposing: 2, DecomposingSuspect: 1, Implementing: 1, ImplementingDone: 1, ImplementingSuspect: 1,
			VerifyingLinks: 1, VerifyingLinksSuspect: 1}, [3]Level{LevelPartial, LevelPartial, LevelPartial}},
		{"linked, nothing done", all, Counted{Implementing: 1, VerifyingIssues: 1}, [3]Level{LevelNone, LevelPartial, LevelPartial}},
		{"a verifying issue still open", all, Counted{VerifyingLinks: 1, VerifyingIssues: 2, VerifyingIssuesDone: 1},
			[3]Level{LevelNone, LevelNone, LevelPartial}},
		{"verified by a trace link alone", Asks{Verified: true}, Counted{VerifyingLinks: 1}, [3]Level{LevelNA, LevelNA, LevelFull}},
	}
	for _, c := range cases {
		d, i, v := Judge(c.asks, c.counted)
		if got := [3]Level{d, i, v}; got != c.want {
			t.Errorf("%s: judged %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTheTotalsCountOnlyWhereARuleAsks(t *testing.T) {
	rows := []CoverageRow{
		{Decomposed: LevelFull, Implemented: LevelNA, Verified: LevelNone},
		{Decomposed: LevelPartial, Implemented: LevelNA, Verified: LevelNone},
		{Decomposed: LevelNA, Implemented: LevelNA, Verified: LevelFull},
	}
	s := Summarize(rows)
	if s.Decomposed != (Totals{Full: 1, Partial: 1, Total: 2}) || s.Implemented != (Totals{}) || s.Verified != (Totals{Full: 1, None: 2, Total: 3}) {
		t.Errorf("summarized %+v", s)
	}
	only := LevelFilter{Verified: LevelNone}
	kept := 0
	for _, r := range rows {
		if only.Keeps(r) {
			kept++
		}
	}
	if kept != 2 {
		t.Errorf("kept %d rows unverified, want 2", kept)
	}
}

func TestIssueColumnsGoByProjectThenNumber(t *testing.T) {
	keys := []string{"WEB-10", "API-2", "WEB-9", "API-10"}
	slices.SortFunc(keys, compareIssueKeys)
	if want := []string{"API-2", "API-10", "WEB-9", "WEB-10"}; !slices.Equal(keys, want) {
		t.Errorf("ordered %v, want %v", keys, want)
	}
}
