//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// finishedReqIF waits for the worker to end a ReqIF import, and answers it.
func finishedReqIF(t *testing.T, c *client, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(importWait)
	for {
		got := obj(t, want(t, c.get(t, "/api/v1/reqif-imports/"+id), http.StatusOK, "read the import"), "import")
		if state := got["state"]; state == "done" || state == "failed" || state == "cancelled" {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the import is still %v after %s: %v", got["state"], importWait, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// importReqIF uploads a file, starts it without problems, and answers what the worker did.
func importReqIF(t *testing.T, c *client, projectKey, name string, data []byte) map[string]any {
	t.Helper()
	made := obj(t, want(t, c.upload(t, "/api/v1/projects/"+projectKey+"/reqif-imports", name, data), http.StatusCreated, "upload "+name), "import")
	if problems := made["preview"].(map[string]any)["problems"].([]any); len(problems) > 0 {
		t.Fatalf("%s has problems: %v", name, problems)
	}
	id := made["id"].(string)
	want(t, c.post(t, "/api/v1/reqif-imports/"+id+"/start", nil), http.StatusAccepted, "start "+name)
	done := finishedReqIF(t, c, id)
	if done["state"] != "done" || len(done["refused"].([]any)) != 0 {
		t.Fatalf("the import of %s: %v", name, done)
	}
	return done
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func supplierFile(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "reqifio", "testdata", "supplier.reqif"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// attributesByName are a project's attributes by name, each as the API answers it.
func attributesByName(t *testing.T, c *client, projectKey string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+projectKey+"/attributes"), http.StatusOK, "read the attributes"), "attributes") {
		a := each.(map[string]any)
		out[a["name"].(string)] = a
	}
	return out
}

// unzipped are the entries of an archive by name.
func unzipped(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the export is no zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	return out
}

// A project with every kind of attribute, a section, rich text with a table and a picture, and a link goes out as
// a .reqifz and into an empty project, which then holds the same tree, content, files and links.
func TestReqIFExportThenImportMakesTheSameProject(t *testing.T) {
	fromKey, toKey := projectKey(t, "RQA"), projectKey(t, "RQB")
	f := newProjectFixture(t)
	owner := f.owner
	for _, key := range []string{fromKey, toKey} {
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	}
	iface := obj(t, want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/types", map[string]any{"name": "Interface", "icon": "interface", "colour": "#aa00ff"}), http.StatusCreated, "a type"), "type")
	attr := func(body map[string]any) map[string]any {
		return obj(t, want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/attributes", body), http.StatusCreated, "make "+body["name"].(string)), "attribute")
	}
	risk := attr(map[string]any{"name": "Risk", "kind": "select", "options": []any{map[string]any{"name": "Low"}, map[string]any{"name": "High"}}})
	tags := attr(map[string]any{"name": "Tags", "kind": "multiSelect", "options": []any{map[string]any{"name": "Truck"}, map[string]any{"name": "Bus"}}})
	weight, due, safe := attr(map[string]any{"name": "Weight", "kind": "number"}), attr(map[string]any{"name": "Due", "kind": "date"}), attr(map[string]any{"name": "Safe", "kind": "boolean"})
	notes, detail := attr(map[string]any{"name": "Notes", "kind": "text"}), attr(map[string]any{"name": "Detail", "kind": "richText"})
	ref, reviewer := attr(map[string]any{"name": "Reference", "kind": "url"}), attr(map[string]any{"name": "Reviewer", "kind": "user"})

	section := obj(t, want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/requirements", map[string]any{"kind": "section", "title": "Braking"}),
		http.StatusCreated, "write a section"), "requirement")
	want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/requirements", map[string]any{
		"title": "Stop within 40 m", "statement": statementSaying("The vehicle stops."), "typeId": iface["id"], "priority": "must",
		"place": map[string]any{"parentId": section["id"], "afterId": nil}, "labels": []string{"brakes"}, "ownerId": owner.user.String(),
		"attributes": map[string]any{
			risk["id"].(string): optionID(t, risk, "High"), tags["id"].(string): []string{optionID(t, tags, "Truck"), optionID(t, tags, "Bus")},
			weight["id"].(string): 12.5, due["id"].(string): "2026-12-01", safe["id"].(string): true, notes["id"].(string): "Dry road",
			detail["id"].(string): statementSaying("Measured on asphalt."), ref["id"].(string): "https://example.com/brakes",
			reviewer["id"].(string): owner.user.String(),
		},
	}), http.StatusCreated, "write "+fromKey+"-R2")
	picture := obj(t, want(t, owner.uploadAs(t, "/api/v1/requirements/"+fromKey+"-R2/attachments", "curve.png", "image/png", aPicture(t)),
		http.StatusCreated, "attach a picture"), "attachment")
	table := map[string]any{"type": "table", "content": []any{map[string]any{"type": "tableRow", "content": []any{
		map[string]any{"type": "tableHeader", "content": []any{statementSaying("Surface")["content"].([]any)[0]}},
		map[string]any{"type": "tableCell", "content": []any{statementSaying("Dry")["content"].([]any)[0]}},
	}}}}
	statement := map[string]any{"type": "doc", "content": []any{statementSaying("The vehicle stops.")["content"].([]any)[0], table,
		map[string]any{"type": "image", "attrs": map[string]any{"attachmentId": picture["id"], "alt": "Braking curve"}}}}
	want(t, owner.patch(t, "/api/v1/requirements/"+fromKey+"-R2", map[string]any{"statement": statement, "baseRevision": 1}), http.StatusOK, "show the picture")
	want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/requirements", map[string]any{"title": "Warn the driver", "statement": statementSaying("A tone sounds.")}),
		http.StatusCreated, "write "+fromKey+"-R3")
	want(t, owner.post(t, "/api/v1/requirements/"+fromKey+"-R3/links", map[string]any{"typeKey": "derives", "otherKey": fromKey + "-R2"}), http.StatusCreated, "link them")

	plain := want(t, f.plain.get(t, "/api/v1/projects/"+fromKey+"/export.reqif"), http.StatusOK, "a member exports the document")
	if !strings.Contains(plain.Header.Get("Content-Disposition"), fromKey+".reqif") || !bytes.Contains(plain.Raw, []byte("<REQ-IF")) || bytes.Contains(plain.Raw, []byte("xhtml:object")) {
		t.Errorf("the plain export is a document without files: %v", plain.Header)
	}
	exported := want(t, f.plain.get(t, "/api/v1/projects/"+fromKey+"/export.reqifz"), http.StatusOK, "a member exports the archive")
	entries := unzipped(t, exported.Raw)
	if len(entries) != 2 || entries["files/"+picture["id"].(string)+".png"] == nil || entries[fromKey+".reqif"] == nil {
		t.Fatalf("the archive holds %d entries", len(entries))
	}

	done := importReqIF(t, owner, toKey, fromKey+".reqifz", exported.Raw)
	if len(done["imported"].([]any)) != 3 {
		t.Errorf("the import made %v", done["imported"])
	}
	sameList(t, "the tree", treeOf(t, owner, toKey), "1 section Braking", "1.1 requirement Stop within 40 m", "2 requirement Warn the driver")
	copied := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+toKey+"-R2"), http.StatusOK, "read the copy"), "requirement")
	if copied["type"].(map[string]any)["name"] != "Interface" || copied["priority"] != "must" || toStrings(copied["labels"])[0] != "brakes" ||
		copied["owner"].(map[string]any)["id"] != owner.user.String() {
		t.Errorf("the imported requirement: %v", copied)
	}
	there := attributesByName(t, owner, toKey)
	values := copied["attributes"].(map[string]any)
	value := func(name string) any { return values[there[name]["id"].(string)] }
	if value("Risk") != optionID(t, there["Risk"], "High") || len(value("Tags").([]any)) != 2 || value("Weight") != 12.5 || value("Due") != "2026-12-01" ||
		value("Safe") != true || value("Notes") != "Dry road" || value("Reference") != "https://example.com/brakes" || value("Reviewer") != owner.user.String() {
		t.Errorf("the attributes came back as %v", values)
	}
	if !strings.Contains(string(mustJSON(t, value("Detail"))), "Measured on asphalt.") {
		t.Errorf("the rich text attribute came back as %v", value("Detail"))
	}
	files := list(t, want(t, owner.get(t, "/api/v1/requirements/"+toKey+"-R2/attachments"), http.StatusOK, "read its files"), "attachments")
	body := string(mustJSON(t, copied["statement"]))
	if len(files) != 1 || !strings.Contains(body, files[0].(map[string]any)["id"].(string)) || !strings.Contains(body, `"tableHeader"`) {
		t.Errorf("the statement %s shows the files %v", body, files)
	}
	links := list(t, want(t, owner.get(t, "/api/v1/requirements/"+toKey+"-R3/links"), http.StatusOK, "read its links"), "links")
	if len(links) != 1 || links[0].(map[string]any)["other"].(map[string]any)["key"] != toKey+"-R2" {
		t.Errorf("the link points into the new project: %v", links)
	}
	refuse(t, "a section the project lacks", owner.get(t, "/api/v1/projects/"+fromKey+"/export.reqifz?section="+fromKey+"-R99"), http.StatusNotFound, "not_found")
	refuse(t, "somebody who does not view the project", f.outside.get(t, "/api/v1/projects/"+fromKey+"/export.reqif"), http.StatusNotFound, "not_found")
	section2 := want(t, owner.get(t, "/api/v1/projects/"+fromKey+"/export.reqif?section="+fromKey+"-R1"), http.StatusOK, "export the section")
	if bytes.Contains(section2.Raw, []byte("Warn the driver")) || !bytes.Contains(section2.Raw, []byte("Stop within 40 m")) {
		t.Errorf("the section's export holds what is below it alone")
	}
}

