//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// largeDocumentDeadline bounds a request for a document past specdoc.MaxEntries.
const largeDocumentDeadline = 2 * time.Minute

// specCan reads what the specification says its reader may do with each entry, by key.
func specCan(t *testing.T, c *client, key string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+key+"/spec"), http.StatusOK, "read the specification"), "entries") {
		e := each.(map[string]any)
		out[e["node"].(map[string]any)["key"].(string)] = e["can"].(map[string]any)
	}
	return out
}

// canReads fails unless can holds exactly the rights named true and every other one false.
func canReads(t *testing.T, what string, can map[string]any, rights ...string) {
	t.Helper()
	for _, right := range []string{"edit", "delete", "link", "transition", "comment"} {
		if wanted := strings.Contains(" "+strings.Join(rights, " ")+" ", " "+right+" "); can[right] != wanted {
			t.Errorf("%s: %s is %v, want %v (%v)", what, right, can[right], wanted, can)
		}
	}
}

// Each entry of the specification says what its reader may do with it: an editor everything, a reviewer to move it
// and comment, nobody the edits a lock holds, and nobody anything in an archived project (#310).
func TestTheSpecificationSaysWhatItsReaderMayDo(t *testing.T) {
	s := newMoveScene(t, "SC")
	if _, err := s.f.h.super.Exec(t.Context(), `UPDATE role_assignment SET role = 'viewer' WHERE group_id = (SELECT id FROM groups WHERE builtin = 'everyone') AND project_id = $1`, s.id); err != nil {
		t.Fatal(err)
	}
	reviewerID := s.f.h.addPerson(t, "member")
	s.f.h.grantAs(t, s.id, reviewerID, "reviewer")
	reviewer := s.f.api.as(t, reviewerID)
	open, locked := s.key+"-R4", s.key+"-R2"
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+locked+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock R2")

	editor := specCan(t, s.editor, s.key)
	canReads(t, "the editor's open entry", editor[open], "edit", "delete", "link", "transition", "comment")
	canReads(t, "the editor's locked entry", editor[locked], "transition", "comment")
	got := specCan(t, reviewer, s.key)
	canReads(t, "the reviewer's open entry", got[open], "transition", "comment")
	canReads(t, "the reviewer's locked entry", got[locked], "transition", "comment")

	want(t, s.f.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	for who, c := range map[string]*client{"the editor": s.editor, "the reviewer": reviewer} {
		for key, can := range specCan(t, c, s.key) {
			canReads(t, who+"'s "+key+" in an archived project", can)
		}
	}
	// Comments are closed in an archived project too, so the entry offers none.
	refuse(t, "a comment in an archived project", s.editor.post(t, "/api/v1/requirements/"+open+"/comments", commentBody("Still true?")),
		http.StatusConflict, "project_archived")
}

// get_tree answers over MCP what the tree route answers, and a guest's token reads no project it is kept from (#310).
func TestAnAssistantReadsTheTree(t *testing.T) {
	s := newMoveScene(t, "MT")
	_, secret := makeToken(t, s.f.owner, map[string]any{"name": "assistant"})
	_, guestSecret := makeToken(t, s.f.guest, map[string]any{"name": "guest assistant"})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)
	got := nodeOutlines(t, response{Body: a.call(t, "get_tree", map[string]any{"projectKey": s.key})})
	sameList(t, "get_tree", got, s.tree(t, s.key)...)
	if len(got) != 4 {
		t.Errorf("get_tree answered %v", got)
	}
	if msg := connectAssistant(t, s.f.api, guestSecret).refused(t, "get_tree", map[string]any{"projectKey": s.key}); msg == "" {
		t.Error("the guest's refusal says nothing")
	}
}

