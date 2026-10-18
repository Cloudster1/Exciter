//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

// aWordTemplate is a Word file whose body is body around a header naming the project; extra adds or replaces parts.
func aWordTemplate(t *testing.T, body string, extra map[string]string) []byte {
	t.Helper()
	if body == "" {
		body = `<w:p><w:r><w:t>Cover of {{ti</w:t></w:r><w:r><w:t>tle}} on {{date}}</w:t></w:r></w:p><w:p><w:r><w:t>{{content}}</w:t></w:r></w:p>`
	}
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0"?><w:document ` + wordNS + `><w:body>` + body +
			`<w:sectPr><w:headerReference w:type="default" r:id="rId7"/><w:pgSz w:w="11906" w:h="16838"/>` +
			`<w:pgMar w:top="1417" w:right="1134" w:bottom="1134" w:left="1417"/></w:sectPr></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/></Relationships>`,
		"word/header1.xml": `<?xml version="1.0"?><w:hdr ` + wordNS + `><w:p><w:r><w:t>Header of {{project}}</w:t></w:r></w:p></w:hdr>`,
	}
	for name, data := range extra {
		files[name] = data
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (c *client) uploadTemplate(t *testing.T, projectKey, name, fileName string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("name", name)
	part, err := form.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+"/api/v1/projects/"+projectKey+"/export-templates", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return c.do(t, req)
}

// A template is uploaded by the project's administrators and refused with what is wrong; it is read by the
// project's readers, and by everybody once shared; changing and deleting it is audited, and its file reaped.
func TestATemplateIsUploadedCheckedSharedAndDeleted(t *testing.T) {
	s := newDocumentScene(t)
	owner := s.f.owner
	data := aWordTemplate(t, "", nil)
	made := obj(t, want(t, owner.uploadTemplate(t, s.key, "Supplier", "supplier.docx", data), http.StatusCreated, "upload a template"), "template")
	id := made["id"].(string)
	if made["fileName"] != "supplier.docx" || made["include"].(map[string]any)["links"] != true || len(made["placeholders"].([]any)) != 4 {
		t.Errorf("the template made: %v", made)
	}

	refuse(t, "a member who does not administer", s.f.plain.uploadTemplate(t, s.key, "Mine", "mine.docx", data), http.StatusForbidden, "forbidden")
	refuse422(t, "a name taken", owner.uploadTemplate(t, s.key, "supplier", "again.docx", data), "name")
	for what, file := range map[string][]byte{
		"holds macros":           aWordTemplate(t, "", map[string]string{"word/vbaProject.bin": "x"}),
		"Put {{content}}":        aWordTemplate(t, `<w:p><w:r><w:t>No place</w:t></w:r></w:p>`, nil),
		"is not a placeholder":   aWordTemplate(t, `<w:p><w:r><w:t>{{projekt}}</w:t></w:r></w:p><w:p><w:r><w:t>{{content}}</w:t></w:r></w:p>`, nil),
		"is not a Word document": []byte("plain text"),
	} {
		if msg := refuse422(t, what, owner.uploadTemplate(t, s.key, "Bad "+what, "bad.docx", file), "file"); !strings.Contains(msg, what) {
			t.Errorf("the refusal of a file that %s reads %q", what, msg)
		}
	}
	refuse(t, "a file too large", owner.uploadTemplate(t, s.key, "Huge", "huge.docx", bytes.Repeat([]byte("x"), 21<<20)), http.StatusRequestEntityTooLarge, "too_large")

	listed := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/export-templates"), http.StatusOK, "a member lists the templates")
	if got := listed.Body["templates"].([]any); len(got) != 1 || got[0].(map[string]any)["can"].(map[string]any)["edit"] != false {
		t.Errorf("a member reads %v", got)
	}
	refuse(t, "somebody outside the project", s.f.outside.get(t, "/api/v1/export-templates/"+id), http.StatusNotFound, "not_found")
	refuse(t, "somebody outside lists them", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/export-templates"), http.StatusNotFound, "not_found")
	refuse(t, "a member changes it", s.f.plain.patch(t, "/api/v1/export-templates/"+id, map[string]any{"shared": true}), http.StatusForbidden, "forbidden")
	refuse422(t, "an unknown field", owner.patch(t, "/api/v1/export-templates/"+id, map[string]any{"fields": []any{map[string]any{"field": "colour"}}}), "fields")
	changed := obj(t, want(t, owner.patch(t, "/api/v1/export-templates/"+id, map[string]any{
		"shared": true, "fields": []any{map[string]any{"field": "status"}, map[string]any{"field": "attribute", "attribute": "risk"}},
		"include": map[string]any{"links": false, "comments": true, "changeMarks": true},
	}), http.StatusOK, "share it and choose its fields"), "template")
	if changed["shared"] != true || len(changed["fields"].([]any)) != 2 {
		t.Errorf("the template changed: %v", changed)
	}
	want(t, s.f.outside.get(t, "/api/v1/export-templates/"+id), http.StatusOK, "somebody outside reads it once shared")
	file := want(t, s.f.outside.get(t, "/api/v1/export-templates/"+id+"/file"), http.StatusOK, "and downloads its file")
	if !bytes.Equal(file.Raw, data) {
		t.Errorf("the file came back as %d bytes, want %d", len(file.Raw), len(data))
	}
	refuse(t, "a template that is not", owner.get(t, "/api/v1/export-templates/"+uuid.NewString()+"/file"), http.StatusNotFound, "not_found")
	refuse(t, "somebody outside deletes it", s.f.outside.delete(t, "/api/v1/export-templates/"+id), http.StatusForbidden, "forbidden")

	templateID := uuid.MustParse(id)
	audited := func(action string) int {
		return s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, templateID)
	}
	if audited("export_template.uploaded") != 1 || audited("export_template.changed") != 1 {
		t.Errorf("%d uploads and %d changes audited, want 1 each", audited("export_template.uploaded"), audited("export_template.changed"))
	}
	var key string
	if err := s.f.h.super.QueryRow(context.Background(), `SELECT object_key FROM export_template WHERE id = $1`, templateID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	want(t, owner.delete(t, "/api/v1/export-templates/"+id), http.StatusNoContent, "delete it")
	if n := audited("export_template.deleted"); n != 1 {
		t.Errorf("%d audit rows of the deletion, want 1", n)
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, key); n != 1 {
		t.Errorf("the file %s is left for the reaper %d times, want 1", key, n)
	}
	refuse(t, "a template deleted", owner.get(t, "/api/v1/export-templates/"+id), http.StatusNotFound, "not_found")
}

// A Word export through a template takes its frame and fills its placeholders, shows the fields it names and marks
// what changed since a baseline; a template another project keeps to itself, or a baseline of another, is refused.
func TestAWordExportGoesThroughATemplate(t *testing.T) {
	s := newDocumentScene(t)
	owner := s.f.owner
	made := obj(t, want(t, owner.uploadTemplate(t, s.key, "Supplier", "supplier.docx", aWordTemplate(t, "", nil)), http.StatusCreated, "upload a template"), "template")
	id := made["id"].(string)
	want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R3", map[string]any{"statement": statementSaying("A loud tone sounds."), "baseRevision": 2}),
		http.StatusOK, "change R3's statement after the baseline")
	want(t, owner.patch(t, "/api/v1/export-templates/"+id, map[string]any{
		"fields":  []any{map[string]any{"field": "status"}, map[string]any{"field": "attribute", "attribute": "risk"}, map[string]any{"field": "revision"}},
		"include": map[string]any{"links": false, "comments": false, "changeMarks": true},
	}), http.StatusOK, "choose its fields and marks")

	got := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document.docx?template="+id+"&since="+s.baseline), http.StatusOK, "a member exports through it")
	parts := unzipped(t, got.Raw)
	body := string(parts["word/document.xml"])
	for _, want := range []string{"Cover of Brakes on ", "Stop within 40 m", "High", "<w:ins ", "loud"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Word document lacks %q", want)
		}
	}
	for _, not := range []string{"{{", "The law says so.", "Measured on asphalt.", "Specification of Brakes"} {
		if strings.Contains(body, not) {
			t.Errorf("the Word document holds %q", not)
		}
	}
	if header := string(parts["word/header1.xml"]); !strings.Contains(header, "Header of Brakes ("+s.key+")") {
		t.Errorf("the header reads %s", header)
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'specification.exported' AND data->>'project' = $1 AND data->>'template' = 'Supplier'`, s.key); n != 1 {
		t.Errorf("%d exports recorded through the template, want 1", n)
	}
	doc := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/document?template="+id+"&since="+s.baseline), http.StatusOK, "read the document shaped"), "document")
	if r2 := entryOf(t, doc, s.key+"-R2"); len(r2["properties"].([]any)) != 3 || r2["rationale"] != nil || len(r2["links"].([]any)) != 0 {
		t.Errorf("R2 shaped: %v", r2)
	}
	if r3 := entryOf(t, doc, s.key+"-R3"); r3["changes"] == nil || r3["changedFrom"] != float64(1) || doc["changesSince"] != "Release 1" {
		t.Errorf("R3 marked: %v, since %v", r3, doc["changesSince"])
	}
	baseline := want(t, owner.get(t, "/api/v1/baselines/"+s.baseline+"/document.docx?template="+id), http.StatusOK, "export the baseline through it")
	if !bytes.Contains(unzipped(t, baseline.Raw)["word/document.xml"], []byte("Cover of Release 1")) {
		t.Errorf("the baseline's cover is not the template's")
	}

	other := projectKey(t, "TPL")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Elsewhere"}), http.StatusCreated, "make another project")
	theirs := obj(t, want(t, owner.uploadTemplate(t, other, "Theirs", "theirs.docx", aWordTemplate(t, "", nil)), http.StatusCreated, "upload one there"), "template")
	refuse422(t, "a template another project keeps", owner.get(t, "/api/v1/projects/"+s.key+"/document.docx?template="+theirs["id"].(string)), "template")
	refuse422(t, "a template that is no id", owner.get(t, "/api/v1/projects/"+s.key+"/document.docx?template=supplier"), "template")
	// A shared template is offered in every project, so it goes once the test is done.
	t.Cleanup(func() { owner.delete(t, "/api/v1/export-templates/"+theirs["id"].(string)) })
	want(t, owner.patch(t, "/api/v1/export-templates/"+theirs["id"].(string), map[string]any{"shared": true}), http.StatusOK, "share it")
	want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document.docx?template="+theirs["id"].(string)), http.StatusOK, "a shared one is offered everywhere")
	listed := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/export-templates"), http.StatusOK, "list them")
	offered := map[string]bool{}
	for _, each := range listed.Body["templates"].([]any) {
		offered[each.(map[string]any)["id"].(string)] = true
	}
	if !offered[id] || !offered[theirs["id"].(string)] {
		t.Errorf("the project offers %v, want its own and the shared one", offered)
	}
	theirBaseline := obj(t, want(t, owner.post(t, "/api/v1/projects/"+other+"/baselines", map[string]any{"name": "Theirs"}), http.StatusCreated, "take a baseline there"), "baseline")
	refuse422(t, "changes since another project's baseline", owner.get(t, "/api/v1/projects/"+s.key+"/document.docx?template="+id+"&since="+theirBaseline["id"].(string)), "since")
	session := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/review-sessions", map[string]any{
		"title": "Round", "scope": map[string]any{"kind": "section", "rootKey": s.key + "-R1"}, "deadline": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
		"participants": []map[string]any{{"subjectType": "user", "userId": owner.user, "role": "approver"}},
	}),
		http.StatusCreated, "open a review session"), "session")
	report := want(t, owner.get(t, "/api/v1/review-sessions/"+session["id"].(string)+"/report.docx?template="+id), http.StatusOK, "the session's report through it")
	if !bytes.Contains(unzipped(t, report.Raw)["word/header1.xml"], []byte("Header of Brakes")) {
		t.Errorf("the report's header is not the template's")
	}
}

// Straight through SQL as exciter_app: only a project's administrators make its templates, and a template its
// project keeps to itself is not seen from outside.
func TestRawSQLKeepsTemplatesToTheirProjects(t *testing.T) {
	s := newDocumentScene(t)
	made := obj(t, want(t, s.f.owner.uploadTemplate(t, s.key, "Kept", "kept.docx", aWordTemplate(t, "", nil)), http.StatusCreated, "upload a template"), "template")
	ctx := context.Background()
	var projectID uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, s.key).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	member := s.f.h.appConn(t, s.f.plain.user)
	if _, err := member.Exec(ctx, `INSERT INTO export_template (project_id, name, file_name, size, sha256, created_by)
		VALUES ($1, 'Mine', 'mine.docx', 1, repeat('a', 64), $2)`, projectID, s.f.plain.user); err == nil {
		t.Error("a member who does not administer made a template")
	}
	tag, err := member.Exec(ctx, `UPDATE export_template SET shared = true WHERE id = $1`, made["id"])
	if err == nil && tag.RowsAffected() != 0 {
		t.Error("a member shared the project's template")
	}
	outside := s.f.h.appConn(t, s.f.outside.user)
	var seen int
	if err := outside.QueryRow(ctx, `SELECT count(*)::int FROM export_template WHERE id = $1`, made["id"]).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("somebody outside sees %d of the project's own templates, %v", seen, err)
	}
	owner := s.f.h.appConn(t, s.f.owner.user)
	if _, err := owner.Exec(ctx, `UPDATE export_template SET object_key = 'elsewhere' WHERE id = $1`, made["id"]); err == nil {
		t.Error("an administrator pointed the template at another file")
	}
}

// The specification as a static site: the print view as the reader, cut into pages linked to each other, with
// its pictures beside them and a search, recorded as an export.
func TestADocumentDownloadsAsAnHTMLSite(t *testing.T) {
	s := newDocumentScene(t)
	if s.f.h.cfg.Render.URL == "" {
		t.Fatal("EXCITER_RENDER_URL is not set; run the suite with make test-integration against the running stack")
	}
	got := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document.html.zip"), http.StatusOK, "a member downloads the site")
	if got.Header.Get("Content-Type") != "application/zip" || !strings.Contains(got.Header.Get("Content-Disposition"), ".html.zip") {
		t.Errorf("the download: %v", got.Header)
	}
	files := unzipped(t, got.Raw)
	index, part1, part2 := string(files["index.html"]), string(files["part-1.html"]), string(files["part-2.html"])
	if !strings.Contains(index, `href="part-1.html#entry-`+s.key+`-R2"`) || !strings.Contains(index, "search.js") {
		t.Errorf("the index does not lead to the pages: %s", index[:min(len(index), 600)])
	}
	if !strings.Contains(part1, "Stop within 40 m") || !strings.Contains(part2, "Warn the driver") {
		t.Error("a top-level entry does not start a page of its own")
	}
	if !strings.Contains(part2, `href="part-1.html#entry-`+s.key+`-R2"`) && !strings.Contains(part1, `href="part-2.html#entry-`+s.key+`-R3"`) {
		t.Error("a trace link does not lead to the page of its other end")
	}
	if !strings.Contains(string(files["search.js"]), "window.EXCITER_SEARCH") || len(files["style.css"]) == 0 {
		t.Error("the site lacks its search or its style")
	}
	pictures := 0
	for name := range files {
		if strings.HasPrefix(name, "media/") {
			pictures++
		}
	}
	if pictures != 1 || !strings.Contains(part1, `src="media/`) {
		t.Errorf("%d pictures came along, want the one R2 shows", pictures)
	}
	for name, page := range files {
		if strings.HasSuffix(name, ".html") && (strings.Contains(string(page), "<script src=\"/") || strings.Contains(string(page), "/api/v1/")) {
			t.Errorf("%s still reaches for the server", name)
		}
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'specification.exported' AND data->>'project' = $1 AND data->>'format' = 'html'`, s.key); n != 1 {
		t.Errorf("%d site exports recorded, want 1", n)
	}
	baseline := want(t, s.f.owner.get(t, "/api/v1/baselines/"+s.baseline+"/document.html.zip"), http.StatusOK, "a baseline as a site")
	if !bytes.Contains(unzipped(t, baseline.Raw)["index.html"], []byte("Release 1")) {
		t.Error("the baseline's site does not carry its name")
	}
	refuse(t, "a section the project lacks", s.f.owner.get(t, "/api/v1/projects/"+s.key+"/document.html.zip?section="+s.key+"-R99"), http.StatusNotFound, "not_found")
	refuse(t, "a baseline somebody may not see", s.f.outside.get(t, "/api/v1/baselines/"+s.baseline+"/document.html.zip"), http.StatusNotFound, "not_found")
}
