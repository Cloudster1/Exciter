//go:build integration

package test

import (
	"bytes"
	"encoding/csv"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// importWait bounds how long a test waits for the stack's worker to run an import.
const importWait = 60 * time.Second

// uploadCSV sends a file to a project's import, as the web does.
func uploadCSV(t *testing.T, c *client, projectKey, name string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+"/api/v1/projects/"+projectKey+"/import", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return c.do(t, req)
}

// finished waits for the worker to end an import, and answers it.
func finished(t *testing.T, c *client, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(importWait)
	for {
		got := obj(t, want(t, c.get(t, "/api/v1/imports/"+id), http.StatusOK, "read the import"), "import")
		if state := got["state"]; state == "done" || state == "failed" || state == "cancelled" {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the import is still %v after %s: %v", got["state"], importWait, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// treeOf is a project's tree as outline number, kind and title, in reading order.
func treeOf(t *testing.T, c *client, projectKey string) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+projectKey+"/tree"), http.StatusOK, "read the tree"), "nodes") {
		n := each.(map[string]any)
		out = append(out, n["outline"].(string)+" "+n["kind"].(string)+" "+n["title"].(string))
	}
	return out
}

// A project goes out as a file and the file into an empty project, which then holds the same tree, content and
// links; the mapping reports what matches nothing before anything is written.
func TestExportThenImportMakesTheSameProject(t *testing.T) {
	f := newProjectFixture(t)
	owner := f.owner
	for _, key := range []string{"CSVA", "CSVB"} {
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	}
	section := obj(t, want(t, owner.post(t, "/api/v1/projects/CSVA/requirements", map[string]any{"kind": "section", "title": "Checkout"}),
		http.StatusCreated, "write a section"), "requirement")
	under := map[string]any{"parentId": section["id"], "afterId": nil}
	want(t, owner.post(t, "/api/v1/projects/CSVA/requirements", map[string]any{
		"title": "Pay the basket", "statement": statementSaying("The shopper pays at once."), "place": under, "priority": "must",
		"labels": []string{"checkout"}, "ownerId": owner.user.String(),
	}), http.StatusCreated, "write CSVA-R2")
	want(t, owner.post(t, "/api/v1/projects/CSVA/requirements", map[string]any{"title": "=Refund", "statement": statementSaying("Money goes back.")}),
		http.StatusCreated, "write CSVA-R3, a title a spreadsheet would run")
	want(t, owner.post(t, "/api/v1/requirements/CSVA-R3/links", map[string]any{"typeKey": "derives", "otherKey": "CSVA-R2"}), http.StatusCreated, "link them")

	exported := want(t, f.plain.get(t, "/api/v1/projects/CSVA/export.csv"), http.StatusOK, "a member exports")
	if !strings.HasPrefix(exported.Header.Get("Content-Disposition"), "attachment") || exported.Header.Get("X-Export-Truncated") != "" {
		t.Errorf("the export is a whole download: %v", exported.Header)
	}
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(exported.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatalf("the export is not CSV: %v\n%s", err, exported.Raw)
	}
	if strings.Join(records[0], ",") != "key,kind,title,statement,rationale,type,status,owner,priority,labels,links,outline,parent" || len(records) != 4 {
		t.Fatalf("the export: %v", records)
	}
	if records[3][2] != "'=Refund" || records[3][10] != "derives:CSVA-R2" || records[2][12] != "CSVA-R1" || records[2][7] != owner.email(t) {
		t.Errorf("the rows: %v", records)
	}
	refuse(t, "a column nobody has", owner.get(t, "/api/v1/projects/CSVA/export.csv?column=nope"), http.StatusUnprocessableEntity, "validation_failed")

	t.Run("the file imports into an empty project as the same project", func(t *testing.T) {
		made := obj(t, want(t, uploadCSV(t, owner, "CSVB", "csva.csv", exported.Raw), http.StatusCreated, "upload it"), "import")
		if made["state"] != "uploaded" || made["total"] != float64(3) || len(made["preview"].(map[string]any)["problems"].([]any)) != 0 {
			t.Fatalf("the upload maps itself without problems: %v", made)
		}
		id := made["id"].(string)
		want(t, owner.post(t, "/api/v1/imports/"+id+"/start", nil), http.StatusAccepted, "start it")
		done := finished(t, owner, id)
		if done["state"] != "done" || len(done["imported"].([]any)) != 3 || len(done["refused"].([]any)) != 0 {
			t.Fatalf("the import: %v", done)
		}
		sameList(t, "the tree", treeOf(t, owner, "CSVB"), "1 section Checkout", "1.1 requirement Pay the basket", "2 requirement =Refund")
		paid := obj(t, want(t, owner.get(t, "/api/v1/requirements/CSVB-R2"), http.StatusOK, "read the copy"), "requirement")
		if paid["title"] != "Pay the basket" || paid["priority"] != "must" || toStrings(paid["labels"])[0] != "checkout" ||
			paid["owner"].(map[string]any)["id"] != owner.user.String() || paid["revision"] != float64(1) {
			t.Errorf("the imported requirement: %v", paid)
		}
		if text := paid["statement"].(map[string]any)["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; text != "The shopper pays at once." {
			t.Errorf("the statement came back as %v", text)
		}
		links := list(t, want(t, owner.get(t, "/api/v1/requirements/CSVB-R3/links"), http.StatusOK, "read its links"), "links")
		if len(links) != 1 || links[0].(map[string]any)["other"].(map[string]any)["key"] != "CSVB-R2" {
			t.Errorf("the link points into the new project: %v", links)
		}
	})

	t.Run("what matches nothing is reported, and the import waits for it", func(t *testing.T) {
		file := []byte("title,type,owner\nShip it,Feature,nobody@exciter.test\n")
		refuse(t, "a member imports", uploadCSV(t, f.plain, "CSVB", "x.csv", file), http.StatusForbidden, "forbidden")
		refuse(t, "a file that is no CSV", uploadCSV(t, owner, "CSVB", "x.csv", []byte("a,b\n\"x,y")), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a file too large", uploadCSV(t, owner, "CSVB", "x.csv", make([]byte, 8<<20+1)), http.StatusRequestEntityTooLarge, "too_large")
		made := obj(t, want(t, uploadCSV(t, owner, "CSVB", "x.csv", file), http.StatusCreated, "upload it"), "import")
		id := made["id"].(string)
		if n := len(made["preview"].(map[string]any)["problems"].([]any)); n != 2 {
			t.Fatalf("an unknown type and person are %d problems: %v", n, made["preview"])
		}
		refuse(t, "a start with problems", owner.post(t, "/api/v1/imports/"+id+"/start", nil), http.StatusConflict, "import_problems")
		refuse(t, "somebody else's import", f.plain.get(t, "/api/v1/imports/"+id), http.StatusNotFound, "not_found")
		mapped := obj(t, want(t, owner.put(t, "/api/v1/imports/"+id+"/mapping", map[string]any{
			"mapping": made["mapping"], "values": map[string]any{"type": map[string]any{"Feature": "Requirement"}},
			"people": map[string]any{"nobody@exciter.test": map[string]any{}},
		}), http.StatusOK, "answer the word and the person"), "import")
		if n := len(mapped["preview"].(map[string]any)["problems"].([]any)); n != 0 {
			t.Fatalf("answered, %d problems remain: %v", n, mapped["preview"])
		}
		want(t, owner.post(t, "/api/v1/imports/"+id+"/start", nil), http.StatusAccepted, "start it")
		refuse(t, "a mapping after the start", owner.put(t, "/api/v1/imports/"+id+"/mapping", map[string]any{"mapping": made["mapping"]}),
			http.StatusConflict, "conflict")
		if done := finished(t, owner, id); done["state"] != "done" || len(done["imported"].([]any)) != 1 {
			t.Errorf("the answered import: %v", done)
		}
		discarded := obj(t, want(t, uploadCSV(t, owner, "CSVB", "x.csv", file), http.StatusCreated, "upload again"), "import")["id"].(string)
		want(t, owner.delete(t, "/api/v1/imports/"+discarded), http.StatusNoContent, "discard it")
		refuse(t, "a discarded import", owner.delete(t, "/api/v1/imports/"+discarded), http.StatusNotFound, "not_found")
	})
}

// email is the address of the person a client acts for.
func (c *client) email(t *testing.T) string {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/auth/me"), http.StatusOK, "who am I"), "user")["email"].(string)
}
