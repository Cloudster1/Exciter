package docx

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// A test execution's report (#251): its target and testers, the counts by outcome, and each case iteration with its
// steps, what they came to, its files and its issues.

// stepShares are the widths of a case's steps table, in percent of the text.
var stepShares = []int{6, 32, 26, 12, 24}

// ExecutionReportBytesWith writes an execution's report, into a Word template when one is given (#214).
func ExecutionReportBytesWith(ctx context.Context, rep *testexec.Report, exportedBy string, at time.Time, lang string, t *Template,
	values Fill) ([]byte, error) {
	w := newWriter(ctx, lang, Reader{})
	w.layout, w.fill = t, values
	if t == nil {
		w.b.WriteString(xml.Header)
		w.b.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `"><w:body>`)
	}
	f := frame{width: w.pageText()}
	e := rep.Execution
	w.para(f, paraProps{style: "Title"}, func() { w.text(e.Title, runProps{}) })
	w.para(f, paraProps{style: "Subtitle"}, func() { w.text(w.words.pick("Test execution of ", "Testdurchführung von ")+e.ProjectKey, runProps{}) })
	lines := []string{w.words.pick("Plan: ", "Plan: ") + e.PlanTitle, w.words.pick("Managed by ", "Verantwortlich: ") + e.Manager.Name}
	if e.Baseline != nil {
		lines = append(lines, w.words.pick("Baseline: ", "Baseline: ")+e.Baseline.Name)
	}
	if e.Version != "" {
		lines = append(lines, w.words.pick("Version: ", "Version: ")+e.Version)
	}
	if e.Configuration != "" {
		lines = append(lines, w.words.pick("Configuration: ", "Konfiguration: ")+e.Configuration)
	}
	var testers []string
	for _, s := range e.Testers {
		testers = append(testers, s.Name)
	}
	lines = append(lines, w.words.pick("Testers: ", "Testende: ")+strings.Join(testers, ", "),
		w.words.pick("Started ", "Begonnen ")+w.words.day(e.OpenedAt))
	if e.ClosedAt != nil {
		lines = append(lines, w.words.executionEnd(e.State)+" "+w.words.day(*e.ClosedAt))
	} else {
		lines = append(lines, w.words.pick("Still open", "Noch offen"))
	}
	p := e.Progress
	lines = append(lines, fmt.Sprintf(w.words.pick("%d passed, %d failed, %d blocked, %d skipped, %d not run of %d",
		"%d bestanden, %d fehlgeschlagen, %d blockiert, %d übersprungen, %d nicht ausgeführt von %d"),
		p.Passed, p.Failed, p.Blocked, p.Skipped, p.NotRun, p.Total), w.words.exported(exportedBy, w.words.day(at)))
	if e.CloseComment != "" {
		lines = append(lines, e.CloseComment)
	}
	for _, line := range lines {
		w.para(f, paraProps{style: "CoverLine"}, func() { w.text(line, runProps{}) })
	}

	w.para(f, paraProps{style: "Heading1"}, func() { w.text(w.words.pick("Test cases", "Testfälle"), runProps{}) })
	for _, c := range rep.Cases {
		heading := c.Run.Case.Key + " " + c.Run.Case.Title
		if len(c.Run.Values) > 0 {
			var values []string
			for name, v := range c.Run.Values {
				values = append(values, name+" = "+v)
			}
			heading += " (" + strings.Join(sortedStrings(values), ", ") + ")"
		}
		w.para(f, paraProps{style: "Heading2", keepNext: true}, func() { w.text(heading, runProps{}) })
		meta := fmt.Sprintf(w.words.pick("Revision %d: %s", "Revision %d: %s"), c.Run.Revision, w.words.outcome(c.Run.Outcome))
		if c.Run.RecordedBy != nil {
			meta += ", " + c.Run.RecordedBy.Name
		}
		if c.Run.CI != nil {
			meta += w.words.pick(", from CI ", ", aus CI ") + c.Run.CI.Suite + " " + c.Run.CI.Name
		}
		w.para(f, paraProps{style: "EntryMeta"}, func() { w.text(meta, runProps{}) })
		if c.Run.Note != "" {
			w.para(f, paraProps{}, func() { w.text(c.Run.Note, runProps{}) })
		}
		if len(c.Steps) > 0 {
			recorded := map[int]testexec.StepResult{}
			for _, r := range c.Results {
				recorded[r.Step] = r
			}
			w.grid(f, stepShares, []string{"#", w.words.pick("Action", "Aktion"), w.words.pick("Expected result", "Erwartetes Ergebnis"),
				w.words.pick("Outcome", "Ergebnis"), w.words.pick("Actual result", "Tatsächliches Ergebnis")}, func(row func(...string)) {
				for i, s := range c.Steps {
					r := recorded[i+1]
					row(fmt.Sprint(i+1), plainOf(s.Action), plainOf(s.Expected), w.words.outcome(r.Outcome), r.Note)
				}
			})
		}
		if len(c.Files) > 0 {
			w.para(f, paraProps{}, func() {
				w.text(w.words.pick("Files: ", "Dateien: ")+strings.Join(c.Files, ", "), runProps{italic: true})
			})
		}
		if len(c.Issues) > 0 {
			w.para(f, paraProps{}, func() {
				w.text(w.words.pick("Issues: ", "Issues: ")+strings.Join(c.Issues, ", "), runProps{bold: true})
			})
		}
	}
	if w.err != nil {
		return nil, w.err
	}
	var body []byte
	if t != nil {
		body = w.framed(w.b.String())
	} else {
		w.b.WriteString(defaultSectPr())
		w.b.WriteString(`</w:body></w:document>`)
		body = []byte(w.b.String())
	}
	var buf bytes.Buffer
	core := coreProperties(&specdoc.Document{Title: e.Title, ProjectName: e.ProjectKey, ExportedBy: exportedBy, ExportedAt: at}, lang)
	if err := w.pack(&buf, body, at, core, lang); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// plainOf is a document's words on one line.
func plainOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	root, err := document.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(document.PlainText(root)), " ")
}

func sortedStrings(in []string) []string { return slices.Sorted(slices.Values(in)) }

// outcome names what a step or a case came to, in the reader's language; nil is not run.
func (w words) outcome(o *testexec.Outcome) string {
	if o == nil {
		return w.pick("Not run", "Nicht ausgeführt")
	}
	switch *o {
	case testexec.Passed:
		return w.pick("Passed", "Bestanden")
	case testexec.Failed:
		return w.pick("Failed", "Fehlgeschlagen")
	case testexec.Blocked:
		return w.pick("Blocked", "Blockiert")
	}
	return w.pick("Skipped", "Übersprungen")
}

// executionEnd says how an execution ended.
func (w words) executionEnd(s testexec.State) string {
	if s == testexec.Aborted {
		return w.pick("Aborted", "Abgebrochen")
	}
	return w.pick("Closed", "Abgeschlossen")
}

// ExecutionReportFileName names an execution's report after its project, its title and the day.
func ExecutionReportFileName(rep *testexec.Report, at time.Time, ext string) string {
	name := rep.Execution.ProjectKey
	if slug := strings.Trim(unslug.ReplaceAllString(strings.ToLower(rep.Execution.Title), "-"), "-"); slug != "" {
		name += "-" + slug
	}
	return name + "-test-report-" + at.Format(time.DateOnly) + "." + ext
}
