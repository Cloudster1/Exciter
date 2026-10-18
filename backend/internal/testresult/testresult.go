// Package testresult keeps the test results CI uploads (#200): each test case names the requirements it verifies,
// and its latest result per branch is evidence for them until the requirement is revised past it.
package testresult

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// Agreed limits of test results (#224).
const (
	// MaxRunMB bounds the files one upload carries, together.
	MaxRunMB    = 20
	MaxRunBytes = MaxRunMB << 20
	MaxRunFiles = 100
	// MaxRunCases bounds the test cases one upload holds, over all its files.
	MaxRunCases = 20000
	// MaxKeysPerCase bounds the requirements one test case names.
	MaxKeysPerCase  = 20
	MaxNameLength   = 200
	MaxURLLength    = 2000
	MaxMessageChars = 2000
	// MaxReported bounds the keys a run reports back; the rest are counted.
	MaxReported = 200
	// KeptRuns is how many finished runs are kept per repository, branch and job; the latest is never pruned.
	KeptRuns = 20
	// IdleBranchDays is how long a branch that does not count keeps its runs after its last upload.
	IdleBranchDays = 90
	// DefaultCountingBranch is the branch whose results count until a project names another.
	DefaultCountingBranch = "main"
)

// Outcome is how one test case ended.
type Outcome string

const (
	Passed  Outcome = "passed"
	Failed  Outcome = "failed"
	Skipped Outcome = "skipped"
	Errored Outcome = "errored"
)

// Outcomes lists every Outcome, worst first, the order a requirement's tests are summed up in.
var Outcomes = []Outcome{Errored, Failed, Skipped, Passed}

// RunState is where an upload stands; the worker takes it from queued to done or failed.
type RunState string

const (
	RunQueued  RunState = "queued"
	RunRunning RunState = "running"
	RunDone    RunState = "done"
	RunFailed  RunState = "failed"
)

// RunStates lists every RunState, for the API document.
var RunStates = []RunState{RunQueued, RunRunning, RunDone, RunFailed}

// Problem is why a key a test case named verifies nothing.
type Problem string

const (
	ProblemUnknown Problem = "unknown"
	ProblemTrashed Problem = "trashed"
	ProblemSection Problem = "section"
)

// Problems lists every Problem, for the API document.
var Problems = []Problem{ProblemUnknown, ProblemTrashed, ProblemSection}

// RunForm is the form fields of an upload, beside its JUnit files.
type RunForm struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Branch     string `json:"branch"`
	// Job tells apart the runs of one commit, such as unit and browser tests; empty is one job.
	Job string `json:"job,omitempty"`
	// RunURL is the CI run's address, http or https.
	RunURL string `json:"runUrl,omitempty"`
}

// Reported is a key a test case named that verifies nothing, and why.
type Reported struct {
	Key     string  `json:"key"`
	Test    string  `json:"test"`
	Problem Problem `json:"problem"`
}

// Counts sums a run's test cases by outcome.
type Counts struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Errored int `json:"errored"`
}

// Run is one upload: what it named, where the worker has it, and what it reported back.
type Run struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"projectKey"`
	RunForm
	State RunState `json:"state"`
	// Cases counts the test cases read, Matched those naming a requirement it verifies.
	Cases    int        `json:"cases"`
	Matched  int        `json:"matched"`
	Counts   Counts     `json:"counts"`
	Reported []Reported `json:"reported"`
	// Unreported counts the keys beyond MaxReported.
	Unreported int                   `json:"unreported"`
	Error      string                `json:"error"`
	UploadedBy requirement.PersonRef `json:"uploadedBy"`
	Via        *via.Via              `json:"via"`
	UploadedAt time.Time             `json:"uploadedAt"`
	FinishedAt *time.Time            `json:"finishedAt"`
}

// CommitLink is where Armature shows a result's commit: the issue whose development holds it.
type CommitLink struct {
	IssueKey string `json:"issueKey"`
	URL      string `json:"url"`
}

// Result is a test case's latest result on one branch, for one requirement it verifies.
type Result struct {
	Test       string  `json:"test"`
	Suite      string  `json:"suite"`
	Repository string  `json:"repository"`
	Branch     string  `json:"branch"`
	Job        string  `json:"job"`
	Outcome    Outcome `json:"outcome"`
	Message    string  `json:"message"`
	DurationMs int     `json:"durationMs"`
	// Revision is the requirement's revision current when the run was uploaded; Outdated says it was revised since.
	Revision int         `json:"revision"`
	Outdated bool        `json:"outdated"`
	Counts   bool        `json:"counts"`
	Commit   string      `json:"commit"`
	RunID    uuid.UUID   `json:"runId"`
	RunURL   string      `json:"runUrl"`
	At       time.Time   `json:"at"`
	Armature *CommitLink `json:"armature"`
}

// Settings is a project's choice of the branch whose results count for coverage, the matrix and the queries.
type Settings struct {
	CountingBranch string     `json:"countingBranch"`
	UpdatedAt      *time.Time `json:"updatedAt"`
}

// SettingsInput sets the counting branch.
type SettingsInput struct {
	CountingBranch string `json:"countingBranch"`
}
