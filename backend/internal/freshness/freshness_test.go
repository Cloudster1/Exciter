package freshness

import (
	"context"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
)

func TestMemoryTrackerRoundTrip(t *testing.T) {
	tr := NewMemoryTracker(time.Minute)
	ctx := context.Background()

	if got := tr.Required(ctx, "s:1"); got != 0 {
		t.Errorf("a key that never wrote requires %v, want 0", got)
	}
	tr.Note(ctx, "s:1", 500)
	if got := tr.Required(ctx, "s:1"); got != 500 {
		t.Errorf("Required = %v, want 500", got)
	}
	if got := tr.Required(ctx, "s:2"); got != 0 {
		t.Errorf("one key's write leaked into another: got %v", got)
	}
}

func TestMemoryTrackerKeepsTheNewerPosition(t *testing.T) {
	tr := NewMemoryTracker(time.Minute)
	ctx := context.Background()

	tr.Note(ctx, "s", 900)
	// A slower request finishing later must not walk the requirement back.
	tr.Note(ctx, "s", 100)
	if got := tr.Required(ctx, "s"); got != 900 {
		t.Errorf("Required = %v, want the newer position 900", got)
	}
}

func TestMemoryTrackerExpires(t *testing.T) {
	tr := NewMemoryTracker(30 * time.Second)
	ctx := context.Background()
	clock := time.Now()
	tr.now = func() time.Time { return clock }

	tr.Note(ctx, "s", 42)
	clock = clock.Add(20 * time.Second)
	tr.Note(ctx, "s", 10)
	clock = clock.Add(20 * time.Second)
	if got := tr.Required(ctx, "s"); got != 42 {
		t.Fatalf("a later write, even an older position, extends the entry: Required = %v, want 42", got)
	}
	clock = clock.Add(31 * time.Second)
	if got := tr.Required(ctx, "s"); got != 0 {
		t.Errorf("Required = %v after expiry, want 0", got)
	}
}

func TestMemoryTrackerIgnoresEmptyInput(t *testing.T) {
	tr := NewMemoryTracker(time.Minute)
	ctx := context.Background()
	tr.Note(ctx, "", 100)
	tr.Note(ctx, "s", 0)
	if got := tr.Required(ctx, ""); got != 0 {
		t.Errorf("Required(\"\") = %v, want 0", got)
	}
	if got := tr.Required(ctx, "s"); got != 0 {
		t.Errorf("a zero position should not be recorded, got %v", got)
	}
}

// The script compares positions as strings, which only orders them if every
// one has the same width.
func TestEncodingOrdersLikeTheNumbers(t *testing.T) {
	pairs := [][2]db.LSN{{1, 2}, {0xF, 0x10}, {0xFFFFFFFF, 0x100000000}, {1 << 53, 1<<53 + 1}}
	for _, p := range pairs {
		if !(encode(p[0]) < encode(p[1])) {
			t.Errorf("%s does not sort before %s", encode(p[0]), encode(p[1]))
		}
	}
}

var (
	_ Tracker = (*MemoryTracker)(nil)
	_ Tracker = (*ValkeyTracker)(nil)
)
