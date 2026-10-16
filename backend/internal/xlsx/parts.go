package xlsx

import (
	"encoding/xml"
	"strconv"
	"strings"
)

const (
	nsMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

	relOffice = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relApp    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"
	relSheet  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
	relStyles = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relShared = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings"
)

const contentTypes = xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
	`<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>` +
	`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
	`</Types>`

const packageRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + relOffice + `" Target="xl/workbook.xml"/>` +
	`<Relationship Id="rId2" Type="` + relApp + `" Target="docProps/app.xml"/>` +
	`</Relationships>`

const appProperties = xml.Header + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Exciter</Application></Properties>`

const workbookRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + relSheet + `" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="` + relStyles + `" Target="styles.xml"/>` +
	`<Relationship Id="rId3" Type="` + relShared + `" Target="sharedStrings.xml"/>` +
	`</Relationships>`

func workbook(name string) string {
	return xml.Header + `<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `"><sheets><sheet name="` + escape(sheetName(name)) +
		`" sheetId="1" r:id="rId1"/></sheets></workbook>`
}

// styles lists a font plain and bold, the two fills every workbook starts with and one per colour, and a format per
// style in styleOrder, which a cell's s names by place.
func styles() string {
	var colours []string
	fillOf := map[Style]int{}
	for _, s := range styleOrder {
		colour, ok := fills[s]
		if !ok {
			continue
		}
		at := -1
		for i, c := range colours {
			if c == colour {
				at = i
			}
		}
		if at < 0 {
			at = len(colours)
			colours = append(colours, colour)
		}
		// The first two fills are none and gray125, as a spreadsheet expects them.
		fillOf[s] = at + 2
	}
	var b strings.Builder
	b.WriteString(xml.Header + `<styleSheet xmlns="` + nsMain + `">`)
	b.WriteString(`<fonts count="3"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font>` +
		`<font><i/><sz val="11"/><color rgb="FF64748B"/><name val="Calibri"/></font></fonts>`)
	b.WriteString(`<fills count="` + strconv.Itoa(len(colours)+2) + `"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>`)
	for _, c := range colours {
		b.WriteString(`<fill><patternFill patternType="solid"><fgColor rgb="FF` + c + `"/><bgColor indexed="64"/></patternFill></fill>`)
	}
	b.WriteString(`</fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>`)
	b.WriteString(`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`)
	b.WriteString(`<cellXfs count="` + strconv.Itoa(len(styleOrder)) + `">`)
	for _, s := range styleOrder {
		font := 0
		switch s {
		case Header:
			font = 1
		case Note:
			font = 2
		}
		fill := fillOf[s]
		b.WriteString(`<xf numFmtId="0" fontId="` + strconv.Itoa(font) + `" fillId="` + strconv.Itoa(fill) + `" borderId="0" xfId="0"`)
		if font > 0 {
			b.WriteString(` applyFont="1"`)
		}
		if fill > 0 {
			b.WriteString(` applyFill="1"`)
		}
		b.WriteString(`/>`)
	}
	b.WriteString(`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	return b.String()
}
