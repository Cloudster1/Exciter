package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestATokensProjectsFollowItsPersonAndNoOther(t *testing.T) {
	person, other := uuid.New(), uuid.New()
	a, b := uuid.MustParse("019a0000-0000-7000-8000-00000000000a"), uuid.MustParse("019a0000-0000-7000-8000-00000000000b")

	limited := WithUserInProjects(context.Background(), person, []uuid.UUID{a, b})
	if id, ok := UserFrom(limited); !ok || id != person {
		t.Fatalf("the limited context acts for %v %v, want %v", id, ok, person)
	}
	projects, only := ProjectsFrom(limited)
	if !only || len(projects) != 2 {
		t.Fatalf("the limit is lost: %v %v", projects, only)
	}
	if got := projectsSetting(projects); got != "{"+a.String()+","+b.String()+"}" {
		t.Errorf("the setting reads %q", got)
	}
	// Somebody else named on the same context, as a worker does, is not
	// held to the token of the person before.
	if _, only := ProjectsFrom(WithUser(limited, other)); only {
		t.Error("naming another person kept the token's limit")
	}
	if _, only := ProjectsFrom(WithUser(context.Background(), person)); only {
		t.Error("a person without a token is limited")
	}
	// A token whose every project is gone reaches none, never all of them.
	none, only := ProjectsFrom(WithUserInProjects(context.Background(), person, nil))
	if !only || projectsSetting(none) != "{}" {
		t.Errorf("a token without projects left = %v %q, want limited to none", only, projectsSetting(none))
	}
}

func TestNobodyIsNamedUntilSomebodyIs(t *testing.T) {
	if _, ok := UserFrom(context.Background()); ok {
		t.Error("a bare context names somebody")
	}
	if _, ok := UserFrom(WithUser(context.Background(), uuid.Nil)); ok {
		t.Error("the nil id counts as a person")
	}
}

func TestPinsOnlyEverRise(t *testing.T) {
	ctx := PinLSN(PinLSN(context.Background(), 200), 100)
	if got := pinFrom(ctx).requiredLSN; got != 200 {
		t.Errorf("the pin fell back to %d, want 200", got)
	}
	if !pinFrom(PinPrimary(ctx)).forcePrimary || pinFrom(PinPrimary(ctx)).requiredLSN != 200 {
		t.Error("pinning to the primary lost the position")
	}
}

// What one read saw holds every later read made with the context to it, and only a step forward is news (#374).
func TestAReadNeverGoesBackOnWhatAnEarlierOneSaw(t *testing.T) {
	ctx := TrackReads(PinLSN(context.Background(), 100), nil)
	tracker := trackerFrom(ctx)
	if got := pinFrom(ctx).requiredLSN; got != 100 {
		t.Fatalf("before any read the pin is %d, want the caller's own 100", got)
	}
	if !tracker.saw(300) {
		t.Error("a read seeing further than any before was not news")
	}
	if tracker.saw(200) || tracker.saw(300) {
		t.Error("a read seeing less, or as much, was taken for news")
	}
	if got := pinFrom(ctx).requiredLSN; got != 300 {
		t.Errorf("after a read saw 300 the pin is %d, want 300", got)
	}
	if got := pinFrom(PinLSN(ctx, 400)).requiredLSN; got != 400 {
		t.Errorf("a later write's 400 gave %d", got)
	}
	if trackerFrom(context.Background()) != nil {
		t.Error("an untracked context has a tracker")
	}
}
