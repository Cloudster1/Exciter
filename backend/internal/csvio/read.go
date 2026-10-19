package csvio

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/attribute"
)

// Targets a column fills besides an attribute, which is attr:<name>.
const (
	TargetKey       = "key"
	TargetKind      = "kind"
	TargetTitle     = "title"
	TargetStatement = "statement"
	TargetRationale = "rationale"
	TargetType      = "type"
	TargetStatus    = "status"
	TargetOwner     = "owner"
	TargetPriority  = "priority"
	TargetLabels    = "labels"
	TargetParent    = "parent"
	TargetOutline   = "outline"
	TargetLinks     = "links"
	// TargetPreconditions, TargetSteps and TargetParameters fill what a test case carries (#250).
	TargetPreconditions = "preconditions"
	TargetSteps         = "steps"
	TargetParameters    = "parameters"
	// TargetSeverity and the others rate a risk before and after mitigation (#259).
	TargetSeverity         = "severity"
	TargetProbability      = "probability"
	TargetDetection        = "detection"
	TargetSeverityAfter    = "severityAfter"
	TargetProbabilityAfter = "probabilityAfter"
	TargetDetectionAfter   = "detectionAfter"
	// AttributePrefix names an attribute's target, followed by its name.
	AttributePrefix = "attr:"
)

// BuiltInTargets are the targets every project has, in the order a mapping offers them.
var BuiltInTargets = []string{TargetKey, TargetKind, TargetTitle, TargetStatement, TargetRationale, TargetType, TargetStatus,
	TargetOwner, TargetPriority, TargetLabels, TargetParent, TargetOutline, TargetLinks, TargetPreconditions, TargetSteps, TargetParameters,
	TargetSeverity, TargetProbability, TargetDetection, TargetSeverityAfter, TargetProbabilityAfter, TargetDetectionAfter}

// manyValued are the targets several columns may fill at once.
var manyValued = map[string]bool{TargetLabels: true, TargetLinks: true}

// Row is one record of the file, with the line it starts on, so a problem points where a person can look.
type Row struct {
	Line  int      `json:"line"`
	Cells []string `json:"cells"`
}

// FileError is a file that cannot be read as CSV, with the sentence to show.
type FileError struct{ Message string }

func (e *FileError) Error() string { return e.Message }

// byteOrderMark is what a spreadsheet writes before the first header, as UTF-8 spells it.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// Read parses a file into headers and rows, comma or semicolon separated, refusing one too long to import.
func Read(data []byte) ([]string, []Row, error) {
	// The mark goes before parsing: a quoted header after it would not open its field.
	data = bytes.TrimPrefix(data, byteOrderMark)
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = separator(data)
	r.FieldsPerRecord = -1
	var (
		headers []string
		rows    []Row
	)
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, &FileError{fmt.Sprintf("The file is not CSV: %v. Save it from the spreadsheet as CSV and upload it again.", err)}
		}
		line, _ := r.FieldPos(0)
		if headers == nil {
			headers = make([]string, len(record))
			for n, h := range record {
				headers[n] = strings.TrimSpace(h)
			}
			continue
		}
		if blank(record) {
			continue
		}
		rows = append(rows, Row{Line: line, Cells: record})
		if len(rows) > MaxImportRows {
			return nil, nil, &FileError{fmt.Sprintf("The file has more than %d rows. Split it, and import the parts one after the other.", MaxImportRows)}
		}
	}
	if headers == nil {
		return nil, nil, &FileError{"The file is empty. Upload a file with a header row and a row for each requirement."}
	}
	return headers, rows, nil
}

// separator is a semicolon when the header row has more of them than commas, as a spreadsheet set to a
// language that writes decimal commas saves it.
func separator(data []byte) rune {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		return ';'
	}
	return ','
}

func blank(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// aliases are the header names a file from here, or a plain spreadsheet, gives each target.
var aliases = map[string]string{
	"key": TargetKey, "id": TargetKey, "requirement key": TargetKey,
	"kind":  TargetKind,
	"title": TargetTitle, "name": TargetTitle, "summary": TargetTitle, "heading": TargetTitle,
	"statement": TargetStatement, "text": TargetStatement, "description": TargetStatement, "requirement": TargetStatement,
	"rationale": TargetRationale, "reason": TargetRationale,
	"type": TargetType, "status": TargetStatus,
	"owner": TargetOwner, "assignee": TargetOwner,
	"priority": TargetPriority,
	"labels":   TargetLabels, "label": TargetLabels, "tags": TargetLabels,
	"parent": TargetParent, "parent key": TargetParent,
	"outline": TargetOutline, "number": TargetOutline, "no.": TargetOutline,
	"links": TargetLinks, "link": TargetLinks, "trace links": TargetLinks,
	"preconditions": TargetPreconditions, "precondition": TargetPreconditions,
	"steps": TargetSteps, "test steps": TargetSteps, "parameters": TargetParameters,
	"severity": TargetSeverity, "probability": TargetProbability, "occurrence": TargetProbability, "detection": TargetDetection,
	"severityafter": TargetSeverityAfter, "severity after": TargetSeverityAfter,
	"probabilityafter": TargetProbabilityAfter, "probability after": TargetProbabilityAfter, "occurrence after": TargetProbabilityAfter,
	"detectionafter": TargetDetectionAfter, "detection after": TargetDetectionAfter,
}

// Guess maps headers to targets by name, case aside, so a file exported from here maps itself. A file with a
// parent column builds the tree from it, and its outline column is left alone.
func Guess(headers []string, defs []attribute.Def) Mapping {
	m := Mapping{}
	for at, h := range headers {
		name := strings.ToLower(strings.TrimSpace(h))
		target, ok := aliases[name]
		if !ok {
			target, ok = attributeTarget(name, defs)
		}
		if !ok {
			continue
		}
		if _, taken := m[target]; taken && !manyValued[target] {
			continue
		}
		m[target] = append(m[target], at)
	}
	if len(m[TargetParent]) > 0 {
		delete(m, TargetOutline)
	}
	return m
}

// attributeTarget matches a header to an attribute by its name, with or without the attr: prefix.
func attributeTarget(header string, defs []attribute.Def) (string, bool) {
	header = strings.TrimPrefix(header, AttributePrefix)
	for _, d := range defs {
		if !d.Retired && strings.EqualFold(d.Name, header) {
			return AttributePrefix + d.Name, true
		}
	}
	return "", false
}

// targetsOf are every target a column may fill in a project with these attributes.
func targetsOf(defs []attribute.Def) []string {
	out := append([]string{}, BuiltInTargets...)
	for _, d := range defs {
		if !d.Retired {
			out = append(out, AttributePrefix+d.Name)
		}
	}
	return out
}

// values reads every cell a target is mapped to, in column order.
func (r Row) values(m Mapping, target string) []string {
	var out []string
	for _, at := range m[target] {
		if at < len(r.Cells) {
			if v := uncell(strings.TrimSpace(r.Cells[at])); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// value is the cell a single valued target is mapped to.
func (r Row) value(m Mapping, target string) string {
	if v := r.values(m, target); len(v) > 0 {
		return v[0]
	}
	return ""
}

// splitList reads the several values one cell may hold, separated by commas or semicolons.
func splitList(text string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == ',' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
