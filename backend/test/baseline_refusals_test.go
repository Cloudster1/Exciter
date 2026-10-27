//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/baseline"
)

// Baselines and their signatures refused (#316): every refusal answers its status and code and leaves the baseline,
// its signatures and the signing rule as they were, through the API and straight through SQL.

// signingPath is where the project's signing rule is read and set.
func (s *baselineScene) signingPath() string {
	return "/api/v1/projects/" + s.key + "/baseline-signing"
}

// takeNamed takes a baseline of the whole project, answering its id.
func (s *baselineScene) takeNamed(t *testing.T, name string) string {
	t.Helper()
	return obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": name}), http.StatusCreated, "take "+name), "baseline")["id"].(string)
}

// baselineRow reads a baseline as its name, whether it is locked and how many signatures it holds, straight from the
// database so a refusal is checked against what is stored.
func (s *baselineScene) baselineRow(t *testing.T, id string) string {
	t.Helper()
	var (
		name    string
		locked  bool
		signers int
	)
	err := s.h.super.QueryRow(t.Context(), `SELECT b.name, b.locked_at IS NOT NULL, (SELECT count(*)::int FROM baseline_signature s WHERE s.baseline_id = b.id)
		FROM baseline b WHERE b.id = $1`, id).Scan(&name, &locked, &signers)
	if errors.Is(err, pgx.ErrNoRows) {
		return "gone"
	}
	if err != nil {
		t.Fatal(err)
	}
	state := "open"
	if locked {
		state = "locked"
	}
	return name + " " + state + " " + strings.Repeat("s", signers)
}

// wall takes the project from everyone, so only those granted it read it; carol keeps reading it by a grant of her own.
func (s *baselineScene) wall(t *testing.T) {
	t.Helper()
	if _, err := s.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, s.projectID); err != nil {
		t.Fatal(err)
	}
	s.h.grantAs(t, s.projectID, s.carolID, "viewer")
}

