//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Signed baselines and locked requirements (#205): a signed baseline never changes, and a locked requirement takes no
// revision, move, link or trash while it still takes comments, reviews and transitions.

// signingRule names dave in person and carol and the owner through a group, which the whole test keeps.
func (s *baselineScene) signingRule(group uuid.UUID, required int) map[string]any {
	return map[string]any{"required": required, "signers": []map[string]any{
		{"subjectType": "user", "userId": s.daveID}, {"subjectType": "group", "groupId": group},
	}}
}

func signingOf(t *testing.T, c *client, path string) map[string]any {
	t.Helper()
	b := obj(t, want(t, c.get(t, path), http.StatusOK, "read the baseline"), "baseline")
	signing, _ := b["signing"].(map[string]any)
	return signing
}

func TestABaselineIsSignedAndKeptForGood(t *testing.T) {
	s := newBaselineScene(t, "BSG")
	rule := "/api/v1/projects/" + s.key + "/baseline-signing"
	id := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline"), "baseline")["id"].(string)
	signatures := s.of(id) + "/signatures"
	group := s.h.makeGroup(t, "Signers", s.carolID, s.ownerID)
	s.h.givePassword(t, s.daveID, "dave-signs-here")
	s.h.givePassword(t, s.carolID, "carol-signs-here")
	s.h.givePassword(t, s.bobID, "bob-signs-here")

	t.Run("nobody signs until the administrators name who does", func(t *testing.T) {
		if got := signingOf(t, s.dave, s.of(id)); got != nil {
			t.Errorf("a baseline of a project nobody signs reads signing %v", got)
		}
		if r := want(t, s.carol.get(t, rule), http.StatusOK, "read the rule"); r.Body["rule"] != nil {
			t.Errorf("a new project has the signing rule %v", r.Body["rule"])
		}
		refuse(t, "signing without a rule", s.dave.post(t, signatures, map[string]any{"meaning": "I release.", "password": "dave-signs-here"}),
			http.StatusConflict, "no_signing_rule")
		refuse(t, "an editor names who signs", s.bob.put(t, rule, s.signingRule(group, 2)), http.StatusForbidden, "forbidden")
		fieldError(t, s.owner.put(t, rule, map[string]any{"required": 0, "signers": []any{}}), "required")
		fieldError(t, s.owner.put(t, rule, map[string]any{"required": 1, "signers": []any{}}), "signers")
		fieldError(t, s.owner.put(t, rule, map[string]any{"required": 2, "signers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}), "required")
		got := obj(t, want(t, s.owner.put(t, rule, s.signingRule(group, 2)), http.StatusOK, "name who signs"), "rule")
		if got["required"] != float64(2) || len(got["signers"].([]any)) != 2 {
			t.Errorf("the rule reads %v", got)
		}
	})

	var meaning string
	t.Run("a signer reads what the signature means, and somebody else is not asked", func(t *testing.T) {
		got := signingOf(t, s.dave, s.of(id))
		meaning, _ = got["meaning"].(string)
		if got["canSign"] != true || got["frozen"] != false || got["required"] != float64(2) ||
			!strings.HasPrefix(meaning, "I release the baseline Release 1 of "+s.key) {
			t.Fatalf("dave reads signing %v", got)
		}
		if bobs := signingOf(t, s.bob, s.of(id)); bobs["canSign"] != false || bobs["meaning"] != "" {
			t.Errorf("bob, who does not sign, reads %v", bobs)
		}
	})

	t.Run("a signature takes the meaning shown and the password again, in a session", func(t *testing.T) {
		fieldError(t, s.dave.post(t, signatures, map[string]any{"meaning": meaning}), "password")
		fieldError(t, s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "not-it"}), "password")
		refuse(t, "a meaning the baseline no longer says", s.dave.post(t, signatures, map[string]any{"meaning": "I agree.", "password": "dave-signs-here"}),
			http.StatusConflict, "baseline_changed")
		_, secret := makeToken(t, s.dave, map[string]any{"name": "script"})
		script := &client{api: s.dave.api, user: s.daveID, http: &http.Client{}, bearer: secret}
		refuse(t, "a token signs", script.post(t, signatures, map[string]any{"meaning": meaning, "password": "dave-signs-here"}),
			http.StatusForbidden, "session_only")
		refuse(t, "bob signs", s.bob.post(t, signatures, map[string]any{"meaning": meaning, "password": "bob-signs-here"}),
			http.StatusForbidden, "not_a_signer")

		b := obj(t, want(t, s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "dave-signs-here"}), http.StatusOK, "dave signs"), "baseline")
		signing := b["signing"].(map[string]any)
		if signing["signatures"] != float64(1) || signing["signed"] != false || signing["frozen"] != true || signing["canSign"] != false || b["locked"] != true {
			t.Errorf("after one signature the baseline reads %v", b)
		}
		refuse(t, "dave signs twice", s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "dave-signs-here"}),
			http.StatusConflict, "already_signed")
	})

	t.Run("the first signature froze who signs", func(t *testing.T) {
		want(t, s.owner.put(t, rule, map[string]any{"required": 1, "signers": []map[string]any{{"subjectType": "user", "userId": s.bobID}}}),
			http.StatusOK, "bob signs from now on")
		if got := signingOf(t, s.bob, s.of(id)); got["required"] != float64(2) || got["canSign"] != false {
			t.Errorf("a change of the rule moved a baseline being signed: %v", got)
		}
		carolMeaning, _ := signingOf(t, s.carol, s.of(id))["meaning"].(string)
		b := obj(t, want(t, s.carol.post(t, signatures, map[string]any{"meaning": carolMeaning, "password": "carol-signs-here"}), http.StatusOK,
			"carol signs through her group"), "baseline")
		if signing := b["signing"].(map[string]any); signing["signed"] != true || signing["signatures"] != float64(2) {
			t.Errorf("after the second signature the baseline reads %v", signing)
		}
	})

	t.Run("a signed baseline keeps its name and is never deleted", func(t *testing.T) {
		refuse(t, "renaming it", s.owner.patch(t, s.of(id), map[string]any{"name": "Release one"}), http.StatusConflict, "baseline_signed")
		refuse(t, "deleting it", s.owner.delete(t, s.of(id)), http.StatusConflict, "baseline_signed")
	})

	t.Run("each signature keeps what it signed, the hash the database computes", func(t *testing.T) {
		var hash string
		if err := s.h.super.QueryRow(context.Background(), `SELECT baseline_content_hash($1)`, id).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		all := list(t, want(t, s.bob.get(t, signatures), http.StatusOK, "list the signatures"), "signatures")
		if len(all) != 2 {
			t.Fatalf("%d signatures, want 2", len(all))
		}
		for _, each := range all {
			g := each.(map[string]any)
			if g["contentHash"] != hash || g["method"] != "password" || !strings.HasPrefix(g["meaning"].(string), "I release the baseline") {
				t.Errorf("a signature reads %v, the hash is %s", g, hash)
			}
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'baseline.signed' AND target_id = $1`, id); n != 2 {
			t.Errorf("%d audit entries for signing, want 2", n)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'baseline_signing.saved' AND target_id = $1`, s.projectID); n != 2 {
			t.Errorf("%d audit entries for naming who signs, want 2", n)
		}
	})

	t.Run("removing the rule stops new baselines being signed, and keeps what was signed", func(t *testing.T) {
		refuse(t, "an editor removes the rule", s.bob.delete(t, rule), http.StatusForbidden, "forbidden")
		want(t, s.owner.delete(t, rule), http.StatusNoContent, "stop signing")
		if got := signingOf(t, s.bob, s.of(id)); got["signed"] != true || got["signatures"] != float64(2) {
			t.Errorf("the signed baseline reads %v without the rule", got)
		}
		later := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Release 2"}), http.StatusCreated, "take another"), "baseline")
		if later["signing"] != nil {
			t.Errorf("a baseline taken without a rule reads signing %v", later["signing"])
		}
	})

	t.Run("its files say it is signed", func(t *testing.T) {
		md := want(t, s.bob.get(t, s.of(id)+"/document.md"), http.StatusOK, "read the baseline as Markdown")
		if !strings.Contains(string(md.Raw), "Signed, with 2 of 2 signatures.") {
			t.Errorf("the Markdown does not say it is signed:\n%.600s", md.Raw)
		}
		matrix := want(t, s.bob.get(t, s.of(id)+"/matrix.csv"), http.StatusOK, "read the baseline's matrix")
		if !strings.Contains(string(matrix.Raw), "This baseline is signed, with 2 of 2 signatures") {
			t.Errorf("the matrix does not say it is signed:\n%s", matrix.Raw)
		}
	})
}

