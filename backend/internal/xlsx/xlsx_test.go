package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

func TestColumnsAreNamedAsASpreadsheetNamesThem(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := ColumnName(i); got != want {
			t.Errorf("column %d is named %s, want %s", i, got, want)
		}
	}
}

// parts reads a workbook back into its parts by name, each checked to be well-formed XML.
func parts(t *testing.T, b []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		d := xml.NewDecoder(bytes.NewReader(body))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed: %v", f.Name, err)
			}
		}
		out[f.Name] = string(body)
	}
	return out
}

func TestAWorkbookHoldsItsSheetFrozenFilteredAndColoured(t *testing.T) {
	var b bytes.Buffer
	err := Write(&b, Sheet{
		Name: "CHK matrix: all [links]",
		Rows: [][]Cell{
			{Text("Key", Header), Text("Title", Header), Text("Links", Header), Text("CHK-R2", Header)},
			{Text("CHK-R1", Plain), Text("Brakes & lights", Plain), Count(2, Plain), Text("Derives", Linked)},
			{Text("CHK-R3", Plain), Text("Horn", Plain), Count(0, Gap), Text("", Plain)},
		},
		FreezeRows: 1, FreezeColumns: 2, Filter: true,
	}, time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, b.Bytes())
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml",
		"xl/sharedStrings.xml", "xl/worksheets/sheet1.xml", "docProps/app.xml"} {
		if _, ok := p[name]; !ok {
			t.Errorf("the workbook lacks %s", name)
		}
	}
	sheet := p["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`<pane xSplit="2" ySplit="1" topLeftCell="C2" activePane="bottomRight" state="frozen"/>`,
		`<autoFilter ref="A1:D1"/>`,
		`<c r="C2" s="0"><v>2</v></c>`,
		`<c r="C3" s="4"><v>0</v></c>`,
		`<c r="D2" s="2" t="s">`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the sheet lacks %s:\n%s", want, sheet)
		}
	}
	if strings.Contains(sheet, `r="D3"`) {
		t.Error("an empty plain cell was written")
	}
	if !strings.Contains(p["xl/sharedStrings.xml"], "Brakes &amp; lights") || !strings.Contains(p["xl/workbook.xml"], `name="CHK matrix  all  links "`) {
		t.Errorf("the strings or the sheet's name read %s, %s", p["xl/sharedStrings.xml"], p["xl/workbook.xml"])
	}
	if !strings.Contains(p["xl/styles.xml"], `<cellXfs count="10">`) || !strings.Contains(p["xl/styles.xml"], `rgb="FFDCFCE7"`) {
		t.Errorf("the styles read %s", p["xl/styles.xml"])
	}
}