// The operations without a refusal of their own in the access log: a rename that is no name, a delete of no
// baseline, and a signing rule removed where there is none, each answered and nothing changed (#316).
func TestABaselineIsRefusedANameItCannotHave(t *testing.T) {
	s := newBaselineScene(t, "BRA")
	id := s.takeNamed(t, "Release 1")
	before := s.baselineRow(t, id)

	t.Run("a rename to no name, too long a name or too long a description is refused on its field", func(t *testing.T) {
		fieldError(t, s.owner.patch(t, s.of(id), map[string]any{"name": " "}), "name")
		fieldError(t, s.owner.patch(t, s.of(id), map[string]any{"name": strings.Repeat("x", baseline.MaxNameLength+1)}), "name")
		fieldError(t, s.owner.patch(t, s.of(id), map[string]any{"description": strings.Repeat("x", baseline.MaxDescriptionLength+1)}), "description")
		if got := s.baselineRow(t, id); got != before {
			t.Errorf("the refused renames left %q, want %q", got, before)
		}
		if got := obj(t, want(t, s.carol.get(t, s.of(id)), http.StatusOK, "read it"), "baseline"); got["description"] != "" {
			t.Errorf("a refused description was kept: %v", got["description"])
		}
	})

	t.Run("a delete takes no input, so only an id that is none or nobody's is refused", func(t *testing.T) {
		refuse(t, "an id that is none", s.owner.delete(t, "/api/v1/baselines/release-1"), http.StatusBadRequest, "bad_request")
		refuse(t, "a baseline nobody has", s.owner.delete(t, s.of(uuid.NewString())), http.StatusNotFound, "not_found")
		if got := s.baselineRow(t, id); got != before {
			t.Errorf("the refused deletes left %q, want %q", got, before)
		}
	})

	t.Run("removing a signing rule where there is none answers 204 and records nothing", func(t *testing.T) {
		want(t, s.owner.delete(t, s.signingPath()), http.StatusNoContent, "remove no rule")
		if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'baseline_signing.removed' AND target_id = $1`, s.projectID); n != 0 {
			t.Errorf("%d audit entries for removing no rule", n)
		}
		refuse(t, "a project nobody has", s.owner.delete(t, "/api/v1/projects/"+projectKey(t, "BRZ")+"/baseline-signing"), http.StatusNotFound, "not_found")
	})
}

// A baseline is taken only of a section of its own project out of the trash, and compared only with another of its
// project (#316).
func TestABaselineIsTakenOfAndComparedWithItsOwnProjectAlone(t *testing.T) {
	s := newBaselineScene(t, "BRS")
	other := projectKey(t, "BRT")
	want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Elsewhere"}), http.StatusCreated, "make another project")
	elsewhere := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+other+"/requirements", map[string]any{"kind": "section", "title": "Steering"}),
		http.StatusCreated, "write a section there"), "requirement")["key"].(string)
	retired := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"kind": "section", "title": "Retired"}),
		http.StatusCreated, "write a section here"), "requirement")["key"].(string)
	want(t, s.owner.delete(t, "/api/v1/requirements/"+retired), http.StatusNoContent, "trash it")

	t.Run("a root in another project, in the trash or nowhere is refused on rootKey, and nothing is taken", func(t *testing.T) {
		for what, key := range map[string]string{"a section of another project": elsewhere, "a section in the trash": retired, "a key nobody has": s.key + "-R999"} {
			if msg := fieldError(t, s.owner.post(t, s.baselines, map[string]any{"name": what, "rootKey": key}), "rootKey"); !strings.Contains(msg, s.key) {
				t.Errorf("%s: the refusal says %q", what, msg)
			}
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM baseline WHERE project_id = $1`, s.projectID); n != 0 {
			t.Errorf("the refusals took %d baselines", n)
		}
	})

	t.Run("a comparison or a matrix with another project's baseline is refused on with", func(t *testing.T) {
		theirs := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+other+"/baselines", map[string]any{"name": "Theirs"}), http.StatusCreated,
			"take one there"), "baseline")["id"].(string)
		mine := s.takeNamed(t, "Mine")
		// The owner reads both, so the refusal is of the other project and not of a baseline out of sight.
		want(t, s.owner.get(t, s.of(theirs)), http.StatusOK, "the owner reads theirs")
		fieldError(t, s.owner.get(t, s.of(mine)+"/compare?with="+theirs), "with")
		fieldError(t, s.owner.get(t, s.of(mine)+"/matrix?with="+theirs), "with")
		fieldError(t, s.owner.get(t, s.of(mine)+"/matrix.csv?with="+theirs), "with")
	})
}

