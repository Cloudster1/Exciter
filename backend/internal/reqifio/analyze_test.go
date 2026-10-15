package reqifio

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// brakes is a project held in memory: a type, a select attribute, a text attribute, two link types and a member.
func brakes() *world {
	functional := uuid.New()
	w := &world{
		project: &project.Project{ID: uuid.New(), Key: "BRK", Name: "Brakes"},
		types:   map[uuid.UUID]named{functional: {functional, "Functional"}},
		defs: []attribute.Def{
			{ID: uuid.New(), Name: "Risk", Kind: attribute.Select, Options: []attribute.Option{{ID: uuid.New(), Name: "High"}, {ID: uuid.New(), Name: "Medium"}}},
			{ID: uuid.New(), Name: "Notes", Kind: attribute.Text},
		},
		linkTypes: map[uuid.UUID]linkType{},
		statuses:  map[uuid.UUID]named{},
		workflow:  map[uuid.UUID]map[uuid.UUID]bool{},
		members:   map[string]uuid.UUID{"ada@example.com": uuid.New()},
		existing:  map[uuid.UUID]*existing{},
		children:  map[uuid.UUID][]uuid.UUID{},
		links:     map[linkKey]bool{},
		ties:      map[string]map[string]tie{},
	}
	for _, key := range []string{"satisfies", "derives"} {
		id := uuid.New()
		w.linkTypes[id] = linkType{ID: id, Key: key, Name: strings.ToUpper(key[:1]) + key[1:]}
	}
	return w
}

func supplierFile(t *testing.T) *File {
	t.Helper()
	f, _, _, err := Read(supplier(t))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func itemOf(t *testing.T, p Preview, identifier string) SchemaItem {
	t.Helper()
	for _, item := range p.Schema {
		if item.Identifier == identifier {
			return item
		}
	}
	t.Fatalf("no schema item %s in %+v", identifier, p.Schema)
	return SchemaItem{}
}

func TestAnalyzeMapsByNameMakesTheRestAndReadsFields(t *testing.T) {
	a := analyze(supplierFile(t), brakes(), map[string]string{})
	p := a.preview
	if len(p.Problems) > 0 {
		t.Fatalf("problems: %+v", p.Problems)
	}
	for identifier, want := range map[string]MatchBy{
		"_t_req": ByCreate, "_t_heading": BySection, "_a_name": ByField, "_a_text": ByField, "_a_foreign": ByField,
		"_a_risk": ByName, "_a_platforms": ByCreate, "_a_weight": ByCreate, "_a_due": ByCreate, "_rt_satisfies": ByName,
	} {
		if got := itemOf(t, p, identifier).By; got != want {
			t.Errorf("%s matched by %s, want %s", identifier, got, want)
		}
	}
	if got := itemOf(t, p, "_a_platforms").Becomes; got != attribute.MultiSelect {
		t.Errorf("a multi-valued enumeration becomes %s", got)
	}
	if got := itemOf(t, p, "_a_count").Becomes; got != attribute.Number {
		t.Errorf("an integer becomes %s", got)
	}
	if p.New != 4 || p.Updates != 0 || p.Sections != 2 {
		t.Errorf("%d new, %d updates and %d sections; the heading and the specification are sections", p.New, p.Updates, p.Sections)
	}
	if risk := a.schema.attrs["_a_risk"]; len(risk.missing) != 1 || risk.missing[0] != "Low" {
		t.Errorf("the option the project lacks: %+v", risk.missing)
	}
	r1 := a.byRef["_o_r1"]
	if r1.Title != "Stop within 40 m" || r1.Parent != "_o_h1" || !strings.Contains(string(r1.Statement), "shall") {
		t.Errorf("_o_r1 plans %+v", r1)
	}
	if h1 := a.byRef["_o_h1"]; h1.Kind != requirement.KindSection || h1.Title != "Braking" || h1.Parent != "spec:_spec" {
		t.Errorf("the heading plans %+v", h1)
	}
	kinds := map[LossKind]int{}
	for _, l := range p.Losses {
		kinds[l.Kind]++
	}
	if kinds[LossFormatting] != 1 || kinds[LossImage] != 1 {
		t.Errorf("losses %+v: the underlining, and the image a .reqif cannot carry", p.Losses)
	}
	if len(a.relations) != 1 {
		t.Errorf("relations %+v", a.relations)
	}
}

func TestAnalyzeTakesChoicesAndRefusesWhatCannotFit(t *testing.T) {
	w := brakes()
	w.defs = append(w.defs, attribute.Def{ID: uuid.New(), Name: "Weight", Kind: attribute.Text})
	var functional uuid.UUID
	for id := range w.types {
		functional = id
	}
	a := analyze(supplierFile(t), w, map[string]string{})
	if len(a.preview.Problems) != 1 || !strings.Contains(a.preview.Problems[0].Message, "Weight") {
		t.Fatalf("problems %+v: the project's Weight is text and the file's a number", a.preview.Problems)
	}
	a = analyze(supplierFile(t), w, map[string]string{"_a_weight": ChoiceSkip, "_t_req": functional.String()})
	if len(a.preview.Problems) > 0 {
		t.Fatalf("problems after the choices: %+v", a.preview.Problems)
	}
	if item := itemOf(t, a.preview, "_t_req"); item.By != ByChoice || item.MeansName != "Functional" {
		t.Errorf("the chosen type: %+v", item)
	}
	if _, ok := a.byRef["_o_r1"].Attributes[w.defs[2].ID.String()]; ok {
		t.Errorf("a skipped attribute's value came along")
	}
	required := attribute.Def{ID: uuid.New(), Name: "Safety class", Kind: attribute.Text, Required: true, TypeIDs: []uuid.UUID{functional}}
	w.defs = append(w.defs, required)
	a = analyze(supplierFile(t), w, map[string]string{"_a_weight": ChoiceSkip, "_t_req": functional.String()})
	if len(a.preview.Problems) != 2 {
		t.Errorf("problems %+v: both requirements lack the required Safety class", a.preview.Problems)
	}
	a = analyze(supplierFile(t), w, map[string]string{"_a_weight": uuid.NewString()})
	if len(a.preview.Problems) == 0 {
		t.Errorf("a choice naming nothing of the project was taken")
	}
}

func TestAnalyzeUpdatesWhatAnEarlierExchangeTied(t *testing.T) {
	w := brakes()
	r1 := &existing{ID: uuid.New(), Key: "BRK-R7", Kind: requirement.KindRequirement, Revision: 3}
	w.existing[r1.ID] = r1
	w.ties["object"] = map[string]tie{"_o_r1": {Target: r1.ID}}
	a := analyze(supplierFile(t), w, map[string]string{})
	if a.preview.Updates != 1 || a.byRef["_o_r1"].Existing != r1 {
		t.Errorf("%d updates; _o_r1 is BRK-R7 again", a.preview.Updates)
	}
	if a.byRef["_o_r1"].KeepPlace {
		t.Errorf("a requirement of the file's hierarchy keeps its place")
	}
}
