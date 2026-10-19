package csvio

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// plan is what one row of the file becomes: a new requirement, or a change to one the project has.
type plan struct {
	Line int
	// Ref is the row's key, by which other rows name it as a parent or a link's other end.
	Ref string
	// Existing is the requirement the row changes, nil for a new one.
	Existing   *existing
	Kind       requirement.Kind
	Title      string
	Statement  json.RawMessage
	Rationale  json.RawMessage
	TypeID     *uuid.UUID
	Status     *uuid.UUID
	Owner      *uuid.UUID
	OwnerSet   bool
	Priority   *requirement.Priority
	Labels     []string
	Attributes map[string]json.RawMessage
	// Parent is the key or the ref of the row above, empty for the top; ParentSet says the file says which.
	Parent    string
	ParentSet bool
	Links     []planLink
	// TestCase is what the row says a test case carries, nil when its cells say nothing of it.
	TestCase *requirement.TestCase
	// Risk is what the row rates a risk, nil when its cells say nothing of it.
	Risk *requirement.Rating
}

// planLink is a link the row asks for, from it to the other end.
type planLink struct {
	TypeKey string
	Other   string
}

// checkMapping refuses a mapping the file or the project cannot honour, before any row is read.
func (w *world) checkMapping(m Mapping, headers []string) []Problem {
	var problems []Problem
	targets := targetsOf(w.defs)
	if len(m[TargetTitle]) == 0 && len(m[TargetKey]) == 0 {
		problems = append(problems, Problem{Message: "Map a column to the title; every new requirement needs one."})
	}
	if len(m[TargetParent]) > 0 && len(m[TargetOutline]) > 0 {
		problems = append(problems, Problem{Message: "Map the parent or the outline, not both; either one builds the tree."})
	}
	for target, positions := range m {
		if !slices.Contains(targets, target) {
			problems = append(problems, Problem{Message: fmt.Sprintf("%q is not something a column can fill here. Map the column to one of the targets listed.", target)})
			continue
		}
		if len(positions) > 1 && !manyValued[target] {
			problems = append(problems, Problem{Message: fmt.Sprintf("The %s is filled from one column, and %d were mapped to it. Keep one.", target, len(positions))})
		}
		for _, at := range positions {
			if at < 0 || at >= len(headers) {
				problems = append(problems, Problem{Message: fmt.Sprintf("The file has no column %d. Map the columns the file has.", at+1)})
			}
		}
	}
	return problems
}

