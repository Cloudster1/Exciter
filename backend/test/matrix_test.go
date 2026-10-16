//go:build integration

package test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
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
		if page, ok := column["page"].(map[string]any); ok {
			name = page["id"].(string)
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
		if strings.Join(records[0], ",") != "Key,Title,Links,"+s.req("1")+","+s.req("3") {
			t.Errorf("the file's header reads %v", records[0])
		}
	})
}

// matrixKeys reads the keys of a matrix's rows or columns, in order.
func matrixKeys(t *testing.T, m map[string]any, of string) []string {
	t.Helper()
	var out []string
	for _, each := range m[of].([]any) {
		item := each.(map[string]any)
		if req, ok := item["requirement"].(map[string]any); ok {
			item = req
		}
		out = append(out, item["key"].(string))
	}
	return out
}

// matrixCounts reads a matrix's counts of links per row or column.
func matrixCounts(m map[string]any, of string) []int {
	var out []int
	for _, n := range m[of].([]any) {
		out = append(out, int(n.(float64)))
	}
	return out
}

func TestTheMatrixShowsItsGaps(t *testing.T) {
	s := newCoverageScene(t, "MXG")
	matrix := "/api/v1/projects/" + s.key + "/matrix"
	read := func(query string) map[string]any {
		t.Helper()
		return obj(t, want(t, s.carol.get(t, matrix+query), http.StatusOK, "the matrix "+query), "matrix")
	}
	every := "?columnQuery=" + url.QueryEscape("kind = requirement")
	t.Run("every row and column says how many links it has", func(t *testing.T) {
		m := read("")
		if fmt.Sprint(matrixCounts(m, "rowLinks")) != "[2 1 1 0]" || fmt.Sprint(matrixCounts(m, "columnLinks")) != "[2 1 1]" {
			t.Errorf("the counts read rows %v, columns %v", m["rowLinks"], m["columnLinks"])
		}
	})
	t.Run("a column filter makes the columns what it keeps, linked or not", func(t *testing.T) {
		m := read("?columnType=" + s.noteType)
		if fmt.Sprint(matrixKeys(t, m, "columns")) != "["+s.req("4")+"]" || fmt.Sprint(matrixCounts(m, "columnLinks")) != "[0]" ||
			fmt.Sprint(matrixCounts(m, "rowLinks")) != "[0 0 0 0]" || len(m["cells"].([]any)) != 0 {
			t.Errorf("the notes as columns read %v", m)
		}
		m = read(every)
		if m["columnTotal"] != float64(4) || fmt.Sprint(matrixCounts(m, "columnLinks")) != "[2 1 1 0]" {
			t.Errorf("every requirement as a column reads %v", m)
		}
	})
	t.Run("columns are paged as rows are", func(t *testing.T) {
		m := read(every + "&columnLimit=2&columnOffset=2")
		if fmt.Sprint(matrixKeys(t, m, "columns")) != "["+s.req("3")+" "+s.req("4")+"]" || m["columnTotal"] != float64(4) || m["columnOffset"] != float64(2) {
			t.Errorf("the second page of columns reads %v", m)
		}
	})
	t.Run("only gaps keeps what has no link", func(t *testing.T) {
		m := read("?gaps=rows")
		if fmt.Sprint(matrixKeys(t, m, "rows")) != "["+s.req("4")+"]" || m["total"] != float64(1) || len(m["columns"].([]any)) != 0 {
			t.Errorf("the rows without a link read %v", m)
		}
		m = read(every + "&gaps=columns")
		if fmt.Sprint(matrixKeys(t, m, "columns")) != "["+s.req("4")+"]" || m["total"] != float64(4) {
			t.Errorf("the columns without a link read %v", m)
		}
		m = read("?columnType=" + s.noteType + "&gaps=both")
		if len(m["rows"].([]any)) != 4 || fmt.Sprint(matrixKeys(t, m, "columns")) != "["+s.req("4")+"]" {
			t.Errorf("both gaps read %v", m)
		}
	})
	t.Run("what gaps cannot mean is refused", func(t *testing.T) {
		fieldError(t, s.carol.get(t, matrix+"?gaps=columns"), "gaps")
		fieldError(t, s.carol.get(t, matrix+"?gaps=sideways"), "gaps")
		fieldError(t, s.carol.get(t, matrix+"?columns=issues&columnType="+s.noteType), "columnQuery")
		fieldError(t, s.carol.get(t, matrix+"?columnLimit=0"), "columnLimit")
		fieldError(t, s.carol.get(t, matrix+"?columnLimit=501"), "columnLimit")
		bad := s.carol.get(t, matrix+"?columnQuery="+url.QueryEscape("nonsense ="))
		refuse(t, "a column query that does not parse", bad, http.StatusBadRequest, "bad_query")
		if fields, _ := bad.Body["error"].(map[string]any)["fields"].(map[string]any); fields["columnQuery"] == nil {
			t.Errorf("the refusal does not name the column query: %v", bad.Body)
		}
	})
	t.Run("the file carries the counts", func(t *testing.T) {
		file := want(t, s.carol.get(t, matrix+".csv"+every), http.StatusOK, "the file with a column filter")
		records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(records[1], ",") != ",Links,,2,1,1,0" || records[2][2] != "2" || records[5][2] != "0" {
			t.Errorf("the file reads %v", records)
		}
	})
}

