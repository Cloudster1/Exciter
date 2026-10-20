package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

func TestEveryNodeAndMarkOfTheAllowlistIsWritten(t *testing.T) {
	for name := range document.Allowed.Nodes {
		_, block := blockWriters[name]
		_, inline := inlineWriters[name]
		_, part := parts[name]
		if !block && !inline && !part {
			t.Errorf("the node %s is in the allowlist and the Word export leaves it out", name)
		}
	}
	for name := range document.Allowed.Marks {
		if _, ok := markStyles[name]; !ok {
			t.Errorf("the mark %s is in the allowlist and the Word export ignores it", name)
		}
	}
}

var pictureID = uuid.MustParse("0193a6f0-0000-7000-8000-000000000001")

func aPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// aDocument is a locked baseline with a section and a requirement that uses every block, a picture, a rationale,
// every kind of property and a link.
func aDocument() *specdoc.Document {
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	yes := true
	statement := `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"The vehicle "},{"type":"text","text":"shall","marks":[{"type":"bold"}]},
			{"type":"text","text":" stop, see "},{"type":"text","text":"the norm","marks":[{"type":"link","attrs":{"href":"https://example.com/norm"}}]},
			{"type":"text","text":" and "},{"type":"mention","attrs":{"id":"` + pictureID.String() + `","label":"Ada"}},
			{"type":"hardBreak"},{"type":"attachment","attrs":{"attachmentId":"` + pictureID.String() + `","fileName":"plan.pdf"}}]},
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Conditions"}]},
		{"type":"orderedList","attrs":{"start":3},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"dry road"}]}]}]},
		{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"cold"}]}]}]},
		{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
		{"type":"codeBlock","content":[{"type":"text","text":"stop()"}]},
		{"type":"horizontalRule"},
		{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Surface"}]}]},
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Dry"}]}]}]}]},
		{"type":"image","attrs":{"attachmentId":"` + pictureID.String() + `","alt":"Braking curve"}}]}`
	locked := at
	return &specdoc.Document{
		Kind: specdoc.KindBaseline, Title: "Release 1", ProjectKey: "BRK", ProjectName: "Brakes", ExportedAt: at, ExportedBy: "Ada Lovelace",
		Baseline: &specdoc.BaselineInfo{Name: "Release 1", Description: "For the supplier.", TakenAt: at, TakenBy: "Ada Lovelace", LockedAt: &locked},
		Total:    3,
		Entries: []specdoc.Entry{
			{Key: "BRK-R1", Kind: requirement.KindSection, Outline: "1", Depth: 0, Title: "Braking", Revision: 1,
				Statement: json.RawMessage(`{"type":"doc","content":[]}`), Properties: []specdoc.Property{}, Links: []specdoc.Link{}},
			{Key: "BRK-R2", Kind: requirement.KindRequirement, Outline: "1.1", Depth: 1, Title: "Stop within 40 m", Revision: 3,
				Statement: json.RawMessage(statement), Rationale: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The law says so."}]}]}`),
				Properties: []specdoc.Property{
					{Field: specdoc.FieldPriority, Text: "must"},
					{Name: "Safety relevant", Flag: &yes},
					{Name: "Detail", Document: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Measured on asphalt."}]}]}`)},
				},
				Links: []specdoc.Link{{Phrase: "derives from", Key: "SYS-R4", Title: "Stop safely"}}},
		},
	}
}

// parts reads a .docx back into its parts, and holds word/document.xml to being XML.
func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			dec := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, err := dec.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s is no XML: %v", f.Name, err)
				}
			}
		}
	}
	return out
}

