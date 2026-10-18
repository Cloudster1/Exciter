package reqifio

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// xhtmlOf reads XHTML as THE-VALUE of a rich text value holds it.
func xhtmlOf(t *testing.T, s string) *XNode {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(`<THE-VALUE xmlns:xhtml="` + XHTMLNamespace + `">` + s + `</THE-VALUE>`))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	children, err := readXHTML(dec, 0)
	if err != nil {
		t.Fatal(err)
	}
	return &XNode{Children: children}
}

// compact is a document as one line, so two compare as text.
func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

const placed = "0193a6f0-0000-7000-8000-000000000001"

func TestToDocumentReadsTheXHTMLToolsWrite(t *testing.T) {
	place := func(ref string) (Placed, bool) {
		switch ref {
		case "files/pic.png":
			return Placed{ID: placed, Image: true}, true
		case "files/spec.pdf":
			return Placed{ID: placed, FileName: "spec.pdf"}, true
		}
		return Placed{}, false
	}
	for name, c := range map[string]struct{ in, want string }{
		"bare text in a div": {`<xhtml:div>Stop  within
			40 m.</xhtml:div>`, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Stop within 40 m."}]}]}`},
		"paragraphs and marks": {`<xhtml:div><xhtml:p>The <xhtml:b>vehicle</xhtml:b> <xhtml:em>shall</xhtml:em> stop.</xhtml:p><xhtml:p/></xhtml:div>`,
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The "},{"type":"text","text":"vehicle","marks":[{"type":"bold"}]},{"type":"text","text":" "},{"type":"text","text":"shall","marks":[{"type":"italic"}]},{"type":"text","text":" stop."}]},{"type":"paragraph"}]}`},
		"a line break": {`<xhtml:p>one<xhtml:br/>two</xhtml:p>`,
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"one"},{"type":"hardBreak"},{"type":"text","text":"two"}]}]}`},
		"headings past level three": {`<xhtml:h1>A</xhtml:h1><xhtml:h5>B</xhtml:h5>`,
			`{"type":"doc","content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"A"}]},{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"B"}]}]}`},
		"lists": {`<xhtml:ol start="3"><xhtml:li>first</xhtml:li><xhtml:li><xhtml:p>second</xhtml:p><xhtml:ul><xhtml:li>inner</xhtml:li></xhtml:ul></xhtml:li></xhtml:ol>`,
			`{"type":"doc","content":[{"type":"orderedList","attrs":{"start":3},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"inner"}]}]}]}]}]}]}`},
		"a table with a header and a span": {`<xhtml:table><xhtml:thead><xhtml:tr><xhtml:th colspan="2">Head</xhtml:th></xhtml:tr></xhtml:thead><xhtml:tbody><xhtml:tr><xhtml:td>a</xhtml:td><xhtml:td/></xhtml:tr></xhtml:tbody></xhtml:table>`,
			`{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":2},"content":[{"type":"paragraph","content":[{"type":"text","text":"Head"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},{"type":"tableCell","content":[{"type":"paragraph"}]}]}]}]}`},
		"code": {"<xhtml:pre>\nx := 1\ny := 2</xhtml:pre><xhtml:p>use <xhtml:code><xhtml:b>go</xhtml:b></xhtml:code></xhtml:p>",
			`{"type":"doc","content":[{"type":"codeBlock","content":[{"type":"text","text":"x := 1\ny := 2"}]},{"type":"paragraph","content":[{"type":"text","text":"use "},{"type":"text","text":"go","marks":[{"type":"code"}]}]}]}`},
		"a safe link and an unsafe one": {`<xhtml:p><xhtml:a href="https://example.com/a">here</xhtml:a> <xhtml:a href="javascript:alert(1)">there</xhtml:a></xhtml:p>`,
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"here","marks":[{"type":"link","attrs":{"href":"https://example.com/a"}}]},{"type":"text","text":" there"}]}]}`},
		"an image within a paragraph": {`<xhtml:p>See <xhtml:object data="files/pic.png" type="image/png">The curve</xhtml:object> below.</xhtml:p>`,
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"See"}]},{"type":"image","attrs":{"alt":"The curve","attachmentId":"` + placed + `"}},{"type":"paragraph","content":[{"type":"text","text":"below."}]}]}`},
		"a file that is no image": {`<xhtml:p>As in <xhtml:object data="files/spec.pdf" type="application/pdf"/>.</xhtml:p>`,
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"As in "},{"type":"attachment","attrs":{"attachmentId":"` + placed + `","fileName":"spec.pdf"}},{"type":"text","text":"."}]}]}`},
		"nothing": {`<xhtml:div>  </xhtml:div>`, string(document.Empty)},
	} {
		got, _ := ToDocument(xhtmlOf(t, c.in), place)
		if compact(t, got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", name, compact(t, got), c.want)
		}
		if _, err := document.Parse(got); err != nil {
			t.Errorf("%s: the document is refused: %v", name, err)
		}
	}
}

func TestToDocumentSaysWhatItLeftBehind(t *testing.T) {
	in := `<xhtml:div><xhtml:p>H<xhtml:sub>2</xhtml:sub>O <xhtml:u>now</xhtml:u> <xhtml:u>again</xhtml:u></xhtml:p>` +
		`<xhtml:object data="files/missing.png" type="image/png">gone</xhtml:object><xhtml:marquee>moving</xhtml:marquee></xhtml:div>`
	got, losses := ToDocument(xhtmlOf(t, in), nil)
	if !strings.Contains(string(got), "H2O now again") || !strings.Contains(string(got), "gone") || !strings.Contains(string(got), "moving") {
		t.Errorf("the words were not kept: %s", got)
	}
	kinds := map[LossKind]int{}
	for _, l := range losses {
		kinds[l.kind]++
		if !strings.HasSuffix(l.message, ".") {
			t.Errorf("%q is no sentence", l.message)
		}
	}
	if kinds[LossFormatting] != 3 || kinds[LossImage] != 1 {
		t.Errorf("losses %+v: underlining once, subscript once, the unknown element once, and the image", losses)
	}
}

func TestFromDocumentWritesWhatToDocumentReadsBack(t *testing.T) {
	files := func(id string) (string, string, bool) { return "files/" + id + ".png", "image/png", id == placed }
	place := func(ref string) (Placed, bool) { return Placed{ID: placed, Image: true}, ref == "files/"+placed+".png" }
	for name, doc := range map[string]string{
		"empty":           string(document.Empty),
		"marks and links": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a "},{"type":"text","text":"b","marks":[{"type":"bold"},{"type":"italic"}]},{"type":"text","text":" c","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":" d","marks":[{"type":"strike"}]}]}]}`,
		"blocks":          `{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Head"}]},{"type":"paragraph"},{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"q"}]}]},{"type":"horizontalRule"},{"type":"codeBlock","content":[{"type":"text","text":"a\n  b"}]}]}`,
		"lists and a table": `{"type":"doc","content":[{"type":"orderedList","attrs":{"start":2},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]},` +
			`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":2},"content":[{"type":"paragraph","content":[{"type":"text","text":"h"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},{"type":"tableCell","content":[{"type":"paragraph"}]}]}]}]}`,
		"an image": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see"}]},{"type":"image","attrs":{"alt":"curve","attachmentId":"` + placed + `"}}]}`,
	} {
		x := FromDocument(json.RawMessage(doc), files)
		var buf bytes.Buffer
		w := &writer{w: bufioWriter(&buf)}
		w.xhtml(*x)
		if err := w.w.Flush(); err != nil {
			t.Fatal(err)
		}
		back, losses := ToDocument(xhtmlOf(t, buf.String()), place)
		if compact(t, back) != compact(t, json.RawMessage(doc)) || len(losses) > 0 {
			t.Errorf("%s:\n  wrote %s\n   read %s\n   want %s\n losses %+v", name, buf.String(), compact(t, back), compact(t, json.RawMessage(doc)), losses)
		}
	}
}

