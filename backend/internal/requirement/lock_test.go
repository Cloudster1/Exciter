package requirement

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/label"
)

// checkLabels counts a label by its normal form, so two spellings of one label are one of the fifty (#309).
func TestCheckLabelsCountsEachLabelOnce(t *testing.T) {
	got, err := checkLabels([]string{"Release 2", "release-2", "  RELEASE   2 "})
	if err != nil || len(got) != 1 || got[0] != "release-2" {
		t.Fatalf("three spellings of one label read as %v (%v)", got, err)
	}

	names := []string{"Release 2", "release-2"}
	for i := range label.MaxPerRequirement - 1 {
		names = append(names, "label-"+strconv.Itoa(i))
	}
	if got, err := checkLabels(names); err != nil || len(got) != label.MaxPerRequirement {
		t.Errorf("%d distinct labels read as %d (%v)", label.MaxPerRequirement, len(got), err)
	}

	var field *FieldError
	if _, err := checkLabels(append(names, "one-more")); !errors.As(err, &field) || field.Field != "labels" {
		t.Errorf("a label past %d is refused as %v", label.MaxPerRequirement, err)
	}
	var bad *label.FieldError
	if _, err := checkLabels([]string{"a/b"}); !errors.As(err, &bad) || bad.Field != "labels" {
		t.Errorf("a label that is no word is refused as %v", err)
	}
}

// The database names what it refused a lock for by the constraint, and the service turns each into its own error
// and the database's sentence (#309).
func TestLockRefusalsReadTheDatabasesConstraint(t *testing.T) {
	refused := func(constraint, message string) error {
		return fmt.Errorf("lock: %w", &pgconn.PgError{Code: "23514", ConstraintName: constraint, Message: message})
	}
	if err := lockRefusal(refused("requirement_lock_forbidden", "only an administrator")); !errors.Is(err, ErrNotLockKeeper) {
		t.Errorf("requirement_lock_forbidden reads as %v", err)
	}
	if err := lockRefusal(refused("requirement_lock_section", "a section")); !errors.Is(err, ErrSectionNotLocked) {
		t.Errorf("requirement_lock_section reads as %v", err)
	}
	other := refused("requirement_lock_whole", "too long")
	if err := lockRefusal(other); err != other {
		t.Errorf("a refusal of another constraint reads as %v", err)
	}

	const sentence = `CHK-R1 is locked by Ada: "Released in 2.0". Ask a project administrator to unlock it.`
	if got, ok := LockedSentence(refused("requirement_locked", sentence)); !ok || got != sentence {
		t.Errorf("requirement_locked reads as %q, %v", got, ok)
	}
	if got, ok := LockedSentence(refused("requirement_lock_section", "a section")); ok || got != "" {
		t.Errorf("another constraint reads as the locked sentence %q", got)
	}
	if _, ok := LockedSentence(errors.New("down")); ok {
		t.Error("an error that is not the database's reads as locked")
	}
}
