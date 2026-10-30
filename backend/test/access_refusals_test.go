//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/demo"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// auditWindow is how many entries a window of the walk over HTTP holds, small so six entries take three windows.
const auditWindow = 2

// projectRow is a project as the superuser reads it, so a refusal is seen to leave it as it was.
func (h *harness) projectRow(t *testing.T, id string) string {
	t.Helper()
	var row string
	if err := h.super.QueryRow(context.Background(), `
		SELECT concat_ws('|', key, name, archived_at IS NOT NULL,
		       (SELECT string_agg(a.key, ',' ORDER BY a.key) FROM project_key_alias a WHERE a.project_id = p.id))
		FROM project p WHERE id = $1`, id).Scan(&row); err != nil {
		t.Fatalf("read the project %s: %v", id, err)
	}
	return row
}

// A member made the project's administrator by a grant runs it as its maker does, while an editor of the same
// project is refused every change only its administrators make, and the project stays as it was.
func TestAProjectAdministratorByGrantRunsItAndAnEditorDoesNot(t *testing.T) {
	key, moved, gone := projectKey(t, "PAG"), projectKey(t, "PAM"), projectKey(t, "PAX")
	f := newProjectFixture(t)
	adminID, editorID := f.h.namedPerson(t, "Ada Granted"), f.h.namedPerson(t, "Eddie Editor")
	admin, editor := f.api.as(t, adminID), f.api.as(t, editorID)
	id := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Granted"}), http.StatusCreated, "make a project"), "project")["id"].(string)
	grants := func(c *client, rows ...any) response { return c.setGrants(t, key, rows) }
	want(t, grants(f.owner,
		grantRow(subjectOf("user", f.home.user), "admin"),
		grantRow(subjectOf("user", adminID), "admin"),
		grantRow(subjectOf("user", editorID), "editor"),
	), http.StatusOK, "make the member an administrator by grant")

	t.Run("an editor changes nothing only an administrator may", func(t *testing.T) {
		before := f.h.projectRow(t, id)
		refuse(t, "an editor renames the project", editor.patch(t, "/api/v1/projects/"+key, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "an editor gives the project a new key", editor.put(t, "/api/v1/projects/"+key+"/key", map[string]any{"key": moved}), http.StatusForbidden, "forbidden")
		refuse(t, "an editor replaces the grants", grants(editor, grantRow(subjectOf("user", editorID), "admin")), http.StatusForbidden, "forbidden")
		refuse(t, "an editor archives the project", editor.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusForbidden, "forbidden")
		if after := f.h.projectRow(t, id); after != before {
			t.Errorf("the editor's refusals changed the project from %s to %s", before, after)
		}
		got := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+key), http.StatusOK, "read the project"), "project")
		if got["key"] != key || len(got["formerKeys"].([]any)) != 0 {
			t.Errorf("the key moved on an editor's refusal: %v", got)
		}
		if can := obj(t, want(t, editor.get(t, "/api/v1/projects/"+key), http.StatusOK, "read as the editor"), "project", "can"); can["administer"] != false || can["edit"] != true {
			t.Errorf("the editor's rights read %v", can)
		}
	})

	t.Run("the administrator by grant renames it, moves its key and replaces the grants", func(t *testing.T) {
		if can := obj(t, want(t, admin.get(t, "/api/v1/projects/"+key), http.StatusOK, "read as the administrator"), "project", "can"); can["administer"] != true {
			t.Fatalf("a grant of admin does not let the member administer: %v", can)
		}
		renamed := obj(t, want(t, admin.patch(t, "/api/v1/projects/"+key, map[string]any{"name": "Run by grant"}), http.StatusOK, "rename"), "project")
		if renamed["name"] != "Run by grant" {
			t.Errorf("the rename reads %v", renamed)
		}
		got := obj(t, want(t, admin.put(t, "/api/v1/projects/"+key+"/key", map[string]any{"key": moved}), http.StatusOK, "move the key"), "project")
		if got["key"] != moved || len(got["formerKeys"].([]any)) != 1 || got["formerKeys"].([]any)[0] != key {
			t.Errorf("the key moved to %v", got)
		}
		want(t, admin.setGrants(t, moved, []any{
			grantRow(subjectOf("user", f.home.user), "admin"),
			grantRow(subjectOf("user", adminID), "admin"),
			grantRow(subjectOf("user", editorID), "viewer"),
		}), http.StatusOK, "replace the grants")
		if rows := admin.grantsOf(t, moved); !slices.Contains(rows, "viewer:Eddie Editor") || slices.Contains(rows, "editor:Eddie Editor") {
			t.Errorf("the editor is not a viewer alone after the replacement: %v", rows)
		}
		if can := obj(t, want(t, editor.get(t, "/api/v1/projects/"+moved), http.StatusOK, "read as the viewer"), "project", "can"); can["edit"] != false {
			t.Errorf("the replaced grants still let the editor edit: %v", can)
		}
	})

	t.Run("an archived project is deleted by its administrator alone", func(t *testing.T) {
		// Eddie is an editor again, so the refusals below are an editor's and not a viewer's.
		want(t, admin.setGrants(t, moved, []any{
			grantRow(subjectOf("user", f.home.user), "admin"),
			grantRow(subjectOf("user", adminID), "admin"),
			grantRow(subjectOf("user", editorID), "editor"),
		}), http.StatusOK, "make him an editor again")
		if rows := admin.grantsOf(t, moved); !slices.Contains(rows, "editor:Eddie Editor") {
			t.Fatalf("Eddie is not an editor again: %v", rows)
		}
		if can := obj(t, want(t, editor.get(t, "/api/v1/projects/"+moved), http.StatusOK, "read as the editor again"), "project", "can"); can["edit"] != true || can["administer"] != false {
			t.Fatalf("the editor's rights read %v", can)
		}
		want(t, admin.put(t, "/api/v1/projects/"+moved+"/archive", nil), http.StatusOK, "archive")
		before := f.h.projectRow(t, id)
		refuse(t, "the editor deletes it", editor.delete(t, "/api/v1/projects/"+moved), http.StatusForbidden, "forbidden")
		refuse(t, "the editor unarchives it", editor.delete(t, "/api/v1/projects/"+moved+"/archive"), http.StatusForbidden, "forbidden")
		if after := f.h.projectRow(t, id); after != before {
			t.Errorf("the refusals changed the archived project from %s to %s", before, after)
		}
		refuse(t, "deleting a key nobody has", admin.delete(t, "/api/v1/projects/"+gone), http.StatusNotFound, "not_found")
		refuse(t, "archiving a key nobody has", admin.put(t, "/api/v1/projects/"+gone+"/archive", nil), http.StatusNotFound, "not_found")
		want(t, admin.delete(t, "/api/v1/projects/"+moved), http.StatusNoContent, "delete it")
		if n := f.h.countRows(t, `SELECT count(*) FROM project WHERE id = $1`, id); n != 0 {
			t.Errorf("the deleted project is still there %d times", n)
		}
	})
}

