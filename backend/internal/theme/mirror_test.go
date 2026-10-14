package theme

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAMirrorIsTheSameThemeAtTheSameChange(t *testing.T) {
	id := uuid.New()
	at := time.Date(2026, 10, 1, 9, 0, 0, 123456789, time.UTC)
	stored := &MirrorOf{ID: id, UpdatedAt: at.Truncate(time.Microsecond)}
	for _, tc := range []struct {
		name  string
		other *MirrorOf
		same  bool
	}{
		{"Armature's answer with nanoseconds the database dropped", &MirrorOf{ID: id, UpdatedAt: at}, true},
		{"a later change", &MirrorOf{ID: id, UpdatedAt: at.Add(time.Millisecond)}, false},
		{"another theme", &MirrorOf{ID: uuid.New(), UpdatedAt: at}, false},
		{"Armature's built-in theme", nil, false},
	} {
		if got := stored.Same(tc.other); got != tc.same {
			t.Errorf("%s: Same = %v, want %v", tc.name, got, tc.same)
		}
	}
	if (*MirrorOf)(nil).Same(stored) {
		t.Error("no mirror is the same as a theme")
	}
}
