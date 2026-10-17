package trace

import "github.com/cloudster1/exciter/backend/internal/requirement"

// MaxCheckItems bounds each list a trace check answers; the rest are counted (#224).
const MaxCheckItems = 500

// Verdict is a trace check's answer, for a pipeline to stop on.
type Verdict string

const (
	Pass Verdict = "pass"
	Fail Verdict = "fail"
)

// Verdicts lists every Verdict, for the API document.
var Verdicts = []Verdict{Pass, Fail}

// CheckReason is what a trace check fails on when asked.
type CheckReason string

const (
	FailBroken   CheckReason = "broken"
	FailGaps     CheckReason = "gaps"
	FailFailing  CheckReason = "failing"
	FailOutdated CheckReason = "outdated"
	FailOrphaned CheckReason = "orphaned"
)

// CheckReasons lists every CheckReason; DefaultCheckFail is what a check fails on unless asked otherwise.
var (
	CheckReasons     = []CheckReason{FailBroken, FailGaps, FailFailing, FailOutdated, FailOrphaned}
	DefaultCheckFail = []CheckReason{FailBroken, FailGaps, FailFailing}
)

// EvidenceKind says whether evidence is a test result or a code tag.
type EvidenceKind string

const (
	EvidenceTest EvidenceKind = "test"
	EvidenceCode EvidenceKind = "code"
)

// EvidenceKinds lists every EvidenceKind, for the API document.
var EvidenceKinds = []EvidenceKind{EvidenceTest, EvidenceCode}

// CheckBroken is a chain that stops short: its root, the keys it reached and the level it did not.
type CheckBroken struct {
	Root    requirement.Ref `json:"root"`
	Path    []string        `json:"path"`
	Missing string          `json:"missing"`
}

// CheckGap is a requirement whose coverage asks for a dimension it does not hold in full.
type CheckGap struct {
	Requirement requirement.Ref `json:"requirement"`
	Dimension   Dimension       `json:"dimension"`
	Level       Level           `json:"level"`
}

// CheckEvidence is a failing or outdated test result, or an outdated code tag, of a requirement.
type CheckEvidence struct {
	Requirement requirement.Ref `json:"requirement"`
	Kind        EvidenceKind    `json:"kind"`
	// Name is the test's name, or the path and line of the tag.
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Outcome    string `json:"outcome,omitempty"`
	Revision   int    `json:"revision"`
	Commit     string `json:"commit"`
	URL        string `json:"url"`
}

// CheckOrphan is a code tag naming a requirement in the trash or none at all.
type CheckOrphan struct {
	Key        string `json:"key"`
	Problem    string `json:"problem"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	URL        string `json:"url"`
}

// CheckTotals counts each list in whole, before MaxCheckItems cuts it.
type CheckTotals struct {
	Broken   int `json:"broken"`
	Gaps     int `json:"gaps"`
	Failing  int `json:"failing"`
	Outdated int `json:"outdated"`
	Orphaned int `json:"orphaned"`
}

// CheckEvidenceSeen says how much evidence the branch had, so a pipeline sees a check over nothing.
type CheckEvidenceSeen struct {
	Runs  int `json:"runs"`
	Scans int `json:"scans"`
}

// Check is a trace check's verdict and what it found, over the requirements a query keeps on one branch.
type Check struct {
	Verdict Verdict       `json:"verdict"`
	Branch  string        `json:"branch"`
	Fail    []CheckReason `json:"fail"`
	// Chains is false when the project has no trace model, so no chain was followed.
	Chains   bool              `json:"chains"`
	Broken   []CheckBroken     `json:"broken"`
	Gaps     []CheckGap        `json:"gaps"`
	Failing  []CheckEvidence   `json:"failing"`
	Outdated []CheckEvidence   `json:"outdated"`
	Orphaned []CheckOrphan     `json:"orphaned"`
	Totals   CheckTotals       `json:"totals"`
	Evidence CheckEvidenceSeen `json:"evidence"`
	Checked  int               `json:"checked"`
}
