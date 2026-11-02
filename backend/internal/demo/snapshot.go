// Package demo is the suite demo (#270, #488): a written specification of Armature, Exciter, Stator and Rotor each,
// with test cases and a test run, traced to their issues in Armature and to their guides in Stator.
package demo

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The snapshot travels with the binary, so adding the demo needs no network and gives the same demo every time.
//
//go:embed snapshot/*.json
var snapshotFiles embed.FS

// toolFiles is the order the projects are made in.
var toolFiles = []string{"armature", "exciter", "stator", "rotor"}

// Snapshot is what scripts/demo-snapshot.sh took from GitHub: one Tool per repository.
type Snapshot struct {
	Tools []Tool
}

// Tool is one repository's issues. Key prefixes its issues in Armature and its labels in
// Stator, whatever key its project gets in Exciter.
type Tool struct {
	Repo   string  `json:"repo"`
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	Commit string  `json:"commit"`
	Taken  string  `json:"taken"`
	Issues []Issue `json:"issues"`
	// Architecture is drawn by hand rather than taken, so it comes from architecture/, not the snapshot.
	Architecture *Architecture `json:"-"`
	// Spec is written by hand as well, in spec/.
	Spec []SpecFile `json:"-"`
}

// Issue is one GitHub issue as the snapshot keeps it.
type Issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Closed    bool       `json:"closed"`
	ClosedAt  *time.Time `json:"closedAt"`
	Milestone string     `json:"milestone"`
	URL       string     `json:"url"`
}

// Embedded is the snapshot this build carries, with each tool's architecture and specification.
func Embedded() (Snapshot, error) {
	s, err := embeddedIssues()
	if err != nil {
		return s, err
	}
	for i, t := range s.Tools {
		var errs []error
		if s.Tools[i].Spec, errs = readSpec(specFiles, toolDirs[t.Key]); len(errs) > 0 {
			return Snapshot{}, fmt.Errorf("the %s specification: %w", t.Name, errors.Join(errs...))
		}
	}
	return s, nil
}

// embeddedIssues is the snapshot and the architecture, which the specifications are held to.
func embeddedIssues() (Snapshot, error) {
	var s Snapshot
	for _, name := range toolFiles {
		raw, err := snapshotFiles.ReadFile("snapshot/" + name + ".json")
		if err != nil {
			return Snapshot{}, err
		}
		var t Tool
		if err := json.Unmarshal(raw, &t); err != nil {
			return Snapshot{}, fmt.Errorf("read the %s snapshot: %w", name, err)
		}
		if t.Architecture, err = embeddedArchitecture(name); err != nil {
			return Snapshot{}, err
		}
		s.Tools = append(s.Tools, t)
	}
	return s, nil
}

// Taken is the day the snapshot was taken, the latest of its repositories'.
func (s Snapshot) Taken() string {
	taken := ""
	for _, t := range s.Tools {
		taken = max(taken, t.Taken)
	}
	return taken
}
