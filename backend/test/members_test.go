//go:build integration

package test

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// An administrator lists, adds, re-roles and switches off and on the installation's people, each act recorded once;
// the administrator themselves stays as they are.
func TestMembersOverTheAPI(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	home := h.owner(t, "members")
	adminID := h.addPerson(t, "admin")
	memberID := h.addPerson(t, "member")
	admin, member, nobody := a.as(t, adminID), a.as(t, memberID), a.anonymous()

	people := list(t, want(t, admin.get(t, "/api/v1/users"), http.StatusOK, "list the people"), "members")
	if peopleIndex(people, home.user) < 0 || peopleIndex(people, adminID) < 0 || peopleIndex(people, memberID) < 0 {
		t.Fatalf("the people read %v, want the test's three among them", people)
	}
	for id, administers := range map[uuid.UUID]bool{home.user: true, adminID: true, memberID: false} {
		person := people[peopleIndex(people, id)].(map[string]any)
		roles, _ := person["roles"].([]any)
		named := slices.ContainsFunc(roles, func(r any) bool {
			role := r.(map[string]any)
			return role["role"] == "administrator" && role["groupId"] == nil
		})
		if person["role"] != "member" || person["administers"] != administers || named != administers {
			t.Errorf("%s reads %v, want a member who administers: %v, by name", id, person, administers)
		}
	}

	email := "added-" + h.suffix() + "@example.test"
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	made := obj(t, want(t, admin.post(t, "/api/v1/users", map[string]any{"email": email, "role": "member"}), http.StatusCreated, "add a person"), "member")
	password, _ := made["password"].(string)
	if made["newAccount"] != true || len(password) < 12 {
		t.Fatalf("a new person reads %v, want a made password shown once", made)
	}
	if _, err := a.accounts.Login(context.Background(), email, password, "", ""); err != nil {
		t.Errorf("the made password does not sign in: %v", err)
	}
	if code := errorCode(t, want(t, admin.post(t, "/api/v1/users", map[string]any{"email": email, "role": "member"}), http.StatusConflict, "add them again")); code != "conflict" {
		t.Errorf("adding a member twice answers %s", code)
	}
	adminEmail := "added-admin-" + h.suffix() + "@example.test"
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, adminEmail) })
	madeAdmin := obj(t, want(t, admin.post(t, "/api/v1/users", map[string]any{"email": adminEmail, "role": "admin"}), http.StatusCreated, "add an administrator"), "member")
	if n := h.countRows(t, `SELECT count(*) FROM app_user u JOIN role_assignment a ON a.user_id = u.id
		WHERE u.id = $1 AND u.role = 'member' AND a.role = 'administrator' AND a.project_id IS NULL`, madeAdmin["userId"]); madeAdmin["role"] != "admin" || n != 1 {
		t.Errorf("adding somebody as admin made %v with %d administrator grants, want a member who holds the role", madeAdmin, n)
	}
	for _, bad := range []map[string]any{
		{"email": "not an address", "role": "member"},
		{"email": "x-" + h.suffix() + "@example.test", "role": "owner"},
		{"email": "y-" + h.suffix() + "@example.test", "role": "member", "password": "short"},
	} {
		want(t, admin.post(t, "/api/v1/users", bad), http.StatusUnprocessableEntity, "add with a bad field")
	}
	added := uuid.MustParse(made["userId"].(string))

	changed := obj(t, want(t, admin.patch(t, "/api/v1/users/"+added.String(), map[string]any{"role": "guest"}), http.StatusOK, "make a guest"), "member")
	if changed["role"] != "guest" {
		t.Errorf("the role reads %v", changed)
	}
	for what, req := range map[string]struct {
		id   uuid.UUID
		role string
		code int
	}{
		"make an owner":        {added, "owner", http.StatusUnprocessableEntity},
		"make an admin":        {added, "admin", http.StatusUnprocessableEntity},
		"change oneself":       {adminID, "guest", http.StatusConflict},
		"change somebody gone": {uuid.New(), "member", http.StatusNotFound},
	} {
		want(t, admin.patch(t, "/api/v1/users/"+req.id.String(), map[string]any{"role": req.role}), req.code, what)
	}

	want(t, admin.patch(t, "/api/v1/users/"+added.String(), map[string]any{}), http.StatusUnprocessableEntity, "change nothing")
	want(t, admin.delete(t, "/api/v1/users/"+adminID.String()), http.StatusConflict, "switch oneself off")
	want(t, admin.delete(t, "/api/v1/users/"+uuid.New().String()), http.StatusNotFound, "switch somebody gone off")

	// The guest signs in and makes a token, both of which switching them off ends.
	guest := a.anonymous()
	want(t, guest.post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusOK, "the guest signs in")
	want(t, guest.post(t, "/api/v1/tokens", map[string]any{"name": "nightly"}), http.StatusCreated, "the guest makes a token")
	want(t, admin.delete(t, "/api/v1/users/"+added.String()), http.StatusNoContent, "switch the guest off")
	want(t, admin.delete(t, "/api/v1/users/"+added.String()), http.StatusNoContent, "switch them off again")
	h.settle(t)
	want(t, guest.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "the guest's session once switched off")
	for what, sql := range map[string]string{
		"sessions": `SELECT count(*) FROM user_session WHERE user_id = $1`,
		"tokens":   `SELECT count(*) FROM api_token WHERE user_id = $1`,
	} {
		if n := h.countRows(t, sql, added); n != 0 {
			t.Errorf("switching the guest off left %d %s", n, what)
		}
	}
	if code := errorCode(t, want(t, a.anonymous().post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusForbidden, "sign in switched off")); code != "account_switched_off" {
		t.Errorf("a switched-off sign-in answers %s", code)
	}
	people = list(t, want(t, admin.get(t, "/api/v1/users"), http.StatusOK, "list the people"), "members")
	at := peopleIndex(people, added)
	if at < 0 || people[at].(map[string]any)["active"] != false || people[at].(map[string]any)["role"] != "guest" || people[len(people)-1].(map[string]any)["active"] != false {
		t.Errorf("the people read %v, want the guest among those switched off, last, and still a guest", people)
	}
	for _, each := range people[at:] {
		if each.(map[string]any)["active"] != false {
			t.Errorf("somebody switched on comes after the guest switched off: %v", each)
		}
	}

	back := obj(t, want(t, admin.patch(t, "/api/v1/users/"+added.String(), map[string]any{"active": true}), http.StatusOK, "switch the guest on"), "member")
	if back["active"] != true {
		t.Errorf("the guest reads %v once switched on", back)
	}
	h.settle(t)
	want(t, a.anonymous().post(t, "/api/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusOK, "sign in switched on")

	// Any administrator lets another go while another stays, as no owner outranks them any more.
	second := h.addPerson(t, "admin")
	want(t, admin.delete(t, "/api/v1/users/"+second.String()), http.StatusNoContent, "an administrator switches another off")

	want(t, member.get(t, "/api/v1/users"), http.StatusForbidden, "a member lists the people")
	want(t, member.delete(t, "/api/v1/users/"+adminID.String()), http.StatusForbidden, "a member switches somebody off")
	want(t, member.patch(t, "/api/v1/users/"+adminID.String(), map[string]any{"role": "member"}), http.StatusForbidden, "a member re-roles somebody")
	want(t, member.post(t, "/api/v1/users", map[string]any{"email": "z@example.test", "role": "member"}), http.StatusForbidden, "a member adds somebody")
	want(t, nobody.get(t, "/api/v1/users"), http.StatusUnauthorized, "nobody lists the people")

	for action, n := range map[string]int{audit.ActionMemberCreated: 1, audit.ActionMemberRoleChanged: 1, audit.ActionMemberSwitchedOff: 1, audit.ActionMemberSwitchedOn: 1} {
		if got := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2 AND actor_user_id = $3`, action, added, adminID); got != n {
			t.Errorf("%s was recorded %d times, want %d", action, got, n)
		}
	}
}

// Straight through SQL as exciter_app: only an administrator grants administering or switches somebody off, nobody
// is made an owner or loses one's standing, and nobody leaves.
func TestMembershipIsTheAdministratorsThroughSQL(t *testing.T) {
	h := newHarness(t)
	home := h.owner(t, "members-sql")
	memberID := h.addPerson(t, "member")
	otherID := h.addPerson(t, "member")
	asMember := db.WithUser(context.Background(), memberID)

	affected := func(ctx context.Context, sql string, args ...any) (int64, error) {
		var n int64
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, sql, args...)
			n = tag.RowsAffected()
			return err
		})
		return n, err
	}
	if _, err := affected(asMember, `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1)`, memberID); !isPolicyViolation(err) {
		t.Errorf("a member raised themselves = %v, want a policy violation", err)
	}
	if n, err := affected(asMember, `UPDATE app_user SET is_active = false WHERE id = $1`, otherID); err != nil || n != 0 {
		t.Errorf("a member switched somebody off: %d rows, %v", n, err)
	}
	if n, err := affected(asMember, `UPDATE app_user SET role = NULL WHERE id = $1`, otherID); err != nil || n != 0 {
		t.Errorf("a member took somebody's membership away: %d rows, %v", n, err)
	}
	_, err := affected(asMember, `DELETE FROM app_user WHERE id = $1`, otherID)
	permissionDenied(t, "a member deleting somebody's account", err)
	_, err = affected(home.ctx, `DELETE FROM app_user WHERE id = $1`, otherID)
	permissionDenied(t, "the owner deleting an account rather than switching it off", err)
	var pgErr *pgconn.PgError
	if _, err = affected(home.ctx, `UPDATE app_user SET role = NULL WHERE id = $1`, otherID); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("the owner taking a membership away rather than switching it off = %v, want refused", err)
	}
	newcomer := h.outsider(t, "members-sql-newcomer")
	if n, err := affected(asMember, `UPDATE app_user SET role = 'member' WHERE id = $1`, newcomer); err != nil || n != 0 {
		t.Errorf("a member let somebody in: %d rows, %v", n, err)
	}

	if _, err = affected(home.ctx, `UPDATE app_user SET role = 'owner' WHERE id = $1`, memberID); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("an administrator making an owner, no standing any more, through the app role = %v, want refused", err)
	}
	if n, err := affected(home.ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1)`, memberID); err != nil || n != 1 {
		t.Errorf("an administrator could not make a member an administrator: %d rows, %v", n, err)
	}
	asAdmin := db.WithUser(context.Background(), memberID)
	if n, err := affected(asAdmin, `UPDATE app_user SET is_active = false WHERE id = $1`, otherID); err != nil || n != 1 {
		t.Errorf("an administrator could not switch a member off: %d rows, %v", n, err)
	}

}

