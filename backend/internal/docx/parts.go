package docx

import (
	"encoding/xml"
	"slices"
	"strings"
	"time"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsWP  = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	nsA   = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsPic = "http://schemas.openxmlformats.org/drawingml/2006/picture"

	relOffice    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relCore      = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	relApp       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"
	relStyles    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relNumbering = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	relSettings  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings"
	relHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	relImage     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
)

// The colours a document draws with: the light scheme of the built-in theme, since a document is read on white
// whatever its reader chose.
const (
	ruleColor   = "CBD5E1"
	neutralFill = "F1F5F9"
	accentText  = "1D4ED8"
	quoteBorder = "CBD5E1"
	mutedText   = "64748B"
	inkText     = "0F172A"
)

const packageRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + relOffice + `" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="` + relCore + `" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="` + relApp + `" Target="docProps/app.xml"/>` +
	`</Relationships>`

const appProperties = xml.Header + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Exciter</Application></Properties>`

// settings asks Word to read the document as its current version does,
// rather than as a document of an older one.
const settings = xml.Header + `<w:settings xmlns:w="` + nsW + `"><w:defaultTabStop w:val="720"/><w:characterSpacingControl w:val="doNotCompress"/>` +
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`

func contentTypes(pictures []media) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	var exts []string
	for _, m := range pictures {
		if !slices.Contains(exts, m.ext) {
			exts = append(exts, m.ext)
		}
	}
	for _, ext := range exts {
		b.WriteString(`<Default Extension="` + ext + `" ContentType="image/` + ext + `"/>`)
	}
	for _, o := range [][2]string{
		{"/word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"},
		{"/word/styles.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"},
		{"/word/numbering.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"},
		{"/word/settings.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"},
		{"/docProps/core.xml", "application/vnd.openxmlformats-package.core-properties+xml"},
		{"/docProps/app.xml", "application/vnd.openxmlformats-officedocument.extended-properties+xml"},
	} {
		b.WriteString(`<Override PartName="` + o[0] + `" ContentType="` + o[1] + `"/>`)
	}
	b.WriteString(`</Types>`)
	return []byte(b.String())
}

