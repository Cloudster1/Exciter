package quality

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// The corpus marks what a line must find with these brackets, which no requirement writes.
const (
	openMark  = "⟦"
	closeMark = "⟧"
)

type corpusLine struct {
	where    string
	finds    bool
	sentence string
	from, to int
}

func readCorpus(t *testing.T, path string) []corpusLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []corpusLine
	scan := bufio.NewScanner(f)
	for n := 1; scan.Scan(); n++ {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		where := fmt.Sprintf("%s:%d", filepath.ToSlash(path), n)
		sign, sentence, ok := strings.Cut(line, " ")
		if !ok || (sign != "+" && sign != "-") {
			t.Fatalf("%s: a line starts with + or -, then a space", where)
		}
		c := corpusLine{where: where, finds: sign == "-", sentence: sentence}
		if c.finds {
			c.from = strings.Index(sentence, openMark)
			end := strings.Index(sentence, closeMark)
			if c.from < 0 || end < c.from {
				t.Fatalf("%s: a - line marks what it finds in %s and %s", where, openMark, closeMark)
			}
			c.to = end - len(openMark)
			c.sentence = strings.Replace(strings.Replace(sentence, openMark, "", 1), closeMark, "", 1)
		}
		out = append(out, c)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func statementOf(text string) document.Node {
	return document.Node{Type: "doc", Content: []document.Node{{Type: "paragraph", Content: []document.Node{{Type: "text", Text: text}}}}}
}

func corpusPath(lang Language, rule RuleID) string {
	return filepath.Join("testdata", string(lang), string(rule)+".txt")
}

func TestEveryRuleHasACorpusInBothLanguages(t *testing.T) {
	for _, lang := range Languages {
		for _, rule := range RuleIDs {
			if rule == RuleAvoidTerm {
				continue
			}
			finds, passes := 0, 0
			for _, c := range readCorpus(t, corpusPath(lang, rule)) {
				if c.finds {
					finds++
				} else {
					passes++
				}
			}
			if finds == 0 || passes == 0 {
				t.Errorf("%s: wants lines that find and lines that do not", corpusPath(lang, rule))
			}
		}
	}
}

func TestCorpus(t *testing.T) {
	for _, lang := range Languages {
		for _, rule := range RuleIDs {
			if rule == RuleAvoidTerm {
				continue
			}
			for _, c := range readCorpus(t, corpusPath(lang, rule)) {
				t.Run(c.where, func(t *testing.T) {
					var found []Hit
					for _, h := range Examine(Input{Language: lang, Statement: statementOf(c.sentence)}).Hits {
						if h.Rule == rule {
							found = append(found, h)
						}
					}
					if !c.finds {
						if len(found) > 0 {
							t.Errorf("%q finds %q, and should find nothing", c.sentence, found[0].Match)
						}
						return
					}
					from, to := utf16Len(c.sentence[:c.from]), utf16Len(c.sentence[:c.to])
					if len(found) != 1 || found[0].From != from || found[0].To != to || found[0].Block != 0 ||
						found[0].Field != FieldStatement {
						var got []string
						for _, h := range found {
							got = append(got, fmt.Sprintf("%q at %d-%d", h.Match, h.From, h.To))
						}
						t.Errorf("%q finds %v, want only %q", c.sentence, got, c.sentence[c.from:c.to])
					}
				})
			}
		}
	}
}
