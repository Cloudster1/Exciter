package rql

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func sprint(v any) string { return fmt.Sprint(v) }

func TestParseNormalizesWhatWasTyped(t *testing.T) {
	cases := []struct{ in, want string }{
		{`owner = currentUser() and not status in ("In review", Done) order by priority desc, key`,
			`(owner = currentUser() AND NOT status IN ("In review", Done)) ORDER BY priority DESC, key`},
		{`a = 1 OR b = 2 AND c = 3`, `(a = 1 OR (b = 2 AND c = 3))`},
		{`(a = 1 OR b = 2) AND c = 3`, `((a = 1 OR b = 2) AND c = 3)`},
		{`NOT NOT a = 1`, `NOT NOT a = 1`},
		{`labels IS NOT EMPTY`, `labels IS NOT EMPTY`},
		{`owner = EMPTY`, `owner IS EMPTY`},
		{`owner != empty`, `owner IS NOT EMPTY`},
		{`"Risk" ~ 'Glo\'bex'`, `"Risk" ~ "Glo'bex"`},
		{`created >= startOfWeek(-1w)`, `created >= startOfWeek(-1w)`},
		{`updated >= -7d AND revision <= 3`, `(updated >= -7d AND revision <= 3)`},
		{`created = 2026-09-04T10:30`, `created = 2026-09-04T10:30`},
		{`owner = ada@exciter.test`, `owner = ada@exciter.test`},
		{`key NOT IN (CHK-R1, CHK-R2)`, `key NOT IN (CHK-R1, CHK-R2)`},
		{`ORDER BY created`, `ORDER BY created`},
		{``, ``},
		{`   `, ``},
	}
	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got := q.String(); got != c.want {
			t.Errorf("%q\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestParseSaysWhereItWentWrong(t *testing.T) {
	cases := []struct {
		in   string
		pos  int
		want string
	}{
		{`status =`, 9, "ends where a value was expected"},
		{`status`, 7, "operator was expected after status"},
		{`status = "open`, 10, "quote opened at character 10 is never closed"},
		{`(status = x`, 1, "bracket opened at character 1 is never closed"},
		{`status = x y = 2`, 12, `"y" was not expected here`},
		{`status ! x`, 8, `"!" on its own means nothing`},
		{`status = 2026-13`, 10, "not a number, a date or a duration"},
		{`ORDER status`, 7, "ORDER is followed by BY"},
		{`status IN x`, 11, "IN is followed by a list"},
		{`status IN (a, b`, 16, "missing its closing bracket"},
		{`status IS x`, 11, "IS is followed by EMPTY"},
		{`= x`, 1, `A field name was expected, but "=" was found`},
		{`status = AND`, 10, `A value was expected before "AND"`},
		{`status # x`, 8, `"#" cannot be used here`},
		{`created > startOfWeek(x)`, 23, "takes at most a duration"},
		{`status NOT x`, 12, "NOT after a field is followed by IN"},
		{`title ~ "Ä" ä`, 13, `"ä" was not expected here`},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%q: expected an Error, got %v", c.in, err)
		}
		if e.Pos != c.pos || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%q: got %d %q, want %d containing %q", c.in, e.Pos, e.Msg, c.pos, c.want)
		}
	}
}

// Armature's language takes any query; one that a stranger may type into an api is bounded.
func TestParseRefusesWhatIsTooBig(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{strings.Repeat("a", MaxLength+1), "Keep the query to 2000 characters"},
		{strings.Repeat("NOT ", MaxDepth+1) + "a = 1", "nest at most 32 deep"},
		{strings.Repeat("(", MaxDepth+1) + "a = 1" + strings.Repeat(")", MaxDepth+1), "nest at most 32 deep"},
		{"a = 1" + strings.Repeat(" OR a = 1", MaxTerms), "at most 100 conditions"},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		var e *Error
		if !errors.As(err, &e) || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%.40q: got %v, want %q", c.in, err, c.want)
		}
	}
	if _, err := Parse(strings.Repeat("NOT ", MaxDepth-1) + "a = 1"); err != nil {
		t.Errorf("a query just inside the depth: %v", err)
	}
}