// lastAdministratorDatabase is a database of its own, migrated, with an installation whose owner is its one
// administrator: the shared installation always has others, so its last one is never reached.
func lastAdministratorDatabase(t *testing.T, h *harness) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	provider, super, _ := scratchDatabase(t, h, "exciter_last_administrator_test")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := super.Exec(ctx, `INSERT INTO installation (name) VALUES ('Exciter')`); err != nil {
		t.Fatal(err)
	}
	return super, addAdministrator(t, super, "owner")
}

// addAdministrator puts a switched-on administrator into the scratch installation.
func addAdministrator(t *testing.T, super *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := super.QueryRow(context.Background(), `INSERT INTO app_user (email, name, role) VALUES ($1, $2, 'member') RETURNING id`,
		name+"@example.test", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := super.Exec(context.Background(), `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Straight through SQL, even as the superuser: the installation's last active administrator is neither switched
// off, nor made a guest, nor taken out, nor loses the grant or the role that lets them administer, and one goes off
// once another administrator stays.
func TestTheInstallationKeepsAnActiveAdministrator(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	super, owner := lastAdministratorDatabase(t, h)
	var pgErr *pgconn.PgError
	for what, sql := range map[string]string{
		"switching the last active administrator off": `UPDATE app_user SET is_active = false WHERE id = $1`,
		"making them a guest":                         `UPDATE app_user SET role = 'guest' WHERE id = $1`,
		"taking their membership away":                `UPDATE app_user SET role = NULL WHERE id = $1`,
		"deleting their account":                      `DELETE FROM app_user WHERE id = $1`,
	} {
		if _, err := super.Exec(ctx, sql, owner); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s = %v, want refused", what, err)
		}
	}
	for what, sql := range map[string]string{
		"taking their grant away":            `DELETE FROM role_assignment WHERE user_id = $1`,
		"taking administering from the role": `UPDATE role SET permissions = '{view}' WHERE key = 'administrator' AND $1::uuid IS NOT NULL`,
	} {
		if _, err := super.Exec(ctx, sql, owner); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s = %v, want refused", what, err)
		}
	}
	// With nobody who administers, the rest come and go: only a change that takes administering away is weighed.
	plain := addAdministrator(t, super, "plain")
	if _, err := super.Exec(ctx, `DELETE FROM role_assignment WHERE user_id = $1`, plain); err != nil {
		t.Fatalf("taking a second administrator's grant = %v, want allowed", err)
	}
	if _, err := super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, plain); err != nil {
		t.Errorf("switching off somebody who administers nothing = %v, want allowed", err)
	}
	admin := addAdministrator(t, super, "admin")
	if _, err := super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, owner); err != nil {
		t.Errorf("switching the first administrator off beside another = %v, want allowed", err)
	}
	if _, err := super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, admin); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("switching the administrator off once the owner is off = %v, want refused", err)
	}
}

// Two administrators switching each other off at once do not leave the installation without one: the second waits
// for the first and is refused.
func TestTwoAdministratorsCannotSwitchEachOtherOff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	super, owner := lastAdministratorDatabase(t, h)
	one, other := addAdministrator(t, super, "one"), addAdministrator(t, super, "other")
	// With the owner switched off, the two are the only active administrators.
	if _, err := super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, owner); err != nil {
		t.Fatal(err)
	}

	first, err := super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(ctx) }()
	if _, err := first.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, other); err != nil {
		t.Fatalf("the first switch-off = %v", err)
	}
	second := make(chan error, 1)
	// Spelled out, so the wait below finds this statement and no other.
	switchOff := `UPDATE app_user SET is_active = false WHERE id = '` + one.String() + `'`
	go func() {
		_, err := super.Exec(ctx, switchOff)
		second <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for h.countRows(t, `SELECT count(*) FROM pg_stat_activity WHERE wait_event = 'advisory' AND query = $1`, switchOff) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the second switch-off never waited for the first")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := <-second; !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("the second switch-off = %v, want refused once the first committed", err)
	}
}

// The audit log's endpoints answer administrators, refuse members and nobody,
// and an export is itself recorded.
func TestTheAuditLogOverTheAPI(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	adminID := h.addPerson(t, "admin")
	memberID := h.addPerson(t, "member")
	admin, member, nobody := a.as(t, adminID), a.as(t, memberID), a.anonymous()
	want(t, admin.patch(t, "/api/v1/users/"+memberID.String(), map[string]any{"role": "guest"}), http.StatusOK, "make an entry")

	entries := list(t, want(t, admin.get(t, "/api/v1/audit?actor="+adminID.String()), http.StatusOK, "read the log"), "entries")
	if len(entries) != 1 || entries[0].(map[string]any)["action"] != audit.ActionMemberRoleChanged || entries[0].(map[string]any)["actorName"] == "" {
		t.Errorf("the log reads %v", entries)
	}
	facets := obj(t, want(t, admin.get(t, "/api/v1/audit/facets"), http.StatusOK, "facets"), "facets")
	if facets["retentionDays"] != float64(365) || !slices.Contains(facets["actions"].([]any), any(audit.ActionMemberRoleChanged)) {
		t.Errorf("the facets read %v", facets)
	}
	for query, field := range map[string]string{"action=member.danced": "action", "actor=ann": "actor", "from=2026-13-01": "from", "cursor=nonsense": "cursor", "limit=0": "limit"} {
		got := want(t, admin.get(t, "/api/v1/audit?"+query), http.StatusUnprocessableEntity, query)
		if got.Body["error"].(map[string]any)["fields"].(map[string]any)[field] == nil {
			t.Errorf("%s does not name %s: %s", query, field, got.Raw)
		}
	}
	want(t, admin.get(t, "/api/v1/audit/export?from=2026-02-01&to=2026-01-01"), http.StatusUnprocessableEntity, "an export of a range ending before it starts")

	file := want(t, admin.get(t, "/api/v1/audit/export"), http.StatusOK, "export the log")
	if ct := file.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" || !contains(file.Header.Get("Content-Disposition"), "audit-log.csv") {
		t.Errorf("the export is served as %q, %q", ct, file.Header.Get("Content-Disposition"))
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND actor_user_id = $2`, audit.ActionAuditExported, adminID); n != 1 {
		t.Errorf("the export was recorded %d times, want once", n)
	}

	for _, path := range []string{"/api/v1/audit", "/api/v1/audit/facets", "/api/v1/audit/export"} {
		if code := errorCode(t, want(t, member.get(t, path), http.StatusForbidden, "a member reads "+path)); code != "forbidden" {
			t.Errorf("%s refuses a member with %s", path, code)
		}
		want(t, nobody.get(t, path), http.StatusUnauthorized, "nobody reads "+path)
	}
}

// peopleIndex is where the person stands in a list of members, or -1.
func peopleIndex(people []any, id uuid.UUID) int {
	for i, each := range people {
		if each.(map[string]any)["userId"] == id.String() {
			return i
		}
	}
	return -1
}

// Straight through SQL, even as the superuser: a token, a grant and a requirement's owner name a member, and taking
// a role away takes them along, as the keys to the membership did (#108).
func TestOnlyAMemberHoldsWhatAMemberHolds(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	home := h.owner(t, "holds")
	stranger := h.outsider(t, "holds-stranger")
	memberID := h.addPerson(t, "member")
	var project uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ($1, 'Held', $2) RETURNING id`,
		projectKey(t, "HOLD"), home.user).Scan(&project); err != nil {
		t.Fatal(err)
	}
	holds := map[string]func(person uuid.UUID) error{
		"a token": func(person uuid.UUID) error {
			hash := sha256.Sum256([]byte(uuid.NewString()))
			_, err := h.super.Exec(ctx, `INSERT INTO api_token (user_id, name, token_hash, scopes) VALUES ($1, 'held', $2, '{read}')`, person, hash[:])
			return err
		},
		"a grant": func(person uuid.UUID) error {
			_, err := h.super.Exec(ctx, `INSERT INTO role_assignment (project_id, user_id, role) VALUES ($1, $2, 'viewer')`, project, person)
			return err
		},
		"a requirement": func(person uuid.UUID) error {
			_, err := h.super.Exec(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, owner_id)
				VALUES ($1, 'Held', '{"type":"doc"}', 'h', $2)`, project, person)
			return err
		},
	}
	var pgErr *pgconn.PgError
	for what, hold := range holds {
		if err := hold(stranger); !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Errorf("somebody who is no member holding %s = %v, want refused", what, err)
		}
		if err := hold(memberID); err != nil {
			t.Errorf("a member holding %s = %v", what, err)
		}
	}

	if _, err := h.super.Exec(ctx, `UPDATE app_user SET role = NULL WHERE id = $1`, memberID); err != nil {
		t.Fatalf("take the role away: %v", err)
	}
	if n := h.countRows(t, `SELECT (SELECT count(*) FROM api_token WHERE user_id = $1) + (SELECT count(*) FROM role_assignment WHERE user_id = $1)
		+ (SELECT count(*) FROM requirement WHERE owner_id = $1)`, memberID); n != 0 {
		t.Errorf("somebody whose role went still holds %d things only a member holds", n)
	}
}
