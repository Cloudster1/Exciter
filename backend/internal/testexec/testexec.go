// Package testexec keeps test plans and test executions (#251): the test cases a plan groups, an execution of them
// against a baseline, a version and a configuration, and what its testers record. See docs/api-contract-m12.md.
package testexec

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// Agreed limits of plans and executions (#253).
const (
	MaxPlanCases           = 500
	MaxExecutionCases      = 2000
	MaxTesters             = 50
	MaxTitleLength         = 100
	MaxDescriptionLength   = 500
	MaxVersionLength       = 200
	MaxConfigurationLength = 500
	MaxNoteLength          = 4000
	MaxFileMB              = 50
	MaxFilesPerStep        = 20
)

// Outcome is what a step or a case came to.
type Outcome string

const (
	Passed  Outcome = "passed"
	Failed  Outcome = "failed"
	Blocked Outcome = "blocked"
	Skipped Outcome = "skipped"
)

// Outcomes lists every Outcome, for the API document.
var Outcomes = []Outcome{Passed, Failed, Blocked, Skipped}

// State is where an execution stands: open while it is run, then closed or aborted for good.
type State string

const (
	Open    State = "open"
	Closed  State = "closed"
	Aborted State = "aborted"
)

// States lists every State, for the API document.
var States = []State{Open, Closed, Aborted}

// Scope says how a plan holds its cases: picked in order, or found by a query when an execution starts.
type Scope string

const (
	ScopeCases Scope = "cases"
	ScopeQuery Scope = "query"
)

// Scopes lists every Scope, for the API document.
var Scopes = []Scope{ScopeCases, ScopeQuery}

// Source says who recorded a case: a tester, or a result from CI (#252).
type Source string

const (
	SourceTester Source = "tester"
	SourceCI     Source = "ci"
)

// Sources lists every Source, for the API document.
var Sources = []Source{SourceTester, SourceCI}

// CaseRef is a test case as a plan or an execution lists it.
type CaseRef struct {
	ID    uuid.UUID `json:"id"`
	Key   string    `json:"key"`
	Title string    `json:"title"`
	// Trashed cases stay in a plan and are left out of the executions it starts.
	Trashed bool `json:"trashed"`
}

// PlanCan says what the reader may do with a plan.
type PlanCan struct {
	Edit bool `json:"edit"`
}

// Plan is a group of test cases an execution runs.
type Plan struct {
	ID          uuid.UUID `json:"id"`
	ProjectKey  string    `json:"projectKey"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Scope       Scope     `json:"scope"`
	// Query finds the cases of a plan whose scope is query.
	Query string `json:"query"`
	// Cases are the picked cases in order, or what the query finds now.
	Cases     []CaseRef             `json:"cases"`
	CreatedBy requirement.PersonRef `json:"createdBy"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
	Can       PlanCan               `json:"can"`
}

// PlanInput makes a plan.
type PlanInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Scope       Scope  `json:"scope"`
	Query       string `json:"query,omitempty"`
	// Cases are the keys of the picked cases, in order.
	Cases []string `json:"cases,omitempty"`
}

// PlanPatch changes a plan; nil leaves a field alone.
type PlanPatch struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Query       *string `json:"query,omitempty"`
}

// CasesInput sets a plan's picked cases, in order.
type CasesInput struct {
	Cases []string `json:"cases"`
}

// BaselineRef is the baseline an execution runs, kept by name once it is deleted.
type BaselineRef struct {
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
}

// Excluded is a case a plan held that an execution left out, and why.
type Excluded struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Progress counts an execution's case iterations by outcome.
type Progress struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Blocked int `json:"blocked"`
	Skipped int `json:"skipped"`
	NotRun  int `json:"notRun"`
}

// ExecutionCan says what the reader may do with an execution.
type ExecutionCan struct {
	Manage bool `json:"manage"`
	Record bool `json:"record"`
}

// Execution is a plan's cases run against a baseline, a version and a configuration.
type Execution struct {
	ID            uuid.UUID             `json:"id"`
	ProjectKey    string                `json:"projectKey"`
	Title         string                `json:"title"`
	PlanID        *uuid.UUID            `json:"planId"`
	PlanTitle     string                `json:"planTitle"`
	Baseline      *BaselineRef          `json:"baseline"`
	Version       string                `json:"version"`
	Configuration string                `json:"configuration"`
	Manager       requirement.PersonRef `json:"manager"`
	DueAt         *time.Time            `json:"dueAt"`
	State         State                 `json:"state"`
	Testers       []review.Subject      `json:"testers"`
	Excluded      []Excluded            `json:"excluded"`
	Progress      Progress              `json:"progress"`
	// Cases are listed when one execution is read, not in lists.
	Cases        []CaseRun    `json:"cases,omitempty"`
	CloseComment string       `json:"closeComment"`
	OpenedAt     time.Time    `json:"openedAt"`
	ClosedAt     *time.Time   `json:"closedAt"`
	Can          ExecutionCan `json:"can"`
}