var objectIdentifier = regexp.MustCompile(`<SPEC-OBJECT IDENTIFIER="([^"]+)"`)

// A file that went out and comes back changes what it made instead of making it again, and the next export
// writes the identifiers the file came with.
func TestReqIFReimportUpdatesByIdentifier(t *testing.T) {
	ownKey, supKey := projectKey(t, "RQC"), projectKey(t, "RQD")
	f := newProjectFixture(t)
	owner := f.owner
	for _, key := range []string{ownKey, supKey} {
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	}
	for _, title := range []string{"Old title", "Stays"} {
		want(t, owner.post(t, "/api/v1/projects/"+ownKey+"/requirements", map[string]any{"title": title, "statement": statementSaying(title + ".")}),
			http.StatusCreated, "write "+title)
	}
	first := want(t, owner.get(t, "/api/v1/projects/"+ownKey+"/export.reqif"), http.StatusOK, "export").Raw
	changed := bytes.Replace(first, []byte(`THE-VALUE="Old title"`), []byte(`THE-VALUE="New title"`), 1)
	done := importReqIF(t, owner, ownKey, "back.reqif", changed)
	if toStrings(done["updated"])[0] != ownKey+"-R1" || done["unchanged"] != float64(1) || len(done["imported"].([]any)) != 0 {
		t.Errorf("the import back: %v", done)
	}
	sameList(t, "the tree", treeOf(t, owner, ownKey), "1 requirement New title", "2 requirement Stays")
	r1 := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+ownKey+"-R1"), http.StatusOK, "read it"), "requirement")
	if r1["revision"] != float64(2) {
		t.Errorf("the change wrote revision %v", r1["revision"])
	}
	again := want(t, owner.get(t, "/api/v1/projects/"+ownKey+"/export.reqif"), http.StatusOK, "export again").Raw
	if a, b := objectIdentifier.FindAllSubmatch(first, -1), objectIdentifier.FindAllSubmatch(again, -1); len(a) != 2 || len(b) != 2 || string(a[0][1]) != string(b[0][1]) {
		t.Errorf("the identifiers moved between exports")
	}

	t.Run("a supplier's file keeps its identifiers both ways", func(t *testing.T) {
		importReqIF(t, owner, supKey, "supplier.reqif", supplierFile(t))
		sameList(t, "the tree", treeOf(t, owner, supKey), "1 section Brake controller SRS", "1.1 section Braking",
			"1.1.1 requirement Stop within 40 m", "1.1.2 requirement Warn the driver")
		exported := want(t, owner.get(t, "/api/v1/projects/"+supKey+"/export.reqif"), http.StatusOK, "export it").Raw
		for _, identifier := range []string{`IDENTIFIER="_o_r1"`, `IDENTIFIER="_t_req"`, `IDENTIFIER="_rt_satisfies"`, `IDENTIFIER="_a_risk"`, `IDENTIFIER="_risk_high"`} {
			if !bytes.Contains(exported, []byte(identifier)) {
				t.Errorf("the export lacks %s", identifier)
			}
		}
		made := obj(t, want(t, owner.upload(t, "/api/v1/projects/"+supKey+"/reqif-imports", "supplier.reqif", supplierFile(t)), http.StatusCreated, "upload it again"), "import")
		preview := made["preview"].(map[string]any)
		if preview["new"] != float64(0) || preview["updates"] != float64(4) {
			t.Fatalf("the file again: %v new and %v updates", preview["new"], preview["updates"])
		}
		want(t, owner.post(t, "/api/v1/reqif-imports/"+made["id"].(string)+"/start", nil), http.StatusAccepted, "start it")
		if again := finishedReqIF(t, owner, made["id"].(string)); again["unchanged"] != float64(4) || len(again["imported"].([]any)) != 0 {
			t.Errorf("the same file changed something: %v", again)
		}
		if n := len(treeOf(t, owner, supKey)); n != 4 {
			t.Errorf("the tree holds %d nodes after the same file twice", n)
		}
	})
}

