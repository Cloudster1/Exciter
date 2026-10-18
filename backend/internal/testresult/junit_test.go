package testresult

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []Case {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := ReadJUnit(data, DefaultVerifyWords)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return cases
}

func byName(cases []Case, name string) Case {
	for _, c := range cases {
		if strings.HasPrefix(c.Name, name) {
			return c
		}
	}
	return Case{}
}

func TestTheReportsOfCommonRunnersAreReadWithTheirOutcomesAndKeys(t *testing.T) {
	type want struct {
		name, suite string
		outcome     Outcome
		keys        []string
		message     string
	}
	for file, wants := range map[string][]want{
		"go-junit-report.xml": {
			{"TestBrakesEngageWithin100ms", "example.com/checkout/brakes", Passed, []string{"CHK-R42"}, ""},
			{"TestBrakeLightsFollowThePedal", "example.com/checkout/brakes", Failed, []string{"CHK-R43", "CHK-R44"}, "Failed"},
			{"TestNothingInParticular", "example.com/checkout/brakes", Passed, nil, ""},
		},
		"pytest.xml": {
			{"test_engage", "tests.test_brakes", Passed, []string{"CHK-R42"}, ""},
			{"test_lights", "tests.test_brakes", Errored, []string{"CHK-R43", "CHK-R45"}, `failed on setup with "fixture 'pedal' not found"`},
			{"test_later", "tests.test_brakes", Skipped, []string{"CHK-R46"}, "not yet"},
		},
		"surefire.xml": {
			{"engagesWithin100ms", "com.acme.BrakesTest", Passed, []string{"CHK-R42", "CHK-R47"}, ""},
			{"lightsFollowThePedal", "com.acme.BrakesTest", Failed, nil, "expected: <true> but was: <false>"},
		},
		"jest-junit.xml": {
			{"brakes engage", "brakes engage [verifies CHK-R42]", Passed, []string{"CHK-R42"}, ""},
			{"brakes release", "brakes release", Passed, nil, ""},
		},
		"vitest.xml": {
			{"brakes > engage", "src/brakes.test.ts", Passed, []string{"CHK-R42"}, ""},
			{"brakes > lights", "src/brakes.test.ts", Failed, []string{"CHK-R43"}, "expected false to be true"},
		},
	} {
		cases := read(t, file)
		if len(cases) != len(wants) {
			t.Errorf("%s reads %d cases, want %d", file, len(cases), len(wants))
		}
		for _, w := range wants {
			c := byName(cases, w.name)
			if c.Name == "" {
				t.Errorf("%s has no case %q", file, w.name)
				continue
			}
			if c.Suite != w.suite || c.Outcome != w.outcome || !slices.Equal(c.Keys, w.keys) || c.Message != w.message {
				t.Errorf("%s %q reads %+v, want suite %q, %s, keys %v, message %q", file, w.name, c, w.suite, w.outcome, w.keys, w.message)
			}
		}
	}
}

func TestDurationsAreReadInMillisecondsWithSeparators(t *testing.T) {
	if got := byName(read(t, "go-junit-report.xml"), "TestBrakeLights").DurationMs; got != 6 {
		t.Errorf("0.006 s reads %d ms", got)
	}
	for in, want := range map[string]int{"1,234.5": 1234500, "": 0, "-1": 0, "abc": 0, "2": 2000} {
		if got := millis(in); got != want {
			t.Errorf("%q reads %d ms, want %d", in, got, want)
		}
	}
}

func TestKeysAreReadOnlyAfterAVerifyWordAndOnceEach(t *testing.T) {
	k := newKeyReader([]string{"verifies", "tests"})
	for name, want := range map[string][]string{
		"a [verifies CHK-R1]":                        {"CHK-R1"},
		"a [Tests chk-r2; CHK-R3] [verifies CHK-R1]": {"CHK-R1", "CHK-R2", "CHK-R3"},
		"a [verifies CHK-R1, CHK-R1]":                {"CHK-R1"},
		"a [see CHK-R1]":                             nil,
		"a CHK-R1":                                   nil,
		"a [verifies CHK-R0, nothing]":               nil,
	} {
		if got := k.caseOf(xmlCase{Name: name}, "s").Keys; !slices.Equal(got, want) {
			t.Errorf("%q names %v, want %v", name, got, want)
		}
	}
	tagged := k.caseOf(xmlCase{Name: "t", Properties: []xmlProperty{{Name: "tags", Value: "[tests CHK-R5]"}, {Name: "tag", Value: "smoke"}}}, "s")
	if !slices.Equal(tagged.Keys, []string{"CHK-R5"}) {
		t.Errorf("a tag names %v", tagged.Keys)
	}
}

func TestACaseNamesAtMostSoManyKeys(t *testing.T) {
	keys := make([]string, 0, MaxKeysPerCase+5)
	for i := range MaxKeysPerCase + 5 {
		keys = append(keys, "CHK-R"+strconv.Itoa(i+1))
	}
	c := newKeyReader(DefaultVerifyWords).caseOf(xmlCase{Name: "t [verifies " + strings.Join(keys, ",") + "]"}, "s")
	if len(c.Keys) != MaxKeysPerCase {
		t.Errorf("a case names %d keys, want %d", len(c.Keys), MaxKeysPerCase)
	}
}

func TestWhatIsNotAJUnitReportIsRefusedByName(t *testing.T) {
	for _, data := range []string{"", "not xml", "<html><body/></html>", `{"tests": []}`} {
		if _, err := ReadJUnit([]byte(data), DefaultVerifyWords); !errors.Is(err, ErrNotJUnit) {
			t.Errorf("%q reads as %v", data, err)
		}
	}
	_, err := ReadFiles([]File{{Name: "ok.xml", Data: []byte(`<testsuite name="a"><testcase name="b"/></testsuite>`)}, {Name: "page.html", Data: []byte("<html/>")}}, DefaultVerifyWords)
	if !errors.Is(err, ErrNotJUnit) || FileName(err) != "page.html" {
		t.Errorf("a stray file answers %v naming %q", err, FileName(err))
	}
}

func TestAnUploadHoldsAtMostSoManyCases(t *testing.T) {
	var b strings.Builder
	b.WriteString("<testsuite name=\"s\">")
	for range MaxRunCases + 1 {
		b.WriteString(`<testcase name="c"/>`)
	}
	b.WriteString("</testsuite>")
	if _, err := ReadJUnit([]byte(b.String()), DefaultVerifyWords); !errors.Is(err, ErrTooManyCases) {
		t.Errorf("one case too many reads as %v", err)
	}
	half := []byte(strings.Replace(b.String(), `<testcase name="c"/>`, "", MaxRunCases/2-1))
	if _, err := ReadFiles([]File{{Name: "a", Data: half}, {Name: "b", Data: half}}, DefaultVerifyWords); !errors.Is(err, ErrTooManyCases) {
		t.Errorf("two halves over the limit read as %v", err)
	}
}

func TestLongNamesAndMessagesAreCutAtWholeCharacters(t *testing.T) {
	long := strings.Repeat("ä", MaxCaseNameLength+10)
	c := newKeyReader(DefaultVerifyWords).caseOf(xmlCase{Name: long, Failure: &xmlOutcome{Text: strings.Repeat("ü", MaxMessageChars+3)}}, "s")
	if len([]rune(c.Name)) != MaxCaseNameLength || len([]rune(c.Message)) != MaxMessageChars {
		t.Errorf("cut to %d and %d characters", len([]rune(c.Name)), len([]rune(c.Message)))
	}
}
