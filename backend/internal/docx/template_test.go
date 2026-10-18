package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

const templateTypes = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="png" ContentType="image/png"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml"/>` +
	`<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>%s</Types>`

// aTemplate is a German template: its heading style named "heading 1" under the id Word gives it in German, a cover
// line with placeholders split across runs as Word splits typing, {{content}}, a closing line and a header.
func aTemplate(t *testing.T, body string, extra map[string]string) []byte {
	t.Helper()
	if body == "" {
		body = `<w:p><w:r><w:t>Spezifikation {{pro</w:t></w:r><w:r><w:b/><w:t>ject}} vom {{date}}</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>{{content}}</w:t></w:r></w:p><w:p><w:r><w:t>Ende</w:t></w:r></w:p>`
	}
	files := map[string]string{
		"[Content_Types].xml": strings.Replace(templateTypes, "%s", extra["types"], 1),
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `"><w:body>` + body +
			`<w:sectPr><w:headerReference w:type="default" r:id="rId7"/><w:pgSz w:w="12240" w:h="15840"/>` +
			`<w:pgMar w:top="1000" w:right="1000" w:bottom="1000" w:left="1240"/></w:sectPr></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
			`<Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"word/header1.xml": `<?xml version="1.0"?><w:hdr xmlns:w="` + nsW + `"><w:p><w:r><w:t>{{title}}</w:t></w:r></w:p></w:hdr>`,
		"word/styles.xml": `<?xml version="1.0"?><w:styles xmlns:w="` + nsW + `"><w:style w:type="paragraph" w:styleId="berschrift1">` +
			`<w:name w:val="heading 1"/><w:rPr><w:color w:val="C00000"/></w:rPr></w:style></w:styles>`,
		"word/numbering.xml": `<?xml version="1.0"?><w:numbering xmlns:w="` + nsW + `"><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"/></w:abstractNum>` +
			`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`,
	}
	for name, data := range extra {
		if name != "types" {
			files[name] = data
		}
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestATemplateFramesTheExportWithItsStylesHeaderAndPlaceholders(t *testing.T) {
	tpl, err := ReadTemplate(aTemplate(t, "", nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tpl.Placeholders, ",") != "content,project,date,title" {
		t.Errorf("the placeholders read %v", tpl.Placeholders)
	}
	if tpl.width != 12240-1240-1000 {
		t.Errorf("the text is %d wide", tpl.width)
	}
	data, err := BytesWith(context.Background(), aDocument(), "de", Reader{}, tpl, Fill{"project": "CHK & Co", "date": "2026-10-18", "title": "Bremsen"})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, data)
	doc := files["word/document.xml"]
	for name, part := range files {
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			if err := xml.Unmarshal([]byte(part), new(struct{})); err != nil {
				t.Errorf("%s is not well formed: %v", name, err)
			}
		}
	}
	for _, want := range []string{"Spezifikation CHK &amp; Co vom 2026-10-18", "Ende", `w:pStyle w:val="berschrift1"`, `r:id="rTplrId7"`, `w:w="12240"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document lacks %q", want)
		}
	}
	if strings.Contains(doc, "{{") || strings.Contains(doc, `w:pStyle w:val="Heading1"`) || strings.Contains(doc, `w:val="Title"`) {
		t.Error("the document keeps a placeholder, the export's own heading style, or its cover")
	}
	if !strings.Contains(files["word/header1.xml"], "Bremsen") {
		t.Errorf("the header reads %s", files["word/header1.xml"])
	}
	if styles := files["word/styles.xml"]; !strings.Contains(styles, `w:styleId="berschrift1"`) || !strings.Contains(styles, `w:styleId="EntryMeta"`) ||
		strings.Contains(styles, `w:styleId="Heading1"`) {
		t.Errorf("the styles are not merged by name: %s", styles[:min(len(styles), 400)])
	}
	if numbering := files["word/numbering.xml"]; !strings.Contains(numbering, `w:abstractNumId="1"`) || strings.Count(numbering, `w:numId="1"`) != 1 {
		t.Errorf("the numbering is not merged after the template's")
	}
	if !strings.Contains(files["word/_rels/document.xml.rels"], `Id="rTplrId7"`) || strings.Contains(files["word/_rels/document.xml.rels"], `Target="styles.xml" Id="rTpl`) {
		t.Errorf("the relationships read %s", files["word/_rels/document.xml.rels"])
	}
	types := files["[Content_Types].xml"]
	if !strings.Contains(types, `/word/header1.xml`) || strings.Contains(types, "template.main") {
		t.Errorf("the content types read %s", types)
	}
}

func TestATemplateThatCannotFrameAnExportIsRefusedWithWhatIsWrong(t *testing.T) {
	for name, tc := range map[string]struct {
		body  string
		extra map[string]string
		want  string
	}{
		"no content":        {body: `<w:p><w:r><w:t>Nothing</w:t></w:r></w:p>`, want: "Put {{content}} on a paragraph of its own where"},
		"content twice":     {body: `<w:p><w:r><w:t>{{content}}</w:t></w:r></w:p><w:p><w:r><w:t>{{content}}</w:t></w:r></w:p>`, want: "in one paragraph only"},
		"content in a line": {body: `<w:p><w:r><w:t>See {{content}}</w:t></w:r></w:p>`, want: "no other words beside it"},
		"content in a table": {body: `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>{{content}}</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
			want: "out of the table"},
		"an unknown placeholder": {body: `<w:p><w:r><w:t>{{projekt}}</w:t></w:r></w:p><w:p><w:r><w:t>{{content}}</w:t></w:r></w:p>`,
			want: "{{projekt}} is not a placeholder"},
		"macros": {extra: map[string]string{"word/vbaProject.bin": "x"}, want: "holds macros"},
	} {
		_, err := ReadTemplate(aTemplate(t, tc.body, tc.extra))
		var refusal *TemplateError
		if !errors.As(err, &refusal) || !strings.Contains(refusal.Message, tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if _, err := ReadTemplate([]byte("not a zip")); !errors.Is(err, ErrNotWord) {
		t.Errorf("a file that is no zip: %v", err)
	}
}
