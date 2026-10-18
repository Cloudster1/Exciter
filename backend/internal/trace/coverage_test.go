package trace

import (
	"slices"
	"testing"
)

func TestARequirementIsJudgedAsTheContractsTableSays(t *testing.T) {
	all := Asks{Decomposed: true, Implemented: true, Verified: true}
	tests := Asks{Verified: true, VerifiedFrom: []Source{SourceTests}}
	both := Asks{Verified: true, VerifiedFrom: []Source{SourceLinks, SourceTests}}
	code := Asks{Implemented: true, ImplementedFrom: []Source{SourceCode}}
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
		{"tests do not count unless asked", Asks{Verified: true}, Counted{Tests: 2, TestsWhole: 2}, [3]Level{LevelNA, LevelNA, LevelNone}},
		{"verified by passing tests", tests, Counted{Tests: 2, TestsWhole: 2}, [3]Level{LevelNA, LevelNA, LevelFull}},
		{"a test failing or outdated", tests, Counted{Tests: 2, TestsWhole: 1}, [3]Level{LevelNA, LevelNA, LevelPartial}},
		{"links alone do not count for tests", tests, Counted{VerifyingLinks: 1}, [3]Level{LevelNA, LevelNA, LevelNone}},
		{"a link with nothing tested yet", both, Counted{VerifyingLinks: 1}, [3]Level{LevelNA, LevelNA, LevelFull}},
		{"passing tests beside a suspect link", both, Counted{VerifyingLinks: 1, VerifyingLinksSuspect: 1, Tests: 1, TestsWhole: 1},
			[3]Level{LevelNA, LevelNA, LevelPartial}},
		{"neither source holds anything", both, Counted{}, [3]Level{LevelNA, LevelNA, LevelNone}},
		{"implemented by current code", code, Counted{CodeImplementing: 2, CodeImplementingWhole: 2}, [3]Level{LevelNA, LevelFull, LevelNA}},
		{"implemented by outdated code", code, Counted{CodeImplementing: 2, CodeImplementingWhole: 1}, [3]Level{LevelNA, LevelPartial, LevelNA}},
		{"code does not implement unless asked", Asks{Implemented: true}, Counted{CodeImplementing: 1, CodeImplementingWhole: 1},
			[3]Level{LevelNA, LevelNone, LevelNA}},
		{"verified by code that verifies", Asks{Verified: true, VerifiedFrom: []Source{SourceCode}}, Counted{CodeVerifying: 1, CodeVerifyingWhole: 1},
			[3]Level{LevelNA, LevelNA, LevelFull}},
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
	slices.SortFunc(keys, CompareIssueKeys)
	if want := []string{"API-2", "API-10", "WEB-9", "WEB-10"}; !slices.Equal(keys, want) {
		t.Errorf("ordered %v, want %v", keys, want)
	}
}