func TestFromDocumentLeavesOutFilesTheExportDoesNotCarry(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"@"},{"type":"mention","attrs":{"id":"` + placed + `","label":"Ada"}},{"type":"attachment","attrs":{"attachmentId":"` + placed + `","fileName":"plan.pdf"}}]},{"type":"image","attrs":{"attachmentId":"` + placed + `"}}]}`
	x := FromDocument(json.RawMessage(doc), nil)
	if words := Words(x); !strings.Contains(words, "@Ada") || !strings.Contains(words, "plan.pdf") {
		t.Errorf("the words are %q", words)
	}
	x.walk(func(n *XNode) bool {
		if n.Name == "object" {
			t.Errorf("an object was written for a file the export does not carry")
		}
		return true
	})
}

func TestFromDocumentWritesATermsWords(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Press the "},{"type":"text","text":"brake pedal","marks":[{"type":"bold"},{"type":"term","attrs":{"termId":"` + placed + `"}}]}]}]}`
	var buf bytes.Buffer
	w := &writer{w: bufioWriter(&buf)}
	w.xhtml(*FromDocument(json.RawMessage(doc), nil))
	if err := w.w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "Press the <xhtml:strong>brake pedal</xhtml:strong>") || strings.Contains(got, placed) {
		t.Errorf("wrote %s", got)
	}
}