// A link made or removed from a matrix cell (#149) goes through the requirement's own operations, so the matrix
// follows it at once, coverage once the worker has cleared its cache, and a reader is refused as anywhere.
func TestTheMatrixFollowsLinksMadeFromItsCells(t *testing.T) {
	s := newCoverageScene(t, "MXC")
	matrix := "/api/v1/projects/" + s.key + "/matrix"
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"decomposed": true}), http.StatusOK, "ask every type to be decomposed")
	if _, rows, _ := s.coverage(t, s.carol, ""); rows[s.req("4")] != "none na na" {
		t.Fatalf("R4 before it is linked reads %q", rows[s.req("4")])
	}
	cell := s.req("4") + ":" + s.req("2")
	refuse(t, "carol, a reader", s.carol.post(t, "/api/v1/requirements/"+s.req("4")+"/links", map[string]any{"typeKey": "derives", "otherKey": s.req("2"), "direction": "incoming"}),
		http.StatusForbidden, "forbidden")
	link := obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("4")+"/links", map[string]any{"typeKey": "derives", "otherKey": s.req("2"), "direction": "incoming"}),
		http.StatusCreated, "bob links R2 to R4 from R4's row"), "link")
	m := obj(t, want(t, s.carol.get(t, matrix), http.StatusOK, "the matrix"), "matrix")
	if got := matrixCells(t, m)[cell]; got != "Derives/incoming" {
		t.Errorf("the cell %s reads %q right after the link", cell, got)
	}
	s.h.drained(t)
	if _, rows, _ := s.coverage(t, s.carol, ""); rows[s.req("4")] != "full na na" {
		t.Errorf("R4 once linked reads %q", rows[s.req("4")])
	}
	want(t, s.bob.delete(t, "/api/v1/links/"+link["id"].(string)), http.StatusNoContent, "bob removes the link")
	m = obj(t, want(t, s.carol.get(t, matrix), http.StatusOK, "the matrix"), "matrix")
	if got := matrixCells(t, m)[cell]; got != "" {
		t.Errorf("the cell %s still reads %q after the link went", cell, got)
	}
	s.h.drained(t)
	if _, rows, _ := s.coverage(t, s.carol, ""); rows[s.req("4")] != "none na na" {
		t.Errorf("R4 unlinked again reads %q", rows[s.req("4")])
	}
}