// An import reports what does not fit before anything is written, takes choices until it starts, and refuses
// whoever does not administer the project.
func TestReqIFImportAsksBeforeItWrites(t *testing.T) {
	key := projectKey(t, "RQE")
	f := newProjectFixture(t)
	owner := f.owner
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	want(t, owner.post(t, "/api/v1/projects/"+key+"/attributes", map[string]any{"name": "Weight", "kind": "text"}), http.StatusCreated, "a text attribute named Weight")
	path := "/api/v1/projects/" + key + "/reqif-imports"
	refuse(t, "a member imports", f.plain.upload(t, path, "s.reqif", supplierFile(t)), http.StatusForbidden, "forbidden")
	refuse(t, "a file that is no ReqIF", owner.upload(t, path, "s.reqif", []byte("<html/>")), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a file too large", owner.upload(t, path, "s.reqif", make([]byte, 64<<20+1)), http.StatusRequestEntityTooLarge, "too_large")

	made := obj(t, want(t, owner.upload(t, path, "s.reqif", supplierFile(t)), http.StatusCreated, "upload it"), "import")
	id := made["id"].(string)
	if problems := made["preview"].(map[string]any)["problems"].([]any); len(problems) != 1 || !strings.Contains(problems[0].(map[string]any)["message"].(string), "Weight") {
		t.Fatalf("the text Weight is the one problem: %v", problems)
	}
	refuse(t, "a start with problems", owner.post(t, "/api/v1/reqif-imports/"+id+"/start", nil), http.StatusConflict, "import_problems")
	refuse(t, "somebody else's import", f.plain.get(t, "/api/v1/reqif-imports/"+id), http.StatusNotFound, "not_found")
	refuse(t, "somebody else maps it", f.plain.put(t, "/api/v1/reqif-imports/"+id+"/choices", map[string]any{"choices": map[string]any{}}), http.StatusNotFound, "not_found")
	chosen := obj(t, want(t, owner.put(t, "/api/v1/reqif-imports/"+id+"/choices", map[string]any{"choices": map[string]any{"_a_weight": "skip"}}),
		http.StatusOK, "leave Weight out"), "import")
	if problems := chosen["preview"].(map[string]any)["problems"].([]any); len(problems) != 0 {
		t.Fatalf("with Weight left out, problems remain: %v", problems)
	}
	want(t, owner.post(t, "/api/v1/reqif-imports/"+id+"/start", nil), http.StatusAccepted, "start it")
	refuse(t, "choices after the start", owner.put(t, "/api/v1/reqif-imports/"+id+"/choices", map[string]any{"choices": map[string]any{}}), http.StatusConflict, "conflict")
	if done := finishedReqIF(t, owner, id); done["state"] != "done" || len(done["imported"].([]any)) != 4 {
		t.Errorf("the import: %v", done)
	}
	discarded := obj(t, want(t, owner.upload(t, path, "s.reqif", supplierFile(t)), http.StatusCreated, "upload again"), "import")["id"].(string)
	want(t, owner.delete(t, "/api/v1/reqif-imports/"+discarded), http.StatusNoContent, "discard it")
	refuse(t, "a discarded import", owner.delete(t, "/api/v1/reqif-imports/"+discarded), http.StatusNotFound, "not_found")
}

// A project's glossary goes out as terms of their own type in a specification of their own, and an import of the
// file leaves it out with a loss, making no type and no requirement of it (#210).
func TestReqIFImportLeavesTheGlossaryOut(t *testing.T) {
	fromKey, toKey := projectKey(t, "RQG"), projectKey(t, "RQH")
	f := newProjectFixture(t)
	owner := f.owner
	for _, key := range []string{fromKey, toKey} {
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	}
	want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/terms", map[string]any{"name": "Brake pedal", "definition": docOf("The lever the driver presses.")}),
		http.StatusCreated, "define a term")
	want(t, owner.post(t, "/api/v1/projects/"+fromKey+"/requirements", map[string]any{"title": "Stop", "statement": statementSaying("The brake pedal stops the car.")}),
		http.StatusCreated, "write "+fromKey+"-R1")
	exported := want(t, owner.get(t, "/api/v1/projects/"+fromKey+"/export.reqif"), http.StatusOK, "export").Raw
	if !bytes.Contains(exported, []byte(`"exciter-glossary-term"`)) {
		t.Fatalf("the export carries no glossary")
	}
	made := obj(t, want(t, owner.upload(t, "/api/v1/projects/"+toKey+"/reqif-imports", fromKey+".reqif", exported), http.StatusCreated, "upload it"), "import")
	preview := made["preview"].(map[string]any)
	var told bool
	for _, each := range preview["losses"].([]any) {
		if l := each.(map[string]any); l["message"] == "The glossary of the file is left out; terms are not imported." {
			told = true
		}
	}
	if !told || preview["new"] != float64(1) || strings.Contains(string(mustJSON(t, preview["schema"])), "exciter-glossary") {
		t.Errorf("the preview: %v", preview)
	}
	want(t, owner.post(t, "/api/v1/reqif-imports/"+made["id"].(string)+"/start", nil), http.StatusAccepted, "start it")
	if done := finishedReqIF(t, owner, made["id"].(string)); done["state"] != "done" || len(done["imported"].([]any)) != 1 {
		t.Fatalf("the import: %v", done)
	}
	sameList(t, "the tree", treeOf(t, owner, toKey), "1 requirement Stop")
	for _, each := range list(t, want(t, owner.get(t, "/api/v1/projects/"+toKey+"/types"), http.StatusOK, "its types"), "types") {
		if name := each.(map[string]any)["name"]; name == "Glossary term" {
			t.Errorf("the import made the type %v", name)
		}
	}
}