// A move to another project posts requirement.moved once for each requirement it moves, saying where from and to
// and who moved it; a move within the project posts none (#310).
func TestAMoveToAnotherProjectIsPostedOncePerRequirement(t *testing.T) {
	s := newMoveScene(t, "MW")
	s.f.h.keepWebhooks(t)
	to := projectKey(t, "MWT")
	s.project(t, to, "Supplier", "editor", true)
	bin, address := hookBin(t)
	makeHook(t, s.f.owner, "Moves", address, []string{s.key, to}, webhook.TopicRequirementMoved, webhook.TopicRequirementChanged)

	want(t, s.move(s.editor, t, s.key+"-R4", map[string]any{"place": place(s.ids[s.key+"-R1"], nil)}), http.StatusOK, "move R4 into the section")
	awaitTopic(t, bin, webhook.TopicRequirementChanged, `"field":"place"`, 1)
	s.f.h.drained(t)
	for _, d := range binDeliveries(t, bin) {
		if d.env["topic"] == webhook.TopicRequirementMoved {
			t.Errorf("a move within the project was posted as moved: %s", d.Body)
		}
	}
	want(t, s.move(s.editor, t, s.key+"-R1", map[string]any{"projectKey": to, "place": place(nil, nil)}), http.StatusOK, "move the section away")
	moved := awaitTopic(t, bin, webhook.TopicRequirementMoved, to+"-R", 4)
	s.f.h.drained(t)
	sameList(t, "the tree there", s.tree(t, to), to+"-R1 1", to+"-R2 1.1", to+"-R3 1.2", to+"-R4 1.3")
	// R4 went first below the section, so it takes the second number there in reading order.
	wanted := map[string]string{s.key + "-R1": to + "-R1", s.key + "-R4": to + "-R2", s.key + "-R2": to + "-R3", s.key + "-R3": to + "-R4"}
	seen := map[string]int{}
	for _, d := range binDeliveries(t, bin) {
		if d.env["topic"] != webhook.TopicRequirementMoved {
			continue
		}
		p := d.payload()
		from, toKey := p["fromKey"].(string), p["toKey"].(string)
		seen[from]++
		req := p["requirement"].(map[string]any)
		if wanted[from] != toKey || req["key"] != toKey || !strings.HasSuffix(req["url"].(string), "/p/"+to+"/r/"+toKey) ||
			p["actor"].(map[string]any)["id"] != s.editor.user.String() {
			t.Errorf("a move reads %s", d.Body)
		}
	}
	for from := range wanted {
		if seen[from] != 1 {
			t.Errorf("%s was posted moved %d times, want once", from, seen[from])
		}
	}
	if len(moved) != len(wanted) || len(seen) != len(wanted) {
		t.Errorf("%d moves posted about %v, want one for each of %d", len(moved), seen, len(wanted))
	}
}

// A section's document holds the section and what is below it, however deep it sits; one in the trash is none (#310).
func TestTheDocumentOfANestedSection(t *testing.T) {
	s := newMoveScene(t, "DN")
	in := func(parent string) map[string]any { return place(s.ids[parent], nil) }
	key := s.key
	// R1 Braking holds R2 and R3; R5 goes after R3 with R6 below it, then R7.
	nested := s.write(t, key, map[string]any{"kind": "section", "title": "Wet roads", "place": place(s.ids[key+"-R1"], s.ids[key+"-R3"])})
	s.write(t, key, map[string]any{"title": "Brakes engage on a wet road", "place": in(key + "-R5")})
	s.write(t, key, map[string]any{"title": "Brakes warn when worn", "place": place(s.ids[key+"-R1"], nested)})
	sameList(t, "the tree", s.tree(t, key), key+"-R1 1", key+"-R2 1.1", key+"-R3 1.2", key+"-R5 1.3", key+"-R6 1.3.1", key+"-R7 1.4", key+"-R4 2")

	doc := obj(t, want(t, s.editor.get(t, "/api/v1/projects/"+key+"/document?section="+key+"-R5"), http.StatusOK, "read section 1.3"), "document")
	var got []string
	for _, each := range doc["entries"].([]any) {
		e := each.(map[string]any)
		got = append(got, fmt.Sprintf("%s %s %v", e["key"], e["outline"], e["depth"]))
	}
	sameList(t, "the section's document", got, key+"-R5 1.3 0", key+"-R6 1.3.1 1")
	if doc["total"] != float64(2) || doc["kind"] != "section" || doc["title"] != "Wet roads" {
		t.Errorf("the section's document counts %v as %v titled %v", doc["total"], doc["kind"], doc["title"])
	}
	want(t, s.editor.delete(t, "/api/v1/requirements/"+key+"-R5"), http.StatusNoContent, "trash section 1.3")
	refuse(t, "a section in the trash", s.editor.get(t, "/api/v1/projects/"+key+"/document?section="+key+"-R5"), http.StatusNotFound, "not_found")
	refuse(t, "a section in the trash as Word", s.editor.get(t, "/api/v1/projects/"+key+"/document.docx?section="+key+"-R5"), http.StatusNotFound, "not_found")
}