// An administrator by grant may hand the project over, taking their own admin grant away once another holds one.
func TestAnAdministratorByGrantHandsTheProjectOver(t *testing.T) {
	key := projectKey(t, "PAH")
	f := newProjectFixture(t)
	leavingID := f.h.namedPerson(t, "Lea Leaving")
	leaving := f.api.as(t, leavingID)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Handed over"}), http.StatusCreated, "make a project")
	want(t, f.owner.setGrants(t, key, []any{grantRow(subjectOf("user", leavingID), "admin")}), http.StatusOK, "make her its administrator")

	want(t, leaving.setGrants(t, key, []any{
		grantRow(subjectOf("user", f.home.user), "admin"),
		grantRow(subjectOf("user", leavingID), "editor"),
	}), http.StatusOK, "hand the project over")
	sameList(t, "the grants as stored", f.owner.grantsOf(t, key), "admin:Person of projects", "editor:Lea Leaving")
	if can := obj(t, want(t, leaving.get(t, "/api/v1/projects/"+key), http.StatusOK, "read as the editor she is now"), "project", "can"); can["administer"] != false || can["edit"] != true {
		t.Errorf("after handing it over she reads %v", can)
	}
	refuse(t, "her granting herself again", leaving.post(t, "/api/v1/role-assignments", map[string]any{"role": "admin", "projectKey": key, "userId": leavingID}),
		http.StatusForbidden, "forbidden")
}

