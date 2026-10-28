package codetrace

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fieldOf names the field an error refuses, empty for none.
func fieldOf(err error) string {
	var field *FieldError
	if errors.As(err, &field) {
		return field.Field
	}
	return ""
}

func TestCheckWordsTrimsKeepsEachWordOnceAndHoldsTheLimits(t *testing.T) {
	eleven := make([]string, MaxWords+1)
	for i := range eleven {
		eleven[i] = "word" + string(rune('a'+i))
	}
	for _, c := range []struct {
		name  string
		in    Words
		want  Words
		field string
	}{
		{"trimmed, blanks left out", Words{Implements: []string{"  implements ", "", " "}, Verifies: []string{"\tverifies"}},
			Words{Implements: []string{"implements"}, Verifies: []string{"verifies"}}, ""},
		{"a word given twice, in any case, is kept once as first written", Words{Implements: []string{"Satisfies", "satisfies", "implements"}, Verifies: []string{"verifies"}},
			Words{Implements: []string{"Satisfies", "implements"}, Verifies: []string{"verifies"}}, ""},
		{"a word in both relations", Words{Implements: []string{"checks"}, Verifies: []string{"Checks"}}, Words{}, "verifies"},
		{"no word for a relation", Words{Implements: []string{}, Verifies: []string{"verifies"}}, Words{}, "implements"},
		{"only blanks for a relation", Words{Implements: []string{"implements"}, Verifies: []string{"  "}}, Words{}, "verifies"},
		{"more than MaxWords", Words{Implements: []string{"implements"}, Verifies: eleven}, Words{}, "verifies"},
		{"MaxWords once duplicates are dropped", Words{Implements: append(eleven[:MaxWords:MaxWords], "WORDA"), Verifies: []string{"verifies"}},
			Words{Implements: eleven[:MaxWords], Verifies: []string{"verifies"}}, ""},
		{"a word of MaxWordLength", Words{Implements: []string{"a" + strings.Repeat("b", MaxWordLength-1)}, Verifies: []string{"verifies"}},
			Words{Implements: []string{"a" + strings.Repeat("b", MaxWordLength-1)}, Verifies: []string{"verifies"}}, ""},
		{"a word past MaxWordLength", Words{Implements: []string{"a" + strings.Repeat("b", MaxWordLength)}, Verifies: []string{"verifies"}}, Words{}, "implements"},
		{"a word starting with a digit", Words{Implements: []string{"1x"}, Verifies: []string{"verifies"}}, Words{}, "implements"},
		{"a word with a space inside", Words{Implements: []string{"implemented by"}, Verifies: []string{"verifies"}}, Words{}, "implements"},
		{"letters, digits, - and _", Words{Implements: []string{"impl-2_x"}, Verifies: []string{"verifies"}},
			Words{Implements: []string{"impl-2_x"}, Verifies: []string{"verifies"}}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := checkWords(c.in)
			if field := fieldOf(err); field != c.field {
				t.Fatalf("refuses %q (%v), want %q", field, err, c.field)
			}
			if c.field == "" && !reflect.DeepEqual(got, c.want) {
				t.Errorf("keeps %v, want %v", got, c.want)
			}
		})
	}
}