// A link to a requirement of a project its reader may not view is left out of the document, of Word and of Markdown,
// which name neither its key nor its title (#310).
func TestADocumentLeavesOutALinkItsReaderMayNotFollow(t *testing.T) {
	s := newMoveScene(t, "DL")
	hidden := projectKey(t, "DLH")
	hiddenID := s.project(t, hidden, "Hidden", "viewer", false)
	if _, err := s.f.h.super.Exec(t.Context(), `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, hiddenID); err != nil {
		t.Fatal(err)
	}
	s.f.h.settle(t)
	const secret = "Supplier pays the penalty"
	want(t, s.f.owner.post(t, "/api/v1/projects/"+hidden+"/requirements", map[string]any{"title": secret}), http.StatusCreated, "write the hidden requirement")
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+s.key+"-R4/links", map[string]any{"typeKey": "derives", "otherKey": hidden + "-R1"}),
		http.StatusCreated, "link R4 to the hidden requirement")
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+s.key+"-R2/links", map[string]any{"typeKey": "derives", "otherKey": s.key + "-R4"}),
		http.StatusCreated, "link R2 to R4")
	s.f.h.settle(t)
	refuse(t, "the reader views the hidden project", s.editor.get(t, "/api/v1/projects/"+hidden), http.StatusNotFound, "not_found")

	owners := obj(t, want(t, s.f.owner.get(t, "/api/v1/projects/"+s.key+"/document"), http.StatusOK, "the owner reads the document"), "document")
	if links := entryOf(t, owners, s.key+"-R4")["links"].([]any); len(links) != 2 {
		t.Fatalf("the owner reads R4's links as %v, want the visible one and the hidden one", links)
	}
	doc := want(t, s.editor.get(t, "/api/v1/projects/"+s.key+"/document"), http.StatusOK, "the reader reads the document")
	links := entryOf(t, obj(t, doc, "document"), s.key+"-R4")["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["key"] != s.key+"-R2" {
		t.Errorf("the reader reads R4's links as %v, want R2's alone", links)
	}
	word := string(unzipped(t, want(t, s.editor.get(t, "/api/v1/projects/"+s.key+"/document.docx"), http.StatusOK, "export Word").Raw)["word/document.xml"])
	markdown := string(want(t, s.editor.get(t, "/api/v1/projects/"+s.key+"/document.md"), http.StatusOK, "export Markdown").Raw)
	for what, body := range map[string]string{"the document": string(doc.Raw), "the Word file": word, "the Markdown": markdown} {
		if strings.Contains(body, hidden) || strings.Contains(body, secret) {
			t.Errorf("%s names the hidden requirement", what)
		}
	}
	if !strings.Contains(word, "Lights dim at night") || !strings.Contains(markdown, "Lights dim at night") {
		t.Error("the exports lack R4 itself")
	}
}

// A document holds at most specdoc.MaxEntries requirements and says so in Word; the section past the cut is read
// whole on its own (#310).
func TestADocumentPastItsLimitSaysSo(t *testing.T) {
	f := newProjectFixture(t)
	key := projectKey(t, "DM")
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Large"}), http.StatusCreated, "make the project"), "project")
	projectID := uuid.MustParse(project["id"].(string))
	const lastSection = 10
	total := specdoc.MaxEntries + 1
	// Straight through SQL, since a thousand writes over the API would take the test's time. Bulk and Tail with their
	// children make one entry more than the limit, which cuts Tail's last child.
	section := func(title string, children int) {
		var id uuid.UUID
		if err := f.h.super.QueryRow(t.Context(), `INSERT INTO requirement (project_id, kind, title, statement, normative_hash, created_by)
			VALUES ($1, 'section', $2, '{"type":"doc"}', 'h', $3) RETURNING id`, projectID, title, f.home.user).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.h.super.Exec(t.Context(), `INSERT INTO requirement (project_id, title, statement, normative_hash, parent_id, created_by)
			SELECT $1, $2 || ' ' || n, '{"type":"doc"}', 'h', $3, $4 FROM generate_series(1, $5) n ORDER BY n`, projectID, title, id, f.home.user, children); err != nil {
			t.Fatal(err)
		}
	}
	section("Bulk", total-lastSection-1)
	section("Tail", lastSection-1)
	f.h.settle(t)
	lastKey := fmt.Sprintf("%s-R%d", key, total-lastSection+1)
	// The race detector on a busy machine reads a thousand entries past the default deadline; the limit is proven
	// here, not the speed.
	patient := newAPIServerWith(t, f.h, func(_ *apiServer, server *httpapi.Server) { server.RequestTimeout = largeDocumentDeadline })
	reader := patient.as(t, f.plain.user)

	doc := obj(t, want(t, reader.get(t, "/api/v1/projects/"+key+"/document"), http.StatusOK, "read the document"), "document")
	entries := doc["entries"].([]any)
	if len(entries) != specdoc.MaxEntries || doc["total"] != float64(total) {
		t.Fatalf("the document holds %d of %v, want %d of %d", len(entries), doc["total"], specdoc.MaxEntries, total)
	}
	if last := entries[len(entries)-1].(map[string]any); last["title"] != fmt.Sprintf("Tail %d", lastSection-2) {
		t.Errorf("the document ends at %v", last["title"])
	}
	body := string(unzipped(t, want(t, reader.get(t, "/api/v1/projects/"+key+"/document.docx"), http.StatusOK, "export Word").Raw)["word/document.xml"])
	sentence := fmt.Sprintf("This document holds the first %d of the %d requirements", specdoc.MaxEntries, total)
	if at := strings.Index(body, sentence); at < 0 || at < strings.LastIndex(body, fmt.Sprintf("Tail %d", lastSection-2)) {
		t.Errorf("the Word file does not end with %q", sentence)
	}
	part := obj(t, want(t, reader.get(t, "/api/v1/projects/"+key+"/document?section="+lastKey), http.StatusOK, "read the last section"), "document")
	if n := len(part["entries"].([]any)); n != lastSection || part["total"] != float64(lastSection) {
		t.Errorf("the last section holds %d of %v, want all %d", n, part["total"], lastSection)
	}
	if strings.Contains(string(unzipped(t, want(t, reader.get(t, "/api/v1/projects/"+key+"/document.docx?section="+lastKey), http.StatusOK,
		"export the last section").Raw)["word/document.xml"]), "This document holds the first") {
		t.Error("the whole last section says it is cut short")
	}
}
