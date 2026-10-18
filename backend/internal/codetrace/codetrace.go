// Package codetrace keeps the requirement tags a scan of a repository found in its source comments (#201), such as
// [implements CHK-R42@3], one scan per repository and branch.
package codetrace

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// Agreed limits of code scans (#224).
const (
	// MaxScanMB bounds a scan's body.
	MaxScanMB     = 16
	MaxScanBytes  = MaxScanMB << 20
	MaxScanTags   = 50000
	MaxPathLength = 1000
	MaxNameLength = 200
	MaxURLLength  = 2000
	// MaxWords bounds the words of one relation, MaxWordLength each word.
	MaxWords      = 10
	MaxWordLength = 30
	// MaxReported bounds the tags a scan reports back; the rest are counted.
	MaxReported = 200
	// IdleBranchDays is how long a branch that does not count keeps its scan after its last upload.
	IdleBranchDays = 90
	// DefaultLimit and MaxLimit window the project's list of tags.
	DefaultLimit = 50
	MaxLimit     = 500
)

// Relation is what tagged code does for a requirement.
type Relation string

const (
	Implements Relation = "implements"
	Verifies   Relation = "verifies"
)

// Relations lists every Relation, for the API document.
var Relations = []Relation{Implements, Verifies}

// DefaultWords are a project's tag words until an administrator sets others: each relation's own name.
var DefaultWords = Words{Implements: []string{"implements"}, Verifies: []string{"verifies"}}

// State is where a tag stands: naming the current revision, an older one, or nothing it can reach.
type State string

const (
	Current  State = "current"
	Outdated State = "outdated"
	Orphaned State = "orphaned"
)

// States lists every State, for the API document.
var States = []State{Current, Outdated, Orphaned}

// Problem is why a tag a scan carried links nothing.
type Problem string

const (
	ProblemUnknown  Problem = "unknown"
	ProblemTrashed  Problem = "trashed"
	ProblemSection  Problem = "section"
	ProblemWord     Problem = "word"
	ProblemRevision Problem = "revision"
)

// Problems lists every Problem, for the API document.
var Problems = []Problem{ProblemUnknown, ProblemTrashed, ProblemSection, ProblemWord, ProblemRevision}

// FoundTag is one tag as the scanner read it: where, its word, the key, and the revision it names, if any.
type FoundTag struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Word     string `json:"word"`
	Key      string `json:"key"`
	Revision *int   `json:"revision,omitempty"`
}

// ScanInput is a scan of one repository at one commit of a branch; it replaces that branch's scan before.
type ScanInput struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Branch     string `json:"branch"`
	// FileURL is the address of a line at a commit, http or https, with {commit}, {path} and {line} in it.
	FileURL string     `json:"fileUrl,omitempty"`
	Tags    []FoundTag `json:"tags"`
}

// Reported is a tag a scan carried that links nothing, and why.
type Reported struct {
	FoundTag
	Problem Problem `json:"problem"`
}

// Scan is one upload of a scan and what it reported back.
type Scan struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"projectKey"`
	Repository string    `json:"repository"`
	Commit     string    `json:"commit"`
	Branch     string    `json:"branch"`
	FileURL    string    `json:"fileUrl"`
	// Tags counts the tags read, Linked those that name a requirement.
	Tags       int                   `json:"tags"`
	Linked     int                   `json:"linked"`
	Reported   []Reported            `json:"reported"`
	Unreported int                   `json:"unreported"`
	UploadedBy requirement.PersonRef `json:"uploadedBy"`
	Via        *via.Via              `json:"via"`
	UploadedAt time.Time             `json:"uploadedAt"`
}

// Link is one place in the code that implements or verifies a requirement.
type Link struct {
	Repository string   `json:"repository"`
	Branch     string   `json:"branch"`
	Commit     string   `json:"commit"`
	Path       string   `json:"path"`
	Line       int      `json:"line"`
	Word       string   `json:"word"`
	Relation   Relation `json:"relation"`
	// Revision is the one the tag names, or the one current when the tag first appeared on its branch.
	Revision  int       `json:"revision"`
	Outdated  bool      `json:"outdated"`
	Counts    bool      `json:"counts"`
	URL       string    `json:"url"`
	ScannedAt time.Time `json:"scannedAt"`
}

// Tag is a tag as the project lists it: its link, the key it carried and what it reaches.
type Tag struct {
	Link
	Key         string           `json:"key"`
	Requirement *requirement.Ref `json:"requirement"`
	State       State            `json:"state"`
	// Problem says why an orphaned tag reaches nothing.
	Problem Problem `json:"problem,omitempty"`
}

// Words are the words a project's tags use for each relation.
type Words struct {
	Implements []string `json:"implements"`
	Verifies   []string `json:"verifies"`
}

// TagWords is a project's tag words, and the keys its tags may carry, the former ones included, for the scanner.
type TagWords struct {
	Words
	ProjectKeys []string   `json:"projectKeys"`
	UpdatedAt   *time.Time `json:"updatedAt"`
}
