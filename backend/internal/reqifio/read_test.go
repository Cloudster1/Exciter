package reqifio

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngBytes is the smallest PNG there is, a single transparent pixel.
var pngBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func supplier(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "supplier.reqif"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// archive zips files given as name and bytes, in order.
func archive(t *testing.T, files ...any) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i+1 < len(files); i += 2 {
		w, err := zw.Create(files[i].(string))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[i+1].([]byte)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadTakesEveryDatatypeTheHierarchyAndRelations(t *testing.T) {
	f, format, embedded, err := Read(supplier(t))
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatReqIF || len(embedded) != 0 {
		t.Fatalf("a plain document is %s with %d files", format, len(embedded))
	}
	if f.Title != "Brake controller" || f.SourceTool != "Supplier Tool 7.2" || f.Created.IsZero() {
		t.Errorf("the header reads %q from %q at %v", f.Title, f.SourceTool, f.Created)
	}
	kinds := map[DataKind]int{}
	for _, d := range f.DataTypes {
		kinds[d.Kind]++
	}
	for kind, want := range map[DataKind]int{KindString: 1, KindXHTML: 1, KindInteger: 1, KindReal: 1, KindBoolean: 1, KindDate: 1, KindEnum: 2} {
		if kinds[kind] != want {
			t.Errorf("%d datatypes of kind %s, want %d", kinds[kind], kind, want)
		}
	}
	ix := indexOf(f)
	if risk := ix.dataTypes["_dt_risk"]; len(risk.Values) != 2 || risk.Values[1].Name != "Low" || risk.Values[1].Key != 1 {
		t.Errorf("the risk levels read %+v", risk.Values)
	}
	if d := ix.defs["_a_platforms"]; d == nil || !d.Multi || d.Kind != KindEnum || d.DataType != "_dt_platforms" {
		t.Errorf("the platforms definition reads %+v", d)
	}
	if len(f.Objects) != 3 {
		t.Fatalf("%d objects, want 3; the tool extension's is not content", len(f.Objects))
	}
	r1 := ix.objects["_o_r1"]
	if r1.Type != "_t_req" || len(r1.Values) != 9 {
		t.Fatalf("_o_r1 is of %q with %d values", r1.Type, len(r1.Values))
	}
	if v := r1.value("_a_platforms"); v == nil || strings.Join(v.Enum, ",") != "_pf_a,_pf_b" {
		t.Errorf("the platforms value reads %+v", v)
	}
	if v := r1.value("_a_weight"); v == nil || v.Text != "12.5" {
		t.Errorf("the weight reads %+v", v)
	}
	if v := r1.value("_a_text"); v == nil || v.XHTML == nil || !strings.Contains(Words(v.XHTML), "shall") {
		t.Errorf("the text reads %+v", v)
	}
	if len(f.Relations) != 1 || f.Relations[0].Source != "_o_r2" || f.Relations[0].Target != "_o_r1" || f.Relations[0].Type != "_rt_satisfies" {
		t.Errorf("the relations read %+v", f.Relations)
	}
	if len(f.Specifications) != 1 {
		t.Fatalf("%d specifications", len(f.Specifications))
	}
	spec := f.Specifications[0]
	if spec.Name != "Brake controller SRS" || len(spec.Children) != 1 || spec.Children[0].Object != "_o_h1" || len(spec.Children[0].Children) != 2 {
		t.Errorf("the hierarchy reads %+v", spec)
	}
}

func TestReadTakesAnArchiveWithTheFilesItsTextShows(t *testing.T) {
	doc := supplier(t)
	data := archive(t, "export/brakes.reqif", doc, "export/files/curve.png", pngBytes, "export/files/curve.ole", []byte("an OLE object"),
		"export/files/unused.png", pngBytes)
	f, format, embedded, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatReqIFZ {
		t.Errorf("an archive reads as %s", format)
	}
	if len(embedded) != 1 || embedded[0].Path != "files/curve.png" || embedded[0].ContentType != "image/png" || !bytes.Equal(embedded[0].Data, pngBytes) {
		t.Fatalf("the archive gave %+v; the picture the OLE object falls back to, and nothing unused", embedded)
	}
	if len(f.Images) != 1 || f.Images[0].Size != len(pngBytes) {
		t.Errorf("the file lists %+v", f.Images)
	}
}

func TestReadRefusesWhatIsNoReqIFInASentence(t *testing.T) {
	doc := string(supplier(t))
	many := strings.Replace(doc, "<SPEC-OBJECTS>", "<SPEC-OBJECTS>"+strings.Repeat(`<SPEC-OBJECT IDENTIFIER="x" LAST-CHANGE="2026-09-02T10:00:00Z"/>`, MaxObjects), 1)
	for name, data := range map[string][]byte{
		"not XML":            []byte("Key,Title\nA-1,Brakes\n"),
		"empty":              {},
		"another root":       []byte(`<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"/>`),
		"another namespace":  []byte(`<?xml version="1.0"?><REQ-IF xmlns="urn:something:else"/>`),
		"another charset":    []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><REQ-IF xmlns="` + Namespace + `"/>`),
		"cut short":          []byte(doc[:len(doc)/2]),
		"too many objects":   []byte(many),
		"no document inside": archive(t, "files/curve.png", pngBytes),
		"two documents":      archive(t, "a.reqif", []byte(doc), "b.reqif", []byte(doc)),
		"a damaged archive":  append([]byte("PK\x03\x04"), []byte("rest")...),
	} {
		_, _, _, err := Read(data)
		var fileErr *FileError
		if !errors.As(err, &fileErr) {
			t.Errorf("%s: %v, want a FileError", name, err)
			continue
		}
		if !strings.HasSuffix(fileErr.Message, ".") || fileErr.Message[0] < 'A' || fileErr.Message[0] > 'Z' {
			t.Errorf("%s: %q is no sentence", name, fileErr.Message)
		}
	}
}

func TestReadRefusesAHierarchyDeeperThanATree(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= MaxDepth; i++ {
		fmt.Fprintf(&b, `<CHILDREN><SPEC-HIERARCHY IDENTIFIER="h%d" LAST-CHANGE="2026-09-02T10:00:00Z"><OBJECT><SPEC-OBJECT-REF>_o_r1</SPEC-OBJECT-REF></OBJECT>`, i)
	}
	for i := 0; i <= MaxDepth; i++ {
		b.WriteString(`</SPEC-HIERARCHY></CHILDREN>`)
	}
	deep := `<?xml version="1.0"?><REQ-IF xmlns="` + Namespace + `"><CORE-CONTENT><REQ-IF-CONTENT><SPECIFICATIONS>` +
		`<SPECIFICATION IDENTIFIER="s" LAST-CHANGE="2026-09-02T10:00:00Z">` + b.String() + `</SPECIFICATION>` +
		`</SPECIFICATIONS></REQ-IF-CONTENT></CORE-CONTENT></REQ-IF>`
	var fileErr *FileError
	if _, _, _, err := Read([]byte(deep)); !errors.As(err, &fileErr) || !strings.Contains(fileErr.Message, "levels deep") {
		t.Errorf("a hierarchy %d deep: %v", MaxDepth+1, err)
	}
}

func TestReadKeepsTheWordsOfRichTextNestedPastTheLimit(t *testing.T) {
	inner := strings.Repeat("<xhtml:span>", MaxXHTMLDepth+5) + "deep words" + strings.Repeat("</xhtml:span>", MaxXHTMLDepth+5)
	doc := strings.Replace(string(supplier(t)), "A warning sounds", inner, 1)
	f, _, _, err := Read([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	v := indexOf(f).objects["_o_r2"].value("_a_text")
	if !strings.Contains(Words(v.XHTML), "deep words") {
		t.Errorf("the deep text was lost: %q", Words(v.XHTML))
	}
}