// planRow reads one row into what it becomes, and what in it keeps the import from starting.
func (w *world) planRow(row Row, m Mapping, c Choices, headers []string) (plan, []Problem) {
	p := plan{Line: row.Line, Kind: requirement.KindRequirement, Attributes: map[string]json.RawMessage{}}
	var problems []Problem
	refuse := func(target, format string, args ...any) {
		problems = append(problems, Problem{Row: row.Line, Column: columnName(m, target, headers), Message: fmt.Sprintf(format, args...)})
	}
	if key := strings.ToUpper(row.value(m, TargetKey)); key != "" {
		p.Ref = key
		if e, ok := w.existing[key]; ok {
			p.Existing = &e
			p.Kind = e.Kind
		} else if projectKey, _, ok := requirement.ParseKey(key); ok && projectKey == w.project.Key {
			refuse(TargetKey, "%s is not a requirement of %s out of the trash. Clear the key to make a new requirement, or correct it.", key, w.project.Key)
		}
	}
	if kind := strings.ToLower(row.value(m, TargetKind)); kind != "" {
		switch {
		case !slices.Contains(requirement.Kinds, requirement.Kind(kind)):
			refuse(TargetKind, "%q is not a kind. Write requirement or section.", kind)
		case p.Existing != nil && requirement.Kind(kind) != p.Existing.Kind:
			refuse(TargetKind, "%s is a %s, and the file says %s. A requirement keeps its kind; correct the cell.", p.Existing.Key, p.Existing.Kind, kind)
		default:
			p.Kind = requirement.Kind(kind)
		}
	}
	section := p.Kind == requirement.KindSection
	p.Title = row.value(m, TargetTitle)
	if p.Title == "" && p.Existing == nil {
		refuse(TargetTitle, "Line %d has no title. Give every new requirement a title.", row.Line)
	}
	if text := row.value(m, TargetStatement); text != "" {
		p.Statement = Document(text)
	}
	if text := row.value(m, TargetRationale); text != "" {
		if section {
			refuse(TargetRationale, "Line %d is a section, which has no rationale. Put its words in the statement.", row.Line)
		}
		p.Rationale = Document(text)
	}
	if word := row.value(m, TargetType); word != "" {
		if section {
			refuse(TargetType, "Line %d is a section, which has no type. Clear the cell.", row.Line)
		} else if _, id, ok := w.meaning(TargetType, word, c); ok {
			p.TypeID = &id
		}
	}
	if word := row.value(m, TargetStatus); word != "" && !section {
		if name, id, ok := w.meaning(TargetStatus, word, c); ok {
			if p.Existing != nil && !strings.EqualFold(name, p.Existing.Status) {
				refuse(TargetStatus, "%s is %s, and the file says %s. A requirement that exists changes its status by its transitions; clear the cell or take the transition first.",
					p.Existing.Key, p.Existing.Status, name)
			} else if p.Existing == nil {
				p.Status = &id
			}
		}
	}
	if name := row.value(m, TargetOwner); name != "" || len(m[TargetOwner]) > 0 {
		p.OwnerSet = !section
		if member, _ := w.person(name, c); name != "" && member != nil && !section {
			p.Owner = &member.ID
		}
	}
	if word := row.value(m, TargetPriority); word != "" && !section {
		if name, _, ok := w.meaning(TargetPriority, word, c); ok {
			priority := requirement.Priority(name)
			p.Priority = &priority
		}
	}
	for _, cell := range row.values(m, TargetLabels) {
		p.Labels = append(p.Labels, splitList(cell)...)
	}
	for _, d := range w.defs {
		target := AttributePrefix + d.Name
		cell := row.value(m, target)
		if cell == "" {
			continue
		}
		if section {
			refuse(target, "Line %d is a section, which has no attributes. Clear the cell.", row.Line)
			continue
		}
		value, err := w.attributeValue(d, cell, c)
		if err != "" {
			refuse(target, "%s", err)
			continue
		}
		if value != nil {
			p.Attributes[d.ID.String()] = value
		}
	}
	w.planTestCase(row, m, &p, section, refuse)
	w.planRisk(row, m, &p, section, refuse)
	if len(m[TargetParent]) > 0 {
		p.ParentSet, p.Parent = true, strings.ToUpper(row.value(m, TargetParent))
	}
	for _, cell := range row.values(m, TargetLinks) {
		for _, each := range splitList(cell) {
			typeKey, other, ok := strings.Cut(each, ":")
			typeKey, other = strings.ToLower(strings.TrimSpace(typeKey)), strings.ToUpper(strings.TrimSpace(other))
			switch {
			case !ok || typeKey == "" || other == "":
				refuse(TargetLinks, "%q is not a link. Write the link type's key, a colon and the other requirement's key, such as derives:%s-R1.", each, w.project.Key)
			case !w.linkTypes[typeKey]:
				refuse(TargetLinks, "%s uses no link type %q. Use the key of one of its link types, such as derives.", w.project.Key, typeKey)
			default:
				p.Links = append(p.Links, planLink{TypeKey: typeKey, Other: other})
			}
		}
	}
	return p, problems
}

// attributeValue writes a cell as the JSON value its attribute stores, or says what is wrong with it.
func (w *world) attributeValue(d attribute.Def, cell string, c Choices) (json.RawMessage, string) {
	target := AttributePrefix + d.Name
	encode := func(v any) json.RawMessage {
		out, _ := json.Marshal(v)
		return out
	}
	switch d.Kind {
	case attribute.Text:
		if utf8.RuneCountInString(cell) > attribute.MaxTextLength {
			return nil, fmt.Sprintf("%s takes at most %d characters, and a cell has more. Shorten it, or put it in the statement.", d.Name, attribute.MaxTextLength)
		}
		return encode(cell), ""
	case attribute.RichText:
		return Document(cell), ""
	case attribute.Number:
		n, err := strconv.ParseFloat(strings.ReplaceAll(cell, ",", "."), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Sprintf("%s takes a number, and %q is none. Write it as 12 or 2.5.", d.Name, cell)
		}
		return encode(n), ""
	case attribute.Select:
		if _, id, ok := w.meaning(target, cell, c); ok {
			return encode(id.String()), ""
		}
		return nil, ""
	case attribute.MultiSelect:
		ids := []string{}
		for _, word := range splitList(cell) {
			if _, id, ok := w.meaning(target, word, c); ok {
				ids = append(ids, id.String())
			}
		}
		return encode(ids), ""
	case attribute.Date:
		day, err := time.Parse(time.DateOnly, cell)
		if err != nil {
			return nil, fmt.Sprintf("%s takes a date, and %q is none. Write it as 2026-01-31.", d.Name, cell)
		}
		return encode(day.Format(time.DateOnly)), ""
	case attribute.User:
		if member, _ := w.person(cell, c); member != nil {
			return encode(member.ID.String()), ""
		}
		return nil, ""
	case attribute.Boolean:
		switch strings.ToLower(cell) {
		case "true", "yes", "1", "x", "ja":
			return encode(true), ""
		case "false", "no", "0", "nein":
			return encode(false), ""
		}
		return nil, fmt.Sprintf("%s takes yes or no, and %q is neither.", d.Name, cell)
	case attribute.URL:
		if u, err := url.Parse(cell); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Sprintf("%s takes a web address, and %q is none. Write it in full, such as https://example.com.", d.Name, cell)
		}
		return encode(cell), ""
	}
	return nil, ""
}
