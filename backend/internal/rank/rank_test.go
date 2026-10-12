package rank

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func TestBetweenProducesSomethingInTheMiddle(t *testing.T) {
	tests := []struct{ a, b string }{
		{"", ""},
		{"", "V"},
		{"V", ""},
		{"A", "B"},
		{"A", "z"},
		{"0V", "0W"},
		{"V", "VV"},
		{"aaa", "aab"},
	}
	for _, tt := range tests {
		got, err := Between(tt.a, tt.b)
		if err != nil {
			t.Errorf("Between(%q, %q): %v", tt.a, tt.b, err)
			continue
		}
		if tt.a != "" && got <= tt.a {
			t.Errorf("Between(%q, %q) = %q, which does not sort after %q", tt.a, tt.b, got, tt.a)
		}
		if tt.b != "" && got >= tt.b {
			t.Errorf("Between(%q, %q) = %q, which does not sort before %q", tt.a, tt.b, got, tt.b)
		}
	}
}

func TestBetweenRejectsBoundsInTheWrongOrder(t *testing.T) {
	for _, tt := range []struct{ a, b string }{
		{"B", "A"},
		{"V", "V"},
		{"zz", "za"},
	} {
		if got, err := Between(tt.a, tt.b); err == nil {
			t.Errorf("Between(%q, %q) = %q, want an error", tt.a, tt.b, got)
		}
	}
}

func TestBetweenRejectsInvalidRanks(t *testing.T) {
	for _, bad := range []string{"!", "A-B", "V ", "A0"} {
		if _, err := Between(bad, ""); err == nil {
			t.Errorf("Between(%q, \"\") accepted an invalid rank", bad)
		}
	}
}

func TestSequenceIsAscending(t *testing.T) {
	seq, err := Sequence(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(seq) != 50 {
		t.Fatalf("got %d ranks, want 50", len(seq))
	}
	if !slices.IsSorted(seq) {
		t.Errorf("Sequence is not in ascending order: %v", seq[:10])
	}
	// Every rank must be distinct, or two cards share a slot.
	seen := map[string]bool{}
	for _, r := range seq {
		if seen[r] {
			t.Fatalf("Sequence repeated %q", r)
		}
		seen[r] = true
	}
}

// Repeatedly inserting into the same gap is the adversarial case: each insert
// has to find room inside an ever-shrinking interval. The ranks must stay
// correctly ordered and must not grow without bound.
func TestRepeatedInsertionIntoTheSameGap(t *testing.T) {
	lo, hi := "A", "B"
	longest := 0

	for i := range 200 {
		mid, err := Between(lo, hi)
		if err != nil {
			t.Fatalf("insert %d between %q and %q: %v", i, lo, hi, err)
		}
		if mid <= lo || mid >= hi {
			t.Fatalf("insert %d produced %q, which is not strictly between %q and %q", i, mid, lo, hi)
		}
		longest = max(longest, len(mid))
		// Always squeeze into the lower half, the worst case for growth.
		hi = mid
	}

	// Each insert can add at most one character, and in practice far fewer.
	if longest > 210 {
		t.Errorf("ranks grew to %d characters over 200 inserts", longest)
	}
	t.Logf("longest rank after 200 adversarial inserts: %d characters", longest)
}

// A board is reordered by dragging cards around at random. However the list is
// shuffled, reading the ranks back in sorted order must reproduce the intended
// order exactly.
// card is a board card reduced to what ordering needs.
type card struct {
	name string
	rank string
}

func TestRandomReorderingKeepsTheIntendedOrder(t *testing.T) {
	seq, err := Sequence(12)
	if err != nil {
		t.Fatal(err)
	}
	cards := make([]card, len(seq))
	for i, r := range seq {
		cards[i] = card{name: string(rune('a' + i)), rank: r}
	}

	random := rand.New(rand.NewPCG(1, 2))
	for move := range 300 {
		// Take a card out and drop it at another position.
		from := random.IntN(len(cards))
		moved := cards[from]
		rest := slices.Delete(slices.Clone(cards), from, from+1)
		to := random.IntN(len(rest) + 1)

		var before, after string
		if to > 0 {
			before = rest[to-1].rank
		}
		if to < len(rest) {
			after = rest[to].rank
		}

		newRank, err := Between(before, after)
		if err != nil {
			t.Fatalf("move %d: Between(%q, %q): %v", move, before, after, err)
		}
		moved.rank = newRank

		cards = slices.Insert(rest, to, moved)

		// The order the ranks imply must match the order we just built.
		sorted := slices.Clone(cards)
		slices.SortFunc(sorted, func(x, y card) int { return strings.Compare(x.rank, y.rank) })
		for i := range cards {
			if sorted[i].name != cards[i].name {
				t.Fatalf("move %d: ranks imply %v but the list is %v",
					move, namesOf(sorted), namesOf(cards))
			}
		}
	}
}

func namesOf(cards []card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.name
	}
	return out
}

