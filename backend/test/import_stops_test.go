//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/csv"
	"fmt"
	"hash/crc32"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/exporttemplate"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
)

// longImport is how many rows or objects a file has whose import is still running when the test stops it.
const longImport = 300

// stopPoll is how often a test looks whether an import has made its first requirement, so it stops it running.
const stopPoll = 10 * time.Millisecond

// stopSettle is how long a stopped import's worker may take to finish the row it was on.
const stopSettle = 2 * time.Second

// importScene is a project the installation's owner runs, with a second administrator.
type importScene struct {
	f          *projectFixture
	key        string
	projectID  uuid.UUID
	admin      *client
	adminID    uuid.UUID
	importPath string
	reqifPath  string
}

func newImportScene(t *testing.T, prefix string) *importScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &importScene{f: f, key: projectKey(t, prefix)}
	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Imports"}), http.StatusCreated, "make the project"), "project")
	s.projectID = uuid.MustParse(made["id"].(string))
	s.adminID = f.h.addPerson(t, "member")
	f.h.grantAs(t, s.projectID, s.adminID, "admin")
	s.admin = f.api.as(t, s.adminID)
	s.importPath, s.reqifPath = "/api/v1/imports/", "/api/v1/reqif-imports/"
	return s
}

// requirements counts the requirements of the scene's project, as the superuser sees them.
func (s *importScene) requirements(t *testing.T) int {
	t.Helper()
	return s.f.h.countRows(t, `SELECT count(*) FROM requirement WHERE project_id = $1`, s.projectID)
}

// titledRows is a CSV file of n requirements, each with a title of its own.
func titledRows(n int) []byte {
	var b strings.Builder
	b.WriteString("title\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "Requirement %d\n", i)
	}
	return []byte(b.String())
}

