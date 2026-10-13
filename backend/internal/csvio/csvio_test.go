package csvio

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func TestReadTakesCommasOrSemicolonsAMarkAndQuotedLines(t *testing.T) {
	file := "\xEF\xBB\xBF\"key\";\"title\";\"statement\"\nCHK-R1;Pay;\"The shopper pays.\n\nAt once.\"\n\n;Refund;\n"
	headers, rows, err := Read([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(headers, "|") != "key|title|statement" {
		t.Fatalf("headers %q", headers)
	}
	if len(rows) != 2 || rows[0].Line != 2 || rows[1].Line != 6 || rows[0].Cells[2] != "The shopper pays.\n\nAt once." {
		t.Fatalf("rows %+v", rows)
	}
	for name, bad := range map[string]string{"empty": "", "unclosed": "a,b\n\"x,y"} {
		var fe *FileError
		if _, _, err := Read([]byte(bad)); err == nil || !asFileError(err, &fe) {
			t.Errorf("%s: %v", name, err)
		}
	}
	long := "title\n" + strings.Repeat("x\n", MaxImportRows+1)
	if _, _, err := Read([]byte(long)); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("a file over the rows: %v", err)
	}
}

func asFileError(err error, target **FileError) bool {
	fe, ok := err.(*FileError)
	*target = fe
	return ok
}

var risk = attribute.Def{ID: uuid.New(), Name: "Risk", Kind: attribute.Select, Options: []attribute.Option{{ID: uuid.New(), Name: "High"}, {ID: uuid.New(), Name: "Low"}}}

func TestGuessMapsAnExportedFileBackAndLeavesTheOutlineToAParent(t *testing.T) {
	m := Guess([]string{"key", "Title", "statement", "owner", "Labels", "labels", "attr:Risk", "outline", "parent", "Whatever"}, []attribute.Def{risk})
	want := map[string][]int{"key": {0}, "title": {1}, "statement": {2}, "owner": {3}, "labels": {4, 5}, "attr:Risk": {6}, "parent": {8}}
	if len(m) != len(want) {
		t.Fatalf("mapping %v", m)
	}
	for target, at := range want {
		if strings.Trim(strings.Join(strings.Fields(strings.Trim(jsonOf(m[target]), "[]")), ","), ",") != strings.Trim(jsonOf(at), "[]") {
			t.Errorf("%s maps to %v, want %v", target, m[target], at)
		}
	}
	if _, ok := Guess([]string{"Outline", "Title"}, nil)[TargetOutline]; !ok {
		t.Error("an outline without a parent is not mapped")
	}
}

func jsonOf(v any) string {
	out, _ := json.Marshal(v)
	return string(out)
}

func TestTextBecomesParagraphsAndBack(t *testing.T) {
	text := "The shopper pays.\nAt once.\n\nNo waiting."
	doc := Document(text)
	if !strings.Contains(string(doc), `"hardBreak"`) || strings.Count(string(doc), `"paragraph"`) != 2 {
		t.Fatalf("document %s", doc)
	}
	if got := PlainText(doc); got != text {
		t.Errorf("round trip %q, want %q", got, text)
	}
	if PlainText(nil) != "" || PlainText(json.RawMessage("null")) != "" {
		t.Error("nothing is not empty")
	}
}

func TestACellNeverStartsAFormulaAndReadsBack(t *testing.T) {
	for in, want := range map[string]string{"=SUM(A1)": "'=SUM(A1)", "-1": "'-1", "plain": "plain", "": ""} {
		if got := Cell(in); got != want {
			t.Errorf("Cell(%q) = %q, want %q", in, got, want)
		}
		if got := uncell(Cell(in)); got != in {
			t.Errorf("uncell(Cell(%q)) = %q", in, got)
		}
	}
}

// aWorld is a project of two types, two statuses, one select attribute, a member and one requirement.
func aWorld() *world {
	defaultType := uuid.New()
	return &world{
		project:   &project.Project{Key: "CHK", Settings: project.Settings{DefaultTypeID: &defaultType}},
		defs:      []attribute.Def{risk},
		types:     map[string]named{"functional": {ID: defaultType, Name: "Functional"}, "interface": {ID: uuid.New(), Name: "Interface"}},
		statuses:  map[string]named{"draft": {ID: uuid.New(), Name: "Draft"}, "approved": {ID: uuid.New(), Name: "Approved"}},
		members:   map[string]named{"ada@exciter.test": {ID: uuid.New(), Name: "ada@exciter.test"}},
		existing:  map[string]existing{"CHK-R1": {ID: uuid.New(), Key: "CHK-R1", Kind: requirement.KindRequirement, Status: "Draft", Revision: 2}},
		linkTypes: map[string]bool{"derives": true},
		elsewhere: func(key string) bool { return key == "SYS-R1" },
	}
}

func TestAnalysisMatchesWordsAndPeopleAndReportsEverythingElse(t *testing.T) {
	headers := []string{"key", "title", "type", "status", "owner", "attr:Risk", "parent", "links"}
	rows := []Row{
		{Line: 2, Cells: []string{"CHK-R1", "Pay", "functional", "Draft", "ada@exciter.test", "High", "", "derives:SYS-R1"}},
		{Line: 3, Cells: []string{"OLD-R7", "Refund", "Feature", "Approved", "bob@exciter.test", "Medium", "OLD-R8", "derives:OLD-R7, derives:NOPE-R1"}},
		{Line: 4, Cells: []string{"CHK-R9", "", "", "", "", "", "", "relates:CHK-R1"}},
	}
	w := aWorld()
	m := Guess(headers, w.defs)
	result := w.analyze(headers, rows, m, Choices{})
	messages := map[string]bool{}
	for _, p := range result.preview.Problems {
		messages[p.Message] = true
	}
	for _, want := range []string{
		`The type "Feature" matches nothing in CHK. Map it to one the project has, or change the file.`,
		`The value of Risk "Medium" matches nothing in CHK. Map it to one the project has, or change the file.`,
		"Nobody who may view CHK has the address bob@exciter.test. Choose who it is, or nobody.",
		"CHK-R9 is not a requirement of CHK out of the trash. Clear the key to make a new requirement, or correct it.",
		"Line 4 has no title. Give every new requirement a title.",
		"The parent OLD-R8 is neither a requirement of CHK nor a row of the file. Correct it, or clear it.",
		"NOPE-R1 is no requirement you can see, in the file or out of it. Correct the link, or leave it out.",
		`CHK uses no link type "relates". Use the key of one of its link types, such as derives.`,
	} {
		if !messages[want] {
			t.Errorf("missing problem %q in %v", want, result.preview.Problems)
		}
	}
	if len(result.preview.Problems) != 8 {
		t.Errorf("%d problems, want 8: %v", len(result.preview.Problems), result.preview.Problems)
	}

	answered := w.analyze(headers, rows[:2], m, Choices{
		Values: map[string]map[string]string{"type": {"Feature": "Interface"}, "attr:Risk": {"Medium": "Low"}},
		People: map[string]PersonChoice{"bob@exciter.test": {}},
	})
	for _, p := range answered.preview.Problems {
		if strings.Contains(p.Message, "matches nothing") || strings.Contains(p.Message, "Nobody who") {
			t.Errorf("an answered word or person is still a problem: %v", p)
		}
	}
	first, second := answered.plans[0], answered.plans[1]
	if first.Existing == nil || first.Status != nil || first.Owner == nil || len(first.Links) != 1 {
		t.Errorf("the row of CHK-R1 changes it, keeps its status and names its owner: %+v", first)
	}
	if second.Existing != nil || second.Ref != "OLD-R7" || second.TypeID == nil || second.Owner != nil || !second.OwnerSet {
		t.Errorf("the row of another project's key is new, named by its key, and owned by nobody: %+v", second)
	}
	if string(second.Attributes[risk.ID.String()]) != `"`+risk.Options[1].ID.String()+`"` {
		t.Errorf("the mapped option is stored by its id: %s", second.Attributes[risk.ID.String()])
	}
}

func TestTheOutlineBuildsTheTreeFromTheFilesOrder(t *testing.T) {
	headers := []string{"outline", "title", "kind"}
	rows := []Row{
		{Line: 2, Cells: []string{"1", "Checkout", "section"}},
		{Line: 3, Cells: []string{"1.1", "Pay", ""}},
		{Line: 4, Cells: []string{"1.2", "Refund", ""}},
		{Line: 5, Cells: []string{"2.1", "Orphan", ""}},
		{Line: 6, Cells: []string{"x.1", "Wrong", ""}},
	}
	w := aWorld()
	result := w.analyze(headers, rows, Guess(headers, nil), Choices{})
	if p := result.plans; p[1].Parent != "line:2" || p[2].Parent != "line:2" || p[0].Kind != requirement.KindSection || p[0].Ref != "line:2" {
		t.Errorf("the rows under 1 name its line: %+v", p)
	}
	if len(result.preview.Problems) != 2 {
		t.Errorf("an outline whose parent is missing and one of the wrong shape: %v", result.preview.Problems)
	}
	if problems := w.checkMapping(Mapping{"parent": {0}, "outline": {0}, "title": {1}}, headers); len(problems) != 1 {
		t.Errorf("a parent and an outline at once: %v", problems)
	}
}

func TestStatusOfAnExistingRequirementMovesByTransitionsAlone(t *testing.T) {
	w := aWorld()
	headers := []string{"key", "status"}
	result := w.analyze(headers, []Row{{Line: 2, Cells: []string{"CHK-R1", "Approved"}}}, Guess(headers, nil), Choices{})
	if len(result.preview.Problems) != 1 || !strings.Contains(result.preview.Problems[0].Message, "changes its status by its transitions") {
		t.Errorf("problems %v", result.preview.Problems)
	}
}
