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

	"github.com/google/uuid"
)

// wordPart is a paragraph of a test document: a heading of a level, or a paragraph when level is 0, with the
// identifier Word keeps for it.
type wordPart struct {
	level  int
	paraID string
	text   string
}

// aWordDocument is a .docx of parts, with the heading styles Word writes and the identifier it keeps for a document.
func aWordDocument(t *testing.T, parts ...wordPart) []byte {
	t.Helper()
	const w = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	var body strings.Builder
	for _, p := range parts {
		body.WriteString(`<w:p w14:paraId="` + p.paraID + `">`)
		if p.level > 0 {
			body.WriteString(`<w:pPr><w:pStyle w:val="Heading` + string(rune('0'+p.level)) + `"/></w:pPr>`)
		}
		body.WriteString(`<w:r><w:t xml:space="preserve">` + p.text + `</w:t></w:r></w:p>`)
	}
	heading := func(n string) string {
		return `<w:style w:type="paragraph" w:styleId="Heading` + n + `"><w:name w:val="heading ` + n + `"/><w:basedOn w:val="Normal"/></w:style>`
	}
	files := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" ` +
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/styles.xml": `<w:styles ` + w + `><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
			heading("1") + heading("2") + `</w:styles>`,
		"word/settings.xml": `<w:settings ` + w + ` xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml"><w15:docId w15:val="{0000AAAA-0000-4000-8000-00000000BEEF}"/></w:settings>`,
		"word/document.xml": `<w:document ` + w + ` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"><w:body>` + body.String() + `</w:body></w:document>`,
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

// brakeSpec is a chapter with a section holding a requirement and a sentence that explains it.
func brakeSpec(t *testing.T, requirement, explanation, explanationID string) []byte {
	return aWordDocument(t, wordPart{1, "00000001", "Braking"}, wordPart{2, "00000002", "Pedal"},
		wordPart{0, "00000003", requirement}, wordPart{0, explanationID, explanation},
		wordPart{0, "00000005", "The pedal shall be lit at night."})
}

// uploadWord sends a Word document with the section to import below.
func (c *client) uploadWord(t *testing.T, projectKey, name string, data []byte, parentKey string) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if parentKey != "" {
		_ = form.WriteField("parentKey", parentKey)
	}
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+"/api/v1/projects/"+projectKey+"/word-imports", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return c.do(t, req)
}

// previewTree is an import's tree as lines: depth, kind and title, and how an earlier import was found.
func previewTree(imp map[string]any) []string {
	var out []string
	for _, each := range imp["preview"].(map[string]any)["tree"].([]any) {
		n := each.(map[string]any)
		line := strings.Repeat("  ", int(n["depth"].(float64))) + n["kind"].(string) + " " + n["title"].(string)
		if by, ok := n["matchedBy"].(string); ok {
			line += " by " + by
		}
		out = append(out, line)
	}
	return out
}

// startWord starts an uploaded import and answers what the worker did.
func startWord(t *testing.T, c *client, imp map[string]any) map[string]any {
	t.Helper()
	id := imp["id"].(string)
	want(t, c.post(t, "/api/v1/reqif-imports/"+id+"/start", nil), http.StatusAccepted, "start the import")
	done := finishedReqIF(t, c, id)
	if done["state"] != "done" || len(done["refused"].([]any)) != 0 {
		t.Fatalf("the import: %v", done)
	}
	return done
}

