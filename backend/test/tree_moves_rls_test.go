//go:build integration

package test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// crossMoveRounds is how often two moves in opposite directions are raced, so a deadlock that needs the moves to meet
// in one order has its chances.
const crossMoveRounds = 5

// crossMoveWait bounds one round, well past the deadlock detector's second.
const crossMoveWait = 20 * time.Second

// sqlTree is two projects written straight through SQL as exciter_app by an editor of both.
type sqlTree struct {
	h        *harness
	projects map[string]uuid.UUID
	editorID uuid.UUID
}

func newSQLTree(t *testing.T, keys ...string) *sqlTree {
	t.Helper()
	h := newHarness(t)
	a := h.owner(t, "tree-moves-wall")
	s := &sqlTree{h: h, projects: map[string]uuid.UUID{}, editorID: h.addPerson(t, "member")}
	for _, key := range keys {
		var id uuid.UUID
		if err := h.super.QueryRow(t.Context(), `INSERT INTO project (key, name, created_by) VALUES ($1, $1, $2) RETURNING id`, key, a.user).Scan(&id); err != nil {
			t.Fatal(err)
		}
		s.projects[key] = id
	}
	h.settle(t)
	return s
}

func (s *sqlTree) write(t *testing.T, project string, parent *uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.h.appConn(t, s.editorID).QueryRow(t.Context(), `INSERT INTO requirement (project_id, title, statement, normative_hash, parent_id, created_by)
		VALUES ($1, 'Node', '{"type":"doc"}', 'h', $2, current_actor_id()) RETURNING id`, s.projects[project], parent).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Straight through SQL, two moves between the same two projects in opposite directions both land at the same moment,
// neither waits on the other for good, and each takes a number of its new project nothing else has (#310).
func TestRawSQLMovesBetweenTwoProjectsBothWays(t *testing.T) {
	left, right := projectKey(t, "XL"), projectKey(t, "XR")
	s := newSQLTree(t, left, right)
	for _, key := range []string{left, right} {
		s.write(t, key, nil)
	}
	// A move through SQL keeps the status of the project it left, so each project's requirements hold the other's
	// statuses and neither project can go first; their requirements go before them.
	t.Cleanup(func() {
		ids := []uuid.UUID{s.projects[left], s.projects[right]}
		s.h.cleanupExec(t, s.h.super, `DELETE FROM requirement WHERE project_id = ANY($1)`, ids)
	})
	first, second := s.h.appConn(t, s.editorID), s.h.appConn(t, s.editorID)
	for round := range crossMoveRounds {
		fromLeft, fromRight := s.write(t, left, nil), s.write(t, right, nil)
		ctx, cancel := context.WithTimeout(context.Background(), crossMoveWait)
		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, errs[0] = first.Exec(ctx, `UPDATE requirement SET project_id = $2, parent_id = NULL WHERE id = $1`, fromLeft, s.projects[right])
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errs[1] = second.Exec(ctx, `UPDATE requirement SET project_id = $2, parent_id = NULL WHERE id = $1`, fromRight, s.projects[left])
		}()
		close(start)
		wg.Wait()
		cancel()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: move %d did not land: %v", round, i+1, err)
			}
		}
		for id, project := range map[uuid.UUID]string{fromLeft: right, fromRight: left} {
			if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND project_id = $2`, id, s.projects[project]); n != 1 {
				t.Errorf("round %d: the requirement did not arrive in %s", round, project)
			}
		}
	}
	for key, id := range s.projects {
		if n := s.h.countRows(t, `SELECT count(*) - count(DISTINCT number) FROM requirement WHERE project_id = $1`, id); n != 0 {
			t.Errorf("%d numbers of %s are taken twice", n, key)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE project_id = $1`, id); n != 1+crossMoveRounds {
			t.Errorf("%s holds %d requirements, want %d", key, n, 1+crossMoveRounds)
		}
	}
}

// Straight through SQL, a rank keeps its shape, and a requirement out of the trash goes below none in it, as the
// service's place does not (#310).
func TestRawSQLKeepsRanksAndParentsWhole(t *testing.T) {
	key := projectKey(t, "XP")
	s := newSQLTree(t, key)
	ctx := context.Background()
	editor := s.h.appConn(t, s.editorID)
	parent, live := s.write(t, key, nil), s.write(t, key, nil)
	trashed := s.write(t, key, nil)
	if _, err := s.h.super.Exec(ctx, `UPDATE requirement SET trashed_at = now(), trashed_by = $2, trash_id = id WHERE id = $1`, trashed, s.editorID); err != nil {
		t.Fatal(err)
	}

	t.Run("a rank of the wrong shape", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `UPDATE requirement SET rank = 'a-b' WHERE id = $1`, live); !refusedBy(err, "requirement_rank_shape") {
			t.Errorf("a rank of the wrong shape was written: %v", err)
		}
	})

	t.Run("below a requirement in the trash", func(t *testing.T) {
		if _, err := editor.Exec(ctx, `UPDATE requirement SET parent_id = $2 WHERE id = $1`, live, trashed); !refusedBy(err, "requirement_parent_live") {
			t.Errorf("a requirement went below one in the trash: %v", err)
		}
		if _, err := editor.Exec(ctx, `INSERT INTO requirement (project_id, title, statement, normative_hash, parent_id, created_by)
			VALUES ($1, 'Node', '{"type":"doc"}', 'h', $2, current_actor_id())`, s.projects[key], trashed); !refusedBy(err, "requirement_parent_live") {
			t.Errorf("a requirement was written below one in the trash: %v", err)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE parent_id = $1`, trashed); n != 0 {
			t.Errorf("%d requirements sit below the one in the trash", n)
		}
		if _, err := editor.Exec(ctx, `UPDATE requirement SET parent_id = $2 WHERE id = $1`, live, parent); err != nil {
			t.Errorf("a requirement did not go below a live one: %v", err)
		}
	})
}
