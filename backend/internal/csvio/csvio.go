// Package csvio moves requirements in and out of spreadsheets (#23), shaped
// after Armature's; docs/api-contract-m1.md says how.
package csvio

import (
	"time"

	"github.com/google/uuid"
)

const (
	MaxImportRows = 5000
	// MaxImportMB bounds a file, read whole before its rows are counted.
	MaxImportMB    = 8
	MaxImportBytes = MaxImportMB << 20
	PreviewRows    = 5
	// MaxExportRows bounds one export; past it the file ends with a note row.
	MaxExportRows = 20000
)

// Mapping maps each target to the columns that fill it, by position from 0.
type Mapping map[string][]int

// Column is one column of the file, with its first values.
type Column struct {
	At      int      `json:"at"`
	Name    string   `json:"name"`
	Samples []string `json:"samples"`
}

// Word is a value of the file that names something the project has, or not yet.
type Word struct {
	Target string `json:"target"`
	Value  string `json:"value"`
	Count  int    `json:"count"`
	// Means is what it matched, empty when it matched nothing.
	Means string `json:"means"`
}

// PersonFound is somebody the file names, and the member they matched by email.
type PersonFound struct {
	Name   string     `json:"name"`
	Count  int        `json:"count"`
	Member *uuid.UUID `json:"member"`
	Email  string     `json:"email"`
}

// Problem is something about the file that keeps the import from starting.
type Problem struct {
	// Row is the line of the file, 0 for the file as a whole.
	Row     int    `json:"row"`
	Column  string `json:"column"`
	Message string `json:"message"`
}

// Preview is what the file holds and what the mapping would make of it.
type Preview struct {
	Columns  []Column      `json:"columns"`
	Rows     int           `json:"rows"`
	Words    []Word        `json:"words"`
	People   []PersonFound `json:"people"`
	Problems []Problem     `json:"problems"`
}

// State is where an import stands.
type State string

const (
	// StateUploaded waits for its mapping to be confirmed.
	StateUploaded State = "uploaded"
	StateQueued   State = "queued"
	StateRunning  State = "running"
	StateDone     State = "done"
	StateFailed   State = "failed"
	// StateCancelled was stopped before or while it ran.
	StateCancelled State = "cancelled"
)

// States lists every State, for the API document.
var States = []State{StateUploaded, StateQueued, StateRunning, StateDone, StateFailed, StateCancelled}

// Refusal is a row that was not imported, and why.
type Refusal struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// Import is one file on its way into a project.
type Import struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"projectKey"`
	FileName   string    `json:"fileName"`
	State      State     `json:"state"`
	Mapping    Mapping   `json:"mapping"`
	// Targets are every target a column can map to in this project.
	Targets []string `json:"targets"`
	Preview Preview  `json:"preview"`
	// Done and Total count rows as the worker gets through them.
	Done     int        `json:"done"`
	Total    int        `json:"total"`
	Imported []string   `json:"imported"`
	Updated  []string   `json:"updated"`
	Refused  []Refusal  `json:"refused"`
	Error    string     `json:"error"`
	ByName   string     `json:"byName"`
	Created  time.Time  `json:"createdAt"`
	Finished *time.Time `json:"finishedAt"`
}

// PersonChoice says who a name in the file is: a member, or nobody.
type PersonChoice struct {
	Member *uuid.UUID `json:"member,omitempty"`
}

// MappingInput sets how the file's columns, words and people are read.
type MappingInput struct {
	Mapping Mapping `json:"mapping"`
	// Values map a word of the file to a name the project has, per target.
	Values map[string]map[string]string `json:"values,omitempty"`
	People map[string]PersonChoice      `json:"people,omitempty"`
}
