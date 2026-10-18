package reqifio

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func TestAWordUploadIsReadAgainstTheLimitsOfAReqIFOne(t *testing.T) {
	if docx.MaxUnpackedMB != MaxUnpackedMB || docx.MaxMediaBytes != MaxImageBytes || docx.MaxPartBytes != MaxDocumentBytes {
		t.Errorf("the reader's limits differ from the import's")
	}
}

func TestTheChoicesTellRequirementsFromTheRest(t *testing.T) {
	para := func(style, label, text string) *docx.Block {
		return &docx.Block{Kind: docx.BlockParagraph, StyleID: style, StyleName: style, Label: label, Text: text}
	}
	for _, c := range []struct {
		name    string
		choices map[string]string
		lang    string
		block   *docx.Block
		want    bool
		number  string
	}{
		{"an English keyword by default", nil, "en", para("Normal", "", "The pedal shall return."), true, ""},
		{"a keyword is a whole word", nil, "en", para("Normal", "", "Marshall the pedals."), false, ""},
		{"a German keyword in a German project", nil, "de", para("Normal", "", "Das Pedal MUSS zurückkehren."), true, ""},
		{"no English keyword in a German project", nil, "de", para("Normal", "", "The pedal shall return."), false, ""},
		{"a style chosen", map[string]string{ChoiceWordStyles: "Req, Other"}, "en", para("req", "", "Plain words."), true, ""},
		{"keywords leave when a style is chosen", map[string]string{ChoiceWordStyles: "Req"}, "en", para("Normal", "", "It shall stop."), false, ""},
		{"a pattern over the label", map[string]string{ChoiceWordPattern: `REQ-\d+`}, "en", para("Normal", "REQ-7", "Stop."), true, "REQ-7"},
		{"a pattern's group", map[string]string{ChoiceWordPattern: `\[(\w+-\d+)\]`}, "en", para("Normal", "", "[SYS-12] Stop."), true, "SYS-12"},
		{"keywords chosen", map[string]string{ChoiceWordKeywords: "should"}, "en", para("Normal", "", "It should stop."), true, ""},
		{"nothing set makes everything one", map[string]string{ChoiceWordStyles: "", ChoiceWordPattern: "", ChoiceWordKeywords: ""}, "en",
			para("Normal", "", "Plain words."), true, ""},
	} {
		got, problems := readWordChoices(c.choices, c.lang, 0)
		if len(problems) > 0 {
			t.Fatalf("%s: %v", c.name, problems)
		}
		if is, number := got.classify(c.block, c.block.Text); is != c.want || number != c.number {
			t.Errorf("%s: got %v %q, want %v %q", c.name, is, number, c.want, c.number)
		}
	}
	for key, value := range map[string]string{ChoiceWordPattern: "(", ChoiceWordUnit: "page", ChoiceWordFront: "keep", "word.table.3": "one", "word.table.1": "cells"} {
		if _, problems := readWordChoices(map[string]string{key: value}, "en", 1); len(problems) != 1 || problems[0].Where != key {
			t.Errorf("%s=%s: %v", key, value, problems)
		}
	}
}

func TestAWordIdentityIsReadBack(t *testing.T) {
	b := &wordBuild{ns: "word:doc@project:", seen: map[string]int{}}
	long := strings.Repeat("chapter/", 20)
	id, ids := b.identifier(wordIDs{key: "BRK-R2", number: "REQ|7", paragraph: "1A2B3C4D", path: long})
	if !strings.HasPrefix(id, "word:doc@project:key:BRK-R2|number:REQ/7|paragraph:1A2B3C4D|path:#") || len(id) > 255 {
		t.Errorf("the identity %s", id)
	}
	back, ok := parseWordIdentity(id, b.ns)
	if !ok || back != ids {
		t.Errorf("read back %+v, want %+v", back, ids)
	}
	if again, _ := b.identifier(wordIDs{key: "BRK-R2", number: "REQ|7", paragraph: "1A2B3C4D", path: long}); again == id {
		t.Errorf("two parts share the identity %s", again)
	}
	if _, ok := parseWordIdentity(id, "word:other@project:"); ok {
		t.Errorf("another document's identity was read as this one's")
	}
	if got := wordTitle("REQ-7", "The pedal shall return within one second. It is tested daily."); got != "REQ-7 The pedal shall return within one second." {
		t.Errorf("the title %q", got)
	}
}

