//go:build integration

package test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/render"
)

// documentScene is a project with a type, attributes, a section, a requirement that shows a picture and links to
// another, and a baseline taken before the requirement changed.
type documentScene struct {
	f        *projectFixture
	key      string
	baseline string
}

func newDocumentScene(t *testing.T) *documentScene {
	t.Helper()
	f := newProjectFixture(t)
	owner := f.owner
	s := &documentScene{f: f, key: projectKey(t, "DOC")}
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Brakes"}), http.StatusCreated, "make the project")
	attr := func(body map[string]any) map[string]any {
		return obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/attributes", body), http.StatusCreated, "make "+body["name"].(string)), "attribute")
	}
	risk := attr(map[string]any{"name": "Risk", "kind": "select", "options": []any{map[string]any{"name": "Low"}, map[string]any{"name": "High"}}})
	safe := attr(map[string]any{"name": "Safe", "kind": "boolean"})
	detail := attr(map[string]any{"name": "Detail", "kind": "richText"})
	section := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"kind": "section", "title": "Braking"}),
		http.StatusCreated, "write the section"), "requirement")
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{
		"title": "Stop within 40 m", "statement": statementSaying("The vehicle stops."), "rationale": statementSaying("The law says so."),
		"priority": "must", "labels": []string{"brakes"}, "ownerId": owner.user.String(),
		"place":      map[string]any{"parentId": section["id"], "afterId": nil},
		"attributes": map[string]any{risk["id"].(string): optionID(t, risk, "High"), safe["id"].(string): true, detail["id"].(string): statementSaying("Measured on asphalt.")},
	}), http.StatusCreated, "write "+s.key+"-R2")
	picture := obj(t, want(t, owner.uploadAs(t, "/api/v1/requirements/"+s.key+"-R2/attachments", "curve.png", "image/png", aPicture(t)),
		http.StatusCreated, "attach a picture"), "attachment")
	want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"statement": showing(picture["id"].(string)), "baseRevision": 1}),
		http.StatusOK, "show the picture")
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Warn the driver", "statement": statementSaying("A tone sounds.")}),
		http.StatusCreated, "write "+s.key+"-R3")
	want(t, owner.post(t, "/api/v1/requirements/"+s.key+"-R3/links", map[string]any{"typeKey": "derives", "otherKey": s.key + "-R2"}), http.StatusCreated, "link them")
	taken := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline"), "baseline")
	s.baseline = taken["id"].(string)
	want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R3", map[string]any{"title": "Warn the driver loudly", "baseRevision": 1}), http.StatusOK, "change R3")
	return s
}

func entryOf(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	for _, each := range doc["entries"].([]any) {
		if e := each.(map[string]any); e["key"] == key {
			return e
		}
	}
	t.Fatalf("the document holds no %s: %v", key, doc["entries"])
	return nil
}

// propertiesOf are an entry's properties as name and value, a field by its key.
func propertiesOf(e map[string]any) map[string]any {
	out := map[string]any{}
	for _, each := range e["properties"].([]any) {
		p := each.(map[string]any)
		name := p["name"].(string)
		if p["field"] != "" {
			name = p["field"].(string)
		}
		switch {
		case p["document"] != nil:
			out[name] = "document"
		case p["flag"] != nil:
			out[name] = p["flag"]
		default:
			out[name] = p["text"]
		}
	}
	return out
}