// A group's grant across the installation is an administrator's alone to give and take, granting it again is the
// one grant, and the next sign-in follows it.
func TestAGroupsGrantIsKeptFromMembers(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, aliceEmail, "member")
	admin, _ := adminOf(t, h, a)
	member := a.as(t, h.addPerson(t, "member"))
	const group = "exciter-administrators"

	id := mapGroup(t, admin, group, "administrator")
	if b := signedIn(t, a, "alice", alicePassword); !b.administers(t) {
		t.Fatal("alice does not administer with her group granted the administrator role")
	}

	t.Run("a member takes nothing away and grants nothing", func(t *testing.T) {
		refuse(t, "a member takes the group's grant", member.delete(t, "/api/v1/role-assignments/"+id), http.StatusForbidden, "forbidden")
		refuse(t, "a member grants the group a role", member.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "groupId": providerGroup(t, admin, group)}),
			http.StatusForbidden, "forbidden")
		if n := h.countRows(t, `SELECT count(*) FROM role_assignment WHERE id = $1`, id); n != 1 {
			t.Fatal("the group's grant went on a member's refusal")
		}
	})

	t.Run("the same grant posted again is the one grant", func(t *testing.T) {
		if again := mapGroup(t, admin, group, "administrator"); again != id {
			t.Errorf("granting the group again answered the grant %s, want %s", again, id)
		}
		if n := h.countRows(t, `SELECT count(*) FROM role_assignment a JOIN groups g ON g.id = a.group_id
			WHERE g.external_ref = $1 AND a.project_id IS NULL AND a.role = 'administrator'`, group); n != 1 {
			t.Fatalf("the group holds the role %d times", n)
		}
	})

	t.Run("taken away, the next sign-in follows", func(t *testing.T) {
		want(t, admin.delete(t, "/api/v1/role-assignments/"+id), http.StatusNoContent, "take the group's grant")
		if b := signedIn(t, a, "alice", alicePassword); b.roleIn(t) != "member" || b.administers(t) {
			t.Fatal("alice still administers after her group's grant went")
		}
		if role := h.standing(t, aliceEmail); role != "member" {
			t.Fatalf("alice's membership is %s", role)
		}
	})
}

