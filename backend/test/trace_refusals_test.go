//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// hiddenProject makes a project with one requirement that only its maker, an installation administrator, sees.
func hiddenProject(t *testing.T, h *harness, owner *client, key string) string {
	t.Helper()
	p := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Hidden"}), http.StatusCreated, "make "+key), "project")
	if _, err := h.super.Exec(t.Context(), `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, p["id"]); err != nil {
		t.Fatal(err)
	}
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brake fluid boils above 230 C"}), http.StatusCreated, "write in "+key)
	h.settle(t)
	return key + "-R1"
}

// A requirement holds at most trace.MaxLinksPerRequirement links: one more is refused from either end, naming both
// keys and the limit, and one removed makes room again (#313).
func TestARequirementHoldsAtMostItsShareOfLinks(t *testing.T) {
	key := projectKey(t, "CAP")
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Crowded"}), http.StatusCreated, "make a project"), "project")
	for _, title := range []string{"Brakes engage", "Brakes hold"} {
		want(t, f.plain.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	hub, spare := key+"-R1", key+"-R2"
	hubID := obj(t, want(t, f.plain.get(t, "/api/v1/requirements/"+hub), http.StatusOK, "read the hub"), "requirement")["id"]
	ctx := t.Context()
	// Written past the service, since only the cap is under test and five hundred requests would only be slower.
	if _, err := f.h.super.Exec(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by)
		SELECT $1, 'Spoke ' || g, '{"type":"doc"}', 'h', $2 FROM generate_series(1, $3::int) g`, project["id"], f.home.user, trace.MaxLinksPerRequirement); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.super.Exec(ctx, `INSERT INTO trace_link (type_id, source_id, target_id, created_by)
		SELECT (SELECT id FROM link_type WHERE project_id IS NULL AND key = 'relates'), $1, r.id, $2
		FROM requirement r WHERE r.project_id = $3 AND r.title LIKE 'Spoke %' ORDER BY r.number`, hubID, f.home.user, project["id"]); err != nil {
		t.Fatal(err)
	}
	f.h.settle(t)
	live := func() int {
		return f.h.countRows(t, `SELECT count(*) FROM trace_link l JOIN requirement r ON r.id IN (l.source_id, l.target_id)
			WHERE l.deleted_at IS NULL AND r.project_id = $1 AND r.title NOT LIKE 'Spoke %'`, project["id"])
	}

	for _, c := range []struct{ from, to string }{{hub, spare}, {spare, hub}} {
		msg := refuse422(t, "a link from "+c.from+" past the hub's share",
			f.plain.post(t, "/api/v1/requirements/"+c.from+"/links", map[string]any{"typeKey": "derives", "otherKey": c.to}), "otherKey")
		if wanted := fmt.Sprintf("%s or %s already holds %d links", c.from, c.to, trace.MaxLinksPerRequirement); !strings.HasPrefix(msg, wanted) {
			t.Errorf("the refusal says %q, want it to start %q", msg, wanted)
		}
	}
	if n := live(); n != trace.MaxLinksPerRequirement {
		t.Errorf("the refusals left %d live links on the hub, want %d", n, trace.MaxLinksPerRequirement)
	}

	var spoke string
	if err := f.h.super.QueryRow(ctx, `SELECT id FROM trace_link WHERE source_id = $1 ORDER BY created_at LIMIT 1`, hubID).Scan(&spoke); err != nil {
		t.Fatal(err)
	}
	want(t, f.plain.delete(t, "/api/v1/links/"+spoke), http.StatusNoContent, "remove one of the hub's links")
	want(t, f.plain.post(t, "/api/v1/requirements/"+hub+"/links", map[string]any{"typeKey": "derives", "otherKey": spare}), http.StatusCreated,
		"link the hub once it has room")
}

