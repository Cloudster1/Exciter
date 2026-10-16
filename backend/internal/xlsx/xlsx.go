// Package xlsx writes a workbook of one sheet as Office Open XML (#154), by hand with archive/zip and encoding/xml as
// internal/docx writes Word, since the matrix and the chain report need only text, counts, fills and a frozen header.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Style is how a cell looks: its fill tells a link from a gap, and its weight a header from a body.
type Style int

const (
	Plain Style = iota
	Header
	Linked
	Suspect
	Gap
	Break
	Added
	Removed
	Changed
	Note
)

// Cell is one cell: text, or a count when Count is set.
type Cell struct {
	Text  string
	Count *int
	Style Style
}

// Text is a cell of text in a style.
func Text(s string, style Style) Cell { return Cell{Text: s, Style: style} }

// Count is a cell holding a number, so a spreadsheet sums and sorts it.
func Count(n int, style Style) Cell { return Cell{Count: &n, Style: style} }

// Sheet is what a workbook holds: rows of cells, the first FreezeRows rows and FreezeColumns columns kept in view, and
// an autofilter over the first row when Filter is set.
type Sheet struct {
	Name          string
	Rows          [][]Cell
	FreezeRows    int
	FreezeColumns int
	Filter        bool
}

// Column widths in characters, within which each column fits its longest cell.
const (
	minWidth = 8
	maxWidth = 60
	// widthPad is room around the text, which a spreadsheet's own measure leaves.
	widthPad = 2
	// maxSheetName is the most a spreadsheet takes in a sheet's name.
	maxSheetName = 31
	// letters name columns, A to Z, then AA.
	letters = 26
)

// The fills a style draws with, the light scheme of the built-in theme as a document's are.
var fills = map[Style]string{
	Header:  "F1F5F9",
	Linked:  "DCFCE7",
	Suspect: "FEF3C7",
	Gap:     "FEE2E2",
	Break:   "FEE2E2",
	Added:   "DCFCE7",
	Removed: "FEE2E2",
	Changed: "FEF3C7",
}

// styleOrder is every style in the order styles.xml lists their formats, so a cell names its style by place.
var styleOrder = []Style{Plain, Header, Linked, Suspect, Gap, Break, Added, Removed, Changed, Note}

// Write writes the sheet as a workbook, its parts stamped with modified.
func Write(w io.Writer, s Sheet, modified time.Time) error {
	strs := &sharedStrings{at: map[string]int{}}
	sheet := sheetXML(s, strs)
	zw := zip.NewWriter(w)
	for _, part := range []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"docProps/app.xml", appProperties},
		{"xl/workbook.xml", workbook(s.Name)},
		{"xl/_rels/workbook.xml.rels", workbookRels},
		{"xl/styles.xml", styles()},
		{"xl/sharedStrings.xml", strs.xml()},
		{"xl/worksheets/sheet1.xml", sheet},
	} {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: part.name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, part.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ColumnName is the letters a spreadsheet names the column at index i by, from 0: A, B, ..., Z, AA.
func ColumnName(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / letters {
		name = string(rune('A'+(i-1)%letters)) + name
	}
	return name
}

type sharedStrings struct {
	list []string
	at   map[string]int
}

func (s *sharedStrings) index(text string) int {
	if i, ok := s.at[text]; ok {
		return i
	}
	s.at[text] = len(s.list)
	s.list = append(s.list, text)
	return len(s.list) - 1
}

func (s *sharedStrings) xml() string {
	var b strings.Builder
	b.WriteString(xml.Header + `<sst xmlns="` + nsMain + `" count="` + strconv.Itoa(len(s.list)) + `" uniqueCount="` + strconv.Itoa(len(s.list)) + `">`)
	for _, text := range s.list {
		b.WriteString(`<si><t xml:space="preserve">`)
		b.WriteString(escape(text))
		b.WriteString(`</t></si>`)
	}
	b.WriteString(`</sst>`)
	return b.String()
}

func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func sheetXML(s Sheet, strs *sharedStrings) string {
	widths := map[int]int{}
	for _, row := range s.Rows {
		for i, c := range row {
			n := utf8.RuneCountInString(c.Text)
			if c.Count != nil {
				n = len(strconv.Itoa(*c.Count))
			}
			widths[i] = max(widths[i], n)
		}
	}
	var b strings.Builder
	b.WriteString(xml.Header + `<worksheet xmlns="` + nsMain + `" xmlns:r="` + nsRel + `">`)
	b.WriteString(`<sheetViews><sheetView workbookViewId="0">`)
	if s.FreezeRows > 0 || s.FreezeColumns > 0 {
		b.WriteString(`<pane xSplit="` + strconv.Itoa(s.FreezeColumns) + `" ySplit="` + strconv.Itoa(s.FreezeRows) + `" topLeftCell="` +
			ColumnName(s.FreezeColumns) + strconv.Itoa(s.FreezeRows+1) + `" activePane="bottomRight" state="frozen"/>`)
	}
	b.WriteString(`</sheetView></sheetViews>`)
	if len(widths) > 0 {
		b.WriteString(`<cols>`)
		for i := 0; i < len(widths); i++ {
			width := min(max(widths[i]+widthPad, minWidth), maxWidth)
			b.WriteString(`<col min="` + strconv.Itoa(i+1) + `" max="` + strconv.Itoa(i+1) + `" width="` + strconv.Itoa(width) + `" customWidth="1"/>`)
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range s.Rows {
		b.WriteString(`<row r="` + strconv.Itoa(r+1) + `">`)
		for c, cell := range row {
			ref := ColumnName(c) + strconv.Itoa(r+1)
			style := strconv.Itoa(styleIndex(cell.Style))
			switch {
			case cell.Count != nil:
				b.WriteString(`<c r="` + ref + `" s="` + style + `"><v>` + strconv.Itoa(*cell.Count) + `</v></c>`)
			case cell.Text == "" && cell.Style == Plain:
			default:
				b.WriteString(`<c r="` + ref + `" s="` + style + `" t="s"><v>` + strconv.Itoa(strs.index(cell.Text)) + `</v></c>`)
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if s.Filter && len(s.Rows) > 0 && len(s.Rows[0]) > 0 {
		b.WriteString(`<autoFilter ref="A1:` + ColumnName(len(s.Rows[0])-1) + `1"/>`)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func styleIndex(s Style) int {
	for i, each := range styleOrder {
		if each == s {
			return i
		}
	}
	return 0
}

func sheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = "Sheet1"
	}
	if utf8.RuneCountInString(name) > maxSheetName {
		name = string([]rune(name)[:maxSheetName])
	}
	return name
}