// A project's document holds every entry with its table and links, a section's what is below it, and a baseline's
// each requirement as the baseline took it.
func TestADocumentReadsAProjectASectionAndABaseline(t *testing.T) {
	s := newDocumentScene(t)
	owner := s.f.owner
	doc := obj(t, want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document"), http.StatusOK, "a member reads the document"), "document")
	if doc["kind"] != "project" || doc["title"] != "Brakes" || doc["total"] != float64(3) || len(doc["entries"].([]any)) != 3 {
		t.Fatalf("the document: %v", doc)
	}
	r2 := entryOf(t, doc, s.key+"-R2")
	if r2["outline"] != "1.1" || r2["depth"] != float64(1) || r2["revision"] != float64(2) || r2["rationale"] == nil {
		t.Errorf("R2 reads %v", r2)
	}
	props := propertiesOf(r2)
	if props["type"] == "" || props["status"] == "" || props["priority"] != "must" || props["labels"] != "brakes" || props["owner"] == "" ||
		props["Risk"] != "High" || props["Safe"] != true || props["Detail"] != "document" {
		t.Errorf("R2's table reads %v", props)
	}
	links := r2["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["key"] != s.key+"-R3" || links[0].(map[string]any)["phrase"] == "" {
		t.Errorf("R2 is linked from R3, read from R2: %v", links)
	}
	if out := entryOf(t, doc, s.key+"-R3")["links"].([]any); len(out) != 1 || out[0].(map[string]any)["key"] != s.key+"-R2" {
		t.Errorf("R3 links to R2: %v", out)
	}

	section := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/document?section="+s.key+"-R1"), http.StatusOK, "read the section"), "document")
	if section["kind"] != "section" || section["title"] != "Braking" || section["sectionKey"] != s.key+"-R1" || len(section["entries"].([]any)) != 2 ||
		entryOf(t, section, s.key+"-R1")["depth"] != float64(0) {
		t.Errorf("the section's document: %v", section)
	}

	taken := obj(t, want(t, owner.get(t, "/api/v1/baselines/"+s.baseline+"/document"), http.StatusOK, "read the baseline"), "document")
	if taken["kind"] != "baseline" || taken["baseline"].(map[string]any)["name"] != "Release 1" {
		t.Fatalf("the baseline's document: %v", taken)
	}
	if r3 := entryOf(t, taken, s.key+"-R3"); r3["title"] != "Warn the driver" || r3["revision"] != float64(1) || len(r3["links"].([]any)) != 1 {
		t.Errorf("the baseline holds R3 as it was: %v", r3)
	}
	if _, ok := propertiesOf(entryOf(t, taken, s.key+"-R2"))["owner"]; ok {
		t.Errorf("a baseline keeps no owner, and its document shows none")
	}

	refuse(t, "a section the project lacks", owner.get(t, "/api/v1/projects/"+s.key+"/document?section="+s.key+"-R99"), http.StatusNotFound, "not_found")
	refuse(t, "somebody who does not view the project", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/document"), http.StatusNotFound, "not_found")
	refuse(t, "a baseline somebody may not see", s.f.outside.get(t, "/api/v1/baselines/"+s.baseline+"/document"), http.StatusNotFound, "not_found")
}

// The Word file holds the cover, every entry and the picture, and leaving is recorded in the audit log.
func TestADocumentDownloadsAsWord(t *testing.T) {
	s := newDocumentScene(t)
	got := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document.docx"), http.StatusOK, "a member exports Word")
	if !strings.HasPrefix(got.Header.Get("Content-Type"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document") ||
		!strings.Contains(got.Header.Get("Content-Disposition"), s.key+"-") || got.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("the download: %v", got.Header)
	}
	entries := unzipped(t, got.Raw)
	body := string(entries["word/document.xml"])
	for _, want := range []string{"Brakes", "Specification of Brakes", "1.1 Stop within 40 m", "The law says so.", "Measured on asphalt.", "Must", "Warn the driver"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Word document lacks %q", want)
		}
	}
	if entries["word/media/picture1.png"] == nil {
		t.Errorf("the picture did not come along")
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'specification.exported' AND data->>'project' = $1 AND data->>'format' = 'docx'
		AND actor_user_id = $2`, s.key, s.f.plain.user); n != 1 {
		t.Errorf("%d audit entries for the member's export, want 1", n)
	}
	baseline := want(t, s.f.owner.get(t, "/api/v1/baselines/"+s.baseline+"/document.docx"), http.StatusOK, "export the baseline as Word")
	if !strings.Contains(baseline.Header.Get("Content-Disposition"), "release-1") || !bytes.Contains(unzipped(t, baseline.Raw)["word/document.xml"], []byte("Baseline of Brakes")) {
		t.Errorf("the baseline's Word file: %v", baseline.Header)
	}
	refuse(t, "a section the project lacks", s.f.owner.get(t, "/api/v1/projects/"+s.key+"/document.docx?section="+s.key+"-R99"), http.StatusNotFound, "not_found")
	refuse(t, "a baseline somebody may not see", s.f.outside.get(t, "/api/v1/baselines/"+s.baseline+"/document.docx"), http.StatusNotFound, "not_found")
}

// The PDF is the print view printed by the stack's render service as the reader, with a token that is gone
// afterwards; without the service, or with a token held to some projects, it is refused in a sentence.
func TestADocumentPrintsAsPDFThroughTheRenderService(t *testing.T) {
	s := newDocumentScene(t)
	if s.f.h.cfg.Render.URL == "" {
		t.Fatal("EXCITER_RENDER_URL is not set; run the suite with make test-integration against the running stack")
	}
	got := want(t, s.f.plain.get(t, "/api/v1/projects/"+s.key+"/document.pdf"), http.StatusOK, "a member prints the document")
	if !bytes.HasPrefix(got.Raw, []byte("%PDF-")) || got.Header.Get("Content-Type") != "application/pdf" || !strings.Contains(got.Header.Get("Content-Disposition"), ".pdf") {
		t.Fatalf("the PDF: %v, %q", got.Header, got.Raw[:min(len(got.Raw), 40)])
	}
	if !bytes.Contains(got.Raw, []byte("Brakes")) {
		t.Errorf("the PDF does not carry its title")
	}
	if n := s.f.h.countRows(t, `SELECT count(*) FROM api_token WHERE for_render AND user_id = $1`, s.f.plain.user); n != 0 {
		t.Errorf("%d print tokens were left behind", n)
	}
	section := want(t, s.f.owner.get(t, "/api/v1/projects/"+s.key+"/document.pdf?section="+s.key+"-R1"), http.StatusOK, "print the section")
	baseline := want(t, s.f.owner.get(t, "/api/v1/baselines/"+s.baseline+"/document.pdf"), http.StatusOK, "print the baseline")
	if !bytes.HasPrefix(section.Raw, []byte("%PDF-")) || !bytes.HasPrefix(baseline.Raw, []byte("%PDF-")) {
		t.Errorf("a section and a baseline print too")
	}
	// The print reads through the replica as its own token, served by the stack's api, which shares the positions of
	// read-your-writes with this one as an installation's api processes do: what the reader just wrote is printed too.
	shared := newAPIServerWith(t, s.f.h, func(_ *apiServer, server *httpapi.Server) { server.Fresh = s.f.h.stackFreshness(t) })
	owner := shared.as(t, s.f.owner.user)
	fresh := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Release 2"}), http.StatusCreated, "take another baseline"), "baseline")
	if got := want(t, owner.get(t, "/api/v1/baselines/"+fresh["id"].(string)+"/document.pdf"), http.StatusOK, "print it at once"); !bytes.HasPrefix(got.Raw, []byte("%PDF-")) {
		t.Errorf("a baseline printed at once: %q", got.Raw[:min(len(got.Raw), 40)])
	}
	refuse(t, "a section the project lacks", s.f.owner.get(t, "/api/v1/projects/"+s.key+"/document.pdf?section="+s.key+"-R99"), http.StatusNotFound, "not_found")
	refuse(t, "a baseline somebody may not see", s.f.outside.get(t, "/api/v1/baselines/"+s.baseline+"/document.pdf"), http.StatusNotFound, "not_found")

	_, secret := makeToken(t, s.f.owner, map[string]any{"name": "one project", "projects": []string{s.key}})
	held := &client{api: s.f.api, user: s.f.owner.user, http: &http.Client{}, bearer: secret}
	refuse(t, "a token held to some projects", held.get(t, "/api/v1/projects/"+s.key+"/document.pdf"), http.StatusForbidden, "forbidden")

	off := newAPIServerWith(t, s.f.h, func(_ *apiServer, server *httpapi.Server) { server.Renderer = render.Unavailable{} })
	refuse(t, "a server without the render service", off.as(t, s.f.owner.user).get(t, "/api/v1/projects/"+s.key+"/document.pdf"), http.StatusServiceUnavailable, "render_unavailable")
}