// A waiting person is turned away by an administrator alone, and let in only as a member or an administrator;
// every refusal leaves them waiting.
func TestAJoinRequestIsAnsweredByAnAdministratorAsMemberOrAdmin(t *testing.T) {
	h := newHarness(t)
	h.singleSignOn(t)
	a := newAPIServer(t, h)
	admin := a.as(t, h.addPerson(t, "admin"))
	member := a.as(t, h.addPerson(t, "member"))
	waiting := h.outsider(t, "waits")
	if _, err := h.super.Exec(context.Background(), `INSERT INTO join_request (user_id) VALUES ($1)`, waiting); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	stillWaits := func(what string) {
		t.Helper()
		if n := h.countRows(t, `SELECT count(*) FROM join_request WHERE user_id = $1`, waiting); n != 1 {
			t.Errorf("after %s the request is there %d times, want once", what, n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM app_user WHERE id = $1 AND role IS NULL`, waiting); n != 1 {
			t.Errorf("after %s the waiting person was given a role", what)
		}
	}
	path := joinRequestsPath + "/" + waiting.String()

	refuse(t, "a member turns somebody away", member.delete(t, path), http.StatusForbidden, "forbidden")
	stillWaits("a member's refusal")
	for _, role := range []string{"guest", "wizard"} {
		got := want(t, admin.post(t, path+"/admit", map[string]string{"role": role}), http.StatusUnprocessableEntity, "admit as "+role)
		if code := errorCode(t, got); code != "validation_failed" {
			t.Errorf("admitting as %s answers %s", role, code)
		}
		if msg := fieldError(t, got, "role"); msg == "" {
			t.Errorf("admitting as %s names no role", role)
		}
		stillWaits("admitting as " + role)
	}
	refuse(t, "a malformed id", admin.delete(t, joinRequestsPath+"/not-an-id"), http.StatusBadRequest, "bad_request")
	refuse(t, "somebody who is not waiting", admin.delete(t, joinRequestsPath+"/"+uuid.NewString()), http.StatusNotFound, "not_found")
	stillWaits("the refusals")
	want(t, admin.delete(t, path), http.StatusNoContent, "turn them away")
	if n := h.countRows(t, `SELECT count(*) FROM join_request WHERE user_id = $1`, waiting); n != 0 {
		t.Errorf("the request stays after an administrator answered it")
	}
}

// A provider saved without a client id is refused by field, and the one saved before still answers.
func TestAProviderWithoutAClientIDIsRefusedAndTheSavedOneKept(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	admin, _ := adminOf(t, h, a)
	saved := map[string]any{"issuer": "https://id.example.test/realms/acme", "clientId": "exciter", "clientSecret": "kept", "enabled": true, "createGroups": false}
	want(t, admin.put(t, providerPath, saved), http.StatusOK, "save the provider")

	for what, body := range map[string]map[string]any{
		"no client id":      {"issuer": "https://other.example.test", "enabled": true, "createGroups": true},
		"a blank client id": {"issuer": "https://other.example.test", "clientId": "   ", "enabled": true, "createGroups": true},
	} {
		got := want(t, admin.put(t, providerPath, body), http.StatusUnprocessableEntity, what)
		if code := errorCode(t, got); code != "validation_failed" {
			t.Errorf("%s answers %s", what, code)
		}
		if msg := fieldError(t, got, "clientId"); msg == "" {
			t.Errorf("%s names no clientId", what)
		}
	}
	p := obj(t, want(t, admin.get(t, providerPath), http.StatusOK, "read the provider"), "provider")
	if p["issuer"] != saved["issuer"] || p["clientId"] != "exciter" || p["hasSecret"] != true || p["createGroups"] != false {
		t.Errorf("the refusals changed the saved provider: %v", p)
	}
}

// Linking the demo again is an administrator's alone: a member is refused, the demo stays done and no step is queued.
func TestAMemberCannotLinkTheSuiteDemoAgain(t *testing.T) {
	h := newHarness(t)
	h.keepSuiteDemo(t)
	keys := []string{projectKey(t, "DLA"), projectKey(t, "DLX")}
	a := newAPIServerWith(t, h, func(_ *apiServer, s *httpapi.Server) {
		s.Demo = h.demoService(t).WithSnapshot(demoFixture(), demoTopic, keys)
	})
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM outbox_event WHERE topic = $1`, demoTopic) })
	ctx := context.Background()
	since := h.started(t)
	owner := h.owner(t, "demo-links")
	admin, member := a.as(t, owner.user), a.as(t, h.addPerson(t, "member"))
	if _, err := h.super.Exec(ctx, `INSERT INTO suite_demo (taken, project_keys, state, kind) VALUES ('2026-10-20', $1, 'done', 'build')`, keys); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	demoRow := func() string {
		var row string
		if err := h.super.QueryRow(ctx, `SELECT concat_ws('|', state, kind, step, steps, requested_by) FROM suite_demo`).Scan(&row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	queued := func() int {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = ANY($1) AND created_at >= $2`,
			[]string{demoTopic, demo.TopicRequested}, since)
	}

	before := demoRow()
	refuse(t, "a member links the demo again", member.post(t, "/api/v1/installation/suite-demo/links", nil), http.StatusForbidden, "forbidden")
	if after := demoRow(); after != before {
		t.Errorf("a member's refusal moved the demo from %s to %s", before, after)
	}
	if n := queued(); n != 0 {
		t.Errorf("a member's refusal queued %d demo steps", n)
	}
	relinked := obj(t, want(t, admin.post(t, "/api/v1/installation/suite-demo/links", nil), http.StatusAccepted, "an administrator links it again"), "demo")
	if relinked["state"] != "queued" || relinked["kind"] != "links" {
		t.Errorf("linking again reads %v", relinked)
	}
	if n := queued(); n != 1 {
		t.Errorf("an administrator's link queued %d steps, want one", n)
	}
}

// Over HTTP the log narrows by target, kind of target and days, a cursor walks it with no entry twice, and an
// export narrowed by action holds that action alone and is recorded with the filter.
func TestTheAuditLogIsNarrowedAndWalkedOverHTTP(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	adminID := h.addPerson(t, "admin")
	admin := a.as(t, adminID)
	since := h.started(t)
	annID := h.namedPerson(t, "Ann Narrow")
	project := uuid.New()
	day := time.Date(2002, time.April, 1, 0, 0, 0, 0, time.UTC)
	at := func(days int) time.Time { return day.AddDate(0, 0, days).Add(12 * time.Hour) }
	planted := []uuid.UUID{
		h.plantEntry(t, "project.created", &adminID, "project", &project, at(0)),
		h.plantEntry(t, "project.key_changed", &annID, "project", &project, at(1)),
		h.plantEntry(t, "member.admitted", &adminID, "user", &annID, at(2)),
		h.plantEntry(t, "member.role_changed", &adminID, "user", &annID, at(2)),
		h.plantEntry(t, "member.role_changed", &adminID, "user", &annID, at(3)),
		h.plantEntry(t, "project.archived", &annID, "project", &project, at(4)),
	}
	h.settle(t)
	stretch := "from=" + at(0).Format("2006-01-02") + "&to=" + at(4).Format("2006-01-02")
	ids := func(r response) []string {
		var out []string
		for _, each := range list(t, r, "entries") {
			out = append(out, each.(map[string]any)["id"].(string))
		}
		return out
	}
	idsOf := func(which ...int) []string {
		var out []string
		for _, i := range which {
			out = append(out, planted[i].String())
		}
		return out
	}
	read := func(query string) []string {
		t.Helper()
		return ids(want(t, admin.get(t, "/api/v1/audit?"+query), http.StatusOK, query))
	}

	sameList(t, "by target", read("target="+project.String()), idsOf(5, 1, 0)...)
	sameList(t, "by kind of target", read(stretch+"&targetType=user"), idsOf(4, 3, 2)...)
	sameList(t, "by days", read("from="+at(1).Format("2006-01-02")+"&to="+at(2).Format("2006-01-02")), idsOf(3, 2, 1)...)
	sameList(t, "by target and kind", read("target="+annID.String()+"&targetType=project"))

	t.Run("a cursor walks the stretch window by window", func(t *testing.T) {
		var walked []string
		cursor := ""
		for range len(planted) + 1 {
			query := stretch + "&limit=" + strconv.Itoa(auditWindow)
			if cursor != "" {
				query += "&cursor=" + url.QueryEscape(cursor)
			}
			got := want(t, admin.get(t, "/api/v1/audit?"+query), http.StatusOK, "a window")
			page := ids(got)
			if len(page) > auditWindow {
				t.Fatalf("a window of %d holds %d entries", auditWindow, len(page))
			}
			walked = append(walked, page...)
			next, _ := got.Body["next"].(string)
			if next == "" {
				break
			}
			cursor = next
		}
		sameList(t, "the walk", walked, idsOf(5, 4, 3, 2, 1, 0)...)
	})

	t.Run("an export narrowed by action holds that action alone and records the filter", func(t *testing.T) {
		file := want(t, admin.get(t, "/api/v1/audit/export?action="+audit.ActionMemberRoleChanged), http.StatusOK, "export by action")
		lines, err := csv.NewReader(bytes.NewReader(file.Raw)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) < 3 {
			t.Fatalf("the export holds %d lines, want the header and at least the two planted", len(lines))
		}
		ours := 0
		for _, line := range lines[1:] {
			if line[1] != audit.ActionMemberRoleChanged {
				t.Errorf("the export holds a %s entry", line[1])
			}
			if line[5] == annID.String() && line[2] == adminID.String() {
				ours++
			}
		}
		if ours != 2 {
			t.Errorf("the export holds %d of the two planted role changes", ours)
		}
		var raw []byte
		if err := h.super.QueryRow(context.Background(), `
			SELECT data FROM audit_log WHERE action = $1 AND actor_user_id = $2 AND created_at >= $3`,
			audit.ActionAuditExported, adminID, since).Scan(&raw); err != nil {
			t.Fatalf("the export was not recorded once: %v", err)
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		if data["action"] != audit.ActionMemberRoleChanged || data["entries"] != float64(len(lines)-1) || len(data) != 2 {
			t.Errorf("the export is recorded as %v, want the action and %d entries", data, len(lines)-1)
		}
	})
}

// Straight through SQL as exciter_app, a plain member switches nothing of the installation's, while an
// administrator's same statements reach its one row.
func TestRawSQLKeepsTheInstallationsSwitchesToAdministrators(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	adminID, plainID := h.addPerson(t, "admin"), h.addPerson(t, "member")
	switches := func() string {
		var row string
		if err := h.super.QueryRow(ctx, `SELECT concat_ws('|', oauth_connections, default_theme_id) FROM installation`).Scan(&row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	statements := map[string]string{
		"switching OAuth off":        `UPDATE installation SET oauth_connections = false`,
		"clearing the default theme": `UPDATE installation SET default_theme_id = NULL`,
	}

	before := switches()
	plain := h.appConn(t, plainID)
	for what, sql := range statements {
		touchesNothing(t, plain, "a plain member "+what, sql)
	}
	if after := switches(); after != before {
		t.Errorf("a plain member's statements changed the installation from %s to %s", before, after)
	}

	tx, err := h.appConn(t, adminID).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for what, sql := range statements {
		if tag, err := tx.Exec(ctx, sql); err != nil || tag.RowsAffected() != 1 {
			t.Errorf("an administrator %s reached %d rows (%v), want the one", what, tag.RowsAffected(), err)
		}
	}
}

// A malformed id or body is refused before anything is touched, and switching OAuth takes a yes or a no alone.
func TestMalformedAccessRequestsAreRefusedAsTheyAre(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	admin := a.as(t, h.addPerson(t, "admin"))
	before := h.countRows(t, `SELECT count(*) FROM installation WHERE oauth_connections`)

	refuse(t, "switching off a malformed id", admin.delete(t, "/api/v1/users/not-an-id"), http.StatusBadRequest, "bad_request")
	refuse(t, "turning away a malformed id", admin.delete(t, joinRequestsPath+"/not-an-id"), http.StatusBadRequest, "bad_request")
	refuse(t, "OAuth switched with a word", admin.put(t, "/api/v1/installation/oauth-connections", map[string]any{"enabled": "off"}), http.StatusBadRequest, "bad_request")
	refuse(t, "OAuth switched with a field it does not take", admin.put(t, "/api/v1/installation/oauth-connections", map[string]any{"enabled": true, "everyone": false}), http.StatusBadRequest, "bad_request")
	if after := h.countRows(t, `SELECT count(*) FROM installation WHERE oauth_connections`); after != before {
		t.Errorf("a refused switch moved OAuth connections from %d to %d", before, after)
	}
	refuse(t, "signing out without a session", a.anonymous().post(t, "/api/v1/auth/logout", nil), http.StatusUnauthorized, "unauthorized")
}
