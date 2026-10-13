package csvio

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// analysis is what a file and its mapping would make, and what keeps it from starting.
type analysis struct {
	preview Preview
	plans   []plan
}

// analyze reads every row against the project, as both the mapping step and the worker do, so the import that
// runs is the one that was shown.
func (w *world) analyze(headers []string, rows []Row, m Mapping, c Choices) analysis {
	out := analysis{preview: Preview{Columns: columns(headers, rows), Rows: len(rows), Words: []Word{}, People: []PersonFound{}, Problems: []Problem{}}}
	if problems := w.checkMapping(m, headers); len(problems) > 0 {
		out.preview.Problems = problems
		return out
	}
	words, problems := w.words(rows, m, c, headers)
	out.preview.Words = words
	people, unanswered := w.people(rows, m, c)
	out.preview.People = people
	problems = append(problems, unanswered...)
	for _, row := range rows {
		p, rowProblems := w.planRow(row, m, c, headers)
		out.plans = append(out.plans, p)
		problems = append(problems, rowProblems...)
	}
	problems = append(problems, w.structure(out.plans, rows, m, headers)...)
	problems = append(problems, w.required(out.plans)...)
	out.preview.Problems = append(out.preview.Problems, problems...)
	return out
}

// columns are the file's columns with their first values, which the mapping step shows.
func columns(headers []string, rows []Row) []Column {
	out := make([]Column, len(headers))
	for at, name := range headers {
		samples := []string{}
		for _, row := range rows {
			if len(samples) == PreviewRows {
				break
			}
			if at < len(row.Cells) && strings.TrimSpace(row.Cells[at]) != "" {
				samples = append(samples, strings.TrimSpace(row.Cells[at]))
			}
		}
		out[at] = Column{At: at, Name: name, Samples: samples}
	}
	return out
}

// structure resolves what the rows say about each other: keys named once, parents and link ends that exist,
// and the outline's numbers into parents.
func (w *world) structure(plans []plan, rows []Row, m Mapping, headers []string) []Problem {
	var problems []Problem
	refuse := func(line int, target, format string, args ...any) {
		problems = append(problems, Problem{Row: line, Column: columnName(m, target, headers), Message: fmt.Sprintf(format, args...)})
	}
	refs := map[string]int{}
	for i, p := range plans {
		if p.Ref == "" {
			continue
		}
		if first, taken := refs[p.Ref]; taken {
			refuse(p.Line, TargetKey, "%s is on line %d already. Give each row its own key, or clear it.", p.Ref, plans[first].Line)
			continue
		}
		refs[p.Ref] = i
	}
	if len(m[TargetOutline]) > 0 {
		outlines := map[string]int{}
		for i, row := range rows {
			number := strings.TrimSuffix(row.value(m, TargetOutline), ".")
			if number == "" {
				continue
			}
			if !outlineShape(number) {
				refuse(row.Line, TargetOutline, "%q is not an outline number. Write it as 2 or 2.3.1.", number)
				continue
			}
			outlines[number] = i
			plans[i].ParentSet = true
			if at := strings.LastIndex(number, "."); at >= 0 {
				parent, ok := outlines[number[:at]]
				if !ok {
					refuse(row.Line, TargetOutline, "%s comes before %s in the file, or is not there. Put every row after the one it is under.", number[:at], number)
					continue
				}
				// A row above without a key is named by its line, which no key looks like.
				plans[i].Parent = refOf(plans, parent)
			}
		}
	}
	for i, p := range plans {
		if p.ParentSet && p.Parent != "" && !w.knows(p.Parent, refs) {
			refuse(p.Line, TargetParent, "The parent %s is neither a requirement of %s nor a row of the file. Correct it, or clear it.", p.Parent, w.project.Key)
			plans[i].Parent = ""
		}
		for _, l := range p.Links {
			if !w.knows(l.Other, refs) && !w.elsewhere(l.Other) {
				refuse(p.Line, TargetLinks, "%s is no requirement you can see, in the file or out of it. Correct the link, or leave it out.", l.Other)
			}
		}
	}
	return problems
}

// lineRef names a row without a key, so a row under it can still say whose it is.
const lineRef = "line:"

func refOf(plans []plan, i int) string {
	if plans[i].Ref != "" {
		return plans[i].Ref
	}
	ref := fmt.Sprintf("%s%d", lineRef, plans[i].Line)
	plans[i].Ref = ref
	return ref
}

// knows says whether a key names a row of the file or a requirement of the project.
func (w *world) knows(key string, refs map[string]int) bool {
	if _, ok := refs[key]; ok {
		return true
	}
	_, ok := w.existing[key]
	return ok || strings.HasPrefix(key, lineRef)
}

func outlineShape(number string) bool {
	for _, part := range strings.Split(number, ".") {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}

// required refuses a new requirement without a value its type requires, before anything is written.
func (w *world) required(plans []plan) []Problem {
	var problems []Problem
	for _, p := range plans {
		if p.Existing != nil || p.Kind != requirement.KindRequirement {
			continue
		}
		typeID := p.TypeID
		if typeID == nil {
			typeID = w.project.Settings.DefaultTypeID
		}
		if err := attribute.Missing(w.defs, typeID, p.Attributes); err != nil {
			problems = append(problems, Problem{Row: p.Line, Message: sentence(err)})
		}
	}
	return problems
}

// sentence is a refusal's message, without the field an API error would name.
func sentence(err error) string {
	var field *attribute.FieldError
	if errors.As(err, &field) {
		return field.Message
	}
	return err.Error()
}