// ExecutionInput starts an execution of a plan.
type ExecutionInput struct {
	Title         string           `json:"title"`
	PlanID        uuid.UUID        `json:"planId"`
	BaselineID    *uuid.UUID       `json:"baselineId,omitempty"`
	Version       string           `json:"version,omitempty"`
	Configuration string           `json:"configuration,omitempty"`
	DueAt         *time.Time       `json:"dueAt,omitempty"`
	Testers       []review.Subject `json:"testers"`
}

// ExecutionPatch changes what an execution says; its cases are fixed once it starts.
type ExecutionPatch struct {
	Title         *string    `json:"title,omitempty"`
	Version       *string    `json:"version,omitempty"`
	Configuration *string    `json:"configuration,omitempty"`
	DueAt         *time.Time `json:"dueAt,omitempty"`
}

// CloseInput closes an execution, or aborts it so it counts for nothing.
type CloseInput struct {
	Comment string `json:"comment,omitempty"`
	Abort   bool   `json:"abort,omitempty"`
}

// CaseRun is one iteration of a case in an execution, at the revision it was pinned to.
type CaseRun struct {
	Case      CaseRef `json:"case"`
	Iteration int     `json:"iteration"`
	Revision  int     `json:"revision"`
	// Values are the parameters' values of this iteration, by name.
	Values     map[string]string      `json:"values"`
	Outcome    *Outcome               `json:"outcome"`
	Note       string                 `json:"note"`
	Source     *Source                `json:"source"`
	RecordedBy *requirement.PersonRef `json:"recordedBy"`
	RecordedAt *time.Time             `json:"recordedAt"`
	// CI names the result from CI that recorded it, when one did.
	CI *CIResult `json:"ci"`
}

// CIResult is the result from CI a case was recorded from.
type CIResult struct {
	Suite      string    `json:"suite"`
	Name       string    `json:"name"`
	Commit     string    `json:"commit"`
	UploadedAt time.Time `json:"uploadedAt"`
}

// StepResult is what was recorded for one step of a case iteration.
type StepResult struct {
	Step       int                    `json:"step"`
	Outcome    *Outcome               `json:"outcome"`
	Note       string                 `json:"note"`
	RecordedBy *requirement.PersonRef `json:"recordedBy"`
	RecordedAt *time.Time             `json:"recordedAt"`
}

// File is a file recorded on a step or a case.
type File struct {
	ID          uuid.UUID             `json:"id"`
	Step        *int                  `json:"step"`
	FileName    string                `json:"fileName"`
	ContentType string                `json:"contentType"`
	Size        int64                 `json:"size"`
	UploadedBy  requirement.PersonRef `json:"uploadedBy"`
	CreatedAt   time.Time             `json:"createdAt"`
}

// Issue is an issue filed in Armature from a failure.
type Issue struct {
	Key       string                `json:"key"`
	Step      *int                  `json:"step"`
	CreatedBy requirement.PersonRef `json:"createdBy"`
	CreatedAt time.Time             `json:"createdAt"`
}

// Case is one case iteration to run: its steps at the pinned revision, and what was recorded.
type Case struct {
	Run           CaseRun                 `json:"run"`
	Preconditions json.RawMessage         `json:"preconditions"`
	Steps         []requirement.Step      `json:"steps"`
	Results       []StepResult            `json:"results"`
	Files         []File                  `json:"files"`
	Issues        []Issue                 `json:"issues"`
	Iterations    int                     `json:"iterations"`
	Parameters    []requirement.Parameter `json:"parameters"`
}

// StepInput records one step.
type StepInput struct {
	Step    int     `json:"step"`
	Outcome Outcome `json:"outcome"`
	Note    string  `json:"note,omitempty"`
}

// RecordInput records a case iteration; Outcome follows from the steps when absent.
type RecordInput struct {
	Iteration int         `json:"iteration"`
	Steps     []StepInput `json:"steps,omitempty"`
	Outcome   *Outcome    `json:"outcome,omitempty"`
	Note      string      `json:"note,omitempty"`
}

// FailureIssueInput files a failed case iteration as an issue in Armature.
type FailureIssueInput struct {
	Iteration       int     `json:"iteration"`
	Step            *int    `json:"step,omitempty"`
	ArmatureProject string  `json:"armatureProject"`
	TypeID          *string `json:"typeId,omitempty"`
	Summary         string  `json:"summary,omitempty"`
}

// CaseResult is a test case's result in one execution, as its page lists them.
type CaseResult struct {
	ExecutionID    uuid.UUID  `json:"executionId"`
	ExecutionTitle string     `json:"executionTitle"`
	State          State      `json:"state"`
	Version        string     `json:"version"`
	Configuration  string     `json:"configuration"`
	Iteration      int        `json:"iteration"`
	Revision       int        `json:"revision"`
	Outcome        *Outcome   `json:"outcome"`
	Source         *Source    `json:"source"`
	RecordedAt     *time.Time `json:"recordedAt"`
}

// Report is what an execution's report holds.
type Report struct {
	Execution Execution    `json:"execution"`
	Cases     []ReportCase `json:"cases"`
}

// ReportCase is one case iteration in a report, with its steps and what they came to.
type ReportCase struct {
	Run     CaseRun            `json:"run"`
	Steps   []requirement.Step `json:"steps"`
	Results []StepResult       `json:"results"`
	Files   []string           `json:"files"`
	Issues  []string           `json:"issues"`
}