// A Word document is read below a section and previewed before anything is written; the choices change what it
// makes; and whoever may not import, or what is no Word document or no section, is refused.
func TestWordImportAsksBeforeItWrites(t *testing.T) {
	key := projectKey(t, "WDA")
	f := newProjectFixture(t)
	owner := f.owner
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	section := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"kind": "section", "title": "Supplier"}),
		http.StatusCreated, "write a section"), "requirement")
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Loose", "statement": statementSaying("Not a section.")}),
		http.StatusCreated, "write a requirement")
	doc := brakeSpec(t, "The pedal shall return within 1 s.", "This text explains the pedal.", "00000004")

	refuse(t, "a member imports", f.plain.uploadWord(t, key, "brakes.docx", doc, ""), http.StatusForbidden, "forbidden")
	refuse(t, "a file that is no Word document", owner.uploadWord(t, key, "brakes.docx", []byte("<html/>"), ""), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a file too large", owner.uploadWord(t, key, "brakes.docx", make([]byte, 64<<20+1), ""), http.StatusRequestEntityTooLarge, "too_large")
	for _, parent := range []string{key + "-R2", key + "-R99"} {
		got := owner.uploadWord(t, key, "brakes.docx", doc, parent)
		refuse(t, "below "+parent, got, http.StatusUnprocessableEntity, "validation_failed")
		if !strings.Contains(string(got.Raw), "parentKey") {
			t.Errorf("below %s: the refusal names no parentKey: %s", parent, got.Raw)
		}
	}

	made := obj(t, want(t, owner.uploadWord(t, key, "brakes.docx", doc, section["key"].(string)), http.StatusCreated, "upload it"), "import")
	if made["format"] != "docx" || made["total"] != float64(4) {
		t.Errorf("the import: %v", made)
	}
	sameList(t, "the preview", previewTree(made), "section Braking", "  section Pedal", "    requirement The pedal shall return within 1 s.",
		"    informative This text explains the pedal.", "    requirement The pedal shall be lit at night.")
	preview := made["preview"].(map[string]any)
	if styles := preview["styles"].([]any); len(styles) != 3 || preview["tables"] != float64(0) || len(preview["problems"].([]any)) != 0 {
		t.Errorf("the preview: %v", preview)
	}
	sameList(t, "nothing written yet", treeOf(t, owner, key), "1 section Supplier", "2 requirement Loose")

	id := made["id"].(string)
	choices := "/api/v1/reqif-imports/" + id + "/choices"
	refuse(t, "a pattern that is no pattern", owner.put(t, choices, map[string]any{"choices": map[string]any{"word.pattern": "("}}), http.StatusUnprocessableEntity, "validation_failed")
	heading := obj(t, want(t, owner.put(t, choices, map[string]any{"choices": map[string]any{"word.unit": "heading"}}), http.StatusOK, "one per heading"), "import")
	sameList(t, "one requirement per heading", previewTree(heading), "section Braking", "  requirement Pedal")
	if heading["total"] != float64(2) {
		t.Errorf("the total follows the choices: %v", heading["total"])
	}
	lit := obj(t, want(t, owner.put(t, choices, map[string]any{"choices": map[string]any{"word.keywords": "lit"}}), http.StatusOK, "a keyword"), "import")
	sameList(t, "the keyword lit", previewTree(lit), "section Braking", "  section Pedal", "    informative The pedal shall return within 1 s.",
		"    informative This text explains the pedal.", "    requirement The pedal shall be lit at night.")

	want(t, owner.put(t, choices, map[string]any{"choices": map[string]any{}}), http.StatusOK, "the choices as they were")
	done := startWord(t, owner, made)
	if len(done["imported"].([]any)) != 4 {
		t.Errorf("the import made %v", done["imported"])
	}
	sameList(t, "the tree", treeOf(t, owner, key), "1 section Supplier", "1.1 section Braking", "1.1.1 section Pedal",
		"1.1.1.1 requirement The pedal shall return within 1 s.", "1.1.1.2 requirement The pedal shall be lit at night.", "2 requirement Loose")
	pedal := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+key+"-R4"), http.StatusOK, "read the section"), "requirement")
	if !strings.Contains(string(mustJSON(t, pedal["statement"])), "This text explains the pedal.") {
		t.Errorf("the informative text is not the section's statement: %v", pedal["statement"])
	}

	want(t, owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive the project")
	refuse(t, "an archived project", owner.uploadWord(t, key, "brakes.docx", doc, ""), http.StatusConflict, "project_archived")
	want(t, owner.delete(t, "/api/v1/projects/"+key+"/archive"), http.StatusOK, "unarchive the project")
}

