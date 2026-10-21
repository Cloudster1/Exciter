package oauth

import (
	"testing"
	"time"
)

func TestAnExchangeOnlyNarrowsTheScopeHeld(t *testing.T) {
	everything, reading, evidence := scope{}, scope{readOnly: true}, scope{evidence: true}
	for _, c := range []struct {
		asked string
		held  scope
		want  scope
		ok    bool
	}{
		{"", everything, everything, true},
		{"", reading, reading, true},
		{"mcp", everything, everything, true},
		{"mcp:read", everything, reading, true},
		{"mcp:read", reading, reading, true},
		{"mcp", reading, scope{}, false},
		{"evidence", everything, scope{}, false},
		{"evidence", evidence, evidence, true},
		{"mcp:read", evidence, scope{}, false},
		{"admin", everything, scope{}, false},
	} {
		if got, ok := narrowScope(c.asked, c.held); got != c.want || ok != c.ok {
			t.Errorf("%q of %+v narrows to %+v %v, want %+v %v", c.asked, c.held, got, ok, c.want, c.ok)
		}
	}
}

func TestANarrowerTokenEndsNoLaterThanItsSubject(t *testing.T) {
	left := 40 * time.Minute
	for _, c := range []struct {
		asked string
		want  time.Duration
		ok    bool
	}{
		{"", left, true},
		{"900", 15 * time.Minute, true},
		{"3600", left, true},
		{"1", time.Second, true},
		{"0", 0, false},
		{"3601", 0, false},
		{"-5", 0, false},
		{"15m", 0, false},
	} {
		if got, ok := narrowLifetime(c.asked, left); got != c.want || ok != c.ok {
			t.Errorf("%q with %v left lasts %v %v, want %v %v", c.asked, left, got, ok, c.want, c.ok)
		}
	}
}
