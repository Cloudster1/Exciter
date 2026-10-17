package review

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A new round pins anew only what was revised since its pin; what was not keeps its decisions, and what is in the
// trash or waits on a review of its own is left as it was.
func TestARoundPinsAnewOnlyWhatWasRevised(t *testing.T) {
	state := func(revision, now int, trashed, pending bool) pinState {
		return pinState{pinned: pinned{requirementID: uuid.New(), revision: revision, round: 1}, now: now, trashed: trashed, pending: pending}
	}
	revised, kept, trashed, waiting := state(1, 2, false, false), state(3, 3, false, false), state(1, 2, true, false), state(1, 2, false, true)
	got := pinAgain([]pinState{kept, revised, trashed, waiting})
	if len(got) != 1 || got[0].requirementID != revised.requirementID {
		t.Errorf("a round pins %v, want only the revised one", got)
	}
	if pinAgain([]pinState{kept}) != nil {
		t.Error("a round with nothing revised pins something")
	}
}

func TestADeadlineIsAnHourToAYearAhead(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at time.Time
		ok bool
	}{
		{now.Add(30 * time.Minute), false},
		{now.Add(2 * time.Hour), true},
		{now.Add(MaxDeadline + time.Hour), false},
	} {
		at := c.at
		if err := checkDeadline(&at, now); (err == nil) != c.ok {
			t.Errorf("a deadline at %v: %v, want ok %v", at, err, c.ok)
		}
	}
	if err := checkDeadline(nil, now); err != nil {
		t.Errorf("no deadline: %v", err)
	}
}