// manyObjects is a ReqIF document of n requirements, one after the other in one specification.
func manyObjects(n int) []byte {
	const changed = `LAST-CHANGE="2026-09-02T10:00:00Z"`
	var objects, children strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&objects, `<SPEC-OBJECT IDENTIFIER="_o%d" %s><TYPE><SPEC-OBJECT-TYPE-REF>_t_req</SPEC-OBJECT-TYPE-REF></TYPE><VALUES>`+
			`<ATTRIBUTE-VALUE-STRING THE-VALUE="Requirement %d"><DEFINITION><ATTRIBUTE-DEFINITION-STRING-REF>_a_name</ATTRIBUTE-DEFINITION-STRING-REF></DEFINITION>`+
			`</ATTRIBUTE-VALUE-STRING></VALUES></SPEC-OBJECT>`, i, changed, i)
		fmt.Fprintf(&children, `<SPEC-HIERARCHY IDENTIFIER="_h%d" %s><OBJECT><SPEC-OBJECT-REF>_o%d</SPEC-OBJECT-REF></OBJECT></SPEC-HIERARCHY>`, i, changed, i)
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<REQ-IF xmlns="http://www.omg.org/spec/ReqIF/20110401/reqif.xsd"><THE-HEADER><REQ-IF-HEADER IDENTIFIER="_hdr"><TITLE>Many</TITLE></REQ-IF-HEADER></THE-HEADER>
<CORE-CONTENT><REQ-IF-CONTENT><DATATYPES><DATATYPE-DEFINITION-STRING IDENTIFIER="_dt_string" ` + changed + ` LONG-NAME="String" MAX-LENGTH="1000"/></DATATYPES>
<SPEC-TYPES><SPEC-OBJECT-TYPE IDENTIFIER="_t_req" ` + changed + ` LONG-NAME="Requirement"><SPEC-ATTRIBUTES>
<ATTRIBUTE-DEFINITION-STRING IDENTIFIER="_a_name" ` + changed + ` LONG-NAME="ReqIF.Name"><TYPE><DATATYPE-DEFINITION-STRING-REF>_dt_string</DATATYPE-DEFINITION-STRING-REF></TYPE></ATTRIBUTE-DEFINITION-STRING>
</SPEC-ATTRIBUTES></SPEC-OBJECT-TYPE><SPECIFICATION-TYPE IDENTIFIER="_st" ` + changed + ` LONG-NAME="Module"/></SPEC-TYPES>
<SPEC-OBJECTS>` + objects.String() + `</SPEC-OBJECTS>
<SPECIFICATIONS><SPECIFICATION IDENTIFIER="_spec" ` + changed + ` LONG-NAME="Many"><TYPE><SPECIFICATION-TYPE-REF>_st</SPECIFICATION-TYPE-REF></TYPE>
<CHILDREN>` + children.String() + `</CHILDREN></SPECIFICATION></SPECIFICATIONS></REQ-IF-CONTENT></CORE-CONTENT></REQ-IF>`)
}

// stopWhileRunning starts an import, waits for its first requirement and stops it, answering it as it ended.
func stopWhileRunning(t *testing.T, c *client, path, id string) map[string]any {
	t.Helper()
	want(t, c.post(t, path+id+"/start", nil), http.StatusAccepted, "start the import")
	deadline := time.Now().Add(importWait)
	for {
		got := obj(t, want(t, c.get(t, path+id), http.StatusOK, "read the import"), "import")
		if got["state"] != "queued" && got["state"] != "running" {
			t.Fatalf("the import ended before it could be stopped: %v", got["state"])
		}
		if len(got["imported"].([]any)) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the import made nothing in %s: %v", importWait, got)
		}
		time.Sleep(stopPoll)
	}
	want(t, c.delete(t, path+id), http.StatusNoContent, "stop the running import")
	time.Sleep(stopSettle)
	return obj(t, want(t, c.get(t, path+id), http.StatusOK, "read the stopped import"), "import")
}

// keptWhatItMade checks a stopped import: cancelled with fewer rows than its total, each of them still there, and
// nothing more than the row it was on when it stopped.
func (s *importScene) keptWhatItMade(t *testing.T, stopped map[string]any) {
	t.Helper()
	imported := toStrings(stopped["imported"])
	if stopped["state"] != "cancelled" || stopped["finishedAt"] == nil {
		t.Errorf("the stopped import reads %v", stopped["state"])
	}
	if total := int(stopped["total"].(float64)); len(imported) == 0 || len(imported) >= total {
		t.Errorf("the stopped import made %d of %d", len(imported), total)
	}
	for _, key := range imported {
		want(t, s.f.owner.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key+", which the stopped import made")
	}
	// The row the worker was on when it stopped may be written without being counted.
	if n := s.requirements(t); n < len(imported) || n > len(imported)+1 {
		t.Errorf("the project holds %d requirements after an import stopped at %d", n, len(imported))
	}
}

// A CSV import stopped while it runs ends cancelled and keeps what it made; a second stop discards it, and the
// requirements stay (#318).
func TestAStoppedCSVImportKeepsWhatItMade(t *testing.T) {
	s := newImportScene(t, "ICS")
	made := obj(t, want(t, uploadCSV(t, s.f.owner, s.key, "long.csv", titledRows(longImport)), http.StatusCreated, "upload a long file"), "import")
	id := made["id"].(string)
	if made["total"] != float64(longImport) {
		t.Fatalf("the long file holds %v rows", made["total"])
	}
	stopped := stopWhileRunning(t, s.f.owner, s.importPath, id)
	s.keptWhatItMade(t, stopped)
	kept := s.requirements(t)

	want(t, s.f.owner.delete(t, s.importPath+id), http.StatusNoContent, "discard the stopped import")
	refuse(t, "a discarded import", s.f.owner.get(t, s.importPath+id), http.StatusNotFound, "not_found")
	refuse(t, "a discarded import discarded again", s.f.owner.delete(t, s.importPath+id), http.StatusNotFound, "not_found")
	if n := s.requirements(t); n != kept {
		t.Errorf("discarding the import took requirements with it: %d, want %d", n, kept)
	}
}

// The same for ReqIF: stopped while it runs, it ends cancelled and keeps what it made (#318).
func TestAStoppedReqIFImportKeepsWhatItMade(t *testing.T) {
	s := newImportScene(t, "IRS")
	made := obj(t, want(t, s.f.owner.upload(t, "/api/v1/projects/"+s.key+"/reqif-imports", "many.reqif", manyObjects(longImport)),
		http.StatusCreated, "upload a long file"), "import")
	id := made["id"].(string)
	if problems := made["preview"].(map[string]any)["problems"].([]any); len(problems) != 0 || made["total"] != float64(longImport) {
		t.Fatalf("the long file: %v objects, problems %v", made["total"], problems)
	}
	stopped := stopWhileRunning(t, s.f.owner, s.reqifPath, id)
	s.keptWhatItMade(t, stopped)
	kept := s.requirements(t)

	want(t, s.f.owner.delete(t, s.reqifPath+id), http.StatusNoContent, "discard the stopped import")
	refuse(t, "a discarded import", s.f.owner.get(t, s.reqifPath+id), http.StatusNotFound, "not_found")
	if n := s.requirements(t); n != kept {
		t.Errorf("discarding the import took requirements with it: %d, want %d", n, kept)
	}
}

// csvJob reads an import's mapping and state as the table keeps them.
func (s *importScene) csvJob(t *testing.T, id string) string {
	t.Helper()
	var mapping, state string
	if err := s.f.h.super.QueryRow(context.Background(), `SELECT mapping::text, state FROM import_job WHERE id = $1`, id).Scan(&mapping, &state); err != nil {
		t.Fatalf("read the import: %v", err)
	}
	return state + " " + mapping
}

// reqifJob reads an import's choices and state as the table keeps them.
func (s *importScene) reqifJob(t *testing.T, id string) string {
	t.Helper()
	var choices, state string
	if err := s.f.h.super.QueryRow(context.Background(), `SELECT choices::text, state FROM reqif_import WHERE id = $1`, id).Scan(&choices, &state); err != nil {
		t.Fatalf("read the import: %v", err)
	}
	return state + " " + choices
}

// An import is read and stopped by every administrator of its project, but mapped and started by its maker alone,
// and only while they still run the project; the database refuses another administrator the same (#318).
func TestOnlyTheMakerMapsAndStartsAnImport(t *testing.T) {
	s := newImportScene(t, "IMK")
	file := []byte("title,type\nShip it,Requirement\n")

	t.Run("another administrator reads a CSV import, may not map or start it, and may stop it", func(t *testing.T) {
		made := obj(t, want(t, uploadCSV(t, s.f.owner, s.key, "x.csv", file), http.StatusCreated, "upload"), "import")
		id := made["id"].(string)
		before := s.csvJob(t, id)
		got := obj(t, want(t, s.admin.get(t, s.importPath+id), http.StatusOK, "another administrator reads it"), "import")
		if got["id"] != id || got["state"] != "uploaded" {
			t.Errorf("another administrator reads %v", got)
		}
		refuse(t, "another administrator maps it", s.admin.put(t, s.importPath+id+"/mapping", map[string]any{"mapping": map[string]any{"title": []int{0}}}),
			http.StatusForbidden, "forbidden")
		refuse(t, "another administrator starts it", s.admin.post(t, s.importPath+id+"/start", nil), http.StatusForbidden, "forbidden")
		if after := s.csvJob(t, id); after != before {
			t.Errorf("the refusals changed the import from %s to %s", before, after)
		}
		if n := s.requirements(t); n != 0 {
			t.Errorf("the refusals wrote %d requirements", n)
		}
		// An administrator may clean up after somebody who left, as the policy has always let them.
		want(t, s.admin.delete(t, s.importPath+id), http.StatusNoContent, "another administrator discards it")
		refuse(t, "the discarded import", s.f.owner.get(t, s.importPath+id), http.StatusNotFound, "not_found")
	})

	t.Run("another administrator reads a ReqIF import, may not choose for or start it, and may stop it", func(t *testing.T) {
		made := obj(t, want(t, s.f.owner.upload(t, "/api/v1/projects/"+s.key+"/reqif-imports", "s.reqif", supplierFile(t)), http.StatusCreated, "upload"), "import")
		id := made["id"].(string)
		before := s.reqifJob(t, id)
		want(t, s.admin.get(t, s.reqifPath+id), http.StatusOK, "another administrator reads it")
		refuse(t, "another administrator chooses", s.admin.put(t, s.reqifPath+id+"/choices", map[string]any{"choices": map[string]any{"_a_weight": "skip"}}),
			http.StatusForbidden, "forbidden")
		refuse(t, "another administrator starts it", s.admin.post(t, s.reqifPath+id+"/start", nil), http.StatusForbidden, "forbidden")
		if after := s.reqifJob(t, id); after != before {
			t.Errorf("the refusals changed the import from %s to %s", before, after)
		}
		want(t, s.admin.delete(t, s.reqifPath+id), http.StatusNoContent, "another administrator discards it")
		refuse(t, "the discarded import", s.f.owner.get(t, s.reqifPath+id), http.StatusNotFound, "not_found")
	})

	t.Run("a maker who no longer runs the project may not map or start, and may still discard", func(t *testing.T) {
		makerID := s.f.h.addPerson(t, "member")
		s.f.h.grantAs(t, s.projectID, makerID, "admin")
		maker := s.f.api.as(t, makerID)
		csvID := obj(t, want(t, uploadCSV(t, maker, s.key, "x.csv", file), http.StatusCreated, "upload a CSV"), "import")["id"].(string)
		reqifID := obj(t, want(t, maker.upload(t, "/api/v1/projects/"+s.key+"/reqif-imports", "s.reqif", supplierFile(t)), http.StatusCreated, "upload a ReqIF"),
			"import")["id"].(string)
		if _, err := s.f.h.super.Exec(context.Background(), `UPDATE role_assignment SET role = 'editor' WHERE project_id = $1 AND user_id = $2`, s.projectID, makerID); err != nil {
			t.Fatal(err)
		}
		s.f.h.settle(t)
		csvBefore, reqifBefore := s.csvJob(t, csvID), s.reqifJob(t, reqifID)
		refuse(t, "a former administrator maps", maker.put(t, s.importPath+csvID+"/mapping", map[string]any{"mapping": map[string]any{"title": []int{0}}}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a former administrator starts a CSV import", maker.post(t, s.importPath+csvID+"/start", nil), http.StatusForbidden, "forbidden")
		refuse(t, "a former administrator chooses", maker.put(t, s.reqifPath+reqifID+"/choices", map[string]any{"choices": map[string]any{}}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a former administrator starts a ReqIF import", maker.post(t, s.reqifPath+reqifID+"/start", nil), http.StatusForbidden, "forbidden")
		if s.csvJob(t, csvID) != csvBefore || s.reqifJob(t, reqifID) != reqifBefore || s.requirements(t) != 0 {
			t.Error("the refusals changed an import or wrote a requirement")
		}
		want(t, maker.delete(t, s.importPath+csvID), http.StatusNoContent, "the maker discards the CSV import")
		want(t, maker.delete(t, s.reqifPath+reqifID), http.StatusNoContent, "the maker discards the ReqIF import")
	})

	t.Run("a mapping naming what the file or the project lacks is a problem to answer, not a refusal", func(t *testing.T) {
		made := obj(t, want(t, uploadCSV(t, s.f.owner, s.key, "x.csv", file), http.StatusCreated, "upload"), "import")
		id := made["id"].(string)
		refuse(t, "a mapping that is no JSON object", s.f.owner.put(t, s.importPath+id+"/mapping", []any{"title"}), http.StatusBadRequest, "bad_request")
		mapped := obj(t, want(t, s.f.owner.put(t, s.importPath+id+"/mapping", map[string]any{"mapping": map[string]any{"title": []int{7}, "attr:Nothing": []int{0}}}),
			http.StatusOK, "map a column the file lacks and an attribute nobody has"), "import")
		if problems := mapped["preview"].(map[string]any)["problems"].([]any); len(problems) == 0 {
			t.Errorf("the mapping has no problems: %v", mapped["preview"])
		}
		refuse(t, "a start with that mapping", s.f.owner.post(t, s.importPath+id+"/start", nil), http.StatusConflict, "import_problems")
		if !strings.HasPrefix(s.csvJob(t, id), "uploaded ") || s.requirements(t) != 0 {
			t.Error("the refused start moved the import or wrote a requirement")
		}
	})

	t.Run("straight through SQL another administrator may stop an import and neither map nor start it", func(t *testing.T) {
		ctx := context.Background()
		csvID := obj(t, want(t, uploadCSV(t, s.f.owner, s.key, "x.csv", file), http.StatusCreated, "upload a CSV"), "import")["id"].(string)
		reqifID := obj(t, want(t, s.f.owner.upload(t, "/api/v1/projects/"+s.key+"/reqif-imports", "s.reqif", supplierFile(t)), http.StatusCreated, "upload a ReqIF"),
			"import")["id"].(string)
		other := s.f.h.appConn(t, s.adminID)
		for what, sql := range map[string]string{
			"maps a CSV import":     `UPDATE import_job SET mapping = '{}' WHERE id = '` + csvID + `'`,
			"starts a CSV import":   `UPDATE import_job SET state = 'queued' WHERE id = '` + csvID + `'`,
			"chooses for a ReqIF":   `UPDATE reqif_import SET choices = '{"_a_weight": "skip"}' WHERE id = '` + reqifID + `'`,
			"starts a ReqIF import": `UPDATE reqif_import SET state = 'queued' WHERE id = '` + reqifID + `'`,
		} {
			if _, err := other.Exec(ctx, sql); !isPolicyViolation(err) {
				t.Errorf("another administrator %s: %v", what, err)
			}
		}
		for _, sql := range []string{
			`UPDATE import_job SET state = 'cancelled' WHERE id = '` + csvID + `'`,
			`UPDATE reqif_import SET state = 'cancelled' WHERE id = '` + reqifID + `'`,
		} {
			if tag, err := other.Exec(ctx, sql); err != nil || tag.RowsAffected() != 1 {
				t.Errorf("another administrator stops an import: %v", err)
			}
		}
	})
}

// An archived project takes no import and no new template, and keeps the templates it has as they are (#318).
func TestAnArchivedProjectTakesNoImportAndKeepsItsTemplates(t *testing.T) {
	s := newImportScene(t, "IAR")
	owner := s.f.owner
	template := obj(t, want(t, owner.uploadTemplate(t, s.key, "Supplier", "supplier.docx", aWordTemplate(t, "", nil)), http.StatusCreated, "upload a template"), "template")
	templateID := template["id"].(string)
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	s.f.h.settle(t)

	refuse(t, "a CSV import", uploadCSV(t, owner, s.key, "x.csv", titledRows(1)), http.StatusConflict, "project_archived")
	refuse(t, "a ReqIF import", owner.upload(t, "/api/v1/projects/"+s.key+"/reqif-imports", "s.reqif", supplierFile(t)), http.StatusConflict, "project_archived")
	refuse(t, "a new template", owner.uploadTemplate(t, s.key, "Another", "another.docx", aWordTemplate(t, "", nil)), http.StatusConflict, "project_archived")
	refuse(t, "a template renamed", owner.patch(t, "/api/v1/export-templates/"+templateID, map[string]any{"name": "Renamed"}), http.StatusConflict, "project_archived")
	refuse(t, "a template deleted", owner.delete(t, "/api/v1/export-templates/"+templateID), http.StatusConflict, "project_archived")

	if n := s.f.h.countRows(t, `SELECT count(*) FROM import_job WHERE project_id = $1`, s.projectID) +
		s.f.h.countRows(t, `SELECT count(*) FROM reqif_import WHERE project_id = $1`, s.projectID); n != 0 {
		t.Errorf("the archived project holds %d imports", n)
	}
	kept := list(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/export-templates"), http.StatusOK, "list the templates"), "templates")
	if len(kept) != 1 || kept[0].(map[string]any)["name"] != "Supplier" {
		t.Errorf("the archived project's templates read %v", kept)
	}
}

// A start reads the project again: a type the mapping matched and that is gone since is a problem, the start is
// refused, and nothing is written (#318).
func TestAStartReadsTheProjectAgain(t *testing.T) {
	s := newImportScene(t, "IAG")
	owner := s.f.owner
	types := "/api/v1/projects/" + s.key + "/types"
	feature := obj(t, want(t, owner.post(t, types, map[string]any{"name": "Feature", "icon": "test", "colour": "#22aa55"}), http.StatusCreated, "make a type"), "type")
	made := obj(t, want(t, uploadCSV(t, owner, s.key, "x.csv", []byte("title,type\nShip it,Feature\n")), http.StatusCreated, "upload"), "import")
	id := made["id"].(string)
	if problems := made["preview"].(map[string]any)["problems"].([]any); len(problems) != 0 {
		t.Fatalf("Feature is a type of the project, yet: %v", problems)
	}

	want(t, owner.delete(t, types+"/"+feature["id"].(string)), http.StatusNoContent, "delete the type")
	refuse(t, "a start after the type is gone", owner.post(t, s.importPath+id+"/start", nil), http.StatusConflict, "import_problems")
	got := obj(t, want(t, owner.get(t, s.importPath+id), http.StatusOK, "read the import"), "import")
	if got["state"] != "uploaded" {
		t.Errorf("the refused import reads %v", got["state"])
	}
	problems := got["preview"].(map[string]any)["problems"].([]any)
	if len(problems) == 0 || !strings.Contains(fmt.Sprint(problems), "Feature") {
		t.Errorf("the import does not name the type that is gone: %v", problems)
	}
	if n := s.requirements(t); n != 0 {
		t.Errorf("the refused start wrote %d requirements", n)
	}
}

// A CSV export is the project's readers' alone, and carries an attribute asked for by its id under its name, which
// an import maps back to the same attribute (#318).
func TestACSVExportCarriesAnAttributeByItsName(t *testing.T) {
	s := newImportScene(t, "ICX")
	owner := s.f.owner
	notes := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/attributes", map[string]any{"name": "Notes", "kind": "text"}),
		http.StatusCreated, "define an attribute"), "attribute")["id"].(string)
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Pay the basket", "attributes": map[string]any{notes: "Checked"}}),
		http.StatusCreated, "write "+s.key+"-R1")
	path := "/api/v1/projects/" + s.key + "/export.csv?column=key&column=" + notes

	refuse(t, "somebody who does not view the project", s.f.outside.get(t, path), http.StatusNotFound, "not_found")
	exported := want(t, owner.get(t, path), http.StatusOK, "export the attribute by its id")
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(exported.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatalf("the export is not CSV: %v", err)
	}
	if strings.Join(records[0], ",") != "key,attr:Notes,outline,parent" || len(records) != 2 || records[1][1] != "Checked" {
		t.Fatalf("the export: %v", records)
	}

	records[1][1] = "Checked twice"
	var file bytes.Buffer
	_ = csv.NewWriter(&file).WriteAll(records)
	made := obj(t, want(t, uploadCSV(t, owner, s.key, "notes.csv", file.Bytes()), http.StatusCreated, "upload the changed file"), "import")
	if cols := made["mapping"].(map[string]any)["attr:Notes"]; fmt.Sprint(cols) != "[1]" {
		t.Fatalf("the attribute's column maps to %v: %v", cols, made["mapping"])
	}
	want(t, owner.post(t, s.importPath+made["id"].(string)+"/start", nil), http.StatusAccepted, "start it")
	if done := finished(t, owner, made["id"].(string)); done["state"] != "done" || len(done["updated"].([]any)) != 1 {
		t.Fatalf("the import: %v", done)
	}
	got := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.key+"-R1"), http.StatusOK, "read the requirement"), "requirement")
	if value := got["attributes"].(map[string]any)[notes]; value != "Checked twice" {
		t.Errorf("the import wrote Notes as %v: %v", value, got["attributes"])
	}
}

// padded is an XML part of a package: open and close around enough spaces to make it size bytes.
func padded(open, close string, size int) string {
	return open + strings.Repeat(" ", size-len(open)-len(close)) + close
}

// zipOf packs the parts into an archive, each as small as it compresses.
func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bombPictures is how many pictures a Word bomb shows, each within the limit of one part and of one picture.
const bombPictures = 5

// wordBomb is a Word document whose pictures are each within their limit and together unpack past what a document
// may. Pictures are read whole without being parsed, so the server spends its time on the limit alone.
func wordBomb(t *testing.T) []byte {
	t.Helper()
	size := docx.MaxPartBytes - 1
	if bombPictures*size <= docx.MaxUnpackedBytes {
		t.Fatalf("the pictures unpack into %d bytes, within the limit", bombPictures*size)
	}
	blank := make([]byte, size)
	var packed bytes.Buffer
	fw, err := flate.NewWriter(&packed, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(blank)
	_ = fw.Close()
	sum := crc32.ChecksumIEEE(blank)

	const drawing = `xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	var body, rels strings.Builder
	for i := 1; i <= bombPictures; i++ {
		fmt.Fprintf(&body, `<w:p><w:r><w:drawing><wp:inline><wp:docPr id="%d" name="Picture %d"/><a:graphic><a:graphicData><pic:pic><pic:blipFill>`+
			`<a:blip r:embed="rIdPic%d"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`, i, i, i)
		fmt.Fprintf(&rels, `<Relationship Id="rIdPic%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image%d.png"/>`, i, i)
	}
	const relsNS = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/>` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="png" ContentType="image/png"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": relsNS + `<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" ` +
			`Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": relsNS + rels.String() + `</Relationships>`,
		"word/document.xml":            `<w:document ` + wordNS + ` ` + drawing + `><w:body>` + body.String() + `<w:p><w:r><w:t>{{content}}</w:t></w:r></w:p></w:body></w:document>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(data))
	}
	for i := 1; i <= bombPictures; i++ {
		w, err := zw.CreateRaw(&zip.FileHeader{Name: fmt.Sprintf("word/media/image%d.png", i), Method: zip.Deflate, CRC32: sum,
			CompressedSize64: uint64(packed.Len()), UncompressedSize64: uint64(size)})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(packed.Bytes())
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A file small to upload and large to unpack is refused, naming it, and nothing is kept (#318).
func TestAnArchiveThatUnpacksTooFarIsRefused(t *testing.T) {
	s := newImportScene(t, "IZB")
	owner := s.f.owner
	imports := func() int {
		return s.f.h.countRows(t, `SELECT count(*) FROM reqif_import WHERE project_id = $1`, s.projectID)
	}

	t.Run("a .reqifz whose document unpacks past its limit", func(t *testing.T) {
		bomb := zipOf(t, map[string]string{"big.reqif": padded(`<?xml version="1.0"?><REQ-IF>`, `</REQ-IF>`, reqifio.MaxDocumentBytes+1)})
		if len(bomb) > reqifio.MaxImportBytes/100 {
			t.Fatalf("the bomb is %d bytes to upload", len(bomb))
		}
		msg := refuse422(t, "the bomb", owner.upload(t, "/api/v1/projects/"+s.key+"/reqif-imports", "bomb.reqifz", bomb), "file")
		if !strings.Contains(msg, "big.reqif unpacks into more than 32 MB") {
			t.Errorf("the refusal reads %q", msg)
		}
		if n := imports(); n != 0 {
			t.Errorf("%d imports kept", n)
		}
	})

	bomb := wordBomb(t)
	t.Run("a Word document whose parts together unpack past the limit", func(t *testing.T) {
		msg := refuse422(t, "the bomb", owner.uploadWord(t, s.key, "bomb.docx", bomb, ""), "file")
		if !strings.Contains(msg, "unpacks into more than 128 MB") {
			t.Errorf("the refusal reads %q", msg)
		}
		if n := imports(); n != 0 {
			t.Errorf("%d imports kept", n)
		}
	})

	t.Run("a template whose parts together unpack past the limit", func(t *testing.T) {
		refuse(t, "the bomb", owner.uploadTemplate(t, s.key, "Bomb", "bomb.docx", bomb), http.StatusRequestEntityTooLarge, "too_large")
		if n := s.f.h.countRows(t, `SELECT count(*) FROM export_template WHERE project_id = $1`, s.projectID); n != 0 {
			t.Errorf("%d templates kept", n)
		}
	})
}

// A shared template is read outside its project and changed by nobody there, and its fields are refused when one
// comes twice or there are too many (#318).
func TestASharedTemplateIsChangedOnlyInItsProject(t *testing.T) {
	s := newImportScene(t, "ITF")
	owner := s.f.owner
	made := obj(t, want(t, owner.uploadTemplate(t, s.key, "Supplier", "supplier.docx", aWordTemplate(t, "", nil)), http.StatusCreated, "upload a template"), "template")
	path := "/api/v1/export-templates/" + made["id"].(string)
	want(t, owner.patch(t, path, map[string]any{"shared": true, "fields": []any{map[string]any{"field": "status"}}}), http.StatusOK, "share it")
	before := string(want(t, owner.get(t, path), http.StatusOK, "read it").Raw)

	want(t, s.f.outside.get(t, path), http.StatusOK, "somebody outside reads it shared")
	refuse(t, "somebody outside renames it", s.f.outside.patch(t, path, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
	refuse(t, "somebody outside unshares it", s.f.outside.patch(t, path, map[string]any{"shared": false}), http.StatusForbidden, "forbidden")
	twice := []any{map[string]any{"field": "owner"}, map[string]any{"field": "owner"}}
	if msg := refuse422(t, "the same field twice", owner.patch(t, path, map[string]any{"fields": twice}), "fields"); msg != "Show each field once." {
		t.Errorf("the refusal reads %q", msg)
	}
	tooMany := make([]any, exporttemplate.MaxFields+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"field": "attribute", "attribute": fmt.Sprintf("Attribute %d", i)}
	}
	if msg := refuse422(t, "too many fields", owner.patch(t, path, map[string]any{"fields": tooMany}), "fields"); !strings.Contains(msg, "at most 60 fields") {
		t.Errorf("the refusal reads %q", msg)
	}
	if after := string(want(t, owner.get(t, path), http.StatusOK, "read it again").Raw); after != before {
		t.Errorf("the refusals changed the template:\n%s\n%s", before, after)
	}
}