// Stator pages as columns (#150): the pages the rows are linked to, broken ones left out, each link named by its
// relation and suspect as the requirement page shows it, and titled in the file as the reader sees them.
func TestTheMatrixLaysOutStatorPages(t *testing.T) {
	s := newCoverageScene(t, "MXP")
	s.h.keepStator(t)
	tenant := stubTenant(t)
	statorOffers(t)
	want(t, s.owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, s.bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	pages := statorPages(t, tenant)
	timing, braking, threat := pages["Brake timing"], pages["Braking system"], pages["Threat model"]
	for _, l := range []struct {
		req, relation, page string
		version, seen       int
		broken              bool
	}{
		{"1", "specifies", timing, 2, 2, false}, {"2", "describes", timing, 2, 2, false}, {"1", "relates", braking, 3, 2, false},
		{"3", "specifies", threat, 1, 1, false}, {"4", "specifies", braking, 3, 3, true},
	} {
		if _, err := s.h.super.Exec(t.Context(), `INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, page_version, page_version_seen, reviewed_rev, broken_at)
			VALUES ($1, $2, $3, 'ENG', $4, $5, $6, 1, CASE WHEN $7 THEN now() END)`, s.reqIDs[l.req], l.relation, l.page, statorURL(t)+"/s/ENG/p/"+l.page, l.version, l.seen, l.broken); err != nil {
			t.Fatal(err)
		}
	}
	// Written straight through SQL, so no read of the API's waits for it on a replica unless told to.
	s.h.settle(t)
	matrix := "/api/v1/projects/" + s.key + "/matrix?columns=pages"
	t.Run("the pages the rows reach are the columns, a broken link left out", func(t *testing.T) {
		m := obj(t, want(t, s.bob.get(t, matrix), http.StatusOK, "the page matrix"), "matrix")
		got := matrixCells(t, m)
		wantCells := map[string]string{
			s.req("1") + ":" + timing: "specifies/outgoing", s.req("2") + ":" + timing: "describes/outgoing",
			s.req("1") + ":" + braking: "relates/outgoing", s.req("3") + ":" + threat: "specifies/outgoing",
		}
		if fmt.Sprint(got) != fmt.Sprint(wantCells) || len(m["columns"].([]any)) != 3 {
			t.Errorf("the page matrix reads %v", got)
		}
		for _, c := range m["cells"].([]any) {
			cell := c.(map[string]any)
			link := cell["links"].([]any)[0].(map[string]any)
			if link["kind"] != "stator" || (link["name"] == "relates") != (link["suspect"] == true) {
				t.Errorf("a page link reads %v; only the one whose page moved on is suspect", link)
			}
		}
	})
	t.Run("relations narrow the page links", func(t *testing.T) {
		m := obj(t, want(t, s.bob.get(t, matrix+"&relation=describes"), http.StatusOK, "the describing pages"), "matrix")
		if got := matrixCells(t, m); len(got) != 1 || got[s.req("2")+":"+timing] != "describes/outgoing" {
			t.Errorf("the describing pages read %v", got)
		}
		fieldError(t, s.bob.get(t, matrix+"&relation=implements"), "relation")
		fieldError(t, s.bob.get(t, matrix+"&linkType=derives"), "linkType")
		fieldError(t, s.bob.get(t, matrix+"&columnType="+s.noteType), "columnQuery")
	})
	t.Run("the file titles the pages as the reader sees them", func(t *testing.T) {
		header := func(c *client) string {
			file := want(t, c.get(t, "/api/v1/projects/"+s.key+"/matrix.csv?columns=pages"), http.StatusOK, "the page file")
			records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			return strings.Join(records[0], ",")
		}
		bobs := header(s.bob)
		if !strings.Contains(bobs, "Brake timing") || !strings.Contains(bobs, "Braking system") || !strings.Contains(bobs, "ENG/"+threat) {
			t.Errorf("bob's file is headed %q; the Threat model is the admin's to read", bobs)
		}
		if carols := header(s.carol); !strings.Contains(carols, "ENG/"+timing) || strings.Contains(carols, "Brake timing") {
			t.Errorf("carol, without a Stator token, has a file headed %q", carols)
		}
	})
}