func TestASpecificationIsWrittenAsAWordDocument(t *testing.T) {
	png := aPNG(t)
	read := Reader{Picture: func(_ context.Context, id uuid.UUID) (io.ReadCloser, error) {
		if id != pictureID {
			return nil, nil
		}
		return io.NopCloser(bytes.NewReader(png)), nil
	}}
	data, err := Bytes(context.Background(), aDocument(), "en", read)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, data)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/media/picture1.png", "docProps/core.xml"} {
		if _, ok := files[name]; !ok {
			t.Errorf("the document lacks %s", name)
		}
	}
	body := files["word/document.xml"]
	for _, want := range []string{
		"Release 1", "Baseline of Brakes (BRK)", "Locked on October 15, 2026", "Exported by Ada Lovelace", "Contents",
		`w:anchor="_Entry2"`, `w:name="_Entry2"`, `w:pStyle w:val="Heading2"`, "1.1 Stop within 40 m", "BRK-R2 · revision 3",
		"Rationale", "The law says so.", "Priority", "Must", "Safety relevant", "Yes", "Measured on asphalt.",
		"derives from", "SYS-R4", "@Ada", "plan.pdf", "stop()", "Conditions", `w:pStyle w:val="TextHeading2"`, "<w:drawing>", "Braking curve",
		"This document holds the first 2 of the 3 requirements.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("word/document.xml lacks %q", want)
		}
	}
	if !strings.Contains(files["word/_rels/document.xml.rels"], "https://example.com/norm") {
		t.Errorf("the link's address is not among the relationships")
	}
	if !strings.Contains(files["docProps/core.xml"], "<dc:title>Release 1</dc:title>") {
		t.Errorf("the core properties: %s", files["docProps/core.xml"])
	}
}

func TestADocumentIsWrittenInItsReadersLanguage(t *testing.T) {
	data, err := Bytes(context.Background(), aDocument(), "de", Reader{})
	if err != nil {
		t.Fatal(err)
	}
	body := unzip(t, data)["word/document.xml"]
	for _, want := range []string{"Baseline von Brakes (BRK)", "Gesperrt am 15. Oktober 2026", "Inhalt", "Begründung", "Priorität", "Muss", "Ja", "Ein Bild, das nicht gelesen werden konnte, fehlt hier: Braking curve"} {
		if !strings.Contains(body, want) {
			t.Errorf("the German document lacks %q", want)
		}
	}
}

func TestPicturesPastTheLimitRefuseTheDocument(t *testing.T) {
	read := Reader{Picture: func(context.Context, uuid.UUID) (io.ReadCloser, error) {
		return io.NopCloser(io.LimitReader(zeros{}, MaxBytes+1)), nil
	}}
	if _, err := Bytes(context.Background(), aDocument(), "en", read); !errors.Is(err, ErrTooLarge) {
		t.Errorf("got %v, want ErrTooLarge", err)
	}
}