// Links whose ends are all in an archived project are neither made, removed nor confirmed, and stay as they were
// (#313).
func TestTheLinksOfAnArchivedProjectStayAsTheyAre(t *testing.T) {
	key := projectKey(t, "ARL")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Archived"}), http.StatusCreated, "make a project")
	for _, title := range []string{"Brake pedal reads within 10 ms", "Brake lights follow the pedal", "Brake log keeps every press"} {
		want(t, f.plain.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	id := obj(t, want(t, f.plain.post(t, "/api/v1/requirements/"+key+"-R2/links", map[string]any{"typeKey": "derives", "otherKey": key + "-R1"}),
		http.StatusCreated, "R2 derives from R1"), "link")["id"].(string)
	want(t, f.plain.patch(t, "/api/v1/requirements/"+key+"-R1", map[string]any{"title": "Brake pedal reads within 5 ms", "baseRevision": 1}),
		http.StatusOK, "R1 moves on, so the link waits to be checked")
	want(t, f.owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive the project")
	f.h.settle(t)
	state := func() string {
		var live bool
		var source, target, cleared, links int
		if err := f.h.super.QueryRow(t.Context(), `
			SELECT l.deleted_at IS NULL, l.source_reviewed_rev, l.target_reviewed_rev,
			       (SELECT count(*) FROM trace_link_clearance c WHERE c.link_id = l.id),
			       (SELECT count(*) FROM trace_link x JOIN requirement r ON r.id = x.source_id JOIN project p ON p.id = r.project_id WHERE p.key = $2)
			FROM trace_link l WHERE l.id = $1`, id, key).Scan(&live, &source, &target, &cleared, &links); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("live %v, confirmed at %d and %d, %d clearances, %d links in the project", live, source, target, cleared, links)
	}
	before := state()

	for who, c := range map[string]*client{"an editor": f.plain, "an administrator": f.owner} {
		refuse(t, who+" links", c.post(t, "/api/v1/requirements/"+key+"-R3/links", map[string]any{"typeKey": "relates", "otherKey": key + "-R1"}),
			http.StatusConflict, "project_archived")
		refuse(t, who+" removes the link", c.delete(t, "/api/v1/links/"+id), http.StatusConflict, "project_archived")
		refuse(t, who+" confirms the link", c.post(t, "/api/v1/links/"+id+"/clear", map[string]any{"sourceRevision": 1, "targetRevision": 2, "comment": "Still holds."}),
			http.StatusConflict, "project_archived")
	}
	refuse(t, "a removal by a malformed id", f.plain.delete(t, "/api/v1/links/not-an-id"), http.StatusBadRequest, "bad_request")
	if after := state(); after != before {
		t.Errorf("the refusals left the link %s, want %s", after, before)
	}
}

// The project's suspect list is paged with its total kept, and leaves out a link whose other end the reader does
// not see, in the list and in the count (#313).
func TestTheSuspectListIsPagedAndKeepsToWhatItsReaderSees(t *testing.T) {
	s := newSuspectScene(t, "SPG")
	hidden := hiddenProject(t, s.h, s.owner, projectKey(t, "SPH"))
	unseen := obj(t, want(t, s.owner.post(t, "/api/v1/requirements/"+s.req("3")+"/links", map[string]any{"typeKey": "derives", "otherKey": hidden}),
		http.StatusCreated, "R3 derives from the hidden requirement"), "link")["id"]
	s.revise(t, s.req("1"), 1)
	want(t, s.owner.patch(t, "/api/v1/requirements/"+hidden, map[string]any{"title": "Brake fluid boils above 250 C", "baseRevision": 1}),
		http.StatusOK, "the hidden requirement moves on")
	s.h.settle(t)
	suspects := func(c *client, query string) (map[string]any, []string) {
		body := obj(t, want(t, c.get(t, "/api/v1/projects/"+s.key+"/suspects"+query), http.StatusOK, "the suspect links"+query))
		var ids []string
		for _, each := range body["links"].([]any) {
			ids = append(ids, each.(map[string]any)["id"].(string))
		}
		return body, ids
	}

	if body, ids := suspects(s.owner, ""); body["total"] != float64(3) || len(ids) != 3 {
		t.Errorf("the owner, who sees both projects, has %v waiting: %v", body["total"], ids)
	}
	all, ids := suspects(s.carol, "")
	if all["total"] != float64(2) || len(ids) != 2 || strings.Contains(strings.Join(ids, " "), unseen.(string)) {
		t.Fatalf("carol, who does not see the hidden project, has %v waiting: %v", all["total"], ids)
	}
	page, second := suspects(s.carol, "?limit=1&offset=1")
	if page["total"] != float64(2) || page["limit"] != float64(1) || page["offset"] != float64(1) || len(second) != 1 || second[0] != ids[1] {
		t.Errorf("the second page reads %v, want only %s", page, ids[1])
	}
	if past, none := suspects(s.carol, "?limit=1&offset=2"); past["total"] != float64(2) || len(none) != 0 {
		t.Errorf("past the end reads %v", past)
	}
	fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/suspects?offset=-1"), "offset")
}

// Every revision of the end a link watches turns it, a new description or attribute value as much as a title, and a
// change that writes no revision, of owner or priority, leaves it holding (#313).
func TestEveryRevisionOfTheWatchedEndTurnsALink(t *testing.T) {
	s := newSuspectScene(t, "SRV")
	weight := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/attributes", map[string]any{"name": "Weight", "kind": "number"}),
		http.StatusCreated, "make Weight"), "attribute")["id"].(string)
	revision := func() float64 {
		return obj(t, want(t, s.carol.get(t, "/api/v1/requirements/"+s.req("1")), http.StatusOK, "read R1"), "requirement")["revision"].(float64)
	}

	for _, c := range []struct {
		what   string
		change map[string]any
	}{
		{"a new description", map[string]any{"statement": statementSaying("The sensor reads the pedal within 10 ms at any temperature.")}},
		{"a new attribute value", map[string]any{"attributes": map[string]any{weight: 5}}},
	} {
		base := revision()
		c.change["baseRevision"] = base
		want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), c.change), http.StatusOK, c.what)
		moved := s.linkOf(t, s.carol, s.req("2"), s.link)["suspect"].([]any)
		if len(moved) != 1 || moved[0].(map[string]any)["current"] != base+1 {
			t.Errorf("after %s R2's link reads %v", c.what, moved)
		}
		want(t, s.bob.post(t, "/api/v1/links/"+s.link+"/clear", map[string]any{"sourceRevision": 1, "targetRevision": base + 1, "comment": "Still holds."}),
			http.StatusOK, "confirm the link after "+c.what)
	}

	base := revision()
	want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), map[string]any{"priority": "must", "ownerId": s.bobID.String()}), http.StatusOK,
		"give R1 an owner and a priority")
	if now := revision(); now != base {
		t.Errorf("an owner and a priority made revision %v of R1", now)
	}
	if moved := s.linkOf(t, s.carol, s.req("2"), s.link)["suspect"].([]any); len(moved) != 0 {
		t.Errorf("an owner and a priority turned the link: %v", moved)
	}
}