// aSpecification is a document with text before its first heading, a section with a requirement, informative
// text, a list its colon introduces and a picture with its caption, and a table of rows.
func aSpecification() *docx.Source {
	return &docx.Source{Title: "Brakes", DocID: "D0C", Tables: 1, Styles: []docx.StyleUse{{ID: "Normal", Name: "Normal", Count: 6}},
		Media: []docx.Media{{Path: "word/media/image1.png", ContentType: "image/png", Size: 4}},
		Blocks: []docx.Block{
			{Kind: docx.BlockParagraph, Index: 1, ParaID: "P1", Text: "This document says how the car brakes.", Rich: &docx.XNode{Name: "p", Children: []docx.XNode{{Text: "This document says how the car brakes."}}}},
			{Kind: docx.BlockHeading, Level: 1, StyleName: "heading 1", ParaID: "H1", Text: "Braking"},
			{Kind: docx.BlockParagraph, Index: 3, ParaID: "P3", Text: "The pedal shall stop the car:", Rich: &docx.XNode{Name: "p", Children: []docx.XNode{{Text: "The pedal shall stop the car:"}}}},
			{Kind: docx.BlockList, Index: 4, ParaID: "P4", Text: "dry\nwet", Rich: &docx.XNode{Name: "ul", Children: []docx.XNode{{Name: "li", Children: []docx.XNode{{Name: "p", Children: []docx.XNode{{Text: "dry"}}}}}}}},
			{Kind: docx.BlockPicture, Index: 5, Rich: &docx.XNode{Children: []docx.XNode{{Name: "object", Attrs: map[string]string{"data": "word/media/image1.png", "type": "image/png"}}}},
				Losses: []docx.Loss{{Kind: docx.LossFormatting, Message: "Underlining is not kept; the words are."}}},
			{Kind: docx.BlockParagraph, Index: 6, Caption: true, Text: "Figure 1", Rich: &docx.XNode{Name: "p", Children: []docx.XNode{{Text: "Figure 1"}}}},
			{Kind: docx.BlockParagraph, Index: 7, ParaID: "P7", Text: "Brakes wear.", Rich: &docx.XNode{Name: "p", Children: []docx.XNode{{Text: "Brakes wear."}}}},
			{Kind: docx.BlockHeading, Level: 2, StyleName: "heading 2", ParaID: "H2", Text: "Limits"},
			{Kind: docx.BlockTable, Table: 1, HeaderRows: 1, Text: "ID | Title | Text | Notes",
				Rows: [][]docx.Cell{
					{{Text: "ID", Col: 0}, {Text: "Title", Col: 1}, {Text: "Text", Col: 2}, {Text: "Notes", Col: 3}},
					{{Text: "L-1", Col: 0, ParaID: "R1"}, {Text: "Dry stop", Col: 1}, {Text: "Within 40 m.", Col: 2, Rich: &docx.XNode{Name: "td", Children: []docx.XNode{{Name: "p", Children: []docx.XNode{{Text: "Within 40 m."}}}}}}, {Text: "High", Col: 3}},
				},
				Rich: &docx.XNode{Name: "table"}},
		}}
}

func treeOf(p Preview) []string {
	var out []string
	for _, n := range p.Tree {
		line := strings.Repeat("  ", n.Depth) + string(n.Kind) + " " + n.Title + " (" + n.Source + ")"
		if n.ExistingKey != "" {
			line += " = " + n.ExistingKey + " by " + string(n.MatchedBy)
		}
		out = append(out, line)
	}
	return out
}

func wordJob(src *docx.Source) *job {
	return &job{Import: Import{FileName: "brakes.docx", Format: FormatDocx}, source: src}
}

func TestAWordDocumentBecomesSectionsAndRequirements(t *testing.T) {
	w := brakes()
	a := wordJob(aSpecification()).analyze(w, map[string]string{"word.table.1": TableRows})
	p := a.preview
	if len(p.Problems) > 0 {
		t.Fatalf("problems: %v", p.Problems)
	}
	want := []string{
		"section Brakes (before the first heading)",
		"  informative This document says how the car brakes. (paragraph 1)",
		"section Braking (Heading 1)",
		"  requirement The pedal shall stop the car: (paragraph 3)",
		"  informative Brakes wear. (paragraph 7)",
		"  section Limits (Heading 2)",
		"    requirement Dry stop (table 1 row 1)",
	}
	if got := treeOf(p); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if p.Objects != 5 || p.Sections != 3 || p.New != 5 || p.Tables != 1 || len(p.Styles) != 1 || p.Images != 1 {
		t.Errorf("the counts: %+v", p)
	}
	if p.Tree[3].Losses != 1 || p.LostCount == 0 {
		t.Errorf("the picture's loss is not the requirement's: %+v", p.Tree[3])
	}
	for identifier, want := range map[string]MatchBy{wordTitleDef: ByField, wordStatementDef: ByField, wordChapterDef: ByField} {
		if got := itemOf(t, p, identifier).By; got != want {
			t.Errorf("%s matched by %s", identifier, got)
		}
	}
	var notes, table SchemaItem
	for _, item := range p.Schema {
		switch {
		case item.Name == "Notes":
			notes = item
		case strings.HasPrefix(item.Identifier, wordTablePrefix):
			table = item
		case item.Name == "ID" && item.Field != fieldKey, item.Name == "Title" && item.Field != fieldTitle, item.Name == "Text" && item.Field != fieldStatement:
			t.Errorf("the column %s fills %q", item.Name, item.Field)
		}
	}
	if notes.By != ByName || table.By != ByCreate || table.Name != "ID, Title, Text, Notes" {
		t.Errorf("the table's type %+v and its Notes %+v", table, notes)
	}
	statement := ""
	for _, plan := range a.plans {
		if plan.Title == "The pedal shall stop the car:" {
			statement = string(plan.Statement)
		}
	}
	if !strings.Contains(statement, `"dry"`) || !strings.Contains(statement, `"type":"image"`) || !strings.Contains(statement, "Figure 1") {
		t.Errorf("the list, picture and caption did not join the paragraph: %s", statement)
	}
}