// The same document imported into the same place again updates what changed as a new revision, leaves the rest
// unchanged, and finds a paragraph whose identifier Word changed by its place below its headings.
func TestWordReimportUpdatesByIdentity(t *testing.T) {
	key := projectKey(t, "WDB")
	f := newProjectFixture(t)
	owner := f.owner
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	first := obj(t, want(t, owner.uploadWord(t, key, "brakes.docx", brakeSpec(t, "The pedal shall return within 1 s.", "Explained.", "00000004"), ""),
		http.StatusCreated, "upload it"), "import")
	startWord(t, owner, first)
	before := treeOf(t, owner, key)

	changed := aWordDocument(t, wordPart{1, "00000001", "Braking"}, wordPart{2, "00000002", "Pedal"},
		wordPart{0, "00000003", "The pedal shall return within 2 s."}, wordPart{0, "00000004", "Explained."},
		wordPart{0, "7777AAAA", "The pedal shall be lit at night."})
	again := obj(t, want(t, owner.uploadWord(t, key, "brakes.docx", changed, ""), http.StatusCreated, "upload it again"), "import")
	sameList(t, "the second preview", previewTree(again), "section Braking by paragraph", "  section Pedal by paragraph",
		"    requirement The pedal shall return within 2 s. by paragraph", "    informative Explained.", "    requirement The pedal shall be lit at night. by path")
	if p := again["preview"].(map[string]any); p["new"] != float64(0) || p["updates"] != float64(4) {
		t.Fatalf("the second import: %v new, %v updates", p["new"], p["updates"])
	}
	done := startWord(t, owner, again)
	if updated := toStrings(done["updated"]); len(updated) != 1 || done["unchanged"] != float64(3) || len(done["imported"].([]any)) != 0 {
		t.Errorf("the second import: %v", done)
	}
	r3 := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+key+"-R3"), http.StatusOK, "read it"), "requirement")
	if r3["revision"] != float64(2) || r3["title"] != "The pedal shall return within 2 s." {
		t.Errorf("the changed requirement: revision %v, %v", r3["revision"], r3["title"])
	}
	if n := len(treeOf(t, owner, key)); n != len(before) {
		t.Errorf("the tree held %d and holds %d after the same document twice", len(before), n)
	}
}

// Exciter's own Word export comes back into its project by its requirements' keys, which change nothing it says.
func TestWordImportOfAnExportFindsItsRequirementsByKey(t *testing.T) {
	key := projectKey(t, "WDC")
	f := newProjectFixture(t)
	owner := f.owner
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	section := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"kind": "section", "title": "Braking"}),
		http.StatusCreated, "write a section"), "requirement")
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Stop", "statement": statementSaying("The car shall stop."),
		"place": map[string]any{"parentId": section["id"], "afterId": nil}}), http.StatusCreated, "write a requirement")
	exported := want(t, owner.get(t, "/api/v1/projects/"+key+"/document.docx"), http.StatusOK, "export it").Raw
	made := obj(t, want(t, owner.uploadWord(t, key, key+".docx", exported, ""), http.StatusCreated, "upload the export"), "import")
	sameList(t, "the preview", previewTree(made), "skipped Cover and contents", "section Braking by key", "  requirement Stop by key")
	if done := startWord(t, owner, made); done["unchanged"] != float64(2) || len(done["updated"].([]any)) != 0 || len(done["imported"].([]any)) != 0 {
		t.Errorf("the export came back changed: %v", done)
	}
}

const insertWord = `INSERT INTO reqif_import (project_id, created_by, file_name, format, content, total, parent_id)
	VALUES ($1, $2, 'file.docx', 'docx', '{"blocks":[]}', 0, $3) RETURNING id`

// Straight through SQL as exciter_app: a Word import goes below a section of its own project, fixed once made, and
// only the project's administrators make one.
func TestRawSQLGuardsWordImports(t *testing.T) {
	w := newTraceWorld(t, "word")
	ctx := context.Background()
	admin := w.h.appConn(t, w.a.user)
	var id uuid.UUID
	if err := admin.QueryRow(ctx, insertWord, w.here, w.a.user, w.section).Scan(&id); err != nil {
		t.Fatalf("an administrator imports below a section: %v", err)
	}
	if err := admin.QueryRow(ctx, insertWord, w.other, w.a.user, w.section).Scan(new(uuid.UUID)); !isCheckViolation(err) {
		t.Errorf("an import went below a section of another project: %v", err)
	}
	if err := admin.QueryRow(ctx, insertWord, w.here, w.a.user, w.r1).Scan(new(uuid.UUID)); !isCheckViolation(err) {
		t.Errorf("an import went below a requirement: %v", err)
	}
	if err := w.editor.QueryRow(ctx, insertWord, w.here, w.editorID, w.section).Scan(new(uuid.UUID)); err == nil {
		t.Error("an editor who does not administer the project made an import")
	}
	if _, err := admin.Exec(ctx, `UPDATE reqif_import SET parent_id = $2 WHERE id = $1`, id, w.r2); !isCheckViolation(err) {
		t.Errorf("an import moved below something else: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE reqif_import SET total = 3 WHERE id = $1`, id); err != nil {
		t.Errorf("the count of an uploaded Word import follows its choices: %v", err)
	}
}