// list_links answers what the links route answers, and refuses a requirement the token's person may not see (#313).
func TestAnAssistantListsLinksAsTheRouteDoes(t *testing.T) {
	s := newSuspectScene(t, "SLL")
	hidden := hiddenProject(t, s.h, s.owner, projectKey(t, "SLH"))
	s.revise(t, s.req("1"), 1)
	_, secret := makeToken(t, s.bob, map[string]any{"name": "link reader"})
	s.h.settle(t)
	a := connectAssistant(t, s.bob.api, secret)
	read := func(links []any) []string {
		var out []string
		for _, each := range links {
			l := each.(map[string]any)
			out = append(out, fmt.Sprintf("%s %s %s %s, %d ends moved on", l["id"], l["direction"], l["phrase"], l["other"].(map[string]any)["key"], len(l["suspect"].([]any))))
		}
		return out
	}

	got := read(list(t, response{Body: a.call(t, "list_links", map[string]any{"reqKey": s.req("1")})}, "links"))
	route := read(list(t, want(t, s.bob.get(t, "/api/v1/requirements/"+s.req("1")+"/links"), http.StatusOK, "the links of R1"), "links"))
	if len(route) != 3 {
		t.Fatalf("R1 has %d links, want the scene's three: %v", len(route), route)
	}
	sameList(t, "list_links", got, route...)
	if sentence := a.refused(t, "list_links", map[string]any{"reqKey": hidden}); sentence == "" || strings.Contains(sentence, "Brake fluid") {
		t.Errorf("list_links of a requirement bob does not see says %q", sentence)
	}
}
