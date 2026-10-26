package armature

import (
	"testing"

	"github.com/cloudster1/exciter/backend/internal/testresult"
)

// A commit's address is shown as a link, so only a web address is kept.
func TestACommitKeepsOnlyAWebAddress(t *testing.T) {
	out := map[string]testresult.CommitLink{}
	addCommits(out, "CP-1", []wireCommit{
		{SHA: "AAAA", URL: "https://git.example/acme/commit/aaaa"},
		{SHA: "bbbb", URL: "http://git.example/acme/commit/bbbb"},
		{SHA: "cccc", URL: "javascript:void(0)"},
		{SHA: "dddd", URL: "data:text/html,commit"},
		{SHA: "eeee", URL: "ftp://git.example/eeee"},
		{SHA: "ffff", URL: "/acme/commit/ffff"},
		{SHA: "1111", URL: ""},
	})
	addCommits(out, "CP-2", []wireCommit{{SHA: "aaaa", URL: "https://elsewhere.example/aaaa"}})

	if len(out) != 2 {
		t.Fatalf("kept %v, want only the two web addresses", out)
	}
	if got := out["aaaa"]; got.IssueKey != "CP-1" || got.URL != "https://git.example/acme/commit/aaaa" {
		t.Errorf("aaaa = %v, want CP-1's address, the first issue to name it", got)
	}
	if got := out["bbbb"]; got.URL != "http://git.example/acme/commit/bbbb" {
		t.Errorf("bbbb = %v", got)
	}
}