func TestInitialLeavesRoomOnBothSides(t *testing.T) {
	initial := Initial()

	before, err := Between("", initial)
	if err != nil {
		t.Fatalf("cannot insert before the initial rank: %v", err)
	}
	after, err := Between(initial, "")
	if err != nil {
		t.Fatalf("cannot insert after the initial rank: %v", err)
	}
	if !(before < initial && initial < after) {
		t.Errorf("order is %q, %q, %q; want ascending", before, initial, after)
	}
}

func TestSequentialIsFixedWidthAndOrdered(t *testing.T) {
	var previous string
	for _, n := range []int64{1, 2, 9, 10, 15, 16, 255, 256, 1_000_000} {
		got := Sequential(n)
		if len(got) != 11 {
			t.Errorf("Sequential(%d) = %q, which is %d characters; they must all be the same width", n, got, len(got))
		}
		if previous != "" && got <= previous {
			t.Errorf("Sequential(%d) = %q does not sort after %q", n, got, previous)
		}
		previous = got
	}
}

// The two ways a rank is produced have to interleave: a card dragged between
// two sequentially ranked neighbours must land between them.
func TestSequentialAndBetweenInterleave(t *testing.T) {
	a, b := Sequential(10), Sequential(11)

	middle, err := Between(a, b)
	if err != nil {
		t.Fatalf("Between(%q, %q): %v", a, b, err)
	}
	if !(a < middle && middle < b) {
		t.Errorf("order is %q, %q, %q; want ascending", a, middle, b)
	}

	// And a card dragged to the very front lands before the first.
	front, err := Between("", Sequential(1))
	if err != nil {
		t.Fatal(err)
	}
	if front >= Sequential(1) {
		t.Errorf("%q does not sort before %q", front, Sequential(1))
	}
}

// A sequentially ranked list must not grow when cards are repeatedly dragged
// around it, which is the failure mode fixed width ranks exist to avoid.
func TestSequentialRanksDoNotGrowOverTime(t *testing.T) {
	const cards = 500
	longest := 0
	for n := range int64(cards) {
		longest = max(longest, len(Sequential(n+1)))
	}
	if longest != 11 {
		t.Errorf("after %d cards the longest rank is %d characters, want a constant 11", cards, longest)
	}
}

func TestSpreadFitsARunIntoOneGap(t *testing.T) {
	for _, tt := range []struct{ a, b string }{{"A", "B"}, {"", "V"}, {"V", ""}, {"", ""}} {
		got, err := Spread(tt.a, tt.b, 20)
		if err != nil {
			t.Fatalf("Spread(%q, %q): %v", tt.a, tt.b, err)
		}
		if len(got) != 20 || !slices.IsSorted(got) {
			t.Fatalf("Spread(%q, %q) = %v, want 20 ascending ranks", tt.a, tt.b, got)
		}
		if (tt.a != "" && got[0] <= tt.a) || (tt.b != "" && got[len(got)-1] >= tt.b) {
			t.Errorf("Spread(%q, %q) = %v strays outside its bounds", tt.a, tt.b, got)
		}
	}
	if _, err := Spread("B", "A", 1); err == nil {
		t.Error("Spread accepted bounds in the wrong order")
	}
}
