//go:build integration

package test

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
)

// matrixCells reads a matrix's cells by row and column key, each link as its name and direction from the row.
func matrixCells(t *testing.T, m map[string]any) map[string]string {
	t.Helper()
	rows, columns := m["rows"].([]any), m["columns"].([]any)
	cells := map[string]string{}
	for _, c := range m["cells"].([]any) {
		cell := c.(map[string]any)
		row := rows[int(cell["row"].(float64))].(map[string]any)["key"].(string)
		column := columns[int(cell["column"].(float64))].(map[string]any)
		name, _ := column["issueKey"].(string)
		if req, ok := column["requirement"].(map[string]any); ok {
			name = req["key"].(string)
		}
		var links []string
		for _, l := range cell["links"].([]any) {
			link := l.(map[string]any)
			links = append(links, link["name"].(string)+"/"+link["direction"].(string))
		}
		cells[row+":"+name] = strings.Join(links, ",")
	}
	return cells
}

func TestTheMatrixTakesItsAxesLinkTypesAndDirection(t *testing.T) {
	s := newCoverageScene(t, "MXA")
	matrix := "/api/v1/projects/" + s.key + "/matrix"
	want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("4")+"/links", map[string]any{"typeKey": "relates", "otherKey": s.req("2")}),
		http.StatusCreated, "relate R4 to R2")
	read := func(c *client, query string) map[string]any {
		t.Helper()
		return obj(t, want(t, c.get(t, matrix+query), http.StatusOK, "the matrix "+query), "matrix")
	}
	t.Run("each link says what it is and which way it points from its row", func(t *testing.T) {
		m := read(s.carol, "?linkType=derives")
		cell := m["cells"].([]any)[0].(map[string]any)["links"].([]any)[0].(map[string]any)
		if cell["kind"] != "trace" || cell["typeKey"] != "derives" || cell["id"] == "" || cell["direction"] != "incoming" {
			t.Errorf("R1's link to R2 reads %v", cell)
		}
	})
	t.Run("link types keep their own links, the columns those reach", func(t *testing.T) {
		got := matrixCells(t, read(s.carol, "?linkType=verifies"))
		wantCells := map[string]string{s.req("1") + ":" + s.req("3"): "Verifies/incoming", s.req("3") + ":" + s.req("1"): "Verifies/outgoing"}
		if len(got) != len(wantCells) || got[s.req("1")+":"+s.req("3")] != wantCells[s.req("1")+":"+s.req("3")] ||
			got[s.req("3")+":"+s.req("1")] != wantCells[s.req("3")+":"+s.req("1")] {
			t.Errorf("the verifies matrix reads %v", got)
		}
	})
	t.Run("a direction keeps the links pointing that way, and a symmetric type either way", func(t *testing.T) {
		got := matrixCells(t, read(s.carol, "?direction=outgoing"))
		for _, at := range []string{s.req("2") + ":" + s.req("1"), s.req("3") + ":" + s.req("1"), s.req("4") + ":" + s.req("2"), s.req("2") + ":" + s.req("4")} {
			if got[at] == "" {
				t.Errorf("the outgoing matrix lacks %s: %v", at, got)
			}
		}
		if got[s.req("1")+":"+s.req("2")] != "" || len(got) != 4 {
			t.Errorf("the outgoing matrix holds incoming links: %v", got)
		}
	})
	t.Run("relations narrow issue columns", func(t *testing.T) {
		m := read(s.carol, "?columns=issues&relation=verifies")
		columns := m["columns"].([]any)
		if len(columns) != 1 || columns[0].(map[string]any)["issueKey"] != "WEB-2" {
			t.Errorf("the verifying issues read %v", columns)
		}
		link := m["cells"].([]any)[0].(map[string]any)["links"].([]any)[0].(map[string]any)
		if link["kind"] != "armature" || link["direction"] != "outgoing" {
			t.Errorf("an Armature link reads %v", link)
		}
	})
	t.Run("rows follow the order asked for", func(t *testing.T) {
		rows := read(s.carol, "?sort=key&desc=true")["rows"].([]any)
		if rows[0].(map[string]any)["key"] != s.req("4") || rows[3].(map[string]any)["key"] != s.req("1") {
			t.Errorf("the rows read %v, newest key first", rows)
		}
	})
	t.Run("each row says whether the reader may link it", func(t *testing.T) {
		if row := read(s.bob, "")["rows"].([]any)[0].(map[string]any); row["canLink"] != true {
			t.Errorf("bob, an editor, may not link %v", row)
		}
		if row := read(s.carol, "")["rows"].([]any)[0].(map[string]any); row["canLink"] != false {
			t.Errorf("carol, a reader, may link %v", row)
		}
	})
	t.Run("what the matrix does not know is refused on its field", func(t *testing.T) {
		fieldError(t, s.carol.get(t, matrix+"?linkType=nonsense"), "linkType")
		fieldError(t, s.carol.get(t, matrix+"?direction=sideways"), "direction")
		fieldError(t, s.carol.get(t, matrix+"?relation=implements"), "relation")
		fieldError(t, s.carol.get(t, matrix+"?columns=issues&relation=blocks"), "relation")
		fieldError(t, s.carol.get(t, matrix+"?columns=issues&linkType=derives"), "linkType")
	})
	t.Run("the file follows the same setup", func(t *testing.T) {
		file := want(t, s.carol.get(t, matrix+".csv?linkType=verifies"), http.StatusOK, "the verifies file")
		records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(records[0], ",") != "Key,Title,"+s.req("1")+","+s.req("3") {
			t.Errorf("the file's header reads %v", records[0])
		}
	})
}
