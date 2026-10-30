//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Suspect links (#36): a revision of the end a link's type watches turns it suspect, the far end's people hear of
// it, and clearing confirms it against the revisions read, through the API and straight through SQL.

// suspectScene is a comment world with R2 narrowing R1, a link watching its target; R3 mentioning R1, a link
// watching nothing; and R4 mirroring R1, a link watching both ends.
type suspectScene struct {
	*commentWorld
	narrows, mentions, mirrors string
	link, quiet, mutual        string
}

func (s *suspectScene) req(n string) string { return s.key + "-R" + n }

func (s *suspectScene) linkType(t *testing.T, name, watch string) string {
	t.Helper()
	made := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/link-types", map[string]any{
		"name": name, "forward": strings.ToLower(name), "backward": "is " + strings.ToLower(name) + " by", "suspectEnd": watch,
	}), http.StatusCreated, "make the type "+name), "linkType")
	return made["id"].(string)
}

func (s *suspectScene) write(t *testing.T, title string) {
	t.Helper()
	want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
}

func (s *suspectScene) linkTo(t *testing.T, from, typeID string) string {
	t.Helper()
	made := obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+from+"/links", map[string]any{"typeId": typeID, "otherKey": s.req("1")}),
		http.StatusCreated, "link "+from+" to R1"), "link")
	return made["id"].(string)
}

func (s *suspectScene) revise(t *testing.T, key string, base int) {
	t.Helper()
	want(t, s.bob.patch(t, "/api/v1/requirements/"+key, map[string]any{"title": "Revised " + uuid.NewString()[:8], "baseRevision": base}),
		http.StatusOK, "bob revises "+key)
}

func newSuspectScene(t *testing.T, prefix string) *suspectScene {
	t.Helper()
	s := &suspectScene{commentWorld: newCommentWorld(t, prefix)}
	s.narrows, s.mentions, s.mirrors = s.linkType(t, "Narrows", "target"), s.linkType(t, "Mentions", "none"), s.linkType(t, "Mirrors", "both")
	s.write(t, "Brake pedal sensor reads within 10 ms")
	s.write(t, "Brake lights follow the pedal")
	s.write(t, "Brake log keeps every press")
	s.link, s.quiet, s.mutual = s.linkTo(t, s.req("2"), s.narrows), s.linkTo(t, s.req("3"), s.mentions), s.linkTo(t, s.req("4"), s.mirrors)
	return s
}

// linkOf is the link with id as the requirement key reads it.
func (s *suspectScene) linkOf(t *testing.T, c *client, key, id string) map[string]any {
	t.Helper()
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+key+"/links"), http.StatusOK, "the links of "+key), "links") {
		if l := each.(map[string]any); l["id"] == id {
			return l
		}
	}
	t.Fatalf("%s has no link %s", key, id)
	return nil
}

// suspectCounts is suspectLinks of every row of the grid as c reads it with the query given.
func (s *suspectScene) suspectCounts(t *testing.T, c *client, query string) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+s.key+"/requirements"+query), http.StatusOK, "the grid"), "rows") {
		row := each.(map[string]any)
		out[row["key"].(string)] = row["suspectLinks"].(float64)
	}
	return out
}

func TestALinkTurnsSuspectAndIsCleared(t *testing.T) {
	s := newSuspectScene(t, "SUS")
	want(t, s.carol.put(t, "/api/v1/requirements/"+s.req("2")+"/watch", nil), http.StatusOK, "carol watches R2")
	t.Run("a new link holds until the end its type watches moves on", s.holds)
	t.Run("a revision of the watched end turns the link, and the far end's people hear of it once", s.turns)
	t.Run("the grid, the specification and the project's list show it", s.shown)
	t.Run("clearing confirms the revisions read and is recorded", s.clears)
	t.Run("a type watching both ends turns from either", s.both)
	t.Run("an end in the trash waits, and comes back suspect", s.trash)
}

func (s *suspectScene) holds(t *testing.T) {
	for _, id := range []string{s.link, s.quiet, s.mutual} {
		if l := s.linkOf(t, s.carol, s.req("1"), id); len(l["suspect"].([]any)) != 0 || l["targetRevision"] != float64(1) {
			t.Errorf("a new link reads %v", l)
		}
	}
	s.revise(t, s.req("2"), 1)
	if l := s.linkOf(t, s.carol, s.req("2"), s.link); len(l["suspect"].([]any)) != 0 {
		t.Errorf("a revision of the end nobody watches turned the link: %v", l)
	}
}