// An archived project keeps its baselines and its signatures as they are: taking, renaming, deleting and signing a
// baseline are each refused, and so is a change of who signs (#316).
func TestAnArchivedProjectKeepsItsBaselinesAsTheyAre(t *testing.T) {
	s := newBaselineScene(t, "BRV")
	want(t, s.owner.put(t, s.signingPath(), map[string]any{"required": 1, "signers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}),
		http.StatusOK, "dave signs")
	id := s.takeNamed(t, "Release 1")
	s.h.givePassword(t, s.daveID, "dave-signs-here")
	meaning := signingOf(t, s.dave, s.of(id))["meaning"].(string)
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	before := s.baselineRow(t, id)

	refuse(t, "taking a baseline", s.owner.post(t, s.baselines, map[string]any{"name": "Release 2"}), http.StatusConflict, "project_archived")
	refuse(t, "renaming one", s.owner.patch(t, s.of(id), map[string]any{"name": "Release one"}), http.StatusConflict, "project_archived")
	refuse(t, "locking one", s.owner.patch(t, s.of(id), map[string]any{"locked": true}), http.StatusConflict, "project_archived")
	refuse(t, "deleting one", s.owner.delete(t, s.of(id)), http.StatusConflict, "project_archived")
	refuse(t, "signing one", s.dave.post(t, s.of(id)+"/signatures", map[string]any{"meaning": meaning, "password": "dave-signs-here"}),
		http.StatusConflict, "project_archived")
	refuse(t, "changing who signs", s.owner.put(t, s.signingPath(), map[string]any{"required": 1, "signers": []map[string]any{{"subjectType": "user", "userId": s.carolID}}}),
		http.StatusConflict, "project_archived")
	refuse(t, "stopping signing", s.owner.delete(t, s.signingPath()), http.StatusConflict, "project_archived")

	if got := s.baselineRow(t, id); got != before {
		t.Errorf("the archived project's baseline reads %q, want %q", got, before)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM baseline WHERE project_id = $1`, s.projectID); n != 1 {
		t.Errorf("the archived project holds %d baselines, want the one taken before", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM baseline_signing_rule_signer WHERE project_id = $1 AND user_id = $2`, s.projectID, s.daveID); n != 1 {
		t.Error("the archived project's signing rule changed")
	}
	if got := signingOf(t, s.dave, s.of(id)); got["canSign"] != false {
		t.Errorf("a baseline of an archived project offers dave to sign: %v", got)
	}
}

// Who signs is a person or a group, each once and at most baseline.MaxSigners of them, named by the project's
// administrators alone (#316).
func TestASigningRuleNamesOnlyThoseWhoMaySign(t *testing.T) {
	s := newBaselineScene(t, "BRU")
	person := func(id any) map[string]any { return map[string]any{"subjectType": "user", "userId": id} }
	rule := func(signers ...map[string]any) map[string]any {
		return map[string]any{"required": 1, "signers": signers}
	}
	tooMany := make([]map[string]any, baseline.MaxSigners+1)
	for i := range tooMany {
		tooMany[i] = person(uuid.NewString())
	}

	refuse(t, "a reader names who signs", s.carol.put(t, s.signingPath(), rule(person(s.carolID))), http.StatusForbidden, "forbidden")
	for what, body := range map[string]map[string]any{
		"everyone":                  rule(map[string]any{"subjectType": "everyone"}),
		"the same person twice":     rule(person(s.daveID), person(s.daveID)),
		"an id that is nobody":      rule(person(uuid.NewString())),
		"an id that is no group":    rule(map[string]any{"subjectType": "group", "groupId": uuid.NewString()}),
		"a person without their id": rule(map[string]any{"subjectType": "user"}),
		"more signers than allowed": {"required": 1, "signers": tooMany},
	} {
		t.Run(what, func(t *testing.T) {
			fieldError(t, s.owner.put(t, s.signingPath(), body), "signers")
		})
	}
	if r := want(t, s.carol.get(t, s.signingPath()), http.StatusOK, "read the rule"); r.Body["rule"] != nil {
		t.Errorf("the refusals left the rule %v", r.Body["rule"])
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'baseline_signing.saved' AND target_id = $1`, s.projectID); n != 0 {
		t.Errorf("%d audit entries for refused rules", n)
	}
}

// Somebody who may not read the project sees none of its baselines, their signatures or who signs them; the rule
// may name them, and they sign once they may read it (#316).
func TestABaselineIsOutOfSightOfWhoeverCannotReadItsProject(t *testing.T) {
	s := newBaselineScene(t, "BRO")
	s.wall(t)
	erinID := s.h.addPerson(t, "member")
	erin := s.owner.api.as(t, erinID)
	s.h.givePassword(t, erinID, "erin-signs-here")

	// As an approval rule, the rule names a member who may be given the project later.
	want(t, s.owner.put(t, s.signingPath(), map[string]any{"required": 1, "signers": []map[string]any{{"subjectType": "user", "userId": erinID}}}),
		http.StatusOK, "name erin, who cannot read the project yet")
	id := s.takeNamed(t, "Release 1")

	t.Run("an outsider is answered not found for the baselines, the signatures and the rule", func(t *testing.T) {
		for what, path := range map[string]string{"the baselines": s.baselines, "the baseline": s.of(id), "its signatures": s.of(id) + "/signatures",
			"the signing rule": s.signingPath()} {
			refuse(t, what, erin.get(t, path), http.StatusNotFound, "not_found")
		}
		refuse(t, "signing it", erin.post(t, s.of(id)+"/signatures", map[string]any{
			"meaning":  "I release the baseline Release 1 of " + s.key + " as the agreed state of its requirements.",
			"password": "erin-signs-here"}), http.StatusNotFound, "not_found")
		if got := s.baselineRow(t, id); got != "Release 1 open " {
			t.Errorf("the baseline reads %q after an outsider signed", got)
		}
	})

	t.Run("given the project, the signer named reads it and may sign", func(t *testing.T) {
		s.h.grantAs(t, s.projectID, erinID, "viewer")
		if got := signingOf(t, erin, s.of(id)); got["canSign"] != true {
			t.Errorf("erin, given the project, reads signing %v", got)
		}
	})
}

// A baseline signed once of twice keeps its name and lock, and a signer who leaves the group the first signature
// froze may no longer sign it, while the others complete it (#316).
func TestABaselineBeingSignedKeepsItsNameAndItsSigners(t *testing.T) {
	s := newBaselineScene(t, "BRK")
	group := s.h.makeGroup(t, "Signers", s.carolID, s.ownerID)
	want(t, s.owner.put(t, s.signingPath(), s.signingRule(group, 2)), http.StatusOK, "dave and the group sign, two of them")
	id := s.takeNamed(t, "Release 1")
	signatures := s.of(id) + "/signatures"
	for who, user := range map[string]uuid.UUID{"dave": s.daveID, "carol": s.carolID, "owner": s.ownerID} {
		s.h.givePassword(t, user, who+"-signs-here")
	}
	meaning := signingOf(t, s.dave, s.of(id))["meaning"].(string)
	want(t, s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "dave-signs-here"}), http.StatusOK, "dave signs first")

	t.Run("after one of two signatures it keeps its name, stays and stays locked", func(t *testing.T) {
		refuse(t, "renaming it", s.owner.patch(t, s.of(id), map[string]any{"name": "Release one"}), http.StatusConflict, "baseline_signed")
		refuse(t, "describing it anew", s.owner.patch(t, s.of(id), map[string]any{"description": "Changed."}), http.StatusConflict, "baseline_signed")
		refuse(t, "unlocking it", s.owner.patch(t, s.of(id), map[string]any{"locked": false}), http.StatusConflict, "baseline_signed")
		refuse(t, "deleting it", s.owner.delete(t, s.of(id)), http.StatusConflict, "baseline_signed")
		if got := s.baselineRow(t, id); got != "Release 1 locked s" {
			t.Errorf("the baseline being signed reads %q", got)
		}
	})

	t.Run("a signer who leaves the frozen group may not sign, and the others complete it", func(t *testing.T) {
		if _, err := s.h.super.Exec(t.Context(), `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, group, s.carolID); err != nil {
			t.Fatal(err)
		}
		s.h.settle(t)
		if got := signingOf(t, s.carol, s.of(id)); got["canSign"] != false || got["meaning"] != "" {
			t.Errorf("carol, out of the group, reads signing %v", got)
		}
		refuse(t, "carol signs", s.carol.post(t, signatures, map[string]any{"meaning": meaning, "password": "carol-signs-here"}),
			http.StatusForbidden, "not_a_signer")
		ownerMeaning := signingOf(t, s.owner, s.of(id))["meaning"].(string)
		b := obj(t, want(t, s.owner.post(t, signatures, map[string]any{"meaning": ownerMeaning, "password": "owner-signs-here"}), http.StatusOK,
			"the owner signs through the group"), "baseline")
		if signing := b["signing"].(map[string]any); signing["signed"] != true || signing["signatures"] != float64(2) {
			t.Errorf("after the owner's signature the baseline reads %v", signing)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM baseline_signature WHERE baseline_id = $1 AND signer_id = $2`, id, s.carolID); n != 0 {
			t.Errorf("carol, out of the group, holds %d signatures", n)
		}
	})
}

// Wrong passwords given to sign a baseline count with those given to sign a review: a run of them refuses both for
// a while, even the right password, and once it has passed the baseline is signed (#316).
func TestWrongPasswordsForABaselineCountWithThoseForAReview(t *testing.T) {
	s := newBaselineScene(t, "BRW")
	want(t, s.owner.put(t, s.signingPath(), map[string]any{"required": 1, "signers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}),
		http.StatusOK, "dave signs")
	id := s.takeNamed(t, "Release 1")
	signatures := s.of(id) + "/signatures"
	s.h.givePassword(t, s.daveID, "dave-signs-here")
	meaning := signingOf(t, s.dave, s.of(id))["meaning"].(string)
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/approval-rules", map[string]any{"required": 1,
		"approvers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}), http.StatusOK, "dave approves")
	want(t, s.owner.patch(t, "/api/v1/projects/"+s.key, map[string]any{"settings": map[string]any{"signedApprovals": true}}), http.StatusOK,
		"sign the project's approvals")
	review := obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+s.reqKey+"/reviews", map[string]any{"revision": 1}), http.StatusCreated,
		"ask for a review"), "review")["id"].(string)
	decisions := "/api/v1/reviews/" + review + "/decisions"

	for range auth.MaxSignatureFailures - 1 {
		fieldError(t, s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "wrong"}), "password")
	}
	fieldError(t, s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "password": "wrong"}), "password")
	refuse(t, "the right password for the baseline", s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "dave-signs-here"}),
		http.StatusTooManyRequests, "too_many_attempts")
	refuse(t, "the right password for the review", s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "password": "dave-signs-here"}),
		http.StatusTooManyRequests, "too_many_attempts")
	if got := s.baselineRow(t, id); got != "Release 1 open " {
		t.Errorf("the refused signatures left the baseline %q", got)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM signature_failure WHERE user_id = $1`, s.daveID); n != auth.MaxSignatureFailures {
		t.Errorf("%d wrong passwords counted, want %d", n, auth.MaxSignatureFailures)
	}

	if _, err := s.h.super.Exec(t.Context(), `UPDATE signature_failure SET failed_at = failed_at - make_interval(secs => $2) WHERE user_id = $1`,
		s.daveID, (2 * auth.SignatureFailureWindow).Seconds()); err != nil {
		t.Fatal(err)
	}
	b := obj(t, want(t, s.dave.post(t, signatures, map[string]any{"meaning": meaning, "password": "dave-signs-here"}), http.StatusOK,
		"the right password once the run has passed"), "baseline")
	if b["signing"].(map[string]any)["signed"] != true {
		t.Errorf("the baseline reads %v", b["signing"])
	}
}

// A baseline is signed with a fresh sign-in at the identity provider, as a review's decision is; one spent, one of
// another session and one too old are refused and sign nothing (#316).
func TestABaselineIsSignedAfterSigningInAgainAtTheProvider(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, aliceEmail, "member")
	key := projectKey(t, "SSB")
	owner, _ := adminOf(t, h, a)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make the project")
	// A baseline keeps who took it, so the project goes before the bootstrap owner's own cleanup removes them.
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM project WHERE key = $1`, key) })
	want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes"}), http.StatusCreated, "write one")
	alice := h.userID(t, aliceEmail)
	want(t, owner.put(t, "/api/v1/projects/"+key+"/baseline-signing", map[string]any{"required": 1,
		"signers": []map[string]any{{"subjectType": "user", "userId": alice}}}), http.StatusOK, "alice signs")
	take := func(name string) string {
		return obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/baselines", map[string]any{"name": name}), http.StatusCreated, "take "+name),
			"baseline")["id"].(string)
	}
	first, second := take("Release 1"), take("Release 2")
	signatures := func(id string) int {
		return h.countRows(t, `SELECT count(*) FROM baseline_signature WHERE baseline_id = $1`, id)
	}

	b := signedIn(t, a, "alice", alicePassword)
	sign := func(id string, proof any) (int, string) {
		t.Helper()
		got := b.getJSON(t, "/api/v1/baselines/"+id)["baseline"].(map[string]any)["signing"].(map[string]any)
		resp, body := b.postJSON(t, "/api/v1/baselines/"+id+"/signatures", map[string]any{"meaning": got["meaning"], "reauthentication": proof})
		return resp.StatusCode, body
	}
	page := "/p/" + key + "/baselines/" + first
	landing, err := url.Parse(b.reauthenticate(t, "alice", alicePassword, page))
	if err != nil || landing.Path != page || landing.Query().Get("signature") == "" {
		t.Fatalf("the re-authentication landed on %v", landing)
	}
	proof := landing.Query().Get("signature")

	if status, body := sign(first, proof); status != http.StatusOK {
		t.Fatalf("alice signs after signing in again: %d %s", status, body)
	}
	var listed struct {
		Signatures []struct {
			Method string `json:"method"`
		} `json:"signatures"`
	}
	_ = json.Unmarshal([]byte(mustGet(t, b, "/api/v1/baselines/"+first+"/signatures")), &listed)
	if len(listed.Signatures) != 1 || listed.Signatures[0].Method != "oidc" {
		t.Errorf("the signature reads %+v, want one by the provider", listed)
	}

	var session uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT session_id FROM reauthentication WHERE id = $1`, proof).Scan(&session); err != nil {
		t.Fatal(err)
	}
	stale := insertReauthentication(t, h, alice, session, "now() - interval '20 minutes'", "now() - interval '12 minutes'")
	a.as(t, alice)
	var otherSession uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM user_session WHERE user_id = $1 AND id <> $2 ORDER BY created_at DESC LIMIT 1`,
		alice, session).Scan(&otherSession); err != nil {
		t.Fatal(err)
	}
	foreign := insertReauthentication(t, h, alice, otherSession, "now()", "now() + interval '5 minutes'")

	for what, refused := range map[string]any{"one spent on another baseline": proof, "one too old": stale, "one of another session": foreign} {
		t.Run(what, func(t *testing.T) {
			if status, body := sign(second, refused); status != http.StatusConflict || !strings.Contains(body, "signature_spent") {
				t.Errorf("%d %s", status, body)
			}
		})
	}
	if n := signatures(second); n != 0 {
		t.Errorf("the refused proofs gave %d signatures", n)
	}
}

// insertReauthentication records a sign-in at the provider for a session, made and authenticated at made and good
// until expires, both SQL expressions.
func insertReauthentication(t *testing.T, h *harness, user, session uuid.UUID, made, expires string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, created_at, expires_at)
		VALUES ($1, $2, 'oidc', `+made+`, `+made+`, `+expires+`) RETURNING id`, user, session).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// mustGet reads a page of the API in the browser's session, failing unless it answers 200.
