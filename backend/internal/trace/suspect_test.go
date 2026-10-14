package trace

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMovedFollowsTheTypesSetting(t *testing.T) {
	// The source is at 3, confirmed at 2; the target at 5, confirmed at 4: both moved on once.
	both := Revisions{SourceReviewed: 2, SourceCurrent: 3, TargetReviewed: 4, TargetCurrent: 5}
	source := MovedEnd{End: EndSource, Reviewed: 2, Current: 3}
	target := MovedEnd{End: EndTarget, Reviewed: 4, Current: 5}
	for _, c := range []struct {
		name  string
		watch SuspectEnd
		r     Revisions
		want  []MovedEnd
	}{
		{"target watches the target", SuspectTarget, both, []MovedEnd{target}},
		{"source watches the source", SuspectSource, both, []MovedEnd{source}},
		{"both watches either", SuspectBoth, both, []MovedEnd{source, target}},
		{"none watches nothing", SuspectNone, both, []MovedEnd{}},
		{"an unmoved end holds", SuspectTarget, Revisions{SourceReviewed: 1, SourceCurrent: 9, TargetReviewed: 4, TargetCurrent: 4}, []MovedEnd{}},
		{"an end in the trash waits", SuspectBoth, Revisions{SourceReviewed: 1, SourceCurrent: 2, TargetReviewed: 1, TargetCurrent: 2, Trashed: true}, []MovedEnd{}},
	} {
		if got := Moved(c.watch, c.r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Moved = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestANamedRevisionIsTheCurrentOne(t *testing.T) {
	if err := CheckNamedRevision("sourceRevision", "CHK-R1", 3, 3); err != nil {
		t.Errorf("the current revision: %v", err)
	}
	if err := CheckNamedRevision("sourceRevision", "CHK-R1", 2, 3); !errors.Is(err, ErrRevisionMovedOn) {
		t.Errorf("an older revision = %v, want ErrRevisionMovedOn", err)
	}
	for _, named := range []int{0, 4} {
		if err := CheckNamedRevision("sourceRevision", "CHK-R1", named, 3); fieldOf(err) != "sourceRevision" {
			t.Errorf("revision %d = %v, want a field error on sourceRevision", named, err)
		}
	}
}

func TestAClearanceSaysWhy(t *testing.T) {
	if got, err := CheckClearComment("  Still holds.  "); err != nil || got != "Still holds." {
		t.Errorf("CheckClearComment = %q, %v", got, err)
	}
	for _, comment := range []string{"", "   ", strings.Repeat("ä", MaxClearCommentLength+1)} {
		if _, err := CheckClearComment(comment); fieldOf(err) != "comment" {
			t.Errorf("CheckClearComment(%d characters) = %v, want a field error on comment", len([]rune(comment)), err)
		}
	}
	if _, err := CheckClearComment(strings.Repeat("ä", MaxClearCommentLength)); err != nil {
		t.Errorf("the longest comment: %v", err)
	}
}