func (s *suspectScene) turns(t *testing.T) {
	s.revise(t, s.req("1"), 1)
	l := s.linkOf(t, s.carol, s.req("2"), s.link)
	moved := l["suspect"].([]any)
	if len(moved) != 1 || moved[0].(map[string]any)["end"] != "target" || moved[0].(map[string]any)["reviewed"] != float64(1) ||
		moved[0].(map[string]any)["current"] != float64(2) {
		t.Errorf("R2's link after R1 moved on reads %v", l)
	}
	if l := s.linkOf(t, s.carol, s.req("3"), s.quiet); len(l["suspect"].([]any)) != 0 {
		t.Errorf("a link watching nothing turned: %v", l)
	}
	if l := s.linkOf(t, s.carol, s.req("4"), s.mutual); len(l["suspect"].([]any)) != 1 {
		t.Errorf("a link watching both ends did not turn with R1: %v", l)
	}
	s.revise(t, s.req("1"), 2)
	s.h.drained(t)
	for name, id := range map[string]uuid.UUID{"the owner, who wrote R2": s.ownerID, "carol, who watches it": s.carolID} {
		if n := s.h.countRows(t, `SELECT count(*) FROM notification n JOIN requirement r ON r.id = n.requirement_id
			WHERE n.user_id = $1 AND n.kind = 'suspect' AND n.link_id = $2 AND r.number = 2 AND r.project_id = $3`, id, s.link, s.projectID); n != 1 {
			t.Errorf("%s was told %d times of the link, want once", name, n)
		}
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND kind = 'suspect'`, s.bobID); n != 0 {
		t.Errorf("bob was told %d times of the links his own revision turned", n)
	}
}

func (s *suspectScene) shown(t *testing.T) {
	counts := s.suspectCounts(t, s.carol, "")
	if counts[s.req("1")] != 2 || counts[s.req("2")] != 1 || counts[s.req("3")] != 0 || counts[s.req("4")] != 1 {
		t.Errorf("the grid counts %v", counts)
	}
	only := s.suspectCounts(t, s.carol, "?suspect=true")
	if _, kept := only[s.req("3")]; kept || len(only) != 3 {
		t.Errorf("keeping requirements with a suspect link kept %v", only)
	}
	refuse(t, "a suspect filter that is no boolean", s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?suspect=maybe"), http.StatusUnprocessableEntity, "validation_failed")
	for _, each := range list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/spec"), http.StatusOK, "the specification"), "entries") {
		e := each.(map[string]any)
		if e["node"].(map[string]any)["key"] == s.req("2") && e["suspectLinks"] != float64(1) {
			t.Errorf("the specification shows R2 as %v", e)
		}
	}
	body := want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/suspects"), http.StatusOK, "the suspect links")
	links := list(t, body, "links")
	if obj(t, body)["total"] != float64(2) || len(links) != 2 {
		t.Fatalf("the project's suspect links are %v", links)
	}
	first := links[0].(map[string]any)
	if first["id"] != s.link || first["source"].(map[string]any)["key"] != s.req("2") || first["target"].(map[string]any)["key"] != s.req("1") ||
		first["type"].(map[string]any)["suspectEnd"] != "target" || len(first["ends"].([]any)) != 1 || first["since"] == nil {
		t.Errorf("the link waiting longest reads %v", first)
	}
	refuse(t, "too long a window", s.carol.get(t, "/api/v1/projects/"+s.key+"/suspects?limit=101"), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a project nobody may see", s.carol.get(t, "/api/v1/projects/NOPE"+strings.ToUpper(uuid.NewString()[:4])+"/suspects"), http.StatusNotFound, "not_found")
}

func (s *suspectScene) clears(t *testing.T) {
	clear := "/api/v1/links/" + s.link + "/clear"
	read := map[string]any{"sourceRevision": 2, "targetRevision": 3, "comment": "The lights still follow the faster sensor."}
	refuse(t, "carol, who may only read", s.carol.post(t, clear, read), http.StatusForbidden, "forbidden")
	refuse(t, "an older revision of R1", s.bob.post(t, clear, map[string]any{"sourceRevision": 2, "targetRevision": 2, "comment": "Fine."}),
		http.StatusConflict, "revision_moved_on")
	fieldError(t, s.bob.post(t, clear, map[string]any{"sourceRevision": 2, "targetRevision": 4, "comment": "Fine."}), "targetRevision")
	fieldError(t, s.bob.post(t, clear, map[string]any{"sourceRevision": 2, "targetRevision": 3, "comment": "  "}), "comment")
	fieldError(t, s.bob.post(t, clear, map[string]any{"sourceRevision": 2, "targetRevision": 3, "comment": strings.Repeat("x", 501)}), "comment")
	refuse(t, "a link that is none", s.bob.post(t, "/api/v1/links/"+uuid.NewString()+"/clear", read), http.StatusNotFound, "not_found")
	cleared := obj(t, want(t, s.bob.post(t, clear, read), http.StatusOK, "bob clears the link"), "link")
	if len(cleared["suspect"].([]any)) != 0 || cleared["sourceRevision"] != float64(2) || cleared["targetRevision"] != float64(3) || cleared["direction"] != "outgoing" {
		t.Errorf("the cleared link reads %v", cleared)
	}
	if event := s.h.lastEvent(t, "link.cleared", s.idOf(t, s.req("2"))); event["linkId"] != s.link || event["actorId"] != s.bobID.String() {
		t.Errorf("the event reads %v", event)
	}
	clearances := list(t, want(t, s.carol.get(t, "/api/v1/links/"+s.link+"/clearances"), http.StatusOK, "carol reads the clearances"), "clearances")
	if len(clearances) != 1 || clearances[0].(map[string]any)["comment"] != read["comment"] || clearances[0].(map[string]any)["clearedByName"] != "A member" ||
		clearances[0].(map[string]any)["targetRevision"] != float64(3) {
		t.Errorf("the clearances read %v", clearances)
	}
	refuse(t, "the clearances of a link that is none", s.carol.get(t, "/api/v1/links/"+uuid.NewString()+"/clearances"), http.StatusNotFound, "not_found")
	if counts := s.suspectCounts(t, s.carol, ""); counts[s.req("2")] != 0 || counts[s.req("1")] != 1 {
		t.Errorf("after clearing the grid counts %v", counts)
	}
}

func (s *suspectScene) idOf(t *testing.T, key string) string {
	t.Helper()
	return obj(t, want(t, s.carol.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")["id"].(string)
}

// isNoData recognises what the clearing functions raise for a link the person does not see.
func isNoData(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "P0002"
}

func (s *suspectScene) both(t *testing.T) {
	clear := map[string]any{"sourceRevision": 1, "targetRevision": 3, "comment": "Both still agree."}
	want(t, s.bob.post(t, "/api/v1/links/"+s.mutual+"/clear", clear), http.StatusOK, "bob clears R4's link")
	s.revise(t, s.req("4"), 1)
	l := s.linkOf(t, s.carol, s.req("1"), s.mutual)
	if moved := l["suspect"].([]any); len(moved) != 1 || moved[0].(map[string]any)["end"] != "source" {
		t.Errorf("after its source moved on R4's link reads %v", l)
	}
}

func (s *suspectScene) trash(t *testing.T) {
	want(t, s.owner.delete(t, "/api/v1/requirements/"+s.req("4")), http.StatusNoContent, "trash R4")
	if l := s.linkOf(t, s.bob, s.req("1"), s.mutual); len(l["suspect"].([]any)) != 0 {
		t.Errorf("a link to the trash reads %v", l)
	}
	refuse(t, "clearing a link to the trash", s.bob.post(t, "/api/v1/links/"+s.mutual+"/clear", map[string]any{"sourceRevision": 2, "targetRevision": 3, "comment": "Fine."}),
		http.StatusConflict, "requirement_trashed")
	items := list(t, want(t, s.owner.get(t, "/api/v1/projects/"+s.key+"/trash"), http.StatusOK, "open the trash"), "items")
	want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/trash/"+items[0].(map[string]any)["id"].(string)+"/restore", nil), http.StatusOK, "restore R4")
	if l := s.linkOf(t, s.bob, s.req("1"), s.mutual); len(l["suspect"].([]any)) != 1 {
		t.Errorf("a link back from the trash reads %v", l)
	}
}

// Straight through SQL as exciter_app: confirmed revisions change only by clearing, held to the ends' rights and
// current revisions; clearances never change, and the state shows only what the reader sees.
func TestRawSQLKeepsSuspectLinksHonest(t *testing.T) {
	s := newSuspectScene(t, "SUQ")
	h := s.h
	ctx := context.Background()
	s.revise(t, s.req("1"), 1)
	h.settle(t)
	bob, carol, stranger := h.appConn(t, s.bobID), h.appConn(t, s.carolID), h.appConn(t, h.addPerson(t, "member"))
	// Every member views the project through its grant to everyone, which would let the stranger in.
	if _, err := h.super.Exec(ctx, `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, s.projectID); err != nil {
		t.Fatal(err)
	}
	h.grantAs(t, s.projectID, s.carolID, "viewer")

	if _, err := bob.Exec(ctx, `UPDATE trace_link SET target_reviewed_rev = 2 WHERE id = $1`, s.link); !isPermissionDenied(err) {
		t.Errorf("an editor moved the confirmed revision on without clearing: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO trace_link_clearance (link_id, source_rev, target_rev, comment, cleared_by) VALUES ($1, 1, 2, 'Fine.', $2)`,
		s.link, s.bobID); !isPermissionDenied(err) {
		t.Errorf("an editor wrote a clearance without clearing: %v", err)
	}
	clear := `SELECT trace_link_clear($1, $2, $3, $4)`
	if _, err := carol.Exec(ctx, clear, s.link, 1, 2, "Fine."); !isPermissionDenied(err) {
		t.Errorf("a reader cleared a link: %v", err)
	}
	if _, err := stranger.Exec(ctx, clear, s.link, 1, 2, "Fine."); !isNoData(err) {
		t.Errorf("somebody who sees neither end cleared a link: %v", err)
	}
	if _, err := bob.Exec(ctx, clear, s.link, 1, 1, "Fine."); !refusedBy(err, "trace_link_clear_current") {
		t.Errorf("a link was cleared against a revision R1 has moved past: %v", err)
	}
	if _, err := bob.Exec(ctx, clear, s.link, 1, 2, " "); !refusedBy(err, "trace_link_clearance_comment") {
		t.Errorf("a link was cleared without saying why: %v", err)
	}
	var n int
	if err := stranger.QueryRow(ctx, `SELECT count(*) FROM trace_link_state WHERE id = $1`, s.link).Scan(&n); err != nil || n != 0 {
		t.Errorf("somebody who sees neither end reads the link's state: %d, %v", n, err)
	}
	var suspect bool
	if err := carol.QueryRow(ctx, `SELECT suspect FROM trace_link_state WHERE id = $1`, s.link).Scan(&suspect); err != nil || !suspect {
		t.Errorf("a reader reads the link as suspect %v: %v", suspect, err)
	}
	if _, err := bob.Exec(ctx, clear, s.link, 1, 2, "The lights still follow."); err != nil {
		t.Fatalf("bob could not clear the link: %v", err)
	}
	if err := carol.QueryRow(ctx, `SELECT suspect FROM trace_link_state WHERE id = $1`, s.link).Scan(&suspect); err != nil || suspect {
		t.Errorf("after clearing the link reads as suspect %v: %v", suspect, err)
	}
	if _, err := bob.Exec(ctx, `DELETE FROM trace_link_clearance WHERE link_id = $1`, s.link); !isPermissionDenied(err) {
		t.Errorf("an editor deleted a clearance: %v", err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE trace_link_clearance SET comment = 'Rewritten.' WHERE link_id = $1`, s.link); !refusedBy(err, "trace_link_clearance_fixed") {
		t.Errorf("a clearance was rewritten: %v", err)
	}
	if err := carol.QueryRow(ctx, `SELECT count(*) FROM trace_link_clearance WHERE link_id = $1`, s.link).Scan(&n); err != nil || n != 1 {
		t.Errorf("a reader sees %d clearances: %v", n, err)
	}
	if err := stranger.QueryRow(ctx, `SELECT count(*) FROM trace_link_clearance WHERE link_id = $1`, s.link).Scan(&n); err != nil || n != 0 {
		t.Errorf("somebody who sees neither end sees %d clearances: %v", n, err)
	}
	if _, err := bob.Exec(ctx, `UPDATE armature_issue_link SET reviewed_rev = 9`); !isPermissionDenied(err) {
		t.Errorf("an editor moved an Armature link's confirmed revision on: %v", err)
	}
	if _, err := bob.Exec(ctx, `SELECT armature_issue_link_clear($1, 1, 'Fine.')`, uuid.New()); !isNoData(err) {
		t.Errorf("clearing an Armature link that is none: %v", err)
	}
}
