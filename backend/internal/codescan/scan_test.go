package codescan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func found(tags []Found) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		s := fmt.Sprintf("%d %s %s", t.Line, strings.ToLower(t.Word), t.Key)
		if t.Revision != nil {
			s += fmt.Sprintf("@%d", *t.Revision)
		}
		out = append(out, s)
	}
	return out
}

func TestTagsAreFoundInTheCommentsOfManyLanguages(t *testing.T) {
	s := NewScanner([]string{"implements", "verifies", "tests"}, []string{"CHK", "OLD"})
	for path, c := range map[string]struct {
		src  string
		want []string
	}{
		"brakes.go": {"package brakes\n\n// Engage closes the calipers. [implements CHK-R42@3]\nfunc Engage() {}\n/* [verifies CHK-R43]\n * [tests chk-r44, CHK-R45@1] */\n",
			[]string{"3 implements CHK-R42@3", "5 verifies CHK-R43", "6 tests CHK-R44", "6 tests CHK-R45@1"}},
		"brakes.ts":   {"const url = \"https://x\"; // [implements CHK-R1]\nexport const a = '[implements CHK-R2]';\n", []string{"1 implements CHK-R1"}},
		"brakes.py":   {"def engage():\n    \"\"\"Closes the calipers.\n\n    [implements CHK-R42]\n    \"\"\"\n    return 1  # [verifies CHK-R43]\n", []string{"4 implements CHK-R42", "6 verifies CHK-R43"}},
		"brakes.rb":   {"# [Implements CHK-R7]\nputs 1\n", []string{"1 implements CHK-R7"}},
		"brakes.sql":  {"-- [implements CHK-R8]\nSELECT 1; /* [verifies CHK-R9] */\n", []string{"1 implements CHK-R8", "2 verifies CHK-R9"}},
		"brakes.lisp": {"(defun engage () t) ; [implements CHK-R10]\n", []string{"1 implements CHK-R10"}},
		"brakes.tex":  {"% [verifies CHK-R11]\n\\section{Brakes}\n", []string{"1 verifies CHK-R11"}},
		"brakes.html": {"<p>[implements CHK-R12]</p>\n<!-- [implements CHK-R13]\n[verifies CHK-R14] -->\n", []string{"2 implements CHK-R13", "3 verifies CHK-R14"}},
		"Makefile":    {"# [implements CHK-R15]\nall:\n", []string{"1 implements CHK-R15"}},
		"notes.weird": {"; [implements OLD-R3]\n// [verifies CHK-R16]\n", []string{"1 implements OLD-R3", "2 verifies CHK-R16"}},
		"other.go":    {"// [implements PAY-R1] [see CHK-R2] [implements CHK-R0] implements CHK-R3\n", nil},
	} {
		if got := found(s.File(path, []byte(c.src))); !slices.Equal(got, c.want) {
			t.Errorf("%s reads %v, want %v", path, got, c.want)
		}
	}
}

func TestABinaryOrHugeFileHoldsNoTags(t *testing.T) {
	s := NewScanner([]string{"implements"}, []string{"CHK"})
	if got := s.File("image.go", []byte("// [implements CHK-R1]\x00\x01")); got != nil {
		t.Errorf("a binary file reads %v", got)
	}
	huge := []byte("// [implements CHK-R1]\n" + strings.Repeat("x", MaxFileBytes))
	if got := s.File("huge.go", huge); got != nil {
		t.Errorf("a huge file reads %v", got)
	}
}

func TestATagKeepsTheWordAsWrittenAndThePathItWasFoundIn(t *testing.T) {
	tags := NewScanner([]string{"implements"}, []string{"CHK"}).File("cmd/brakes/main.go", []byte("// [IMPLEMENTS chk-r5]\n"))
	if len(tags) != 1 || tags[0].Word != "IMPLEMENTS" || tags[0].Key != "CHK-R5" || tags[0].Path != "cmd/brakes/main.go" {
		t.Errorf("the tag reads %+v", tags)
	}
}