func mustGet(t *testing.T, b *browser, path string) string {
	t.Helper()
	resp, body := b.get(t, b.api.URL+path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, resp.StatusCode, body)
	}
	return body
}

// An assistant takes a baseline only as far as its person may, and compares one baseline with another (#316).
func TestAnAssistantTakesAndComparesBaselinesAsItsPersonMay(t *testing.T) {
	s := newBaselineScene(t, "BRM")
	_, editors := makeToken(t, s.bob, map[string]any{"name": "assistant"})
	_, owners := makeToken(t, s.owner, map[string]any{"name": "assistant"})
	s.h.settle(t)

	sentence := connectAssistant(t, s.bob.api, editors).refused(t, "take_baseline", map[string]any{"projectKey": s.key, "name": "Release 1"})
	if !strings.Contains(sentence, "administrators") {
		t.Errorf("take_baseline for an editor says %q, not naming who may", sentence)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM baseline WHERE project_id = $1`, s.projectID); n != 0 {
		t.Errorf("an editor's assistant took %d baselines", n)
	}

	a := connectAssistant(t, s.owner.api, owners)
	first := a.call(t, "take_baseline", map[string]any{"projectKey": s.key, "name": "Release 1"})["baseline"].(map[string]any)["id"].(string)
	want(t, s.owner.patch(t, "/api/v1/requirements/"+s.req("3"), map[string]any{"title": "Brake lights follow the pedal at once", "baseRevision": 1}),
		http.StatusOK, "revise R3")
	want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake fluid is checked"}), http.StatusCreated, "write R4")
	second := a.call(t, "take_baseline", map[string]any{"projectKey": s.key, "name": "Release 2"})["baseline"].(map[string]any)["id"].(string)

	comparison := a.call(t, "compare_baseline", map[string]any{"baselineID": first, "with": second})["comparison"].(map[string]any)
	if to, _ := comparison["to"].(map[string]any)["baseline"].(map[string]any); to == nil || to["id"] != second {
		t.Errorf("compare_baseline compared with %v, want Release 2", comparison["to"])
	}
	changes := map[string]string{}
	for _, each := range comparison["items"].([]any) {
		item := each.(map[string]any)
		changes[item["key"].(string)] = item["change"].(string)
	}
	if len(changes) != 2 || changes[s.req("3")] != "changed" || changes[s.req("4")] != "added" {
		t.Errorf("between the two baselines compare_baseline finds %v", changes)
	}
}

// Straight through SQL: a baseline keeps its project and root, a signature needs a rule, only the administrators
// write who signs, the app writes nothing a baseline froze, and an Armature link a baseline holds never changes (#316).
func TestRawSQLKeepsWhatABaselineIsOfAndWhoSignsIt(t *testing.T) {
	s := newBaselineScene(t, "BRQ")
	h := s.h
	ctx := context.Background()
	other := projectKey(t, "BRP")
	otherID := obj(t, want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Elsewhere"}), http.StatusCreated,
		"make another project"), "project")["id"].(string)
	id := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Braking 1.0", "rootKey": s.req("2")}), http.StatusCreated,
		"take the section"), "baseline")["id"].(string)
	h.settle(t)
	owner, bob, dave := h.appConn(t, s.ownerID), h.appConn(t, s.bobID), h.appConn(t, s.daveID)
	keeps := func(t *testing.T) {
		t.Helper()
		if n := h.countRows(t, `SELECT count(*) FROM baseline WHERE id = $1 AND project_id = $2 AND root_id = $3`, id, s.projectID, s.sectionID); n != 1 {
			t.Error("the baseline moved to another project or root")
		}
	}

	t.Run("a baseline keeps what it was taken of: the app may not try, and the superuser is refused by baseline_fixed", func(t *testing.T) {
		for column, value := range map[string]string{"root_id": s.reqID, "project_id": otherID} {
			if _, err := owner.Exec(ctx, `UPDATE baseline SET `+column+` = $2 WHERE id = $1`, id, value); !isPermissionDenied(err) {
				t.Errorf("the owner set %s: %v", column, err)
			}
			if _, err := h.super.Exec(ctx, `UPDATE baseline SET `+column+` = $2 WHERE id = $1`, id, value); !refusedBy(err, "baseline_fixed") {
				t.Errorf("the superuser set %s: %v", column, err)
			}
		}
		keeps(t)
	})

	t.Run("a signature of a baseline whose project names no signers is refused by baseline_signature_no_rule", func(t *testing.T) {
		var session, proof uuid.UUID
		if err := h.super.QueryRow(ctx, `
			INSERT INTO user_session (user_id, token_hash, proof, expires_at) VALUES ($1, decode(md5(random()::text) || md5(random()::text), 'hex'), 'password', now() + interval '1 hour')
			RETURNING id`, s.daveID).Scan(&session); err != nil {
			t.Fatal(err)
		}
		if err := dave.QueryRow(ctx, `INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'password', now(), now() + interval '1 minute') RETURNING id`, s.daveID, session).Scan(&proof); err != nil {
			t.Fatal(err)
		}
		_, err := dave.Exec(ctx, `INSERT INTO baseline_signature (baseline_id, content_hash, signer_id, meaning, method, authenticated_at, session_id, reauthentication_id)
			SELECT $1, baseline_content_hash($1), $2, 'I release.', 'password', r.authenticated_at, r.session_id, r.id FROM reauthentication r WHERE r.id = $3`,
			id, s.daveID, proof)
		if !refusedBy(err, "baseline_signature_no_rule") {
			t.Errorf("a signature without a rule: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM baseline_signing WHERE baseline_id = $1`, id); n != 0 {
			t.Error("a refused signature froze a rule")
		}
	})

	t.Run("an editor writes nothing of the signing rule", func(t *testing.T) {
		if _, err := bob.Exec(ctx, `INSERT INTO baseline_signing_rule (project_id, required, updated_by) VALUES ($1, 1, $2)`, s.projectID, s.bobID); !isPolicyViolation(err) {
			t.Errorf("an editor set a rule: %v", err)
		}
		want(t, s.owner.put(t, s.signingPath(), map[string]any{"required": 1, "signers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}),
			http.StatusOK, "the owner names dave")
		h.settle(t)
		if _, err := bob.Exec(ctx, `INSERT INTO baseline_signing_rule_signer (project_id, subject_type, user_id) VALUES ($1, 'user', $2)`,
			s.projectID, s.bobID); !isPolicyViolation(err) {
			t.Errorf("an editor named himself: %v", err)
		}
		for what, sql := range map[string]string{
			"asks for more signatures": `UPDATE baseline_signing_rule SET required = 2 WHERE project_id = $1`,
			"removes the rule":         `DELETE FROM baseline_signing_rule WHERE project_id = $1`,
			"swaps a signer":           `UPDATE baseline_signing_rule_signer SET user_id = '` + s.bobID.String() + `' WHERE project_id = $1`,
			"removes a signer":         `DELETE FROM baseline_signing_rule_signer WHERE project_id = $1`,
		} {
			if n := affects(t, bob, sql, s.projectID); n != 0 {
				t.Errorf("an editor %s: %d rows", what, n)
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM baseline_signing_rule r JOIN baseline_signing_rule_signer g USING (project_id)
			WHERE r.project_id = $1 AND r.required = 1 AND g.user_id = $2`, s.projectID, s.daveID); n != 1 {
			t.Error("the rule changed under an editor")
		}
	})

	t.Run("the app writes no signer a baseline froze, not even its administrators", func(t *testing.T) {
		refusedOutright(t, owner, "the owner freezes a signer", `INSERT INTO baseline_signer (baseline_id, subject_type, user_id) VALUES ($1, 'user', $2)`, id, s.ownerID)
		for what, sql := range map[string]string{
			"the owner changes a frozen signer": `UPDATE baseline_signer SET user_id = $2 WHERE baseline_id = $1`,
			"the owner removes a frozen signer": `DELETE FROM baseline_signer WHERE baseline_id = $1 AND user_id <> $2`,
		} {
			if _, err := owner.Exec(ctx, sql, id, s.ownerID); !isPermissionDenied(err) {
				t.Errorf("%s: %v", what, err)
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM baseline_signer WHERE baseline_id = $1`, id); n != 0 {
			t.Errorf("%d signers frozen on a baseline nobody signed", n)
		}
	})

	t.Run("an Armature link a baseline holds is never changed or removed, even by the superuser", func(t *testing.T) {
		tx, err := h.super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var held uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO baseline (project_id, name, created_by) VALUES ($1, 'With an issue', $2) RETURNING id`,
			s.projectID, s.ownerID).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO baseline_external_link (baseline_id, link_id, requirement_id, issue_key, relation) VALUES ($1, $2, $3, 'WEB-1', 'implements')`,
			held, uuid.New(), s.reqID); err != nil {
			t.Fatalf("hold an Armature link as the baseline is taken: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE baseline_external_link SET issue_key = 'WEB-2' WHERE baseline_id = $1`, held); !refusedBy(err, "baseline_frozen") {
			t.Errorf("a held Armature link was changed: %v", err)
		}
		if _, err := h.super.Exec(ctx, `DELETE FROM baseline_external_link WHERE baseline_id = $1`, held); !refusedBy(err, "baseline_frozen") {
			t.Errorf("a held Armature link was removed without its baseline: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM baseline_external_link WHERE baseline_id = $1 AND issue_key = 'WEB-1'`, held); n != 1 {
			t.Error("the held Armature link changed")
		}
	})
	keeps(t)
}
