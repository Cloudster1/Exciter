package testresult

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// MaxCaseNameLength bounds a test case's suite and name, as the table keeps them.
const MaxCaseNameLength = 1000

// DefaultVerifyWords are the words a test case names the requirements it verifies with, until the project's tag words
// say others (#201).
var DefaultVerifyWords = []string{"verifies"}

var (
	// ErrNotJUnit is a file that is not a JUnit XML report.
	ErrNotJUnit = errors.New("not a JUnit XML file")
	// ErrTooManyCases is an upload holding more than MaxRunCases test cases.
	ErrTooManyCases = errors.New("too many test cases")
)

// Case is one test case as a report wrote it, with the keys of the requirements it names.
type Case struct {
	Suite      string   `json:"suite"`
	Name       string   `json:"name"`
	Outcome    Outcome  `json:"outcome"`
	Message    string   `json:"message"`
	DurationMs int      `json:"durationMs"`
	Keys       []string `json:"keys"`
}

type xmlProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
	Text  string `xml:",chardata"`
}

type xmlOutcome struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type xmlCase struct {
	Classname  string        `xml:"classname,attr"`
	Name       string        `xml:"name,attr"`
	Time       string        `xml:"time,attr"`
	Properties []xmlProperty `xml:"properties>property"`
	Failure    *xmlOutcome   `xml:"failure"`
	Error      *xmlOutcome   `xml:"error"`
	Skipped    *xmlOutcome   `xml:"skipped"`
}

type xmlSuite struct {
	XMLName xml.Name
	Name    string     `xml:"name,attr"`
	Suites  []xmlSuite `xml:"testsuite"`
	Cases   []xmlCase  `xml:"testcase"`
}

// ReadJUnit reads one JUnit XML report, testsuites or a lone testsuite, nested suites included; words are the
// words a case names the requirements it verifies with.
func ReadJUnit(data []byte, words []string) ([]Case, error) {
	var root xmlSuite
	dec := xml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		return nil, ErrNotJUnit
	}
	if root.XMLName.Local != "testsuites" && root.XMLName.Local != "testsuite" {
		return nil, ErrNotJUnit
	}
	k := newKeyReader(words)
	var out []Case
	var walk func(s xmlSuite, suite string) error
	walk = func(s xmlSuite, suite string) error {
		if s.XMLName.Local == "testsuite" && strings.TrimSpace(s.Name) != "" {
			suite = strings.TrimSpace(s.Name)
		}
		for _, c := range s.Cases {
			if len(out) == MaxRunCases {
				return ErrTooManyCases
			}
			out = append(out, k.caseOf(c, suite))
		}
		for _, inner := range s.Suites {
			if err := walk(inner, suite); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// keyReader finds the keys a case names, in its name, its properties and its tags.
type keyReader struct {
	words   []string
	inBrace *regexp.Regexp
	tagged  *regexp.Regexp
}

var keySplit = regexp.MustCompile(`[\s,;]+`)

func newKeyReader(words []string) keyReader {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, regexp.QuoteMeta(w))
	}
	alt := strings.Join(quoted, "|")
	return keyReader{
		words:   words,
		inBrace: regexp.MustCompile(`(?i)\[\s*(?:` + alt + `)\s+([^\]]+)\]`),
		tagged:  regexp.MustCompile(`(?i)^\s*\[?\s*(?:` + alt + `)\s*[:\s]\s*([^\]]+)\]?\s*$`),
	}
}

func (k keyReader) caseOf(c xmlCase, suite string) Case {
	out := Case{Suite: cut(firstOf(strings.TrimSpace(c.Classname), suite), MaxCaseNameLength),
		Name: cut(strings.TrimSpace(c.Name), MaxCaseNameLength), Outcome: Passed, DurationMs: millis(c.Time)}
	switch {
	case c.Error != nil:
		out.Outcome, out.Message = Errored, message(c.Error)
	case c.Failure != nil:
		out.Outcome, out.Message = Failed, message(c.Failure)
	case c.Skipped != nil:
		out.Outcome, out.Message = Skipped, message(c.Skipped)
	}
	var keys []string
	for _, m := range k.inBrace.FindAllStringSubmatch(c.Name, -1) {
		keys = append(keys, keysIn(m[1])...)
	}
	for _, p := range c.Properties {
		name, value := strings.ToLower(strings.TrimSpace(p.Name)), strings.TrimSpace(firstOf(p.Value, p.Text))
		switch {
		case name == "requirement" || name == "requirements" || slices.ContainsFunc(k.words, func(w string) bool { return strings.EqualFold(w, name) }):
			keys = append(keys, keysIn(value)...)
		case name == "tag" || name == "tags":
			if m := k.tagged.FindStringSubmatch(value); m != nil {
				keys = append(keys, keysIn(m[1])...)
			}
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if len(keys) > MaxKeysPerCase {
		keys = keys[:MaxKeysPerCase]
	}
	out.Keys = keys
	if out.Name == "" {
		out.Name = out.Suite
	}
	return out
}

// keysIn reads the requirement keys of a list, written in any case, each as its canonical key.
func keysIn(list string) []string {
	var out []string
	for _, f := range keySplit.Split(strings.TrimSpace(list), -1) {
		if project, n, ok := requirement.ParseKey(f); ok {
			out = append(out, requirement.Key(project, n))
		}
	}
	return out
}

func message(o *xmlOutcome) string {
	return cut(strings.TrimSpace(firstOf(strings.TrimSpace(o.Message), o.Text)), MaxMessageChars)
}

// millis reads a report's seconds, which some runners write with thousands separators.
func millis(seconds string) int {
	f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(seconds), ",", ""), 64)
	if err != nil || f < 0 {
		return 0
	}
	return int(f * 1000)
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// cut keeps at most n characters, whole runes.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// fileError says which file was not a JUnit report, for the sentence the upload answers.
type fileError struct {
	name string
	err  error
}

func (e *fileError) Error() string { return fmt.Sprintf("%s: %v", e.name, e.err) }
func (e *fileError) Unwrap() error { return e.err }

// File is one uploaded report.
type File struct {
	Name string
	Data []byte
}

// ReadFiles reads every report of an upload, in order, into one list of cases.
func ReadFiles(files []File, words []string) ([]Case, error) {
	var out []Case
	for _, f := range files {
		cases, err := ReadJUnit(f.Data, words)
		if err != nil {
			return nil, &fileError{name: f.Name, err: err}
		}
		if len(out)+len(cases) > MaxRunCases {
			return nil, ErrTooManyCases
		}
		out = append(out, cases...)
	}
	return out, nil
}

// FileName is the name of the file a reading error is about, empty for none.
func FileName(err error) string {
	var fe *fileError
	if errors.As(err, &fe) {
		return fe.name
	}
	return ""
}
