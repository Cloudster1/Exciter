//go:build integration

package test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// Approvals as electronic signatures (#46): in a project that asks for them, a decision is signed with the
// reviewer's password or a fresh sign-in at the identity provider, and the signature keeps what it meant and signed.

// givePassword gives somebody a password, as an administrator setting one would.
func (h *harness) givePassword(t *testing.T, user uuid.UUID, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password, cheapPasswords())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET password_hash = $2 WHERE id = $1`, user, hash); err != nil {
		t.Fatal(err)
	}
}

// signedReview is a review scene whose project signs its approvals, dave approving by name and carol through a group,
// one approval needed, and a review of R1 waiting.
func signedReview(t *testing.T, prefix string) *reviewScene {
	t.Helper()
	s := newReviewScene(t, prefix)
	want(t, s.owner.put(t, s.rules, s.rule(1)), http.StatusOK, "set the rule")
	got := obj(t, want(t, s.owner.patch(t, "/api/v1/projects/"+s.key, map[string]any{"settings": map[string]any{"signedApprovals": true}}),
		http.StatusOK, "sign the project's approvals"), "project")
	if got["settings"].(map[string]any)["signedApprovals"] != true {
		t.Fatalf("the project reads %v", got["settings"])
	}
	s.reviewID = obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask for a review"), "review")["id"].(string)
	return s
}

func TestASignedApprovalIsGivenWithThePassword(t *testing.T) {
	s := signedReview(t, "SIG")
	decisions := "/api/v1/reviews/" + s.reviewID + "/decisions"
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'project.signatures_set' AND target_id = $1`, s.projectID); n != 1 {
		t.Errorf("%d audit entries for signing the project's approvals, want 1", n)
	}
	review := obj(t, want(t, s.dave.get(t, "/api/v1/reviews/"+s.reviewID), http.StatusOK, "dave reads the review"), "review")
	meanings, _ := review["meanings"].(map[string]any)
	if review["signed"] != true || !strings.HasPrefix(meanings["approve"].(string), "I approve revision 1 of "+s.reqKey) ||
		!strings.HasPrefix(meanings["reject"].(string), "I reject revision 1 of "+s.reqKey) {
		t.Fatalf("the signed review reads %v", review)
	}
	if bobs := obj(t, want(t, s.bob.get(t, "/api/v1/reviews/"+s.reviewID), http.StatusOK, "bob reads it"), "review"); len(bobs["meanings"].(map[string]any)) != 0 {
		t.Errorf("somebody who may not decide is shown what to sign: %v", bobs["meanings"])
	}
	me := want(t, s.dave.get(t, "/api/v1/auth/me"), http.StatusOK, "who dave is")
	if me.Body["signsWith"] != "" {
		t.Errorf("dave, without a password or a provider, signs with %v", me.Body["signsWith"])
	}
	s.h.givePassword(t, s.daveID, "dave-signs-here")
	if me := want(t, s.dave.get(t, "/api/v1/auth/me"), http.StatusOK, "who dave is now"); me.Body["signsWith"] != "password" {
		t.Errorf("dave with a password signs with %v", me.Body["signsWith"])
	}

	fieldError(t, s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1}), "password")
	fieldError(t, s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "password": "not-it"}), "password")
	_, secret := makeToken(t, s.dave, map[string]any{"name": "script"})
	script := &client{api: s.dave.api, user: s.daveID, http: &http.Client{}, bearer: secret}
	refuse(t, "a token signs", script.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "password": "dave-signs-here"}),
		http.StatusForbidden, "session_only")
	refuse(t, "a re-authentication that does not exist", s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "reauthentication": uuid.NewString()}),
		http.StatusConflict, "signature_spent")

	got := obj(t, want(t, s.dave.post(t, decisions, map[string]any{"decision": "approve", "revision": 1, "password": "dave-signs-here", "comment": "Signed."}),
		http.StatusOK, "dave signs his approval"), "review")
	if got["state"] != "approved" {
		t.Fatalf("the signed approval leaves the review %v", got["state"])
	}
	signature, _ := got["decisions"].([]any)[0].(map[string]any)["signature"].(map[string]any)
	var hash string
	if err := s.h.super.QueryRow(context.Background(), `SELECT hash FROM requirement_revision WHERE requirement_id = $1 AND number = 1`, s.reqID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if signature == nil || signature["method"] != "password" || signature["revision"] != float64(1) || signature["contentHash"] != hash ||
		!strings.HasPrefix(signature["meaning"].(string), "I approve revision 1 of "+s.reqKey) {
		t.Errorf("the decision's signature reads %v", signature)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM reauthentication WHERE user_id = $1 AND used_at IS NOT NULL`, s.daveID); n != 1 {
		t.Errorf("%d spent re-authentications, want 1", n)
	}

	t.Run("a run of wrong passwords is refused for a while", func(t *testing.T) {
		s.revise(t, 1)
		next := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 2}), http.StatusCreated, "ask again"), "review")["id"].(string)
		s.h.givePassword(t, s.carolID, "carol-signs-here")
		for range auth.MaxSignatureFailures {
			fieldError(t, s.carol.post(t, "/api/v1/reviews/"+next+"/decisions", map[string]any{"decision": "reject", "revision": 2, "password": "wrong"}), "password")
		}
		refuse(t, "the right password after too many wrong ones", s.carol.post(t, "/api/v1/reviews/"+next+"/decisions",
			map[string]any{"decision": "reject", "revision": 2, "password": "carol-signs-here"}), http.StatusTooManyRequests, "too_many_attempts")
	})
}

// reauthenticate signs a browser's person in again at Keycloak, which must show its form whatever session it holds,
// and returns where the app was told to land.
func (b *browser) reauthenticate(t *testing.T, username, password, next string) string {
	t.Helper()
	resp, page := b.get(t, b.api.URL+httpapi.APIPrefix+"/auth/oidc/reauthenticate?next="+url.QueryEscape(next))
	if q := resp.Request.URL.Query(); q.Get("prompt") != "login" || q.Get("max_age") != "0" {
		t.Errorf("the provider was not told to ask again: %s", resp.Request.URL)
	}
	match := loginFormAction.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("Keycloak did not ask again on %s (%s):\n%.800s", resp.Request.URL, resp.Status, page)
	}
	resp, err := b.PostForm(html.UnescapeString(match[1]), url.Values{"username": {username}, "password": {password}, "credentialId": {""}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the login form answered %s, want a redirect into the app", resp.Status)
	}
	b.api.h.settle(t)
	return resp.Header.Get("Location")
}

func TestASignatureAfterSigningInAgainAtTheProvider(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, aliceEmail, "member")
	key := projectKey(t, "SSG")
	owner, _ := adminOf(t, h, a)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make the project")
	want(t, owner.patch(t, "/api/v1/projects/"+key, map[string]any{"settings": map[string]any{"signedApprovals": true}}), http.StatusOK, "sign its approvals")
	alice := h.userID(t, aliceEmail)
	want(t, owner.put(t, "/api/v1/projects/"+key+"/approval-rules", map[string]any{"required": 1, "approvers": []map[string]any{{"subjectType": "user", "userId": alice}}}),
		http.StatusOK, "alice approves")
	review := func(n int) string {
		want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes", "statement": statementSaying("Stop.")}), http.StatusCreated, "write one")
		return obj(t, want(t, owner.post(t, "/api/v1/requirements/"+key+"-R"+strconv.Itoa(n)+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask"), "review")["id"].(string)
	}
	first, second := review(1), review(2)

	b := signedIn(t, a, "alice", alicePassword)
	if _, me := b.me(t); me["signsWith"] != "provider" {
		t.Fatalf("alice signs with %v", me["signsWith"])
	}
	page := "/p/" + key + "/r/" + key + "-R1"
	landing := b.reauthenticate(t, "alice", alicePassword, page)
	target, err := url.Parse(landing)
	if err != nil || target.Host != appHost || target.Path != page || target.Query().Get("signature") == "" {
		t.Fatalf("the re-authentication landed on %q", landing)
	}
	proof := target.Query().Get("signature")

	resp, body := b.postJSON(t, "/api/v1/reviews/"+first+"/decisions", map[string]any{"decision": "approve", "revision": 1, "reauthentication": proof})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alice's signed approval: %s %s", resp.Status, body)
	}
	var got struct {
		Review struct {
			State     string `json:"state"`
			Decisions []struct {
				Signature struct {
					Method string `json:"method"`
				} `json:"signature"`
			} `json:"decisions"`
		} `json:"review"`
	}
	_ = json.Unmarshal([]byte(body), &got)
	if got.Review.State != "approved" || len(got.Review.Decisions) != 1 || got.Review.Decisions[0].Signature.Method != "oidc" {
		t.Errorf("the review reads %s", body)
	}
	if resp, body := b.postJSON(t, "/api/v1/reviews/"+second+"/decisions", map[string]any{"decision": "approve", "revision": 1, "reauthentication": proof}); resp.StatusCode != http.StatusConflict ||
		!strings.Contains(body, "signature_spent") {
		t.Errorf("a re-authentication spent twice: %s %s", resp.Status, body)
	}

	t.Run("a re-authentication of another session is not this one's", func(t *testing.T) {
		a.as(t, alice)
		var otherSession uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM user_session WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, alice).Scan(&otherSession); err != nil {
			t.Fatal(err)
		}
		var foreign uuid.UUID
		if err := h.super.QueryRow(context.Background(), `
			INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'oidc', now(), now() + interval '5 minutes') RETURNING id`, alice, otherSession).Scan(&foreign); err != nil {
			t.Fatal(err)
		}
		if resp, body := b.postJSON(t, "/api/v1/reviews/"+second+"/decisions", map[string]any{"decision": "approve", "revision": 1, "reauthentication": foreign}); resp.StatusCode != http.StatusConflict {
			t.Errorf("another session's re-authentication signed: %s %s", resp.Status, body)
		}
	})
}

func TestRawSQLGuardsSignatures(t *testing.T) {
	s := signedReview(t, "SGQ")
	ctx := context.Background()
	dave := s.h.appConn(t, s.daveID)
	var session uuid.UUID
	if err := s.h.super.QueryRow(ctx, `
		INSERT INTO user_session (user_id, token_hash, proof, expires_at) VALUES ($1, decode(md5(random()::text) || md5(random()::text), 'hex'), 'password', now() + interval '1 hour')
		RETURNING id`, s.daveID).Scan(&session); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := s.h.super.QueryRow(ctx, `SELECT hash FROM requirement_revision WHERE requirement_id = $1 AND number = 1`, s.reqID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	proof := func() uuid.UUID {
		var id uuid.UUID
		if err := dave.QueryRow(ctx, `INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'password', now(), now() + interval '1 minute') RETURNING id`, s.daveID, session).Scan(&id); err != nil {
			t.Fatalf("dave records the password he gave: %v", err)
		}
		return id
	}
	insertSignature := `INSERT INTO review_signature (review_id, requirement_id, revision, content_hash, signer_id, decision, meaning, method, authenticated_at, session_id, reauthentication_id)
		SELECT $1, $2, 1, $3, $4, 'approve', 'I approve.', 'password', r.authenticated_at, r.session_id, r.id FROM reauthentication r WHERE r.id = $5`

	if _, err := dave.Exec(ctx, `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, 'approve')`, s.reviewID, s.daveID); !isCheckViolation(err) {
		t.Errorf("an unsigned decision in a signing project: %v", err)
	}
	if _, err := dave.Exec(ctx, `INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
		VALUES ($1, $2, 'oidc', now(), now() + interval '1 minute')`, s.daveID, session); err == nil {
		t.Error("the app vouched for a sign-in at the identity provider")
	}
	if _, err := dave.Exec(ctx, `INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
		VALUES ($1, $2, 'password', now(), now() + interval '1 minute')`, s.carolID, session); err == nil {
		t.Error("a re-authentication was recorded for somebody else")
	}
	if _, err := dave.Exec(ctx, insertSignature, s.reviewID, s.reqID, "not-the-hash", s.daveID, proof()); !isCheckViolation(err) {
		t.Errorf("a signature of other content: %v", err)
	}

	tx, err := dave.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reauth := proof()
	if _, err := tx.Exec(ctx, insertSignature, s.reviewID, s.reqID, hash, s.daveID, reauth); err != nil {
		t.Fatalf("a signature as the api writes it: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_decision (review_id, reviewer_id, decision) VALUES ($1, $2, 'approve')`, s.reviewID, s.daveID); err != nil {
		t.Fatalf("the decision it stands on: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := dave.Exec(ctx, insertSignature, s.reviewID, s.reqID, hash, s.daveID, reauth); err == nil {
		t.Error("a re-authentication was spent twice")
	}
	if _, err := dave.Exec(ctx, `UPDATE review_signature SET meaning = 'I did not.' WHERE signer_id = $1`, s.daveID); err == nil {
		t.Error("a signature was changed")
	}
	if _, err := dave.Exec(ctx, `DELETE FROM review_signature WHERE signer_id = $1`, s.daveID); err == nil {
		t.Error("a signature was deleted")
	}
	if seesRows(t, s.h.appConn(t, s.carolID), `SELECT count(*) FROM reauthentication WHERE user_id = $1`, s.daveID) != 0 {
		t.Error("somebody sees another's re-authentications")
	}
}
