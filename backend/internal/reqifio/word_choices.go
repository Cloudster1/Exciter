package reqifio

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/cloudster1/exciter/backend/internal/docx"
)

// defaultKeywords make a paragraph a requirement while no style, pattern or keywords are chosen, by the project's
// language.
var defaultKeywords = map[string][]string{"en": {"shall", "must"}, "de": {"muss", "soll"}}

// wordTitleLength bounds the title an import gives a paragraph from its first words.
const wordTitleLength = 80

// wordChoices are the Word choices as a build reads them.
type wordChoices struct {
	unit     string
	front    string
	styles   map[string]bool
	pattern  *regexp.Regexp
	keywords map[string]bool
	// everything says nothing tells requirements from the rest, so every paragraph is one.
	everything bool
	tables     map[int]string
}

// readWordChoices reads the choices a Word import takes beside the schema's, and what is wrong with them.
func readWordChoices(choices map[string]string, lang string, tables int) (wordChoices, []Problem) {
	c := wordChoices{unit: UnitParagraph, front: FrontSection, styles: map[string]bool{}, keywords: map[string]bool{}, tables: map[int]string{}}
	var problems []Problem
	if v, ok := choices[ChoiceWordUnit]; ok && v != "" {
		if v != UnitParagraph && v != UnitHeading {
			problems = append(problems, Problem{Where: ChoiceWordUnit, Message: "Choose paragraph or heading as what makes one requirement."})
		} else {
			c.unit = v
		}
	}
	if v, ok := choices[ChoiceWordFront]; ok && v != "" {
		if v != FrontSection && v != FrontSkip {
			problems = append(problems, Problem{Where: ChoiceWordFront, Message: "Choose section or skip for the text before the first heading."})
		} else {
			c.front = v
		}
	}
	for _, s := range splitList(choices[ChoiceWordStyles]) {
		c.styles[strings.ToLower(s)] = true
	}
	if p := strings.TrimSpace(choices[ChoiceWordPattern]); p != "" {
		re, err := regexp.Compile(p)
		if err != nil {
			problems = append(problems, Problem{Where: ChoiceWordPattern,
				Message: "The pattern is no regular expression the import reads (" + err.Error() + "). Correct it, or clear it."})
		} else {
			c.pattern = re
		}
	}
	keywords, set := choices[ChoiceWordKeywords]
	if !set && len(c.styles) == 0 && c.pattern == nil {
		words, known := defaultKeywords[lang]
		if !known {
			words = defaultKeywords["en"]
		}
		keywords = strings.Join(words, ", ")
	}
	for _, k := range splitList(keywords) {
		c.keywords[strings.ToLower(k)] = true
	}
	c.everything = len(c.styles) == 0 && c.pattern == nil && len(c.keywords) == 0
	for key, v := range choices {
		rest, ok := strings.CutPrefix(key, ChoiceWordTable)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(rest)
		switch {
		case err != nil || n < 1 || n > tables:
			problems = append(problems, Problem{Where: key, Message: "The document has no table " + rest + ". Choose for the tables it has, numbered from 1."})
		case v != TableOne && v != TableRows:
			problems = append(problems, Problem{Where: key, Message: "Choose one or rows for table " + rest + "."})
		default:
			c.tables[n] = v
		}
	}
	return c, problems
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// classify says whether a unit is a requirement, and the number the pattern found in it.
func (c *wordChoices) classify(first *docx.Block, text string) (bool, string) {
	number := c.number(first)
	if c.everything {
		return true, number
	}
	if c.styles[strings.ToLower(first.StyleID)] || c.styles[strings.ToLower(first.StyleName)] {
		return true, number
	}
	if number != "" {
		return true, number
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if c.keywords[w] {
			return true, number
		}
	}
	return false, ""
}

// number is what the pattern matches in a block's label and first words: its first group when it has one.
func (c *wordChoices) number(b *docx.Block) string {
	if c.pattern == nil {
		return ""
	}
	m := c.pattern.FindStringSubmatch(strings.TrimSpace(b.Label + " " + truncate(b.Text, wordTitleLength*2)))
	switch {
	case m == nil:
		return ""
	case len(m) > 1 && m[1] != "":
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(m[0])
}

// wordTitle is a title from a paragraph's first words: its first sentence, cut at a word, an identifying label
// before it.
func wordTitle(label, text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.Join(strings.Fields(line), " ")
	for _, end := range []string{". ", "? ", "! "} {
		if i := strings.Index(line, end); i > 0 {
			line = line[:i+1]
		}
	}
	if r := []rune(line); len(r) > wordTitleLength {
		cut := string(r[:wordTitleLength])
		if i := strings.LastIndex(cut, " "); i > wordTitleLength/2 {
			cut = cut[:i]
		}
		line = strings.TrimRight(cut, " ,;:")
	}
	if label != "" && strings.IndexFunc(label, unicode.IsLetter) >= 0 && !strings.HasPrefix(line, label) {
		line = strings.TrimSpace(label + " " + line)
	}
	return line
}
