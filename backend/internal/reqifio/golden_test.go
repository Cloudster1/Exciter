package reqifio

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// goldenExport is testdata/export.reqif, which make reqif-schema holds to the ReqIF schema.
var goldenExport = filepath.Join("testdata", "export.reqif")

// goldenSnapshot is a project with a section, two requirements of two types, every kind of attribute, rich text with
// a table, a list and a picture, a link, and identifiers an earlier exchange tied.
func goldenSnapshot() *snapshot {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	id := func(n byte) uuid.UUID { return uuid.UUID{0x01, 0x93, 0, 0, 0, 0, 0x70, 0, 0x80, 0, 0, 0, 0, 0, 0, n} }
	typeA, typeB, section, r1, r2, picture := id(1), id(2), id(3), id(4), id(5), id(6)
	risk := attribute.Def{ID: id(10), Name: "Risk", Kind: attribute.Select, Options: []attribute.Option{{ID: id(11), Name: "Low"}, {ID: id(12), Name: "High"}}}
	tags := attribute.Def{ID: id(13), Name: "Tags", Kind: attribute.MultiSelect, Options: []attribute.Option{{ID: id(14), Name: "Truck"}, {ID: id(15), Name: "Bus"}}}
	defs := []attribute.Def{risk, tags,
		{ID: id(16), Name: "Notes", Kind: attribute.Text}, {ID: id(17), Name: "Detail", Kind: attribute.RichText},
		{ID: id(18), Name: "Weight", Kind: attribute.Number}, {ID: id(19), Name: "Due", Kind: attribute.Date},
		{ID: id(20), Name: "Safe", Kind: attribute.Boolean}, {ID: id(21), Name: "Reference", Kind: attribute.URL},
		{ID: id(22), Name: "Reviewer", Kind: attribute.User, TypeIDs: []uuid.UUID{typeA}},
	}
	statement := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The vehicle "},` +
		`{"type":"text","text":"shall","marks":[{"type":"bold"}]},{"type":"text","text":" stop within 40 m."}]},` +
		`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"on dry roads"}]}]}]},` +
		`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Surface"}]}]},` +
		`{"type":"tableCell","attrs":{"colspan":2},"content":[{"type":"paragraph","content":[{"type":"text","text":"Dry"}]}]}]}]},` +
		`{"type":"image","attrs":{"attachmentId":"` + picture.String() + `","alt":"Braking curve"}}]}`)
	priority, status := requirement.PriorityMust, id(30)
	node := func(n uuid.UUID, kind requirement.Kind, title string, parent *uuid.UUID, typeID *uuid.UUID, depth int) requirement.TreeNode {
		return requirement.TreeNode{ID: n, Key: "BRK-R" + string(rune('0'+n[15]-2)), Kind: kind, Title: title, ParentID: parent, TypeID: typeID, Depth: depth}
	}
	return &snapshot{
		project: &project.Project{ID: id(40), Key: "BRK", Name: "Brakes"},
		nodes: []requirement.TreeNode{
			node(section, requirement.KindSection, "Braking", nil, nil, 0),
			node(r1, requirement.KindRequirement, "Stop within 40 m", &section, &typeA, 1),
			node(r2, requirement.KindRequirement, "Warn the driver", nil, &typeB, 0),
		},
		contents: map[uuid.UUID]content{
			section: {Statement: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`), Updated: at},
			r1: {Statement: statement, Rationale: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Law."}]}]}`),
				Status: &status, Owner: "ada@example.com", Priority: &priority, Labels: []string{"brakes", "safety"}, Updated: at,
				Attributes: map[string]json.RawMessage{
					risk.ID.String(): json.RawMessage(`"` + id(12).String() + `"`), tags.ID.String(): json.RawMessage(`["` + id(14).String() + `","` + id(15).String() + `"]`),
					id(16).String(): json.RawMessage(`"Dry road"`), id(17).String(): json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Measured."}]}]}`),
					id(18).String(): json.RawMessage(`12.5`), id(19).String(): json.RawMessage(`"2026-12-01"`), id(20).String(): json.RawMessage(`true`),
					id(21).String(): json.RawMessage(`"https://example.com/brakes"`), id(22).String(): json.RawMessage(`"` + id(50).String() + `"`),
				}},
			r2: {Statement: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"A tone sounds."}]}]}`), Updated: at},
		},
		defs:     defs,
		types:    map[uuid.UUID]named{typeA: {typeA, "Interface"}, typeB: {typeB, "Requirement"}},
		changed:  map[uuid.UUID]time.Time{typeA: at, typeB: at},
		statuses: []named{{status, "Draft"}, {id(31), "Approved"}},
		links:    []exportLink{{ID: id(60), Type: id(61), Source: r2, Target: r1, Created: at}},
		kinds:    map[uuid.UUID]linkType{id(61): {ID: id(61), Key: "derives", Name: "Derives"}},
		people:   map[string]string{id(50).String(): "ada@example.com"},
		ties: map[string]map[uuid.UUID][]tieRow{
			"object": {r1: {{kind: "object", identifier: "_supplier_SR1", target: r1}}},
			"type":   {typeA: {{kind: "type", identifier: "_supplier_interface", target: typeA}}},
		},
		now: at, exportID: "golden", tool: "Exciter test",
	}
}

func TestTheGoldenExportIsCurrent(t *testing.T) {
	snap := goldenSnapshot()
	files := func(id string) (string, string, bool) { return "files/" + id + ".png", "image/png", true }
	var buf bytes.Buffer
	if err := Write(&buf, snap.build(files)); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("REQIF_GOLDEN_UPDATE") != "" {
		if err := os.WriteFile(goldenExport, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(goldenExport)
	if err != nil || !bytes.Equal(golden, buf.Bytes()) {
		t.Fatalf("%s is not what an export writes now; run make reqif-golden, then make reqif-schema, and commit the result", goldenExport)
	}
}

func TestTheGoldenExportReadsBackAsTheProject(t *testing.T) {
	f, _, _, err := Read(must(os.ReadFile(goldenExport)))
	if err != nil {
		t.Fatal(err)
	}
	ix := indexOf(f)
	if o := ix.objects["_supplier_SR1"]; o == nil || o.Type != "_supplier_interface" {
		t.Fatalf("the tied identifiers were not written back: %+v", o)
	}
	if len(f.Specifications) != 1 || len(f.Specifications[0].Children) != 2 || len(f.Specifications[0].Children[0].Children) != 1 {
		t.Errorf("the hierarchy reads %+v", f.Specifications)
	}
	for _, d := range ix.types["_supplier_interface"].Attributes {
		if d.Name == "Reviewer" && d.DataType != dataUser {
			t.Errorf("a person is written as %s", d.DataType)
		}
	}
	if len(f.Relations) != 1 || f.Relations[0].Source == f.Relations[0].Target {
		t.Errorf("the relations read %+v", f.Relations)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