func TestRawSQLGuardsBaselineSignatures(t *testing.T) {
	s := newBaselineScene(t, "BSQ")
	ctx := context.Background()
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/baseline-signing", s.signingRule(s.h.makeGroup(t, "Signers", s.carolID), 1)),
		http.StatusOK, "name who signs")
	id := uuid.MustParse(obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline"), "baseline")["id"].(string))
	dave, bob := s.h.appConn(t, s.daveID), s.h.appConn(t, s.bobID)
	session := func(user uuid.UUID) uuid.UUID {
		var id uuid.UUID
		if err := s.h.super.QueryRow(ctx, `
			INSERT INTO user_session (user_id, token_hash, proof, expires_at) VALUES ($1, decode(md5(random()::text) || md5(random()::text), 'hex'), 'password', now() + interval '1 hour')
			RETURNING id`, user).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	daves, bobs := session(s.daveID), session(s.bobID)
	proof := func(conn *pgx.Conn, user, session uuid.UUID) uuid.UUID {
		var id uuid.UUID
		if err := conn.QueryRow(ctx, `INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'password', now(), now() + interval '1 minute') RETURNING id`, user, session).Scan(&id); err != nil {
			t.Fatalf("record the password given: %v", err)
		}
		return id
	}
	var hash string
	if err := s.h.super.QueryRow(ctx, `SELECT baseline_content_hash($1)`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	sign := `INSERT INTO baseline_signature (baseline_id, content_hash, signer_id, meaning, method, authenticated_at, session_id, reauthentication_id)
		SELECT $1, $2, $3, 'I release.', 'password', r.authenticated_at, r.session_id, r.id FROM reauthentication r WHERE r.id = $4`

	if _, err := dave.Exec(ctx, sign, id, "not-the-hash", s.daveID, proof(dave, s.daveID, daves)); !refusedBy(err, "baseline_signature_wrong_content") {
		t.Errorf("a signature of other content: %v", err)
	}
	if _, err := dave.Exec(ctx, `INSERT INTO baseline_signature (baseline_id, content_hash, signer_id, meaning, method, authenticated_at, session_id, reauthentication_id)
		VALUES ($1, $2, $3, 'I release.', 'password', now(), $4, $5)`, id, hash, s.daveID, daves, uuid.New()); !refusedBy(err, "baseline_signature_unproven") {
		t.Errorf("a signature without a fresh proof: %v", err)
	}
	if _, err := bob.Exec(ctx, sign, id, hash, s.bobID, proof(bob, s.bobID, bobs)); !refusedBy(err, "baseline_signature_not_theirs") {
		t.Errorf("a signature by somebody the rule does not name: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO baseline_signature (baseline_id, content_hash, signer_id, meaning, method, authenticated_at, session_id, reauthentication_id)
		VALUES ($1, $2, $3, 'I release.', 'password', now(), $4, $5)`, id, hash, s.daveID, daves, proof(dave, s.daveID, daves)); err == nil {
		t.Error("bob signed in dave's name")
	}
	reauth := proof(dave, s.daveID, daves)
	if _, err := dave.Exec(ctx, sign, id, hash, s.daveID, reauth); err != nil {
		t.Fatalf("a signature as the api writes it: %v", err)
	}
	if _, err := dave.Exec(ctx, sign, id, hash, s.daveID, reauth); err == nil {
		t.Error("a re-authentication was spent twice")
	}
	if _, err := dave.Exec(ctx, sign, id, hash, s.daveID, proof(dave, s.daveID, daves)); !refusedBy(err, "baseline_signature_once") {
		t.Errorf("a second signature of the same signer: %v", err)
	}
	if _, err := dave.Exec(ctx, `UPDATE baseline_signature SET meaning = 'I did not.' WHERE baseline_id = $1`, id); err == nil {
		t.Error("a signature was changed")
	}
	if _, err := dave.Exec(ctx, `DELETE FROM baseline_signature WHERE baseline_id = $1`, id); err == nil {
		t.Error("a signature was deleted")
	}
	if _, err := dave.Exec(ctx, `INSERT INTO baseline_signing (baseline_id, required) VALUES ($1, 1)`, uuid.New()); err == nil {
		t.Error("the app froze a signing rule by hand")
	}
	owner := s.h.appConn(t, s.ownerID)
	if _, err := owner.Exec(ctx, `UPDATE baseline SET name = 'Renamed' WHERE id = $1`, id); !refusedBy(err, "baseline_signed") {
		t.Errorf("a signed baseline was renamed: %v", err)
	}
	if _, err := owner.Exec(ctx, `DELETE FROM baseline WHERE id = $1`, id); !refusedBy(err, "baseline_signed") {
		t.Errorf("a signed baseline was deleted: %v", err)
	}
	if n := seesRows(t, bob, `SELECT count(*) FROM baseline_signature WHERE baseline_id = $1`, id); n != 1 {
		t.Errorf("a reader of the project sees %d signatures, want 1", n)
	}
}

// lockScene is a baseline scene whose Draft status locks, which every new requirement starts in.
type lockScene struct {
	*baselineScene
	draft string
}

func newLockScene(t *testing.T, prefix string) *lockScene {
	t.Helper()
	s := &lockScene{baselineScene: newBaselineScene(t, prefix)}
	for _, each := range list(t, want(t, s.owner.get(t, "/api/v1/projects/"+s.key+"/statuses"), http.StatusOK, "list the statuses"), "statuses") {
		if st := each.(map[string]any); st["name"] == "Draft" {
			s.draft = st["id"].(string)
		}
	}
	return s
}

func (s *lockScene) lockDraft(t *testing.T, locks bool) {
	t.Helper()
	got := obj(t, want(t, s.owner.patch(t, "/api/v1/projects/"+s.key+"/statuses/"+s.draft, map[string]any{"locks": locks}), http.StatusOK,
		"set whether Draft locks"), "status")
	if got["locks"] != locks {
		t.Fatalf("Draft reads %v", got)
	}
}

func TestALockedRequirementTakesNoChange(t *testing.T) {
	s := newLockScene(t, "LCK")
	r1 := "/api/v1/requirements/" + s.req("1")
	refuse(t, "an editor makes a status lock", s.bob.patch(t, "/api/v1/projects/"+s.key+"/statuses/"+s.draft, map[string]any{"locks": true}),
		http.StatusForbidden, "forbidden")
	s.lockDraft(t, true)

	t.Run("a requirement in a locking status says so, and offers no edit", func(t *testing.T) {
		got := obj(t, want(t, s.bob.get(t, r1), http.StatusOK, "read R1"), "requirement")
		lock, _ := got["lock"].(map[string]any)
		can := got["can"].(map[string]any)
		if lock["by"] != "status" || lock["status"].(map[string]any)["name"] != "Draft" || can["edit"] != false || can["link"] != false ||
			can["delete"] != false || can["comment"] != true || can["transition"] != true {
			t.Errorf("R1 reads lock %v, can %v", lock, can)
		}
		entries := list(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/spec"), http.StatusOK, "read the specification"), "entries")
		if entry := entries[0].(map[string]any); entry["lock"] == nil {
			t.Errorf("the document's entry for R1 reads no lock: %v", entry)
		}
	})

	t.Run("it takes no revision, move, link from it or trash, and says why", func(t *testing.T) {
		r := s.bob.patch(t, r1, map[string]any{"title": "Brakes engage at once", "baseRevision": 1})
		refuse(t, "a new revision", r, http.StatusConflict, "requirement_locked")
		if msg := obj(t, r, "error")["message"].(string); !strings.Contains(msg, s.req("1")+" is locked in the status Draft") {
			t.Errorf("the refusal says %q", msg)
		}
		refuse(t, "a move", s.bob.post(t, r1+"/move", map[string]any{"place": place(s.sectionID, nil)}), http.StatusConflict, "requirement_locked")
		refuse(t, "a link from it", s.bob.post(t, r1+"/links", map[string]any{"typeKey": "relates", "otherKey": s.req("3")}),
			http.StatusConflict, "requirement_locked")
		refuse(t, "the trash", s.bob.delete(t, r1), http.StatusConflict, "requirement_locked")
	})

	t.Run("it still takes comments, a link to it, its owner and priority, and new requirements are written", func(t *testing.T) {
		want(t, s.dave.post(t, r1+"/comments", commentBody("Released as it is.")), http.StatusCreated, "dave comments")
		want(t, s.bob.patch(t, r1, map[string]any{"priority": "must"}), http.StatusOK, "bob sets the priority")
		s.lockDraft(t, false)
		r4 := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake fluid is checked"}),
			http.StatusCreated, "write R4"), "requirement")["key"].(string)
		s.lockDraft(t, true)
		submit, ok := transitionNamed(t, s.bob, r4, "Submit for review")
		if !ok {
			t.Fatal("R4 offers no Submit for review")
		}
		want(t, s.bob.post(t, "/api/v1/requirements/"+r4+"/transitions", map[string]any{"transitionId": submit}), http.StatusOK, "R4 leaves Draft")
		want(t, s.bob.post(t, "/api/v1/requirements/"+r4+"/links", map[string]any{"typeKey": "derives", "otherKey": s.req("1")}),
			http.StatusCreated, "R4 derives from the locked R1")
		want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake pads are checked"}),
			http.StatusCreated, "write R5 in the locking status")
	})

	t.Run("a transition takes it out of the locking status", func(t *testing.T) {
		submit, ok := transitionNamed(t, s.bob, s.req("1"), "Submit for review")
		if !ok {
			t.Fatal("R1 offers no Submit for review")
		}
		want(t, s.bob.post(t, r1+"/transitions", map[string]any{"transitionId": submit}), http.StatusOK, "bob submits R1")
		got := obj(t, want(t, s.bob.get(t, r1), http.StatusOK, "read R1"), "requirement")
		if got["lock"] != nil || got["can"].(map[string]any)["edit"] != true {
			t.Errorf("out of Draft R1 reads lock %v", got["lock"])
		}
	})

	t.Run("an administrator locks it by hand with a reason, and unlocks it", func(t *testing.T) {
		refuse(t, "an editor locks it", s.bob.post(t, r1+"/lock", map[string]any{"reason": "Released"}), http.StatusForbidden, "forbidden")
		fieldError(t, s.owner.post(t, r1+"/lock", map[string]any{"reason": "  "}), "reason")
		got := obj(t, want(t, s.owner.post(t, r1+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock R1"), "requirement")
		if lock := got["lock"].(map[string]any); lock["by"] != "hand" || lock["reason"] != "Released in 2.0" || lock["lockedByName"] == "" {
			t.Errorf("R1 locked by hand reads %v", lock)
		}
		r := s.bob.patch(t, r1, map[string]any{"title": "Brakes engage at once", "baseRevision": 1})
		refuse(t, "a revision of the hand-locked R1", r, http.StatusConflict, "requirement_locked")
		if msg := obj(t, r, "error")["message"].(string); !strings.Contains(msg, "Released in 2.0") {
			t.Errorf("the refusal says %q", msg)
		}
		refuse(t, "a section locked by hand", s.owner.post(t, "/api/v1/requirements/"+s.req("2")+"/lock", map[string]any{"reason": "Done"}),
			http.StatusConflict, "section_not_locked")
		refuse(t, "an editor unlocks it", s.bob.delete(t, r1+"/lock"), http.StatusForbidden, "forbidden")
		if got := obj(t, want(t, s.owner.delete(t, r1+"/lock"), http.StatusOK, "unlock R1"), "requirement"); got["lock"] != nil {
			t.Errorf("unlocked R1 reads lock %v", got["lock"])
		}
		refuse(t, "unlocking it again", s.owner.delete(t, r1+"/lock"), http.StatusConflict, "not_locked_by_hand")
		want(t, s.bob.patch(t, r1, map[string]any{"title": "Brakes engage at once", "baseRevision": 1}), http.StatusOK, "bob revises R1")

		reqID := s.reqID
		for action, n := range map[string]int{"requirement.locked": 1, "requirement.unlocked": 1} {
			if got := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, reqID); got != n {
				t.Errorf("%d audit entries %s, want %d", got, action, n)
			}
		}
		if got := s.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'lock'`, reqID); got != 2 {
			t.Errorf("%d lock changes in R1's activity, want 2", got)
		}
		if got := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'status.locks_set' AND target_id = $1`, s.draft); got < 2 {
			t.Errorf("%d audit entries for Draft locking, want at least 2", got)
		}
	})
}

func TestRawSQLKeepsLockedRequirements(t *testing.T) {
	s := newLockScene(t, "LKQ")
	ctx := context.Background()
	s.lockDraft(t, true)
	bob := s.h.appConn(t, s.bobID)
	var linkType uuid.UUID
	if err := s.h.super.QueryRow(ctx, `SELECT id FROM link_type WHERE NOT acyclic ORDER BY key LIMIT 1`).Scan(&linkType); err != nil {
		t.Fatal(err)
	}
	for what, sql := range map[string]string{
		"a revision": `INSERT INTO requirement_revision (requirement_id, number, title, statement, attributes, hash, created_by)
			SELECT id, revision + 1, 'Changed', statement, attributes, 'h', $2 FROM requirement WHERE id = $1`,
		"a new title":    `UPDATE requirement SET title = 'Changed' WHERE id = $1 AND $2::uuid IS NOT NULL`,
		"a move":         `UPDATE requirement SET rank = rank || 'z' WHERE id = $1 AND $2::uuid IS NOT NULL`,
		"the trash":      `UPDATE requirement SET trashed_at = now(), trashed_by = $2 WHERE id = $1`,
		"a link from it": `INSERT INTO trace_link (type_id, source_id, target_id, created_by) SELECT '` + linkType.String() + `', $1, '` + s.r3ID + `', $2`,
	} {
		if _, err := bob.Exec(ctx, sql, s.reqID, s.bobID); !refusedBy(err, "requirement_locked") {
			t.Errorf("%s of the locked R1: %v", what, err)
		}
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement SET locked_at = now(), locked_by = $2, lock_reason = 'Mine' WHERE id = $1`, s.r3ID, s.bobID); !isPermissionDenied(err) {
		t.Errorf("an editor wrote a hand lock: %v", err)
	}
	if _, err := bob.Exec(ctx, `SELECT requirement_lock($1, 'Mine')`, s.r3ID); !refusedBy(err, "requirement_lock_forbidden") {
		t.Errorf("an editor locked by hand through the function: %v", err)
	}
	if _, err := bob.Exec(ctx, `SELECT requirement_unlock($1)`, s.r3ID); !refusedBy(err, "requirement_lock_forbidden") {
		t.Errorf("an editor unlocked through the function: %v", err)
	}
	touchesNothing(t, bob, "an editor makes a status stop locking", `UPDATE req_status SET locks = false WHERE id = $1`, s.draft)
	owner := s.h.appConn(t, s.ownerID)
	if _, err := owner.Exec(ctx, `UPDATE req_status SET locks = false WHERE id = $1`, s.draft); err != nil {
		t.Fatalf("the owner makes Draft stop locking: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement SET title = 'Changed' WHERE id = $1`, s.reqID); err != nil {
		t.Errorf("an unlocked requirement refused a change: %v", err)
	}
}

// A status that starts locking while a requirement sits in the trash cannot be left from there, so the restore
// brings it back, locked as its status says (#350).
func TestARequirementLockedByItsStatusComesBackFromTheTrash(t *testing.T) {
	s := newLockScene(t, "LKT")
	trash := "/api/v1/projects/" + s.key + "/trash/"
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("1")), http.StatusNoContent, "trash R1")
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("2")), http.StatusNoContent, "trash the section with R3")
	s.lockDraft(t, true)

	want(t, s.bob.post(t, trash+s.reqID+"/restore", nil), http.StatusOK, "restore R1 in the locking Draft")
	want(t, s.bob.post(t, trash+s.sectionID+"/restore", nil), http.StatusOK, "restore the section and R3 in the locking Draft")
	for _, n := range []string{"1", "3"} {
		got := obj(t, want(t, s.bob.get(t, "/api/v1/requirements/"+s.req(n)), http.StatusOK, "read R"+n), "requirement")
		if lock, _ := got["lock"].(map[string]any); got["trashedAt"] != nil || lock["by"] != "status" {
			t.Errorf("restored R%s reads trashedAt %v, lock %v", n, got["trashedAt"], got["lock"])
		}
	}
	refuse(t, "a new revision of the restored R1", s.bob.patch(t, "/api/v1/requirements/"+s.req("1"),
		map[string]any{"title": "Brakes engage at once", "baseRevision": 1}), http.StatusConflict, "requirement_locked")

	ctx := context.Background()
	bob := s.h.appConn(t, s.bobID)
	s.lockDraft(t, false)
	want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("1")), http.StatusNoContent, "trash R1 again")
	s.lockDraft(t, true)
	if _, err := bob.Exec(ctx, `UPDATE requirement SET title = 'Changed' WHERE id = $1`, s.reqID); !refusedBy(err, "requirement_locked") {
		t.Errorf("a new title for the status-locked R1 in the trash: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL WHERE id = $1`, s.reqID); err != nil {
		t.Errorf("an editor restores the status-locked R1 through SQL: %v", err)
	}
}
