package search

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSplitMarksMatchesAsPlainRuns(t *testing.T) {
	cases := []struct {
		name   string
		marked string
		want   []Segment
	}{
		{"nothing matched", "just words", []Segment{{"just words", false}}},
		{"one match", "the " + startSel + "plan" + stopSel + " for", []Segment{{"the ", false}, {"plan", true}, {" for", false}}},
		{"a phrase is one match", startSel + "quick" + stopSel + " " + startSel + "brown" + stopSel + " fox", []Segment{{"quick brown", true}, {" fox", false}}},
		{"words apart stay apart", startSel + "a" + stopSel + " b " + startSel + "c" + stopSel, []Segment{{"a", true}, {" b ", false}, {"c", true}}},
		{"whitespace collapses", "  line one\n\n\tline " + startSel + "two" + stopSel + "  ", []Segment{{"line one line ", false}, {"two", true}}},
		{"stray delimiters drop", stopSel + "x" + startSel + startSel + "y" + stopSel + stopSel, []Segment{{"x", false}, {"y", true}}},
		{"markup stays text", openAngle + "b>" + startSel + "&amp;" + stopSel, []Segment{{"<b>", false}, {"&amp;", true}}},
		{"empty", "", []Segment{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Split(c.marked); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Split(%q) = %#v, want %#v", c.marked, got, c.want)
			}
		})
	}
}

func TestPlainNeverMatches(t *testing.T) {
	got := Plain("a " + startSel + "b" + stopSel)
	if !reflect.DeepEqual(got, []Segment{{"a b", false}}) {
		t.Fatalf("Plain = %#v", got)
	}
	if got := firstWords("one  two\nthree four", 3); got != "one two three" {
		t.Fatalf("firstWords = %q", got)
	}
}

func TestParseQueryReadsEveryFilter(t *testing.T) {
	v := url.Values{
		"q":             {"  pay \"whole basket\" -refund "},
		"project":       {"chk", " Ops ", ""},
		"owner":         {"0199a5e0-0000-7000-8000-000000000001"},
		"label":         {"Safety Critical"},
		"category":      {"approved", "done"},
		"updatedAfter":  {"2026-09-01"},
		"updatedBefore": {"2026-10-01"},
	}
	q, err := ParseQuery(v)
	if err != nil {
		t.Fatal(err)
	}
	if q.Text != `pay "whole basket" -refund` || !reflect.DeepEqual(q.Projects, []string{"CHK", "OPS"}) ||
		len(q.Owners) != 1 || !reflect.DeepEqual(q.Labels, []string{"safety-critical"}) ||
		!reflect.DeepEqual(q.Categories, []string{"approved", "done"}) || q.ByUpdate {
		t.Fatalf("parsed %+v", q)
	}
	if !q.After.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !q.Before.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days %v %v", q.After, q.Before)
	}
}

func TestParseQueryOrders(t *testing.T) {
	for _, c := range []struct {
		values   url.Values
		byUpdate bool
	}{
		{url.Values{"q": {"x"}}, false},
		{url.Values{}, true},
		{url.Values{"q": {"x"}, "sort": {"updated"}}, true},
		{url.Values{"sort": {"relevance"}}, true},
	} {
		q, err := ParseQuery(c.values)
		if err != nil || q.ByUpdate != c.byUpdate {
			t.Errorf("%v: by update %v (%v), want %v", c.values, q.ByUpdate, err, c.byUpdate)
		}
	}
}

func TestParseQueryRefusesBadInput(t *testing.T) {
	for field, v := range map[string]url.Values{
		"q":             {"q": {strings.Repeat("x", MaxQueryLength+1)}},
		"owner":         {"owner": {"alice"}},
		"category":      {"category": {"finished"}},
		"updatedAfter":  {"updatedAfter": {"2026-13-01"}},
		"updatedBefore": {"updatedBefore": {"yesterday"}},
		"sort":          {"sort": {"title"}},
	} {
		_, err := ParseQuery(v)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field || !strings.HasSuffix(fe.Message, ".") {
			t.Errorf("%v: %v, want a sentence on %s", v, err, field)
		}
	}
	if _, err := ParseQuery(url.Values{"q": {strings.Repeat("é", MaxQueryLength)}}); err != nil {
		t.Errorf("200 characters are refused: %v", err)
	}
}

func TestPrefixQueryKeepsOnlyWords(t *testing.T) {
	for typed, want := range map[string]string{
		"":                     "",
		"  !!  ":               "",
		"Run":                  "'run':*A",
		"run bo":               "'run':*A & 'bo':*A",
		"it's a 'b' | c & !d:": "'it':*A & 's':*A & 'a':*A & 'b':*A & 'c':*A & 'd':*A",
		"Crème-brûlée":         "'crème':*A & 'brûlée':*A",
	} {
		if got := PrefixQuery(typed); got != want {
			t.Errorf("PrefixQuery(%q) = %q, want %q", typed, got, want)
		}
	}
	if got := strings.Count(PrefixQuery(strings.Repeat("w ", 40)), "&"); got != maxQuickWords-1 {
		t.Errorf("%d words kept", got+1)
	}
}

func TestKeyPrefixReadsWhatStartsLikeAKey(t *testing.T) {
	for typed, want := range map[string][2]string{
		"chk-r4":  {"CHK", "4"},
		" CHK-R ": {"CHK", ""},
		"CHK-":    {"CHK", ""},
		"chk-42":  {"CHK", "42"},
	} {
		project, digits, ok := KeyPrefix(typed)
		if !ok || project != want[0] || digits != want[1] {
			t.Errorf("KeyPrefix(%q) = %q %q %v, want %v", typed, project, digits, ok, want)
		}
	}
	for _, typed := range []string{"pay", "chk", "c-r1", "chk-rx", "chk r4"} {
		if _, _, ok := KeyPrefix(typed); ok {
			t.Errorf("%q started like a key", typed)
		}
	}
}