// A diagram is embedded as the PNG the renderer draws, described by its title, and one that cannot be drawn is its
// source with a note; one drawn twice is embedded once.
func TestADiagramIsItsPictureOrItsSource(t *testing.T) {
	const source = "@startuml\ntitle Brake circuit\n[Pedal] --> [Caliper]\n@enduml"
	withDiagram := func() *specdoc.Document {
		doc := aDocument()
		raw, _ := json.Marshal(source)
		block := `{"type":"diagram","attrs":{"language":"plantuml","source":` + string(raw) + `}}`
		doc.Entries[1].Statement = json.RawMessage(`{"type":"doc","content":[` + block + `,` + block + `]}`)
		return doc
	}
	drawn := 0
	read := Reader{Diagram: func(_ context.Context, language, got string) ([]byte, error) {
		drawn++
		if language != document.DiagramPlantUML || got != source {
			t.Errorf("asked to draw %s %q", language, got)
		}
		return aPNG(t), nil
	}}
	data, err := Bytes(context.Background(), withDiagram(), "en", read)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, data)
	if _, ok := files["word/media/picture1.png"]; !ok || drawn != 1 {
		t.Errorf("the diagram is not embedded once: drawn %d times", drawn)
	}
	if _, ok := files["word/media/picture2.png"]; ok {
		t.Error("one diagram is embedded twice")
	}
	if body := files["word/document.xml"]; !strings.Contains(body, `descr="Brake circuit"`) || strings.Contains(body, "[Pedal]") {
		t.Error("the drawn diagram is not described by its title, or its source is written beside it")
	}
	undrawn := Reader{Diagram: func(context.Context, string, string) ([]byte, error) { return nil, errors.New("renderer down") }}
	for name, r := range map[string]Reader{"no renderer": {}, "a renderer that fails": undrawn} {
		data, err := Bytes(context.Background(), withDiagram(), "en", r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body := unzip(t, data)["word/document.xml"]
		if !strings.Contains(body, "[Pedal] --&gt; [Caliper]") || !strings.Contains(body, "could not be drawn, so it is shown as its PlantUML source") {
			t.Errorf("%s: the diagram is not written as its source with a note", name)
		}
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestFileNamesSayWhatAndWhen(t *testing.T) {
	doc := aDocument()
	if got := FileName(doc, "docx"); got != "BRK-release-1-2026-10-15.docx" {
		t.Errorf("a baseline is named %s", got)
	}
	doc.Kind, doc.SectionKey = specdoc.KindSection, "BRK-R7"
	if got := FileName(doc, "pdf"); got != "BRK-R7-2026-10-15.pdf" {
		t.Errorf("a section is named %s", got)
	}
}

func TestWordsLinkedToATermAreItsWords(t *testing.T) {
	const id = "0195f000-0000-7000-8000-0000000000a1"
	term := document.Mark{Type: document.MarkTerm, Attrs: map[string]any{"termId": id}}
	nodes := []document.Node{
		{Type: "text", Text: "Press "},
		{Type: "text", Text: "the brake ", Marks: []document.Mark{term}},
		{Type: "text", Text: "pedal", Marks: []document.Mark{{Type: "bold"}, term}},
		{Type: "text", Text: "."},
	}
	plain := newWriter(context.Background(), "en", Reader{})
	plain.inline(nodes, frame{})
	if got := plain.b.String(); strings.Contains(got, "w:hyperlink") || !strings.Contains(got, "the brake ") {
		t.Errorf("without a glossary chapter a term is not plain words: %s", got)
	}
	linked := newWriter(context.Background(), "en", Reader{})
	linked.termAnchor = func(termID string) (string, bool) { return "term_" + termID[len(termID)-2:], termID == id }
	linked.inline(nodes, frame{})
	got := linked.b.String()
	if strings.Count(got, `<w:hyperlink w:anchor="term_a1"`) != 1 || !strings.Contains(got, "pedal</w:t></w:r></w:hyperlink>") {
		t.Errorf("a term's words are not one link to its entry: %s", got)
	}
}

func TestTheGlossaryIsAChapterTheTermsLinkTo(t *testing.T) {
	const id = "0195f000-0000-7000-8000-0000000000b2"
	doc := aDocument()
	doc.Entries[1].Statement = json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[` +
		`{"type":"text","text":"Press the "},{"type":"text","text":"brake pedal","marks":[{"type":"term","attrs":{"termId":"` + id + `"}}]},` +
		`{"type":"text","text":" or the "},{"type":"text","text":"horn","marks":[{"type":"term","attrs":{"termId":"0195f000-0000-7000-8000-0000000000c3"}}]}]}]}`)
	doc.Glossary = []specdoc.GlossaryTerm{{ID: uuid.MustParse(id), Name: "Brake pedal",
		Definition: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The pedal that slows the car."}]}]}`),
		Synonyms:   []string{"pedal", "brake"}, Avoid: []string{"stopper"}, Status: "approved"}}
	for lang, words := range map[string][]string{
		"en": {">Glossary<", "Synonyms: pedal, brake", "Not to use: stopper"},
		"de": {">Glossar<", "Synonyme: pedal, brake", "Nicht verwenden: stopper"},
	} {
		data, err := Bytes(context.Background(), doc, lang, Reader{})
		if err != nil {
			t.Fatal(err)
		}
		files := unzip(t, data)
		body := files["word/document.xml"]
		bookmark := "term_" + strings.ReplaceAll(id, "-", "")
		want := append([]string{
			`w:anchor="_Glossary"`, `w:name="_Glossary"`, `w:name="` + bookmark + `"`,
			`<w:hyperlink w:anchor="` + bookmark + `"`, "brake pedal", "Brake pedal",
			"The pedal that slows the car.", `w:pStyle w:val="TermWords"`,
		}, words...)
		for _, s := range want {
			if !strings.Contains(body, s) {
				t.Errorf("%s: word/document.xml lacks %q", lang, s)
			}
		}
		if strings.Count(body, "w:hyperlink w:anchor=\"term_") != 1 {
			t.Errorf("%s: a term out of the glossary links to it", lang)
		}
		if strings.Index(body, "The pedal that slows the car.") < strings.Index(body, "1.1 Stop within 40 m") {
			t.Errorf("%s: the glossary comes before the entries", lang)
		}
		if !strings.Contains(files["word/styles.xml"], `w:styleId="TermWords"`) {
			t.Errorf("%s: the muted style of a term's words is not defined", lang)
		}
	}
	doc.Glossary = nil
	data, err := Bytes(context.Background(), doc, "en", Reader{})
	if err != nil {
		t.Fatal(err)
	}
	if body := unzip(t, data)["word/document.xml"]; strings.Contains(body, "_Glossary") || strings.Contains(body, `w:anchor="term_`) {
		t.Errorf("a document without terms has a glossary")
	}
}
