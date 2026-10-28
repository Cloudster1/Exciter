package risk

import (
	"testing"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func inEffect(yes bool) Mitigation { return Mitigation{InEffect: yes} }

func TestARisksStateFollowsItsMitigations(t *testing.T) {
	for _, each := range []struct {
		what string
		ms   []Mitigation
		want requirement.RiskState
	}{
		{"nothing mitigates it", nil, requirement.Unmitigated},
		{"one is not in effect", []Mitigation{inEffect(true), inEffect(false)}, requirement.Claimed},
		{"none is in effect", []Mitigation{inEffect(false)}, requirement.Claimed},
		{"all are in effect", []Mitigation{inEffect(true), inEffect(true)}, requirement.Mitigated},
	} {
		if got := stateOf(each.ms); got != each.want {
			t.Errorf("%s: %s, want %s", each.what, got, each.want)
		}
	}
}

// matrixScheme is the five by five scheme as it reads once saved.
func matrixScheme(t *testing.T) *Scheme {
	t.Helper()
	in, err := CheckScheme(fiveByFive())
	if err != nil {
		t.Fatal(err)
	}
	return &Scheme{Method: in.Method, Levels: in.Levels, Matrix: in.Matrix}
}

func TestAViewGivesTheLevelOfTheSideInEffect(t *testing.T) {
	scheme := matrixScheme(t)
	high, low := requirement.Side{Severity: n(5), Probability: n(4)}, requirement.Side{Severity: n(1), Probability: n(1)}
	name := func(l *requirement.RiskLevel) string {
		if l == nil {
			return ""
		}
		return l.Name
	}
	for _, each := range []struct {
		what   string
		scheme *Scheme
		rating requirement.Rating
		ms     []Mitigation
		level  string
		state  requirement.RiskState
	}{
		{"no scheme gives no level", nil, requirement.Rating{Before: high, After: low}, nil, "", requirement.Unmitigated},
		{"unmitigated counts before", scheme, requirement.Rating{Before: high, After: low}, nil, "High", requirement.Unmitigated},
		{"claimed keeps before", scheme, requirement.Rating{Before: high, After: low}, []Mitigation{inEffect(false)}, "High", requirement.Claimed},
		{"mitigated counts after", scheme, requirement.Rating{Before: high, After: low}, []Mitigation{inEffect(true)}, "Low", requirement.Mitigated},
		{"mitigated without an after level falls back to before", scheme, requirement.Rating{Before: high, After: requirement.Side{Severity: n(1)}},
			[]Mitigation{inEffect(true)}, "High", requirement.Mitigated},
	} {
		v := viewOf(each.scheme, each.rating, each.ms)
		if name(v.Level) != each.level || v.State == nil || *v.State != each.state {
			t.Errorf("%s: level %q state %v, want %q %s", each.what, name(v.Level), v.State, each.level, each.state)
		}
		if each.scheme == nil && (v.BeforeLevel != nil || v.BeforeRPN != nil || v.AfterLevel != nil) {
			t.Errorf("%s: the view reads %+v", each.what, v)
		}
		if each.scheme != nil && (v.BeforeRPN == nil || *v.BeforeRPN != 20 || name(v.BeforeLevel) != "High") {
			t.Errorf("%s: before reads %v at %q", each.what, v.BeforeRPN, name(v.BeforeLevel))
		}
	}
}

func TestARiskCountsOnTheSideItStandsOn(t *testing.T) {
	scheme := matrixScheme(t)
	before, after := requirement.Side{Severity: n(4), Probability: n(3)}, requirement.Side{Severity: n(2), Probability: n(3)}
	unrated := requirement.Side{}
	for _, each := range []struct {
		what              string
		rating            requirement.Rating
		ms                []Mitigation
		effective, onSide requirement.Side
	}{
		{"unmitigated", requirement.Rating{Before: before, After: after}, nil, before, before},
		{"claimed", requirement.Rating{Before: before, After: after}, []Mitigation{inEffect(false)}, before, before},
		{"mitigated", requirement.Rating{Before: before, After: after}, []Mitigation{inEffect(true)}, after, before},
		{"mitigated with no after", requirement.Rating{Before: before, After: unrated}, []Mitigation{inEffect(true)}, before, before},
	} {
		r := risky{view: viewOf(scheme, each.rating, each.ms)}
		if got := r.effective(); !sameSide(got, each.effective) {
			t.Errorf("%s: in effect %+v, want %+v", each.what, got, each.effective)
		}
		if got := r.on(SideEffective); !sameSide(got, each.effective) {
			t.Errorf("%s: on the effective side %+v", each.what, got)
		}
		if got := r.on(SideBefore); !sameSide(got, each.onSide) {
			t.Errorf("%s: before mitigation %+v", each.what, got)
		}
	}
	if rated(unrated) || rated(requirement.Side{Severity: n(1)}) || !rated(before) {
		t.Error("a cell needs both severity and probability")
	}
}

func sameSide(a, b requirement.Side) bool {
	eq := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return eq(a.Severity, b.Severity) && eq(a.Probability, b.Probability) && eq(a.Detection, b.Detection)
}
