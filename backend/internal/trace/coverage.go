package trace

import (
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Coverage and the traceability matrix (#38), agreed in docs/api-contract-m3.md.

// Agreed limits.
const (
	CoverageDefaultLimit = 50
	CoverageMaxLimit     = 500
	MatrixDefaultLimit   = 50
	MatrixMaxLimit       = 200
	// MatrixMaxColumns bounds the columns one matrix answers; the rows are paged.
	MatrixMaxColumns = 500
)

// Dimension is one way a requirement is covered.
type Dimension string

const (
	Decomposed  Dimension = "decomposed"
	Implemented Dimension = "implemented"
	Verified    Dimension = "verified"
)

// Dimensions lists every Dimension, in report order.
var Dimensions = []Dimension{Decomposed, Implemented, Verified}

// Level is how far a requirement is covered in one dimension; na when its type's rule does not ask for it.
type Level string

const (
	LevelNone    Level = "none"
	LevelPartial Level = "partial"
	LevelFull    Level = "full"
	LevelNA      Level = "na"
)

// Levels lists every Level, for the API document.
var Levels = []Level{LevelNone, LevelPartial, LevelFull, LevelNA}

// CoverageRule says which dimensions apply to the requirements of one type, or of every type without a rule.
type CoverageRule struct {
	ID          uuid.UUID  `json:"id"`
	TypeID      *uuid.UUID `json:"typeId"`
	TypeName    string     `json:"typeName"`
	Decomposed  bool       `json:"decomposed"`
	Implemented bool       `json:"implemented"`
	Verified    bool       `json:"verified"`
}

// CoverageRuleInput sets one type's rule, or the project's rule for every other type when TypeID is absent.
type CoverageRuleInput struct {
	TypeID      *uuid.UUID `json:"typeId,omitempty"`
	Decomposed  bool       `json:"decomposed"`
	Implemented bool       `json:"implemented"`
	Verified    bool       `json:"verified"`
}

// Totals count the requirements a dimension applies to by level.
type Totals struct {
	Full    int `json:"full"`
	Partial int `json:"partial"`
	None    int `json:"none"`
	Total   int `json:"total"`
}

// CoverageSummary is a project's coverage over the requirements the filter keeps.
type CoverageSummary struct {
	Decomposed  Totals `json:"decomposed"`
	Implemented Totals `json:"implemented"`
	Verified    Totals `json:"verified"`
}

// CoverageRow is one requirement's coverage.
type CoverageRow struct {
	Requirement requirement.Ref `json:"requirement"`
	Decomposed  Level           `json:"decomposed"`
	Implemented Level           `json:"implemented"`
	Verified    Level           `json:"verified"`
}

// MatrixColumns says what a matrix's columns are.
type MatrixColumns string

const (
	ColumnsRequirements MatrixColumns = "requirements"
	ColumnsIssues       MatrixColumns = "issues"
)

// MatrixColumnKinds lists every MatrixColumns, for the API document.
var MatrixColumnKinds = []MatrixColumns{ColumnsRequirements, ColumnsIssues}

// MatrixColumn is a requirement, or an Armature issue by its key.
type MatrixColumn struct {
	Requirement *requirement.Ref `json:"requirement"`
	IssueKey    string           `json:"issueKey"`
}

// MatrixCell is every link between one row and one column, by their places in the answer.
type MatrixCell struct {
	Row    int          `json:"row"`
	Column int          `json:"column"`
	Links  []MatrixLink `json:"links"`
}

// MatrixLink is one link of a cell: a trace link's type, or an Armature link's relation.
type MatrixLink struct {
	Name    string `json:"name"`
	Suspect bool   `json:"suspect"`
}

// Matrix is a page of requirements against every column any of them reaches.
type Matrix struct {
	Rows    []requirement.Ref `json:"rows"`
	Columns []MatrixColumn    `json:"columns"`
	Cells   []MatrixCell      `json:"cells"`
	Total   int               `json:"total"`
	Limit   int               `json:"limit"`
	Offset  int               `json:"offset"`
}

// Asks is which dimensions a requirement's rule asks for.
type Asks struct {
	Decomposed, Implemented, Verified bool
}

// Counted are the links that judge one requirement: trace links where it is the target of a type counting
// decomposition or verification, and its Armature links neither broken nor detached.
type Counted struct {
	Decomposing, DecomposingSuspect                              int
	Implementing, ImplementingDone, ImplementingSuspect          int
	VerifyingLinks, VerifyingLinksSuspect                        int
	VerifyingIssues, VerifyingIssuesDone, VerifyingIssuesSuspect int
}

// Judge answers a requirement's level in each dimension, as the contract's table says.
func Judge(asks Asks, c Counted) (decomposed, implemented, verified Level) {
	decomposed, implemented, verified = LevelNA, LevelNA, LevelNA
	if asks.Decomposed {
		decomposed = level(c.Decomposing > 0, c.DecomposingSuspect == 0)
	}
	if asks.Implemented {
		implemented = level(c.Implementing > 0, c.ImplementingDone > 0 && c.ImplementingSuspect == 0)
	}
	if asks.Verified {
		verified = level(c.VerifyingLinks+c.VerifyingIssues > 0,
			c.VerifyingLinksSuspect+c.VerifyingIssuesSuspect == 0 && c.VerifyingIssuesDone == c.VerifyingIssues)
	}
	return decomposed, implemented, verified
}

func level(linked, whole bool) Level {
	switch {
	case !linked:
		return LevelNone
	case whole:
		return LevelFull
	default:
		return LevelPartial
	}
}

// count adds one requirement's level to the totals of its dimension; na counts nowhere.
func (t *Totals) count(l Level) {
	switch l {
	case LevelFull:
		t.Full++
	case LevelPartial:
		t.Partial++
	case LevelNone:
		t.None++
	default:
		return
	}
	t.Total++
}

// LevelFilter keeps the coverage rows at the levels named; an empty level keeps every one.
type LevelFilter struct {
	Decomposed, Implemented, Verified Level
}

// Keeps says whether a row is at the levels the filter names.
func (f LevelFilter) Keeps(r CoverageRow) bool {
	return (f.Decomposed == "" || f.Decomposed == r.Decomposed) &&
		(f.Implemented == "" || f.Implemented == r.Implemented) &&
		(f.Verified == "" || f.Verified == r.Verified)
}

// Summarize totals every row in each dimension.
func Summarize(rows []CoverageRow) CoverageSummary {
	var s CoverageSummary
	for _, r := range rows {
		s.Decomposed.count(r.Decomposed)
		s.Implemented.count(r.Implemented)
		s.Verified.count(r.Verified)
	}
	return s
}