func TestCheckScanSaysWhichFieldDoesNotHold(t *testing.T) {
	good := func() ScanInput {
		return ScanInput{Repository: "acme/checkout", Commit: "4f2c9a1e7b3d", Branch: "main", FileURL: "https://git.example/{commit}/{path}#L{line}",
			Tags: []FoundTag{{Path: "brakes/engage.go", Line: 12, Word: "implements", Key: "CHK-R1"}}}
	}
	for _, c := range []struct {
		name  string
		edit  func(*ScanInput)
		field string
	}{
		{"a whole scan", func(*ScanInput) {}, ""},
		{"no repository", func(s *ScanInput) { s.Repository = "  " }, "repository"},
		{"a repository past MaxNameLength", func(s *ScanInput) { s.Repository = strings.Repeat("r", MaxNameLength+1) }, "repository"},
		{"a commit that is no SHA", func(s *ScanInput) { s.Commit = "HEAD" }, "commit"},
		{"no commit", func(s *ScanInput) { s.Commit = "" }, "commit"},
		{"no branch", func(s *ScanInput) { s.Branch = "" }, "branch"},
		{"a branch past MaxNameLength", func(s *ScanInput) { s.Branch = strings.Repeat("b", MaxNameLength+1) }, "branch"},
		{"no address of a line", func(s *ScanInput) { s.FileURL = "" }, ""},
		{"an address that is no web address", func(s *ScanInput) { s.FileURL = "javascript:alert(1)//{path}" }, "fileUrl"},
		{"an address without {path}", func(s *ScanInput) { s.FileURL = "https://git.example/{commit}#L{line}" }, "fileUrl"},
		{"an address past MaxURLLength", func(s *ScanInput) { s.FileURL = "https://git.example/{path}/" + strings.Repeat("x", MaxURLLength) }, "fileUrl"},
		{"a tag without a path", func(s *ScanInput) { s.Tags[0].Path = " " }, "tags[0].path"},
		{"a tag's path past MaxPathLength", func(s *ScanInput) { s.Tags[0].Path = strings.Repeat("p", MaxPathLength+1) }, "tags[0].path"},
		{"a tag on line 0", func(s *ScanInput) { s.Tags[0].Line = 0 }, "tags[0].line"},
		{"a tag without a word", func(s *ScanInput) { s.Tags[0].Word = "" }, "tags[0].word"},
		{"a tag's word past MaxWordLength", func(s *ScanInput) { s.Tags[0].Word = strings.Repeat("w", MaxWordLength+1) }, "tags[0].word"},
		{"a tag whose key is no key", func(s *ScanInput) { s.Tags[0].Key = "brakes" }, "tags[0].key"},
		{"the second tag is named by its place", func(s *ScanInput) {
			s.Tags = append(s.Tags, FoundTag{Path: "a.go", Line: 1, Word: "implements", Key: "nope"})
		}, "tags[1].key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := good()
			c.edit(&in)
			if field := fieldOf(checkScan(&in)); field != c.field {
				t.Errorf("refuses %q, want %q", field, c.field)
			}
		})
	}

	t.Run("the fields are trimmed and the commit lowered", func(t *testing.T) {
		in := good()
		in.Repository, in.Commit, in.Branch, in.FileURL = " acme/checkout ", " 4F2C9A1E ", " main\n", " https://git.example/{path} "
		if err := checkScan(&in); err != nil {
			t.Fatal(err)
		}
		if in.Repository != "acme/checkout" || in.Commit != "4f2c9a1e" || in.Branch != "main" || in.FileURL != "https://git.example/{path}" {
			t.Errorf("the scan reads %+v", in)
		}
	})

	t.Run("more than MaxScanTags is too many, not a field", func(t *testing.T) {
		in := good()
		in.Tags = make([]FoundTag, MaxScanTags+1)
		if err := checkScan(&in); !errors.Is(err, ErrTooManyTags) {
			t.Errorf("a scan past the limit: %v", err)
		}
	})
}

func TestRelationOfReadsAWordInAnyCase(t *testing.T) {
	words := Words{Implements: []string{"implements", "Satisfies"}, Verifies: []string{"verifies"}}
	for word, want := range map[string]Relation{
		"implements": Implements,
		"IMPLEMENTS": Implements,
		"satisfies":  Implements,
		"Verifies":   Verifies,
		"tests":      "",
		"":           "",
	} {
		if got := relationOf(words, word); got != want {
			t.Errorf("%q means %q, want %q", word, got, want)
		}
	}
}

func TestPlacedNamesATagRegardlessOfItsLineAndTheWordsCase(t *testing.T) {
	if placed("a.go", "Implements", "CHK-R1") != placed("a.go", "implements", "CHK-R1") {
		t.Error("the word's case tells two places apart")
	}
	for _, other := range [][3]string{{"b.go", "implements", "CHK-R1"}, {"a.go", "verifies", "CHK-R1"}, {"a.go", "implements", "CHK-R2"}} {
		if placed("a.go", "implements", "CHK-R1") == placed(other[0], other[1], other[2]) {
			t.Errorf("%v is taken for the same place", other)
		}
	}
	// The parts are kept apart, so a path ending like a word does not run into the next part.
	if placed("a", "bc", "K-R1") == placed("ab", "c", "K-R1") {
		t.Error("two places run together")
	}
}

func TestFileLinkEscapesEachSegmentOfThePath(t *testing.T) {
	const template = "https://git.example/acme/checkout/blob/{commit}/{path}#L{line}"
	for _, c := range []struct {
		template, path, want string
	}{
		{template, "brakes/engage.go", "https://git.example/acme/checkout/blob/4f2c9a1e/brakes/engage.go#L12"},
		{template, "my docs/brake #1.md", "https://git.example/acme/checkout/blob/4f2c9a1e/my%20docs/brake%20%231.md#L12"},
		{template, "a/b?c.go", "https://git.example/acme/checkout/blob/4f2c9a1e/a/b%3Fc.go#L12"},
		{"", "brakes/engage.go", ""},
	} {
		if got := FileLink(c.template, "4f2c9a1e", c.path, 12); got != c.want {
			t.Errorf("%q links to %q, want %q", c.path, got, c.want)
		}
	}
}
