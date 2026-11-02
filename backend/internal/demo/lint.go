package demo

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/markdown"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// LintEmbedded holds the specifications this build carries to what the demo needs of them.
func LintEmbedded() []error {
	s, err := embeddedIssues()
	if err != nil {
		return []error{err}
	}
	return Lint(specFiles, s)
}

// Lint reads every tool's specification under spec/ in fsys and answers each mistake a writer has to fix: what a file
// gets wrong, an ID twice, a link to nothing, an issue the snapshot lacks, a requirement no test case verifies.
func Lint(fsys fs.FS, s Snapshot) []error {
	issues := map[string]bool{}
	for _, t := range s.Tools {
		for _, i := range t.Issues {
			issues[fmt.Sprintf("%s-%d", t.Key, i.Number)] = true
		}
	}
	var errs []error
	for _, t := range s.Tools {
		files, parsed := readSpec(fsys, toolDirs[t.Key])
		errs = append(errs, parsed...)
		errs = append(errs, lintTool(t.Key, files, issues)...)
	}
	return errs
}

// placed is an item with the file it stands in.
type placed struct {
	file string
	item SpecItem
}

func lintTool(key string, files []SpecFile, issues map[string]bool) []error {
	var errs []error
	fail := func(at placed, format string, args ...any) {
		errs = append(errs, &SpecError{File: at.file, Line: at.item.Line, ID: at.item.ID, Text: fmt.Sprintf(format, args...)})
	}
	byID := map[string]placed{}
	var all []placed
	for _, f := range files {
		file := "spec/" + toolDirs[key] + "/" + f.Name
		for _, sec := range f.Sections {
			for _, title := range sec.Path {
				if utf8.RuneCountInString(title) > requirement.MaxTitleLength {
					errs = append(errs, &SpecError{File: file, Line: 1, Text: fmt.Sprintf("the section title %q is longer than %d characters. Shorten it.",
						title, requirement.MaxTitleLength)})
				}
			}
			errs = append(errs, lintMarkdown(file, 1, "", "introduction", sec.Intro)...)
			for _, it := range sec.Items {
				at := placed{file, it}
				if first, ok := byID[it.ID]; ok {
					fail(at, "the ID is taken by %s line %d. Give this one an ID of its own.", first.file, first.item.Line)
					continue
				}
				byID[it.ID] = at
				all = append(all, at)
			}
		}
	}
	verified := map[string]bool{}
	cases, runs := 0, 0
	for _, at := range all {
		it := at.item
		for _, link := range []struct {
			field string
			ids   []string
		}{{"refines", it.Refines}, {"relates", it.Relates}, {"verifies", it.Verifies}} {
			for _, id := range link.ids {
				target, ok := byID[id]
				switch {
				case !ok:
					fail(at, "%s names %s, which no file of %s defines. Name an ID that exists, or write %s.", link.field, id, toolDirs[key], id)
				case id == it.ID:
					fail(at, "%s names the item itself. Name another.", link.field)
				case link.field == "verifies" && target.item.IsTestCase():
					fail(at, "verifies names %s, which is a test case. Name the requirements it verifies.", id)
				case link.field == "verifies":
					verified[id] = true
				}
			}
		}
		for _, issue := range it.Issues {
			if !issues[issue] {
				fail(at, "issues names %s, which the snapshot does not hold. Name an issue that exists, or run make demo-snapshot if it is new.", issue)
			}
		}
		if it.IsTestCase() {
			cases++
			runs += iterations(it)
			if it.Result != "" && it.Result != resultPassed && it.Note == "" {
				fail(at, "the result is %s and says nothing about it. Add `note:` with a sentence a tester would write.", it.Result)
			}
			if len(it.Steps) > requirement.MaxSteps {
				fail(at, "the test case has %d steps, more than %d. Split it.", len(it.Steps), requirement.MaxSteps)
			}
		}
		if utf8.RuneCountInString(it.Title) > requirement.MaxTitleLength {
			fail(at, "the title is longer than %d characters. Shorten it.", requirement.MaxTitleLength)
		}
		errs = append(errs, lintItem(at)...)
	}
	for _, at := range all {
		if at.item.IsRequirement() && !verified[at.item.ID] {
			fail(at, "no test case verifies this requirement. Write one whose `verifies:` names %s.", at.item.ID)
		}
	}
	if cases > testexec.MaxPlanCases || runs > testexec.MaxExecutionCases {
		errs = append(errs, &SpecError{File: "spec/" + toolDirs[key], Text: fmt.Sprintf("%d test cases run %d times; a plan holds %d and a run %d. Write fewer.",
			cases, runs, testexec.MaxPlanCases, testexec.MaxExecutionCases)})
	}
	return errs
}

// iterations is how often a test case runs: once a value of its parameters.
func iterations(it SpecItem) int {
	if len(it.Parameters) == 0 {
		return 1
	}
	return len(it.Parameters[0].Values)
}

// lintItem holds each piece of an item's Markdown to what Exciter takes.
func lintItem(at placed) []error {
	it := at.item
	errs := lintMarkdown(at.file, it.Line, it.ID, "statement", it.Statement)
	errs = append(errs, lintMarkdown(at.file, it.Line, it.ID, "rationale", it.Rationale)...)
	errs = append(errs, lintMarkdown(at.file, it.Line, it.ID, "preconditions", it.Preconditions)...)
	for i, s := range it.Steps {
		errs = append(errs, lintMarkdown(at.file, it.Line, it.ID, fmt.Sprintf("step %d's action", i+1), s.Action)...)
		errs = append(errs, lintMarkdown(at.file, it.Line, it.ID, fmt.Sprintf("step %d's expected result", i+1), s.Expected)...)
	}
	for _, p := range it.Parameters {
		for _, v := range p.Values {
			if utf8.RuneCountInString(v) > requirement.MaxParameterValueLength || utf8.RuneCountInString(p.Name) > requirement.MaxParameterNameLength {
				errs = append(errs, &SpecError{File: at.file, Line: it.Line, ID: it.ID, Text: fmt.Sprintf(
					"the parameter %s is too long. Keep a name to %d characters and a value to %d.", p.Name,
					requirement.MaxParameterNameLength, requirement.MaxParameterValueLength)})
			}
		}
	}
	return errs
}

func lintMarkdown(file string, line int, id, what, md string) []error {
	if strings.TrimSpace(md) == "" {
		return nil
	}
	check := func(raw json.RawMessage) error { _, err := document.Parse(raw); return err }
	if _, err := requirement.FromMarkdown(what, &md, nil, markdown.Resolve{}, check); err != nil {
		return []error{&SpecError{File: file, Line: line, ID: id, Text: fmt.Sprintf("Exciter refuses the %s: %v. Write it as plain Markdown, "+
			"with a placeholder such as <name> in backticks.", what, strings.TrimSuffix(err.Error(), "."))}}
	}
	return nil
}
