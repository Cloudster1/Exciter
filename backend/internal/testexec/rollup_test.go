package testexec

import "testing"

func TestACaseComesToWhatItsStepsSay(t *testing.T) {
	for name, tc := range map[string]struct {
		recorded []Outcome
		steps    int
		want     string
	}{
		"nothing recorded":          {nil, 3, "-"},
		"a step open":               {[]Outcome{Passed, Passed}, 3, "-"},
		"all passed":                {[]Outcome{Passed, Passed, Passed}, 3, "passed"},
		"a failure while some open": {[]Outcome{Passed, Failed}, 3, "failed"},
		"blocked beats skipped":     {[]Outcome{Blocked, Skipped, Passed}, 3, "blocked"},
		"failed beats blocked":      {[]Outcome{Blocked, Failed, Passed}, 3, "failed"},
		"all skipped":               {[]Outcome{Skipped, Skipped}, 2, "skipped"},
		"passed with some skipped":  {[]Outcome{Skipped, Passed}, 2, "passed"},
		"a case without steps":      {nil, 0, "-"},
	} {
		got := "-"
		if o := Rollup(tc.recorded, tc.steps); o != nil {
			got = string(*o)
		}
		if got != tc.want {
			t.Errorf("%s: %s, want %s", name, got, tc.want)
		}
	}
}
