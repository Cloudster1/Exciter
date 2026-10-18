package csvio

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// A test case's steps and parameters as a spreadsheet cell holds them (#250): plain text a person reads and
// writes, as the statement is.

// stepStart opens a step, "3. Press the pedal."; expectedLine gives its expected result, "Expected: It stops."
var (
	stepStart    = regexp.MustCompile(`^\s*(\d+)[.)]\s+(.*)$`)
	expectedLine = regexp.MustCompile(`(?i)^\s*(expected|erwartet)\s*:\s*(.*)$`)
)

// StepsText writes steps one after another, each numbered, its expected result on a line of its own below it.
func StepsText(steps []requirement.Step) string {
	var b strings.Builder
	for i, s := range steps {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strconv.Itoa(i+1) + ". " + oneLine(PlainText(s.Action)))
		if expected := oneLine(PlainText(s.Expected)); expected != "" {
			b.WriteString("\n   Expected: " + expected)
		}
	}
	return b.String()
}

// StepsOf reads steps back: a numbered line starts one, an "Expected:" line gives its result, and any other line
// goes on what came before it.
func StepsOf(text string) []requirement.Step {
	type draft struct{ action, expected []string }
	var steps []draft
	inExpected := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := stepStart.FindStringSubmatch(line); m != nil {
			steps, inExpected = append(steps, draft{action: []string{m[2]}}), false
			continue
		}
		if len(steps) == 0 {
			steps = append(steps, draft{})
		}
		last := &steps[len(steps)-1]
		if m := expectedLine.FindStringSubmatch(line); m != nil {
			last.expected, inExpected = append(last.expected, m[2]), true
			continue
		}
		if inExpected {
			last.expected = append(last.expected, strings.TrimSpace(line))
		} else {
			last.action = append(last.action, strings.TrimSpace(line))
		}
	}
	out := make([]requirement.Step, 0, len(steps))
	for _, d := range steps {
		s := requirement.Step{Action: Document(strings.Join(d.action, "\n"))}
		if expected := strings.TrimSpace(strings.Join(d.expected, "\n")); expected != "" {
			s.Expected = Document(expected)
		}
		out = append(out, s)
	}
	return out
}

// oneLine joins a text's lines with spaces, so a step's numbering stays the only thing that starts a line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// planTestCase reads what a row's cells say a test case carries, refusing it on a row that is not one.
func (w *world) planTestCase(row Row, m Mapping, p *plan, section bool, refuse func(target, format string, args ...any)) {
	pre, steps, params := row.value(m, TargetPreconditions), row.value(m, TargetSteps), row.value(m, TargetParameters)
	if pre == "" && steps == "" && params == "" {
		return
	}
	typeID := p.TypeID
	if typeID == nil && p.Existing != nil {
		typeID = p.Existing.TypeID
	}
	if typeID == nil && p.Existing == nil {
		typeID = w.project.Settings.DefaultTypeID
	}
	target := TargetSteps
	if steps == "" {
		target = TargetParameters
		if params == "" {
			target = TargetPreconditions
		}
	}
	if section || typeID == nil || !w.testTypes[*typeID] {
		refuse(target, "Line %d has preconditions, steps or parameters, which only a requirement of a type marked as test cases has. Give it such a type, or clear the cells.", row.Line)
		return
	}
	t := &requirement.TestCase{}
	if pre != "" {
		t.Preconditions = Document(pre)
	}
	if steps != "" {
		t.Steps = StepsOf(steps)
	}
	if params != "" {
		t.Parameters = requirement.ParametersOf(params)
	}
	p.TestCase = t
}