func TestTheChoicesChangeWhatTheDocumentMakes(t *testing.T) {
	src := aSpecification()
	heading := wordJob(src).analyze(brakes(), map[string]string{ChoiceWordUnit: UnitHeading, ChoiceWordFront: FrontSkip})
	want := []string{
		"skipped This document says how the car brakes. (before the first heading)",
		"section Braking (Heading 1)",
		"  requirement Limits (Heading 2)",
	}
	if got := treeOf(heading.preview); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("one requirement per heading:\n%s", strings.Join(got, "\n"))
	}
	none := wordJob(src).analyze(brakes(), map[string]string{ChoiceWordKeywords: "wear"})
	var kinds []string
	for _, n := range none.preview.Tree {
		kinds = append(kinds, string(n.Kind))
	}
	if got := strings.Join(kinds, " "); got != "section informative section informative requirement section requirement" {
		t.Errorf("with the keyword wear: %s", got)
	}
}

func TestASecondImportFindsWhatTheFirstMade(t *testing.T) {
	w := brakes()
	first := wordJob(aSpecification()).analyze(w, map[string]string{})
	// What the first import made, as its ties record it.
	w.ties["object"] = map[string]tie{}
	for i, plan := range first.plans {
		id := uuid.New()
		w.existing[id] = &existing{ID: id, Key: "BRK-R" + string(rune('1'+i)), Kind: plan.Kind, Revision: 1}
		w.ties["object"][plan.Ref] = tie{Target: id}
	}
	src := aSpecification()
	src.Blocks[2].ParaID = "EDITED"
	src.Blocks[6].ParaID = "EDITED2"
	again := wordJob(src).analyze(w, map[string]string{})
	if again.preview.New != 0 || again.preview.Updates != len(first.plans) {
		t.Fatalf("the second import: %d new, %d updates\n%s", again.preview.New, again.preview.Updates, strings.Join(treeOf(again.preview), "\n"))
	}
	byTitle := map[string]PreviewNode{}
	for _, n := range again.preview.Tree {
		byTitle[n.Title] = n
	}
	if n := byTitle["The pedal shall stop the car:"]; n.MatchedBy != WordMatchPath {
		t.Errorf("an edited paragraph identifier falls back to the path: %+v", n)
	}
	if n := byTitle["Braking"]; n.MatchedBy != WordMatchParagraph {
		t.Errorf("a heading is found by its paragraph: %+v", n)
	}
	elsewhere := wordJob(aSpecification())
	parent := uuid.New()
	w.existing[parent] = &existing{ID: parent, Key: "BRK-R99", Kind: requirement.KindSection}
	elsewhere.parentID = &parent
	if below := elsewhere.analyze(w, map[string]string{}); below.preview.Updates != 0 {
		t.Errorf("the same document below a section found what it made at the top")
	}
}

func TestAnExportFindsItsRequirementsByKey(t *testing.T) {
	w := brakes()
	id := uuid.New()
	w.existing[id] = &existing{ID: id, Key: "BRK-R2", Kind: requirement.KindRequirement, Revision: 3}
	w.keys = map[string]uuid.UUID{"BRK-R2": id}
	src := &docx.Source{Title: "Release 1", Exported: true, Blocks: []docx.Block{
		{Kind: docx.BlockParagraph, Role: docx.RoleCover, Text: "Baseline of Brakes"},
		{Kind: docx.BlockHeading, Level: 1, Key: "BRK-R1", Text: "Braking"},
		{Kind: docx.BlockHeading, Level: 2, Key: "BRK-R2", Text: "Stop within 40 m"},
		{Kind: docx.BlockParagraph, Text: "The vehicle shall stop.", Rich: &docx.XNode{Name: "p", Children: []docx.XNode{{Text: "The vehicle shall stop."}}}},
		{Kind: docx.BlockParagraph, Role: docx.RoleRationale, StyleID: "Label", Text: "Rationale"},
		{Kind: docx.BlockParagraph, Text: "The law says so.", Rich: &docx.XNode{Name: "p", Children: []docx.XNode{{Text: "The law says so."}}}},
		{Kind: docx.BlockTable, Role: docx.RoleProperties, Table: 1},
	}}
	a := wordJob(src).analyze(w, map[string]string{})
	want := []string{"skipped Cover and contents (before the first requirement)", "section Braking (Heading 1)",
		"  requirement Stop within 40 m (Heading 2) = BRK-R2 by key"}
	if got := treeOf(a.preview); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the tree:\n%s", strings.Join(got, "\n"))
	}
	for _, plan := range a.plans {
		if plan.Existing != nil && (!strings.Contains(string(plan.Statement), "The vehicle shall stop.") || !strings.Contains(string(plan.Rationale), "The law says so.")) {
			t.Errorf("the statement %s and rationale %s", plan.Statement, plan.Rationale)
		}
	}
}
