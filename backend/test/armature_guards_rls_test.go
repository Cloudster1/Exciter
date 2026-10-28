//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Straight through SQL (#326): confirming an Armature trace, the link sync's records and a person's Armature token
// are guarded by the database itself, as the API's refusals are.

// noSuchLink is the SQLSTATE armature_issue_link_clear raises for a link the actor may not see.
const noSuchLink = "P0002"

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestRawSQLGuardsArmatureClearancesAndSyncRecords(t *testing.T) {
	key := projectKey(t, "ACW")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-clear-wall")
	owner := api.as(t, home.user)
	editorID := h.addPerson(t, "member")
	viewerID := h.addPerson(t, "guest")
	strangerID := h.addPerson(t, "guest")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	project := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Walled clearances"}), http.StatusCreated, "make a project"), "project")
	live := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Live"}), http.StatusCreated, "write a requirement"), "requirement")
	h.grantAs(t, uuid.MustParse(project["id"].(string)), viewerID, "viewer")
	ctx := context.Background()
	editor := h.appConn(t, editorID)
	var linkID uuid.UUID
	if err := editor.QueryRow(ctx, `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, created_by) VALUES ($1, 'implements', 'CP-1', $2) RETURNING id`,
		live["id"], editorID).Scan(&linkID); err != nil {
		t.Fatalf("an editor links in their own name: %v", err)
	}
	want(t, owner.patch(t, "/api/v1/requirements/"+key+"-R1", map[string]any{"title": "Live and revised", "baseRevision": 1}), http.StatusOK, "revise the requirement")
	if _, err := h.super.Exec(ctx, `
		INSERT INTO armature_remote_link (requirement_id, issue_key, remote_link_id, url, title, state, synced_at)
		VALUES ($1, 'CP-1', $2, 'https://exciter.example/r/x', 'ACW-R1 Live (implements)', 'synced', now())`, live["id"], uuid.New()); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	clear := `SELECT armature_issue_link_clear($1, $2, 'Still holds.')`
	cleared := func() int {
		return h.countRows(t, `SELECT count(*) FROM trace_link_clearance WHERE armature_link_id = $1`, linkID)
	}

	t.Run("only an editor clears, against the revision the requirement has", func(t *testing.T) {
		if _, err := h.appConn(t, viewerID).Exec(ctx, clear, linkID, 2); !isPermissionDenied(err) {
			t.Errorf("a viewer clearing = %v, want insufficient_privilege", err)
		}
		if _, err := h.appConn(t, strangerID).Exec(ctx, clear, linkID, 2); sqlState(err) != noSuchLink {
			t.Errorf("a guest without the project clearing = %v, want no such link", err)
		}
		for _, named := range []int{1, 3} {
			if _, err := editor.Exec(ctx, clear, linkID, named); !refusedBy(err, "trace_link_clear_current") {
				t.Errorf("clearing against revision %d = %v, want trace_link_clear_current", named, err)
			}
		}
		if _, err := editor.Exec(ctx, `INSERT INTO trace_link_clearance (armature_link_id, source_rev, comment, cleared_by) VALUES ($1, 2, 'Read it.', $2)`,
			linkID, editorID); !isPermissionDenied(err) {
			t.Errorf("a clearance of the editor's own = %v, want permission denied", err)
		}
		if n := cleared(); n != 0 {
			t.Fatalf("refused clearances left %d rows", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_issue_link WHERE id = $1 AND reviewed_rev = 1`, linkID); n != 1 {
			t.Error("a refused clearance moved the revision the link was read at")
		}
	})

	t.Run("a stored clearance is never rewritten", func(t *testing.T) {
		if _, err := editor.Exec(ctx, clear, linkID, 2); err != nil {
			t.Fatalf("the editor clears the link: %v", err)
		}
		if n := cleared(); n != 1 {
			t.Fatalf("%d clearances, want the one made", n)
		}
		if _, err := editor.Exec(ctx, `UPDATE trace_link_clearance SET comment = 'Changed.' WHERE armature_link_id = $1`, linkID); !isPermissionDenied(err) {
			t.Errorf("the editor rewriting the clearance = %v, want permission denied", err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE trace_link_clearance SET comment = 'Changed.' WHERE armature_link_id = $1`, linkID); !refusedBy(err, "trace_link_clearance_fixed") {
			t.Errorf("even the superuser rewriting the clearance = %v, want trace_link_clearance_fixed", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM trace_link_clearance WHERE armature_link_id = $1 AND comment = 'Still holds.'`, linkID); n != 1 {
			t.Error("the clearance's words changed")
		}
	})

	t.Run("only the worker writes the sync records", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `UPDATE armature_remote_link SET state = 'failed', error = 'No.' WHERE requirement_id = $1`, live["id"]); !isPermissionDenied(err) {
			t.Errorf("an editor marking a record = %v, want permission denied", err)
		}
		if _, err := editor.Exec(ctx, `DELETE FROM armature_remote_link WHERE requirement_id = $1`, live["id"]); !isPermissionDenied(err) {
			t.Errorf("an editor forgetting a record = %v, want permission denied", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_remote_link WHERE requirement_id = $1 AND state = 'synced'`, live["id"]); n != 1 {
			t.Error("the sync record changed")
		}
	})

	t.Run("an armature.links event is the writer's own", func(t *testing.T) {
		insert := `INSERT INTO outbox_event (topic, payload) VALUES ('armature.links', $1::jsonb)`
		inOwnersName := `{"requirementId": "` + live["id"].(string) + `", "actorId": "` + home.user.String() + `"}`
		owners := func() int {
			return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = 'armature.links' AND payload ->> 'actorId' = $1`, home.user.String())
		}
		before := owners()
		if _, err := editor.Exec(ctx, insert, inOwnersName); !isPolicyViolation(err) {
			t.Errorf("an event in the owner's name = %v, want outbox_event_own_acts", err)
		}
		if n := owners(); n != before {
			t.Errorf("%d events in the owner's name were written", n-before)
		}
		// The same event in the editor's own name passes, so the actor alone was refused.
		if _, err := editor.Exec(ctx, insert, `{"requirementId": "`+live["id"].(string)+`", "actorId": "`+editorID.String()+`"}`); err != nil {
			t.Errorf("an event in the editor's own name = %v", err)
		}
	})
}

// A role taken away takes the person's Armature token along, pasted or from a sign-in, and none is stored for
// somebody without a role, not even by the admin role a sign-in's exchange writes as.
func TestAPersonWithoutARoleHoldsNoArmatureToken(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-left")
	slug := stubTenant(t)
	owner := api.as(t, home.user)
	pastedID := h.addPerson(t, "member")
	exchangedID := h.addPerson(t, "member")
	stayingID := h.addPerson(t, "member")
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, api.as(t, pastedID).put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob pastes a token")
	want(t, api.as(t, stayingID).put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "dave")}), http.StatusOK, "dave pastes a token")
	exchanged := `
		INSERT INTO armature_token (user_id, token, status, source, refresh_token, access_expires_at)
		VALUES ($1, '\x00', 'ok', 'exchange', '\x00', now() + interval '1 hour')`
	asAdmin := func(sql string, args ...any) error {
		_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		return err
	}
	if err := asAdmin(exchanged, exchangedID); err != nil {
		t.Fatalf("store a token from a sign-in: %v", err)
	}
	h.settle(t)
	tokens := func(id uuid.UUID) int {
		return h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1`, id)
	}
	for _, id := range []uuid.UUID{pastedID, exchangedID} {
		if n := tokens(id); n != 1 {
			t.Fatalf("%s holds %d tokens before the role is taken away", id, n)
		}
		// No request takes a role away; app_user_left guards whatever does, so the role goes straight through SQL.
		h.cleanupExec(t, h.super, `UPDATE app_user SET role = NULL WHERE id = $1`, id)
	}
	h.settle(t)
	for name, id := range map[string]uuid.UUID{"the pasted token": pastedID, "the token from a sign-in": exchangedID} {
		if n := tokens(id); n != 0 {
			t.Errorf("%s outlived its holder's role", name)
		}
	}
	if n := tokens(stayingID); n != 1 {
		t.Errorf("somebody still a member holds %d tokens, want theirs", n)
	}

	for name, id := range map[string]uuid.UUID{"somebody switched off": exchangedID, "somebody never let in": h.outsider(t, "arm-outsider")} {
		if err := asAdmin(exchanged, id); !refusedBy(err, "armature_token_user_id_member") {
			t.Errorf("a token for %s = %v, want armature_token_user_id_member", name, err)
		}
		if n := tokens(id); n != 0 {
			t.Errorf("%s holds %d tokens", name, n)
		}
	}
}
