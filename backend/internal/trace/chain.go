package trace

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// A project's trace model and the chains that follow it (#151), agreed in docs/api-contract-m5.md.

// Agreed limits.
const (
	MaxLevels         = 6
	MaxLevelNameRunes = 40
	ChainDefaultLimit = 50
	ChainMaxLimit     = 200
	// MaxChainLines bounds the lines one page answers, since every root can fan out at every level.
	MaxChainLines = 2000
)

// LevelKind says what a level of the trace model holds.
type LevelKind string

const (
	LevelRequirements LevelKind = "requirements"
	LevelIssues       LevelKind = "issues"
	LevelPages        LevelKind = "pages"
	// LevelTests are the tests whose latest results on the counting branch verify the level before (#200).
	LevelTests LevelKind = "tests"
	// LevelCode are the places in the code whose tags implement or verify the level before (#201).
	LevelCode LevelKind = "code"
)

// LevelKinds lists every LevelKind, for the API document.
var LevelKinds = []LevelKind{LevelRequirements, LevelIssues, LevelPages, LevelTests, LevelCode}

// TestRelations and CodeRelations are the relations a level of tests or of code is reached by.
var (
	TestRelations = []string{"verifies"}
	CodeRelations = []string{"implements", "verifies"}
)

// IssueRelations and PageRelations are Armature's and Stator's relations, which internal/trace sits below; a test of
// the API's keeps them equal to armature.Relations and stator.Relations.
var (
	IssueRelations = []string{"implements", "verifies", "relates"}
	PageRelations  = []string{"specifies", "describes", "relates"}
)

// ErrNotModelKeeper refuses changing a project's trace model to anybody who does not administer it.
var ErrNotModelKeeper = errors.New("only the project's administrators set its trace model")

// ModelStep is one way down from the level before: a link type read in a direction, or an Armature or Stator relation.
type ModelStep struct {
	TypeID    *uuid.UUID `json:"typeId,omitempty"`
	TypeName  string     `json:"typeName,omitempty"`
	Direction Direction  `json:"direction,omitempty"`
	Relation  string     `json:"relation,omitempty"`
}

// ModelLevel is one level of the model: requirements of its types, or the issues or pages the level before reaches.
type ModelLevel struct {
	Name    string                `json:"name"`
	Kind    LevelKind             `json:"kind"`
	Types   []requirement.TypeRef `json:"types"`
	Steps   []ModelStep           `json:"steps"`
	TypeIDs []uuid.UUID           `json:"-"`
}

// ModelCan says what the reader may do with the model.
type ModelCan struct {
	Edit bool `json:"edit"`
}

// Model is a project's trace levels, first to last; empty until an administrator sets them.
type Model struct {
	Levels    []ModelLevel           `json:"levels"`
	UpdatedAt *time.Time             `json:"updatedAt"`
	UpdatedBy *requirement.PersonRef `json:"updatedBy"`
	Can       ModelCan               `json:"can"`
}

// ModelLevelInput is one level as an administrator sets it; the first level has no steps, every later one at least one.
type ModelLevelInput struct {
	Name    string      `json:"name"`
	Kind    LevelKind   `json:"kind"`
	TypeIDs []uuid.UUID `json:"typeIds,omitempty"`
	Steps   []ModelStep `json:"steps,omitempty"`
}

// ModelInput replaces a project's whole model; no levels clears it.
type ModelInput struct {
	Levels []ModelLevelInput `json:"levels"`
}

// ChainNode is what a line holds at one level, and the link that led to it from the level before.
type ChainNode struct {
	Requirement *requirement.Ref `json:"requirement"`
	IssueKey    string           `json:"issueKey,omitempty"`
	Page        *MatrixPage      `json:"page"`
	Test        *ChainTest       `json:"test,omitempty"`
	Code        *ChainCode       `json:"code,omitempty"`
	Via         string           `json:"via,omitempty"`
	Suspect     bool             `json:"suspect"`
	// LastRun is a test case's last result (#252).
	LastRun *LastRun `json:"lastRun,omitempty"`
	// Risk marks a risk with its level and state (#261).
	Risk *requirement.RiskMark `json:"risk,omitempty"`
}

// ChainTest is a test as a chain's node: its case, its latest outcome, and whether a newer revision outdated it.
type ChainTest struct {
	Repository string `json:"repository"`
	Suite      string `json:"suite"`
	Name       string `json:"name"`
	Outcome    string `json:"outcome"`
	Outdated   bool   `json:"outdated"`
}

// ChainCode is a place in the code as a chain's node: the file and line of its tag, and whether it names an older
// revision.
type ChainCode struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Outdated   bool   `json:"outdated"`
	URL        string `json:"url"`
}

// holds says a test passed against the current revision, as a chain needs it to end whole.
func (t *ChainTest) holds() bool { return t.Outcome == "passed" && !t.Outdated }

// ChainLine is one path from a root down the levels shown; fewer nodes than levels is a break below the last.
type ChainLine struct {
	Root   int         `json:"root"`
	Nodes  []ChainNode `json:"nodes"`
	Broken bool        `json:"broken"`
}

// Chains is a page of roots, the first level's requirements, and every line that leaves them.
type Chains struct {
	Levels []ModelLevel      `json:"levels"`
	Roots  []requirement.Ref `json:"roots"`
	Lines  []ChainLine       `json:"lines"`
	Broken int               `json:"broken"`
	// Truncated says a file was cut at the export's lines; a page refuses instead.
	Truncated bool `json:"-"`
	Total     int  `json:"total"`
	Limit     int  `json:"limit"`
	Offset    int  `json:"offset"`
}

// MatrixPage is a Stator page as a column or a chain's node: its id, space and address, its title being Stator's to tell.
type MatrixPage struct {
	ID       uuid.UUID `json:"id"`
	SpaceKey string    `json:"spaceKey"`
	URL      string    `json:"url"`
	Title    string    `json:"title,omitempty"`
}
