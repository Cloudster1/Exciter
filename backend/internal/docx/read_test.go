package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The namespaces a document's body uses, as Word writes them.
const testNamespaces = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml"`

// testStyles are Word's built-in styles a test names, German ids for German Word's headings among them.
const testStyles = `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="berschrift1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Chapter"><w:name w:val="Chapter"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="TOCHeading"><w:name w:val="TOC Heading"/><w:basedOn w:val="Heading1"/><w:pPr><w:outlineLvl w:val="9"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Requirement"><w:name w:val="Requirement"/><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:numId w:val="2"/></w:numPr></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/><w:basedOn w:val="Normal"/></w:style>
</w:styles>`

// testNumbering numbers the headings 1. and 1.1., requirements REQ-1, and lists with bullets and letters.
const testNumbering = `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="10"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1.%2."/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="11"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="REQ-%1"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="12"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="o"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/><w:lvlText w:val="-"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="13"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%1)"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="10"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="11"/></w:num>
<w:num w:numId="3"><w:abstractNumId w:val="12"/></w:num>
<w:num w:numId="4"><w:abstractNumId w:val="13"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="3"/></w:lvlOverride></w:num>
</w:numbering>`

const testRels = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>
<Relationship Id="rId10" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
<Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/norm" TargetMode="External"/>
</Relationships>`

// wordFile is a .docx whose body is body, with the parts above and any of them replaced by parts.
func wordFile(t *testing.T, body string, parts map[string]string) []byte {
	t.Helper()
	files := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="png" ContentType="image/png"/>` +
			`<Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" ` +
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + testNamespaces + `><w:body>` + body + `<w:sectPr/></w:body></w:document>`,
		"word/styles.xml":              testStyles,
		"word/numbering.xml":           testNumbering,
		"word/_rels/document.xml.rels": testRels,
		"word/settings.xml": `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml">` +
			`<w15:docId w15:val="{6A1B2C3D-0000-4000-8000-000000000001}"/></w:settings>`,
		"docProps/core.xml":     `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Brake specification</dc:title></cp:coreProperties>`,
		"word/media/image1.png": "\x89PNG\r\n\x1a\nfake",
	}
	for name, data := range parts {
		if data == "" {
			delete(files, name)
		} else {
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

func p(style, id, runs string) string {
	pPr := ""
	if style != "" {
		pPr = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	attr := ""
	if id != "" {
		attr = ` w14:paraId="` + id + `"`
	}
	return `<w:p` + attr + `>` + pPr + runs + `</w:p>`
}

func r(text string) string { return `<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r>` }

func read(t *testing.T, body string, parts map[string]string) *Source {
	t.Helper()
	src, err := Read(wordFile(t, body, parts))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return src
}

// rich is a block's rich text as JSON, which the tests look into.
func richOf(t *testing.T, b Block) string {
	t.Helper()
	out, err := json.Marshal(b.Rich)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestHeadingsAreReadByStyleAndOutlineLevel(t *testing.T) {
	src := read(t, p("Title", "", r("Brakes"))+
		p("Heading1", "00000001", r("Scope"))+
		p("Heading2", "00000002", r("Pedal"))+
		p("Heading2", "", r("Lever"))+
		p("berschrift1", "", r("Umfang"))+
		p("Chapter", "", r("By its outline"))+
		`<w:p><w:pPr><w:outlineLvl w:val="2"/></w:pPr>`+r("Directly")+`</w:p>`+
		p("TOCHeading", "", r("Contents"))+
		p("Heading1", "", r("Interfaces")), nil)
	if src.Title != "Brakes" || src.DocID != "6A1B2C3D-0000-4000-8000-000000000001" {
		t.Errorf("the title %q and the identifier %q", src.Title, src.DocID)
	}
	var got []string
	for _, b := range src.Blocks {
		got = append(got, string(b.Kind)+" "+itoa(b.Level)+" "+b.Label+" "+b.Text)
	}
	want := []string{"heading 1 1. Scope", "heading 2 1.1. Pedal", "heading 2 1.2. Lever", "heading 1  Umfang", "heading 2  By its outline",
		"heading 3  Directly", "heading 1 2. Interfaces"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if src.Blocks[0].ParaID != "00000001" || src.Blocks[0].StyleName != "heading 1" {
		t.Errorf("the first heading: %+v", src.Blocks[0])
	}
	if len(src.Losses) != 1 || !strings.Contains(src.Losses[0].Message, "table of contents") {
		t.Errorf("the losses: %+v", src.Losses)
	}
}

func TestLabelsAndListsAreToldApart(t *testing.T) {
	bullet := func(level, text string) string {
		return `<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="` + level + `"/><w:numId w:val="3"/></w:numPr></w:pPr>` + r(text) + `</w:p>`
	}
	letter := func(text string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="4"/></w:numPr></w:pPr>` + r(text) + `</w:p>`
	}
	src := read(t, p("Requirement", "", r("The pedal shall stop the car."))+
		p("Requirement", "", r("The lever shall hold it."))+
		p("", "", r("It brakes:"))+
		bullet("0", "dry")+bullet("1", "warm")+bullet("0", "wet")+
		p("", "", r("Then"))+
		letter("first")+letter("second"), nil)
	if len(src.Blocks) != 6 {
		t.Fatalf("%d blocks: %+v", len(src.Blocks), src.Blocks)
	}
	if b := src.Blocks[0]; b.Kind != BlockParagraph || b.Label != "REQ-1" || src.Blocks[1].Label != "REQ-2" {
		t.Errorf("numbered requirements: %+v %+v", b, src.Blocks[1])
	}
	list := src.Blocks[3]
	if list.Kind != BlockList || list.Text != "dry\nwarm\nwet" {
		t.Errorf("the bullets: %+v", list)
	}
	if got := richOf(t, list); !strings.Contains(got, `{"n":"ul","c":[{"n":"li","c":[{"n":"p","c":[{"t":"dry"}]},{"n":"ul","c":[{"n":"li"`) ||
		!strings.Contains(got, `"wet"`) {
		t.Errorf("the nesting: %s", got)
	}
	if got := richOf(t, src.Blocks[5]); !strings.Contains(got, `{"n":"ol","a":{"start":"3"}`) {
		t.Errorf("a list counted from 3: %s", got)
	}
}

func TestTablesKeepMergedCellsAndHeaders(t *testing.T) {
	cell := func(props, text string) string {
		return `<w:tc><w:tcPr>` + props + `</w:tcPr>` + p("", "", r(text)) + `</w:tc>`
	}
	src := read(t, `<w:tbl><w:tblPr><w:tblCaption w:val="Stops"/></w:tblPr>`+
		`<w:tr><w:trPr><w:tblHeader/></w:trPr>`+cell(`<w:gridSpan w:val="2"/>`, "Surface")+cell("", "Distance")+`</w:tr>`+
		`<w:tr>`+cell(`<w:vMerge w:val="restart"/>`, "Road")+cell("", "dry")+cell("", "40 m")+`</w:tr>`+
		`<w:tr>`+cell(`<w:vMerge/>`, "")+cell("", "wet")+cell("", "60 m")+`</w:tr></w:tbl>`, nil)
	if len(src.Blocks) != 1 || src.Tables != 1 {
		t.Fatalf("the blocks: %+v", src.Blocks)
	}
	b := src.Blocks[0]
	if b.Kind != BlockTable || b.Table != 1 || b.HeaderRows != 1 || b.TableTitle != "Stops" || len(b.Rows) != 3 || len(b.Rows[2]) != 2 {
		t.Fatalf("the table: %+v", b)
	}
	if b.Rows[0][0].ColSpan != 2 || b.Rows[1][0].RowSpan != 2 || b.Rows[2][0].Col != 1 || b.Rows[0][1].Col != 2 {
		t.Errorf("the spans: %+v", b.Rows)
	}
	got := richOf(t, b)
	for _, want := range []string{`{"n":"th","a":{"colspan":"2"}`, `{"n":"td","a":{"rowspan":"2"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("the table lacks %s: %s", want, got)
		}
	}
}

const testPicture = `<w:r><w:drawing><wp:inline><wp:extent cx="952500" cy="476250"/><wp:docPr id="1" name="Picture 1" descr="Braking curve"/>` +
	`<a:graphic><a:graphicData><pic:pic><pic:blipFill><a:blip r:embed="rId10"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`

func TestPicturesLinksFieldsAndTrackedChanges(t *testing.T) {
	src := read(t, p("", "", testPicture)+p("Caption", "", r("Figure 1"))+
		p("", "", `<w:hyperlink r:id="rId11">`+r("the norm")+`</w:hyperlink>`+r(" and ")+`<w:hyperlink w:anchor="_Toc1">`+r("below")+`</w:hyperlink>`)+
		p("", "", `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> HYPERLINK "https://example.com/field" </w:instrText></w:r>`+
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`+r("a field")+`<w:r><w:fldChar w:fldCharType="end"/></w:r>`+
			`<w:fldSimple w:instr="HYPERLINK &quot;https://example.com/simple&quot;">`+r("simple")+`</w:fldSimple>`)+
		p("", "", `<w:ins>`+r("kept ")+`</w:ins><w:del><w:r><w:delText>gone</w:delText></w:r></w:del>`+
			`<w:r><w:rPr><w:b/><w:i w:val="0"/><w:u w:val="single"/></w:rPr><w:t>bold</w:t></w:r>`+
			`<w:r><w:rPr><w:strike/></w:rPr><w:t>struck</w:t><w:tab/><w:br/><w:t>next</w:t></w:r>`+
			`<w:r><w:footnoteReference w:id="1"/></w:r><m:oMath><m:r><m:t>x</m:t></m:r></m:oMath>`), nil)
	if len(src.Blocks) != 5 {
		t.Fatalf("%d blocks: %+v", len(src.Blocks), src.Blocks)
	}
	pic := src.Blocks[0]
	if pic.Kind != BlockPicture || !strings.Contains(richOf(t, pic), `{"n":"object","a":{"data":"word/media/image1.png","type":"image/png","width":"100"},"c":[{"t":"Braking curve"}]}`) {
		t.Errorf("the picture: %s", richOf(t, pic))
	}
	if len(src.Media) != 1 || src.Media[0].Path != "word/media/image1.png" || len(src.Media[0].Data) == 0 {
		t.Errorf("the media: %+v", src.Media)
	}
	if !src.Blocks[1].Caption {
		t.Errorf("the caption: %+v", src.Blocks[1])
	}
	links := richOf(t, src.Blocks[2])
	if !strings.Contains(links, `{"n":"a","a":{"href":"https://example.com/norm"},"c":[{"t":"the norm"}]}`) || strings.Count(links, `"n":"a"`) != 1 ||
		src.Blocks[2].Losses[0].Message != lostAnchor {
		t.Errorf("the links: %s %+v", links, src.Blocks[2].Losses)
	}
	fields := richOf(t, src.Blocks[3])
	if !strings.Contains(fields, `"href":"https://example.com/field"},"c":[{"t":"a field"}]`) || !strings.Contains(fields, `"href":"https://example.com/simple"`) ||
		strings.Contains(fields, "HYPERLINK") {
		t.Errorf("the fields: %s", fields)
	}
	tracked := src.Blocks[4]
	if tracked.Text != "kept boldstruck\t\nnext" {
		t.Errorf("the words: %q", tracked.Text)
	}
	got := richOf(t, tracked)
	for _, want := range []string{`{"n":"b","c":[{"t":"bold"}]}`, `{"n":"s","c":[{"t":"struck"}]}`, `{"n":"br"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("the runs lack %s: %s", want, got)
		}
	}
	var lost []string
	for _, l := range tracked.Losses {
		lost = append(lost, l.Message)
	}
	for _, want := range []string{lostDeleted, lostUnderline, lostNotes, lostMath} {
		if !strings.Contains(strings.Join(lost, "\n"), want) {
			t.Errorf("the losses lack %q: %v", want, lost)
		}
	}
}

func TestAFileThatIsNoWordDocumentIsRefused(t *testing.T) {
	for name, data := range map[string][]byte{
		"no zip":         []byte("<html/>"),
		"no document":    wordFile(t, "", map[string]string{"word/document.xml": "", "_rels/.rels": ""}),
		"no content map": wordFile(t, "", map[string]string{"[Content_Types].xml": ""}),
		"not a document": wordFile(t, "", map[string]string{"word/document.xml": "<html/>"}),
	} {
		if _, err := Read(data); !errors.Is(err, ErrNotWord) {
			t.Errorf("%s: got %v, want ErrNotWord", name, err)
		}
	}
}

func TestExcitersOwnExportIsReadBack(t *testing.T) {
	for _, lang := range []string{"en", "de"} {
		data, err := Bytes(context.Background(), aDocument(), lang, Reader{})
		if err != nil {
			t.Fatal(err)
		}
		src, err := Read(data)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if !src.Exported || src.Title != "Release 1" {
			t.Errorf("%s: exported %v, titled %q", lang, src.Exported, src.Title)
		}
		roles := map[Role]int{}
		var headings []string
		for _, b := range src.Blocks {
			roles[b.Role]++
			if b.Kind == BlockHeading {
				headings = append(headings, itoa(b.Level)+" "+b.Key+" "+b.Text)
			}
		}
		if got := strings.Join(headings, ", "); got != "1 BRK-R1 Braking, 2 BRK-R2 Stop within 40 m" {
			t.Errorf("%s: the headings %s", lang, got)
		}
		for _, role := range []Role{RoleCover, RoleRationale, RoleLinks, RoleProperties, RoleNote} {
			if roles[role] == 0 {
				t.Errorf("%s: no block of the %s: %v", lang, role, roles)
			}
		}
		var statement []string
		for _, b := range src.Blocks {
			if b.Role == "" && b.Kind != BlockHeading {
				statement = append(statement, b.Text)
			}
		}
		if got := strings.Join(statement, "|"); !strings.Contains(got, "The vehicle shall stop, see the norm and @Ada") || !strings.Contains(got, "The law says so.") {
			t.Errorf("%s: the text %s", lang, got)
		}
	}
}

// The smallest document Word opens, stored rather than deflated, as the browser tests build it: no numbering, no
// settings and no properties.
func TestTheSmallestDocumentIsRead(t *testing.T) {
	const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	const w = `http://schemas.openxmlformats.org/wordprocessingml/2006/main`
	heading := func(level string) string {
		return `<w:style w:type="paragraph" w:styleId="Heading` + level + `"><w:name w:val="heading ` + level + `"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="` +
			string(rune('0'+level[0]-'1')) + `"/></w:pPr></w:style>`
	}
	files := map[string]string{
		"[Content_Types].xml": xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels": xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" ` +
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" ` +
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"word/styles.xml": xmlHead + `<w:styles xmlns:w="` + w + `"><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
			heading("1") + heading("2") + `</w:styles>`,
		"word/document.xml": xmlHead + `<w:document xmlns:w="` + w + `"><w:body>` +
			`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr>` + r("Braking") + `</w:p><w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr>` + r("Pedal") + `</w:p>` +
			`<w:p>` + r("The brake pedal shall return within 1 s.") + `</w:p><w:p>` + r("This text explains the pedal.") + `</w:p><w:sectPr/></w:body></w:document>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(data))
	}
	_ = zw.Close()
	src, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range src.Blocks {
		got = append(got, string(b.Kind)+" "+itoa(b.Level)+" "+b.Text)
	}
	want := "heading 1 Braking|heading 2 Pedal|paragraph 0 The brake pedal shall return within 1 s.|paragraph 0 This text explains the pedal."
	if strings.Join(got, "|") != want || src.DocID != "" || src.Title != "" {
		t.Errorf("the blocks: %s; id %q, title %q", strings.Join(got, "|"), src.DocID, src.Title)
	}
}