// coreProperties are the document's title, author, date and language, which Word shows in its file information.
func coreProperties(doc *specdoc.Document, lang string) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`)
	b.WriteString(`<dc:title>` + escape(doc.Title) + `</dc:title>`)
	b.WriteString(`<dc:subject>` + escape(doc.ProjectName) + `</dc:subject>`)
	if doc.ExportedBy != "" {
		b.WriteString(`<dc:creator>` + escape(doc.ExportedBy) + `</dc:creator>`)
	}
	b.WriteString(`<dc:language>` + language(lang) + `</dc:language>`)
	at := doc.ExportedAt.UTC().Format(time.RFC3339)
	b.WriteString(`<dcterms:created xsi:type="dcterms:W3CDTF">` + at + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + at + `</dcterms:modified>`)
	b.WriteString(`</cp:coreProperties>`)
	return []byte(b.String())
}

// Fonts: what Word and the office suites ship, with fallbacks they map.
const (
	bodyFont = "Calibri"
	codeFont = "Consolas"
)

// Heading sizes in half points, from the entries at the top down to the deepest style.
var headingSizes = []string{"32", "28", "26", "24", "22", "22"}

// styles is word/styles.xml. The entries' headings are Word's own, by their built-in names, so its navigation pane
// and its tables of contents find them; headings inside a requirement's text are styles of their own.
func styles(lang string) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<w:styles xmlns:w="` + nsW + `">`)
	b.WriteString(`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="` + bodyFont + `" w:hAnsi="` + bodyFont + `" w:eastAsia="` + bodyFont + `" w:cs="` + bodyFont + `"/>`)
	b.WriteString(`<w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="` + language(lang) + `"/></w:rPr></w:rPrDefault>`)
	b.WriteString(`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`)
	para := func(id, name, pPr, rPr string, extra string) {
		b.WriteString(`<w:style w:type="paragraph" w:styleId="` + id + `"><w:name w:val="` + name + `"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/>`)
		b.WriteString(extra)
		if pPr != "" {
			b.WriteString(`<w:pPr>` + pPr + `</w:pPr>`)
		}
		if rPr != "" {
			b.WriteString(`<w:rPr>` + rPr + `</w:rPr>`)
		}
		b.WriteString(`</w:style>`)
	}
	char := func(id, name, rPr string) {
		b.WriteString(`<w:style w:type="character" w:styleId="` + id + `"><w:name w:val="` + name + `"/><w:rPr>` + rPr + `</w:rPr></w:style>`)
	}
	mono := `<w:rFonts w:ascii="` + codeFont + `" w:hAnsi="` + codeFont + `" w:cs="` + codeFont + `"/>`
	muted := `<w:color w:val="` + mutedText + `"/>`
	b.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	para("Title", "Title", `<w:spacing w:before="2400" w:after="160"/>`, `<w:b/><w:color w:val="`+inkText+`"/><w:sz w:val="56"/><w:szCs w:val="56"/>`, `<w:uiPriority w:val="10"/><w:qFormat/>`)
	para("Subtitle", "Subtitle", `<w:spacing w:after="480"/>`, muted+`<w:sz w:val="28"/><w:szCs w:val="28"/>`, `<w:uiPriority w:val="11"/><w:qFormat/>`)
	para("CoverLine", "Cover Line", `<w:spacing w:after="80"/>`, muted, `<w:qFormat/>`)
	for level, size := range headingSizes {
		n := itoa(level + 1)
		para("Heading"+n, "heading "+n,
			`<w:keepNext/><w:keepLines/><w:spacing w:before="`+itoa(max(360-level*40, 200))+`" w:after="40"/><w:outlineLvl w:val="`+itoa(level)+`"/>`,
			`<w:b/><w:bCs/><w:color w:val="`+inkText+`"/><w:sz w:val="`+size+`"/><w:szCs w:val="`+size+`"/>`,
			`<w:uiPriority w:val="9"/><w:qFormat/>`)
	}
	for level := 1; level <= document.MaxHeadingLevel; level++ {
		para("TextHeading"+itoa(level), "Text Heading "+itoa(level), `<w:keepNext/><w:spacing w:before="160" w:after="60"/>`, `<w:b/><w:bCs/>`, `<w:qFormat/>`)
	}
	para("EntryMeta", "Entry Meta", `<w:keepNext/><w:spacing w:after="120"/>`, muted+`<w:sz w:val="18"/><w:szCs w:val="18"/>`, `<w:qFormat/>`)
	para("Label", "Label", `<w:spacing w:before="120" w:after="40"/>`, `<w:b/><w:bCs/>`+muted+`<w:sz w:val="18"/><w:szCs w:val="18"/>`, `<w:qFormat/>`)
	para("Code", "Code",
		`<w:pBdr><w:top w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/><w:left w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/><w:bottom w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/><w:right w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/></w:pBdr><w:shd w:val="clear" w:color="auto" w:fill="F8FAFC"/><w:spacing w:before="120" w:after="200" w:line="240" w:lineRule="auto"/><w:ind w:left="113" w:right="113"/>`,
		mono+`<w:noProof/><w:sz w:val="19"/><w:szCs w:val="19"/>`, `<w:qFormat/>`)
	para("Note", "Note", "", `<w:i/><w:iCs/>`+muted, `<w:qFormat/>`)
	para("TermName", "Term Name", `<w:keepNext/><w:spacing w:before="200" w:after="40"/>`, `<w:b/><w:bCs/>`, `<w:qFormat/>`)
	para("TermWords", "Term Words", `<w:spacing w:after="80"/>`, muted+`<w:sz w:val="18"/><w:szCs w:val="18"/>`, `<w:qFormat/>`)
	para("TOCHeading", "TOC Heading", `<w:keepNext/><w:spacing w:before="240" w:after="120"/>`, `<w:b/><w:color w:val="`+inkText+`"/><w:sz w:val="28"/><w:szCs w:val="28"/>`, `<w:uiPriority w:val="39"/>`)
	para("Spacer", "Spacer", `<w:spacing w:after="0" w:line="160" w:lineRule="exact"/>`, `<w:sz w:val="8"/><w:szCs w:val="8"/>`, `<w:semiHidden/>`)
	for level := 1; level <= contentsDepth; level++ {
		n := itoa(level)
		para("TOC"+n, "toc "+n, `<w:tabs><w:tab w:val="left" w:pos="`+itoa((level-1)*240+tocNumberWidth)+`"/></w:tabs><w:spacing w:after="60"/><w:ind w:left="`+itoa((level-1)*240)+`"/>`, "", `<w:uiPriority w:val="39"/>`)
	}
	char("Hyperlink", "Hyperlink", `<w:color w:val="`+accentText+`"/><w:u w:val="single"/>`)
	char("InlineCode", "Inline Code", mono+`<w:noProof/><w:shd w:val="clear" w:color="auto" w:fill="F1F5F9"/>`)
	b.WriteString(`<w:style w:type="table" w:styleId="ExciterTable"><w:name w:val="Exciter Table"/><w:tblPr><w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + side + ` w:val="single" w:sz="4" w:space="0" w:color="` + ruleColor + `"/>`)
	}
	b.WriteString(`</w:tblBorders><w:tblCellMar><w:top w:w="40" w:type="dxa"/><w:left w:w="` + itoa(cellMargin) + `" w:type="dxa"/><w:bottom w:w="40" w:type="dxa"/><w:right w:w="` + itoa(cellMargin) + `" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	b.WriteString(`<w:style w:type="table" w:styleId="ExciterLayout"><w:name w:val="Exciter Layout"/><w:tblPr><w:tblCellMar><w:top w:w="80" w:type="dxa"/><w:left w:w="` + itoa(cellMargin) + `" w:type="dxa"/><w:bottom w:w="40" w:type="dxa"/><w:right w:w="` + itoa(cellMargin) + `" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	b.WriteString(`</w:styles>`)
	return []byte(b.String())
}

// tocNumberWidth is how far an entry's title sits from its outline number in the contents, in twips.
const tocNumberWidth = 900

// The numbering definitions: bullets, and a numbered list in each of the
// counting styles an ordered list may name.
const bulletNum = 1

var orderedFormats = []struct{ kind, format string }{
	{"1", "decimal"}, {"a", "lowerLetter"}, {"A", "upperLetter"}, {"i", "lowerRoman"}, {"I", "upperRoman"},
}

// bullets are the markers of each level, as browsers draw nested lists.
var bullets = []string{"•", "◦", "▪"}

// orderedNum adds a numbering instance for one ordered list, so each list
// counts on its own from where it starts.
func (w *writer) orderedNum(kind string, start int) int {
	abstract := 1
	for i, f := range orderedFormats {
		if f.kind == kind {
			abstract = i + 1
		}
	}
	w.nums = append(w.nums, listNum{abstract: abstract, start: start})
	return bulletNum + len(w.nums)
}

func (w *writer) numbering() []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<w:numbering xmlns:w="` + nsW + `">`)
	level := func(ilvl int, format, text, font string) {
		b.WriteString(`<w:lvl w:ilvl="` + itoa(ilvl) + `"><w:start w:val="1"/><w:numFmt w:val="` + format + `"/><w:lvlText w:val="` + escape(text) + `"/><w:lvlJc w:val="left"/>`)
		b.WriteString(`<w:pPr><w:ind w:left="` + itoa(listIndent*(ilvl+1)) + `" w:hanging="` + itoa(hanging) + `"/></w:pPr>`)
		if font != "" {
			b.WriteString(`<w:rPr><w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `"/></w:rPr>`)
		}
		b.WriteString(`</w:lvl>`)
	}
	b.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for ilvl := 0; ilvl <= maxListLevel; ilvl++ {
		level(ilvl, "bullet", bullets[ilvl%len(bullets)], bodyFont)
	}
	b.WriteString(`</w:abstractNum>`)
	for i, f := range orderedFormats {
		b.WriteString(`<w:abstractNum w:abstractNumId="` + itoa(i+1) + `"><w:multiLevelType w:val="hybridMultilevel"/>`)
		for ilvl := 0; ilvl <= maxListLevel; ilvl++ {
			level(ilvl, f.format, "%"+itoa(ilvl+1)+".", "")
		}
		b.WriteString(`</w:abstractNum>`)
	}
	b.WriteString(`<w:num w:numId="` + itoa(bulletNum) + `"><w:abstractNumId w:val="0"/></w:num>`)
	for i, n := range w.nums {
		b.WriteString(`<w:num w:numId="` + itoa(bulletNum+i+1) + `"><w:abstractNumId w:val="` + itoa(n.abstract) + `"/>`)
		for ilvl := 0; ilvl <= maxListLevel; ilvl++ {
			b.WriteString(`<w:lvlOverride w:ilvl="` + itoa(ilvl) + `"><w:startOverride w:val="` + itoa(n.start) + `"/></w:lvlOverride>`)
		}
		b.WriteString(`</w:num>`)
	}
	b.WriteString(`</w:numbering>`)
	return []byte(b.String())
}
