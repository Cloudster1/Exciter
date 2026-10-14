//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// themeRows gives the installation a private theme of its owner's with a file, a shared theme of a plain member's,
// and the member's choice of it, all written as the superuser so the policies play no part in making them.
type themeRows struct {
	member                  uuid.UUID
	privateID, sharedID     uuid.UUID
	privateFile, sharedFile uuid.UUID
}

func (h *harness) themeRows(t *testing.T, m member) themeRows {
	t.Helper()
	ctx := context.Background()
	r := themeRows{member: h.addPerson(t, m.org, "member")}
	for _, row := range []struct {
		owner  uuid.UUID
		shared bool
		id     *uuid.UUID
		file   *uuid.UUID
	}{{m.user, false, &r.privateID, &r.privateFile}, {r.member, true, &r.sharedID, &r.sharedFile}} {
		if err := h.super.QueryRow(ctx, `INSERT INTO theme (org_id, owner_id, name, shared) VALUES ($1, $2, $3, $4) RETURNING id`,
			m.org, row.owner, uuid.NewString(), row.shared).Scan(row.id); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(ctx, `INSERT INTO theme_asset (org_id, theme_id, name, content_type, size) VALUES ($1, $2, 'a.svg', 'image/svg+xml', 1) RETURNING id`,
			m.org, *row.id).Scan(row.file); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO user_theme (org_id, user_id, theme_id) VALUES ($1, $2, $3), ($1, $4, $3)`, m.org, r.member, r.sharedID, m.user); err != nil {
		t.Fatal(err)
	}
	return r
}

// affects runs a statement and answers how many rows it touched, failing on an error.
func affects(t *testing.T, conn *pgx.Conn, sql string, args ...any) int64 {
	t.Helper()
	tag, err := conn.Exec(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return tag.RowsAffected()
}

// The service refusing is not proof: straight through SQL as exciter_app, nothing unscoped reaches a theme row, a
// member sees only their own and the shared themes, changes only their own, and chooses only for themselves.
func TestRawSQLKeepsThemesToTheirPeople(t *testing.T) {
	h := newHarness(t)
	a := h.owner(t, "theme-wall-a")
	h.keepDefaultTheme(t)
	rows := h.themeRows(t, a)
	ctx := context.Background()
	count := func(conn *pgx.Conn, sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	tables := []string{"theme", "theme_asset", "user_theme"}

	t.Run("nothing unscoped reaches a theme, and another member changes nobody's", func(t *testing.T) {
		unscoped := h.appConnUnscoped(t)
		for _, table := range tables {
			if n := count(unscoped, `SELECT count(*) FROM `+table); n != 0 {
				t.Errorf("an unscoped connection sees %d rows in %s", n, table)
			}
		}
		stranger := h.appConn(t, a.org, h.addPerson(t, a.org, "member"))
		if n := affects(t, stranger, `UPDATE theme SET name = 'Taken' WHERE id = $1`, rows.sharedID); n != 0 {
			t.Error("another member renamed a shared theme")
		}
		if n := affects(t, stranger, `DELETE FROM theme_asset WHERE theme_id = $1`, rows.sharedID); n != 0 {
			t.Error("another member deleted a shared theme's files")
		}
	})

	plain := h.appConn(t, a.org, rows.member)
	t.Run("a member sees their own and the shared themes, and the files of those alone", func(t *testing.T) {
		if n := count(plain, `SELECT count(*) FROM theme WHERE id IN ($1, $2)`, rows.privateID, rows.sharedID); n != 1 {
			t.Errorf("a member sees %d of the two themes, want the shared one", n)
		}
		if n := count(plain, `SELECT count(*) FROM theme_asset WHERE theme_id = $1`, rows.privateID); n != 0 {
			t.Error("a member sees the files of somebody's private theme")
		}
		if n := count(plain, `SELECT count(*) FROM user_theme WHERE user_id IN ($1, $2)`, rows.member, a.user); n != 1 {
			t.Errorf("a member sees %d choices, want their own", n)
		}
		if n := count(plain, `SELECT theme_in_use($1)`, rows.sharedID); n != 2 {
			t.Errorf("the shared theme is in use by %d, want both", n)
		}
		if n := count(plain, `SELECT theme_in_use($1)`, rows.privateID); n != 0 {
			t.Errorf("a member counts %d people on a theme out of their sight", n)
		}
	})

	owner := h.appConn(t, a.org, a.user)
	admin := h.appConn(t, a.org, h.addPerson(t, a.org, "admin"))
	t.Run("only the owner, or an administrator once shared, changes a theme or its files", func(t *testing.T) {
		if n := affects(t, plain, `UPDATE theme SET name = 'Mine now' WHERE id = $1`, rows.privateID); n != 0 {
			t.Error("a member renamed somebody's private theme")
		}
		if n := affects(t, owner, `UPDATE theme SET name = 'Tidied' WHERE id = $1`, rows.sharedID); n != 1 {
			t.Error("an owner of the installation could not tidy a shared theme")
		}
		if n := affects(t, admin, `UPDATE theme SET name = 'Peeked' WHERE id = $1`, rows.privateID); n != 0 {
			t.Error("an administrator changed somebody's private theme")
		}
		if n := affects(t, admin, `DELETE FROM theme_asset WHERE theme_id = $1`, rows.privateID); n != 0 {
			t.Error("an administrator removed the files of somebody's private theme")
		}
		if _, err := admin.Exec(ctx, `UPDATE theme SET owner_id = $2 WHERE id = $1`, rows.sharedID, a.user); !isCheckViolation(err) {
			t.Errorf("an administrator took a theme over: %v", err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO theme (org_id, owner_id, name) VALUES ($1, $2, 'Forged')`, a.org, a.user); !isPolicyViolation(err) {
			t.Errorf("a member made a theme in somebody else's name: %v", err)
		}
		if _, err := plain.Exec(ctx, `INSERT INTO theme_asset (org_id, theme_id, name, content_type, size) VALUES ($1, $2, 'x.svg', 'image/svg+xml', 1)`, a.org, rows.privateID); !isPolicyViolation(err) {
			t.Errorf("a member added a file to somebody's private theme: %v", err)
		}
		if n := affects(t, plain, `DELETE FROM theme WHERE id = $1`, rows.privateID); n != 0 {
			t.Error("a member deleted somebody's private theme")
		}
	})

	t.Run("a choice is one's own, and of a theme one may see", func(t *testing.T) {
		if _, err := plain.Exec(ctx, `UPDATE user_theme SET theme_id = $1 WHERE user_id = $2`, rows.privateID, rows.member); !isPolicyViolation(err) {
			t.Errorf("a member chose a private theme of somebody else's: %v", err)
		}
		if n := affects(t, plain, `DELETE FROM user_theme WHERE user_id = $1`, a.user); n != 0 {
			t.Error("a member undid somebody else's choice")
		}
		if _, err := plain.Exec(ctx, `INSERT INTO user_theme (org_id, user_id, theme_id) VALUES ($1, $2, NULL)`, a.org, uuid.New()); !isPolicyViolation(err) {
			t.Errorf("a member chose for somebody else: %v", err)
		}
	})

	t.Run("only an administrator names the default, and only a shared theme", func(t *testing.T) {
		if n := affects(t, plain, `UPDATE org SET default_theme_id = $1 WHERE id = $2`, rows.sharedID, a.org); n != 0 {
			t.Error("a member named the default for everyone")
		}
		if _, err := admin.Exec(ctx, `UPDATE org SET default_theme_id = $1 WHERE id = $2`, rows.privateID, a.org); !isCheckViolation(err) {
			t.Errorf("a private theme became the default: %v", err)
		}
		if n := affects(t, admin, `UPDATE org SET default_theme_id = $1 WHERE id = $2`, rows.sharedID, a.org); n != 1 {
			t.Fatal("an administrator could not name a shared theme the default")
		}
		// Its owner is no administrator, yet taking it private takes it off everyone.
		if n := affects(t, plain, `UPDATE theme SET shared = false WHERE id = $1`, rows.sharedID); n != 1 {
			t.Fatal("the owner could not take their theme private")
		}
		if n := h.countRows(t, `SELECT count(*) FROM org WHERE id = $1 AND default_theme_id IS NOT NULL`, a.org); n != 0 {
			t.Error("a private theme stayed the default for everyone")
		}
	})
}
