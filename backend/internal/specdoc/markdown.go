package specdoc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	// Aliased, as specdoc has a document of its own: a stored one, or the empty one.
	rich "github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/markdown"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// MarkdownContentType is what the document is served as in Markdown (#187).
const MarkdownContentType = "text/markdown; charset=utf-8"

// entryHeading is the heading level of an entry at the top of the document, below its title.
const entryHeading = 2

// Its words are English, as the tools' are: the Markdown is read by assistants first, and Word and PDF speak to people.
var markdownFields = map[string]string{
	FieldType: "Type", FieldStatus: "Status", FieldOwner: "Owner", FieldPriority: "Priority", FieldLabels: "Labels",
}

// Markdown is a document written as Markdown in parts, so a reader short of room can stop between two entries.
type Markdown struct {
	Head    string
	Entries []string
	// Glossary is the terms the document defines (#210), after the tail; a document cut short leaves it out.
	Glossary string
	// Tail says the document holds fewer requirements than there are, empty when it holds them all.
	Tail string
}

// String is the whole document.
func (m Markdown) String() string {
	closing := m.Tail
	if m.Glossary != "" {
		closing = strings.TrimPrefix(closing+"\n\n"+m.Glossary, "\n\n")
	}
	return m.Upto(len(m.Entries), closing)
}

// Upto is the head and the first n entries, then the closing words.
func (m Markdown) Upto(n int, closing string) string {
	parts := append([]string{m.Head}, m.Entries[:n]...)
	if closing != "" {
		parts = append(parts, closing)
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// Markdown writes the document as the Markdown of #186, each entry a heading by its depth with its outline and key.
func (d *Document) Markdown() Markdown {
	out := Markdown{Head: d.markdownHead(), Entries: make([]string, len(d.Entries))}
	for i, e := range d.Entries {
		out.Entries[i] = e.markdown()
	}
	out.Glossary = d.markdownGlossary()
	if d.Truncated() {
		out.Tail = fmt.Sprintf("This document holds the first %d of the %d requirements and sections. Read a section to hold the rest.",
			len(d.Entries), d.Total)
	}
	return out
}

func (d *Document) markdownHead() string {
	project := d.ProjectName + " (" + d.ProjectKey + ")"
	of := "Specification of " + project + "."
	switch d.Kind {
	case KindSection:
		of = "Section " + d.SectionKey + " of " + project + "."
	case KindBaseline:
		of = "Baseline of " + project + ", taken by " + d.Baseline.TakenBy + " on " + day(d.Baseline.TakenAt) + "."
		if d.Baseline.LockedAt != nil {
			of += " Locked on " + day(*d.Baseline.LockedAt) + "; it never changes."
		}
		of += d.Baseline.signingSentence()
	}
	parts := []string{markdown.Heading(1, d.Title), markdown.Words(of + " Exported by " + d.ExportedBy + " on " + day(d.ExportedAt) + ".")}
	if d.Baseline != nil && d.Baseline.Description != "" {
		parts = append(parts, markdown.Paragraphs(d.Baseline.Description))
	}
	return strings.Join(parts, "\n\n")
}

func (e Entry) markdown() string {
	parts := []string{markdown.Heading(entryHeading+e.Depth, e.Outline+" "+e.Key+" "+e.Title)}
	if e.Kind == requirement.KindRequirement {
		fields := []string{"- Revision: " + strconv.Itoa(e.Revision)}
		for _, p := range e.Properties {
			fields = append(fields, "- "+markdown.Words(propertyName(p)+": "+propertyValue(p)))
		}
		parts = append(parts, strings.Join(fields, "\n"))
	}
	if s := written(e.Statement); s != "" {
		parts = append(parts, s)
	}
	if s := written(e.Rationale); s != "" {
		parts = append(parts, "**Rationale**", s)
	}
	if len(e.Links) > 0 {
		links := make([]string, len(e.Links))
		for i, l := range e.Links {
			links[i] = "- " + markdown.Words(l.Phrase+" "+l.Key+" "+l.Title)
		}
		parts = append(parts, "**Links**", strings.Join(links, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

// glossaryHeading is the heading of the glossary, at the level of an entry at the top.
const glossaryHeading = "Glossary"

// markdownGlossary writes each term by name, its definition, then the words that mean it and those not to use.
func (d *Document) markdownGlossary() string {
	if len(d.Glossary) == 0 {
		return ""
	}
	parts := []string{markdown.Heading(entryHeading, glossaryHeading)}
	for _, t := range d.Glossary {
		parts = append(parts, "**"+markdown.Words(t.Name)+"**")
		if s := written(t.Definition); s != "" {
			parts = append(parts, s)
		}
		var words []string
		if len(t.Synonyms) > 0 {
			words = append(words, "- "+markdown.Words("Synonyms: "+strings.Join(t.Synonyms, ", ")))
		}
		if len(t.Avoid) > 0 {
			words = append(words, "- "+markdown.Words("Not to use: "+strings.Join(t.Avoid, ", ")))
		}
		if len(words) > 0 {
			parts = append(parts, strings.Join(words, "\n"))
		}
	}
	return strings.Join(parts, "\n\n")
}

// written is a document as #186 writes it, or as its words where Markdown cannot hold it, marked so.
func written(raw json.RawMessage) string {
	if md := markdown.ForReading(raw); md != nil {
		return strings.TrimSpace(*md)
	}
	var root rich.Node
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return ""
	}
	plain := markdown.Paragraphs(rich.PlainText(root))
	if plain == "" {
		return ""
	}
	return "*(as plain text)*\n\n" + plain
}

func propertyName(p Property) string {
	if p.Field != "" {
		return markdownFields[p.Field]
	}
	return p.Name
}

func propertyValue(p Property) string {
	switch {
	case p.Flag != nil && *p.Flag:
		return "yes"
	case p.Flag != nil:
		return "no"
	case p.Document != nil:
		var root rich.Node
		if json.Unmarshal(p.Document, &root) == nil {
			return rich.PlainText(root)
		}
	}
	return p.Text
}

func day(t time.Time) string {
	return t.UTC().Format(time.DateOnly)
}
