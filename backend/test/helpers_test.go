//go:build integration

package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// workerIdle is how long a worker the suite runs waits when nothing is due, short so a test does not wait it out.
const workerIdle = 50 * time.Millisecond

// addPerson makes somebody a member of org with role, behind the policies' back.
func (h *harness) addPerson(t *testing.T, org uuid.UUID, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	email := fmt.Sprintf("%s-%s@example.test", role, h.suffix())
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`, email, "A "+role).Scan(&id); err != nil {
		t.Fatalf("create a %s: %v", role, err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`, org, id, role); err != nil {
		t.Fatalf("make the %s a member: %v", role, err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = $1`, id) })
	h.settle(t)
	return id
}

// namedPerson is addPerson for a member who goes by name.
func (h *harness) namedPerson(t *testing.T, org uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := h.addPerson(t, org, "member")
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET name = $2 WHERE id = $1`, id, name); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	return id
}

// slugOf is the slug an organization is addressed by.
func (h *harness) slugOf(t *testing.T, org uuid.UUID) string {
	t.Helper()
	var slug string
	if err := h.super.QueryRow(context.Background(), `SELECT slug FROM org WHERE id = $1`, org).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	return slug
}

// countRows counts as the superuser, past every policy, to see what is really there.
func (h *harness) countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func sameList(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}
