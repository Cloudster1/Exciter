//go:build integration

package test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Columns from another project, the refusals checkMatrix makes, and a matrix too wide for a file (#315).

// csvRecords reads a downloaded CSV file, its records of any width, so a closing note of one field reads too.
func csvRecords(t *testing.T, r response) [][]string {
	t.Helper()
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(r.Raw, []byte{0xEF, 0xBB, 0xBF})))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("the file is no CSV: %v", err)
	}
	return records
}

// crossScene is two projects everybody reads, rows and columns, where bob edits both and carol only reads; a third
// project only its maker sees.
type crossScene struct {
	f                  *projectFixture
	rows, columns, off string
	bob, carol         *client
}

func newCrossScene(t *testing.T) *crossScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &crossScene{f: f, rows: projectKey(t, "XRW"), columns: projectKey(t, "XCL")}
	bobID, carolID := f.h.addPerson(t, "member"), f.h.addPerson(t, "member")
	s.bob, s.carol = f.api.as(t, bobID), f.api.as(t, carolID)
	for key, titles := range map[string][]string{
		s.rows:    {"Brakes engage within 100 ms", "Brake lights follow the pedal", "Brake log keeps every press"},
		s.columns: {"Caliper clamps within 80 ms", "Lamp lights within 20 ms", "Caliper wear is reported"},
	} {
		p := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Cross " + key}), http.StatusCreated, "make "+key), "project")
		if _, err := f.h.super.Exec(t.Context(), `UPDATE role_assignment SET role = 'viewer' WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, p["id"]); err != nil {
			t.Fatal(err)
		}
		f.h.grantAs(t, uuid.MustParse(p["id"].(string)), bobID, "editor")
		for _, title := range titles {
			want(t, s.bob.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
		}
	}
	for _, l := range []struct{ from, kind, to string }{
		{s.rows + "-R1", "derives", s.columns + "-R1"},
		{s.columns + "-R2", "verifies", s.rows + "-R2"},
		{s.rows + "-R1", "relates", s.columns + "-R3"},
		{s.rows + "-R3", "relates", s.rows + "-R1"},
	} {
		want(t, s.bob.post(t, "/api/v1/requirements/"+l.from+"/links", map[string]any{"typeKey": l.kind, "otherKey": l.to}), http.StatusCreated,
			"link "+l.from+" to "+l.to)
	}
	// In the trash, so carol, who only reads, can see it no more.
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.columns+"-R3"), http.StatusNoContent, "trash a column requirement")
	s.off = projectKey(t, "XHD")
	hiddenProject(t, f.h, f.owner, s.off)
	return s
}

// Another project's requirements as columns, both ways and in the files, without what the reader cannot see; a
// project they cannot see is refused like an unknown one.
func TestTheMatrixLaysOutAnotherProjectsRequirements(t *testing.T) {
	s := newCrossScene(t)
	row := func(n string) string { return s.rows + "-R" + n }
	col := func(n string) string { return s.columns + "-R" + n }
	read := func(c *client, of, query string) map[string]any {
		t.Helper()
		return obj(t, want(t, c.get(t, "/api/v1/projects/"+of+"/matrix"+query), http.StatusOK, "the matrix of "+of+query), "matrix")
	}
	t.Run("rows of one project against the requirements of the other they link to", func(t *testing.T) {
		m := read(s.carol, s.rows, "?columnProject="+s.columns)
		got := matrixCells(t, m)
		if fmt.Sprint(matrixKeys(t, m, "columns")) != "["+col("1")+" "+col("2")+"]" ||
			fmt.Sprint(got) != fmt.Sprint(map[string]string{row("1") + ":" + col("1"): "Derives/outgoing", row("2") + ":" + col("2"): "Verifies/incoming"}) {
			t.Errorf("the rows against the other project read %v with columns %v", got, matrixKeys(t, m, "columns"))
		}
		if fmt.Sprint(matrixCounts(m, "rowLinks")) != "[1 1 0]" {
			t.Errorf("the rows count %v; the link to the trashed column and the one inside the rows' project do not count", m["rowLinks"])
		}
	})
	t.Run("and the other way round", func(t *testing.T) {
		m := read(s.carol, s.columns, "?columnProject="+s.rows)
		got := matrixCells(t, m)
		if fmt.Sprint(matrixKeys(t, m, "rows")) != "["+col("1")+" "+col("2")+"]" ||
			fmt.Sprint(got) != fmt.Sprint(map[string]string{col("1") + ":" + row("1"): "Derives/incoming", col("2") + ":" + row("2"): "Verifies/outgoing"}) {
			t.Errorf("the other project against the rows reads %v", got)
		}
	})
	t.Run("the files name the other project's columns", func(t *testing.T) {
		records := csvRecords(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.rows+"/matrix.csv?columnProject="+s.columns), http.StatusOK, "the file"))
		if strings.Join(records[0], ",") != "Key,Title,Links,"+col("1")+","+col("2") || records[2][3] != "Derives" || records[3][4] != "Verifies" {
			t.Errorf("the file reads %v", records)
		}
		text := workbookText(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.rows+"/matrix.xlsx?columnProject="+s.columns), http.StatusOK, "the workbook"))
		if !strings.Contains(text, col("1")) || !strings.Contains(text, col("2")) || strings.Contains(text, col("3")) {
			t.Errorf("the workbook reads %s", text)
		}
	})
	t.Run("a project the reader cannot see is refused as one nobody has", func(t *testing.T) {
		hidden := fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.rows+"/matrix?columnProject="+s.off), "columnProject")
		unknown := fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.rows+"/matrix?columnProject=NOPE"+s.rows), "columnProject")
		if hidden != unknown {
			t.Errorf("a hidden project is refused with %q, an unknown one with %q", hidden, unknown)
		}
		fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.rows+"/matrix.csv?columnProject="+s.off), "columnProject")
		read(s.f.owner, s.rows, "?columnProject="+s.off)
	})
}

// What checkMatrix refuses reaches the API on its field, for the page and its files alike.
func TestTheMatrixRefusesColumnsItsSettingsDoNotFit(t *testing.T) {
	s := newCrossScene(t)
	for _, path := range []string{"/matrix", "/matrix.csv", "/matrix.xlsx"} {
		base := "/api/v1/projects/" + s.rows + path
		for query, field := range map[string]string{
			"?columns=tests&relation=verifies":  "relation",
			"?columns=code&relation=implements": "relation",
			"?gaps=both":                        "gaps",
			"?columns=pages&columnRepository=x": "columnRepository",
		} {
			msg := fieldError(t, s.carol.get(t, base+query), field)
			if !strings.HasSuffix(msg, ".") {
				t.Errorf("%s%s: the refusal %q is no sentence", path, query, msg)
			}
		}
	}
}

// A file holds every column, so rows reaching more than trace.MatrixMaxColumns are refused there with the sentence
// saying so, while the page still answers a window of them.
func TestAMatrixTooWideForAFileIsRefused(t *testing.T) {
	f := newProjectFixture(t)
	key := projectKey(t, "WIDE")
	p := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Wide"}), http.StatusCreated, "make a project"), "project")
	spokes := trace.MatrixMaxColumns + 2
	ctx := t.Context()
	// Written past the service, since only the width is under test and five hundred requests would only be slower.
	if _, err := f.h.super.Exec(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by)
		SELECT $1, 'Spoke ' || g, '{"type":"doc"}', 'h', $2 FROM generate_series(1, $3::int + 2) g`, p["id"], f.home.user, spokes); err != nil {
		t.Fatal(err)
	}
	// The first two are the hubs, each holding fewer than trace.MaxLinksPerRequirement links.
	if _, err := f.h.super.Exec(ctx, `INSERT INTO trace_link (type_id, source_id, target_id, created_by)
		SELECT (SELECT id FROM link_type WHERE project_id IS NULL AND key = 'relates'),
		       (SELECT id FROM requirement WHERE project_id = $1 AND number = 1 + r.number % 2), r.id, $2
		FROM requirement r WHERE r.project_id = $1 AND r.number > 2`, p["id"], f.home.user); err != nil {
		t.Fatal(err)
	}
	f.h.settle(t)
	columns := spokes + 2
	wanted := (&trace.TooWideError{Columns: columns}).Error()
	for _, file := range []string{"matrix.csv", "matrix.xlsx"} {
		got := f.owner.get(t, "/api/v1/projects/"+key+"/"+file)
		refuse(t, "the whole "+file, got, http.StatusUnprocessableEntity, "validation_failed")
		if msg, _ := got.Body["error"].(map[string]any)["message"].(string); msg != wanted {
			t.Errorf("%s is refused with %q, want %q", file, msg, wanted)
		}
	}
	m := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+key+"/matrix?limit=2&columnLimit=10&columnOffset=500"), http.StatusOK, "a window of the wide matrix"),
		"matrix")
	// The page's two rows are the hubs, so its columns are the spokes they reach.
	if m["columnTotal"] != float64(spokes) || len(m["columns"].([]any)) != spokes-500 || m["total"] != float64(columns) {
		t.Errorf("the last window of columns reads %v of %v, %v rows", len(m["columns"].([]any)), m["columnTotal"], m["total"])
	}
}
