package trace

import (
	"slices"

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
	// MatrixMaxColumns bounds the columns of one page, and of a file, which holds every column.
	MatrixMaxColumns         = 500
	MatrixColumnDefaultLimit = 100
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

// Source is what counts for a dimension: links, as before #200, test results or code tags.
type Source string

const (
	SourceLinks Source = "links"
	SourceTests Source = "tests"
	SourceCode  Source = "code"
)

// VerifiedSources and ImplementedSources list what each dimension may count, for the API document; DefaultSources is
// a rule's without one.
var (
	VerifiedSources    = []Source{SourceLinks, SourceTests, SourceCode}
	ImplementedSources = []Source{SourceLinks, SourceCode}
	DefaultSources     = []Source{SourceLinks}
)

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
	// VerifiedFrom and ImplementedFrom are the sources that count for each dimension, at least one.
	VerifiedFrom    []Source `json:"verifiedFrom"`
	ImplementedFrom []Source `json:"implementedFrom"`
}

// CoverageRuleInput sets one type's rule, or the project's rule for every other type when TypeID is absent.
type CoverageRuleInput struct {
	TypeID      *uuid.UUID `json:"typeId,omitempty"`
	Decomposed  bool       `json:"decomposed"`
	Implemented bool       `json:"implemented"`
	Verified    bool       `json:"verified"`
	// VerifiedFrom and ImplementedFrom are the sources that count for each dimension; links alone when absent.
	VerifiedFrom    []Source `json:"verifiedFrom,omitempty"`
	ImplementedFrom []Source `json:"implementedFrom,omitempty"`
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

// Asks is which dimensions a requirement's rule asks for, and what counts for verified; links when none is named.
type Asks struct {
	Decomposed, Implemented, Verified bool
	VerifiedFrom, ImplementedFrom     []Source
}

// Counted are the links that judge one requirement: trace links where it is the target of a type counting
// decomposition or verification, and its Armature links neither broken nor detached.
type Counted struct {
	Decomposing, DecomposingSuspect                              int
	Implementing, ImplementingDone, ImplementingSuspect          int
	VerifyingLinks, VerifyingLinksSuspect                        int
	VerifyingIssues, VerifyingIssuesDone, VerifyingIssuesSuspect int
	// Tests are the latest results on the branch evidence is read on; TestsWhole those passed against the current
	// revision (#200).
	Tests, TestsWhole int
	// CodeImplementing and CodeVerifying are the code tags of each relation on that branch, the Whole ones naming the
	// current revision (#201).
	CodeImplementing, CodeImplementingWhole, CodeVerifying, CodeVerifyingWhole int
}

// Judge answers a requirement's level in each dimension, as the contract's table says.
func Judge(asks Asks, c Counted) (decomposed, implemented, verified Level) {
	decomposed, implemented, verified = LevelNA, LevelNA, LevelNA
	if asks.Decomposed {
		decomposed = level(c.Decomposing > 0, c.DecomposingSuspect == 0)
	}
	if asks.Implemented {
		from := asks.ImplementedFrom
		if len(from) == 0 {
			from = DefaultSources
		}
		implemented = judgeSources([]held{
			{slices.Contains(from, SourceLinks) && c.Implementing > 0, c.ImplementingDone > 0 && c.ImplementingSuspect == 0},
			{slices.Contains(from, SourceCode) && c.CodeImplementing > 0, c.CodeImplementingWhole == c.CodeImplementing},
		})
	}
	if asks.Verified {
		from := asks.VerifiedFrom
		if len(from) == 0 {
			from = DefaultSources
		}
		verified = judgeSources([]held{
			{slices.Contains(from, SourceLinks) && c.VerifyingLinks+c.VerifyingIssues > 0,
				c.VerifyingLinksSuspect+c.VerifyingIssuesSuspect == 0 && c.VerifyingIssuesDone == c.VerifyingIssues},
			{slices.Contains(from, SourceTests) && c.Tests > 0, c.TestsWhole == c.Tests},
			{slices.Contains(from, SourceCode) && c.CodeVerifying > 0, c.CodeVerifyingWhole == c.CodeVerifying},
		})
	}
	return decomposed, implemented, verified
}

// held is what one source a rule counts holds: anything, and all of it whole.
type held struct {
	any, whole bool
}

// judgeSources is none when no source holds anything and full when each that holds something holds it whole, so a
// source with nothing in it does not keep another from making a requirement full.
func judgeSources(sources []held) Level {
	linked, whole := false, true
	for _, s := range sources {
		if s.any {
			linked = true
			whole = whole && s.whole
		}
	}
	return level(linked, whole)
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
