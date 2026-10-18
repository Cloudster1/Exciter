package quality

import (
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// span is a range of bytes of a block's text.
type span struct{ from, to int }

// token is one word of a sentence; clause counts the commas, semicolons and colons before it.
type token struct {
	from, to int
	text     string
	lower    string
	clause   int
}

// unit is one sentence the rules read: its block's text, where in it the sentence is, and its words.
type unit struct {
	field    Field
	block    int
	text     string
	from, to int
	tokens   []token
}

func newUnit(field Field, block int, text string, s span) *unit {
	return &unit{field: field, block: block, text: text, from: s.from, to: s.to, tokens: tokenize(text, s.from, s.to)}
}

func (u *unit) sentence() string { return u.text[u.from:u.to] }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) }

// rightQuote is the apostrophe a word processor types.
const rightQuote = rune(0x2019)

// isJoiner holds a word together when a letter follows, as in user-friendly or it's.
func isJoiner(r rune) bool { return r == '-' || r == '\'' || r == rightQuote }

func tokenize(text string, from, to int) []token {
	var out []token
	clause := 0
	for i := from; i < to; {
		r, size := utf8.DecodeRuneInString(text[i:to])
		if !isWordRune(r) {
			// A comma inside a number, as in 1,000, does not end a clause.
			if (r == ',' || r == ';' || r == ':') && spaceOrEnd(text, i+size, to) {
				clause++
			}
			i += size
			continue
		}
		start := i
		for i < to {
			r, size = utf8.DecodeRuneInString(text[i:to])
			if isWordRune(r) {
				i += size
				continue
			}
			if isJoiner(r) && i+size < to {
				if next, _ := utf8.DecodeRuneInString(text[i+size : to]); isWordRune(next) {
					i += size
					continue
				}
			}
			break
		}
		w := text[start:i]
		out = append(out, token{from: start, to: i, text: w, lower: strings.ToLower(w), clause: clause})
	}
	return out
}

// tokenFrom is the first token that starts at or after a byte.
func tokenFrom(toks []token, at int) int {
	return sort.Search(len(toks), func(i int) bool { return toks[i].from >= at })
}

func spaceOrEnd(text string, i, to int) bool {
	if i >= to {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[i:to])
	return unicode.IsSpace(r)
}

// abbreviations end with a full stop that does not end the sentence; units such as ms. or min. are left out,
// as a requirement often ends with one.
var abbreviations = set("e.g.", "i.e.", "cf.", "vs.", "approx.", "incl.", "resp.", "nr.", "fig.", "dr.", "mr.",
	"mrs.", "z.b.", "d.h.", "bzw.", "ca.", "bspw.", "evtl.", "ggf.", "inkl.", "u.a.", "z.t.", "vgl.", "abb.", "u.u.",
	"o.ä.", "sog.")

// months follow a German day's full stop, as in 3. Mai, which does not end the sentence.
var months = set("januar", "jänner", "februar", "märz", "april", "mai", "juni", "juli", "august", "september",
	"oktober", "november", "dezember")

// splitSentences cuts a block's text into sentences, at a full stop, mark or semicolon before a space, and at a
// line break.
func splitSentences(text string) []span {
	var out []span
	add := func(from, to int) {
		for from < to {
			r, size := utf8.DecodeRuneInString(text[from:to])
			if !unicode.IsSpace(r) {
				break
			}
			from += size
		}
		for to > from {
			r, size := utf8.DecodeLastRuneInString(text[from:to])
			if !unicode.IsSpace(r) {
				break
			}
			to -= size
		}
		if to > from {
			out = append(out, span{from, to})
		}
	}
	start := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		end := i + size
		switch {
		case r == '\n':
			add(start, i)
			start = end
		case (r == '!' || r == '?' || r == ';') && spaceOrEnd(text, end, len(text)):
			add(start, end)
			start = end
		case r == '.' && spaceOrEnd(text, end, len(text)) && endsSentence(text, i, end):
			add(start, end)
			start = end
		}
		i = end
	}
	add(start, len(text))
	return out
}

func endsSentence(text string, dot, end int) bool {
	next := end
	for next < len(text) {
		r, size := utf8.DecodeRuneInString(text[next:])
		if !unicode.IsSpace(r) || r == '\n' {
			break
		}
		next += size
	}
	if next >= len(text) {
		return true
	}
	// A sentence starts with a capital, so a full stop before a small letter is an abbreviation or a date.
	if r, _ := utf8.DecodeRuneInString(text[next:]); unicode.IsLower(r) {
		return false
	}
	if dot > 0 {
		if r, _ := utf8.DecodeLastRuneInString(text[:dot]); unicode.IsDigit(r) {
			after := next
			for after < len(text) {
				r, size := utf8.DecodeRuneInString(text[after:])
				if !unicode.IsLetter(r) {
					break
				}
				after += size
			}
			if months[strings.ToLower(text[next:after])] {
				return false
			}
		}
	}
	word := dot
	for word > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:word])
		if !unicode.IsLetter(r) && r != '.' {
			break
		}
		word -= size
	}
	return !abbreviations[strings.ToLower(text[word:end])]
}

// utf16Len counts as the editor does.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += max(utf16.RuneLen(r), 1)
	}
	return n
}

// normalized is a phrase in lower case with its spaces collapsed, as keys and word lists compare it.
func normalized(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// entry is one word or phrase a matcher finds.
type entry struct {
	lower string
	runes []rune
	// exact says it was written in capitals and matches only so.
	exact bool
	// inflects lets a German adjective's endings follow it.
	inflects                      bool
	beforeNumber, notBeforeNumber bool
}

// matcher finds words and phrases on word boundaries, the longest at each place.
type matcher struct{ byFirst map[rune][]*entry }

func newMatcher(words []string, inflect bool, g *grammar) *matcher {
	m := &matcher{byFirst: map[rune][]*entry{}}
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.Join(strings.Fields(w), " ")
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		runes := []rune(w)
		last := runes[len(runes)-1]
		e := &entry{lower: strings.ToLower(w), runes: runes, exact: capitals(w), inflects: inflect && unicode.IsLetter(last)}
		if g != nil {
			e.beforeNumber, e.notBeforeNumber = g.beforeNumber[e.lower], g.notBeforeNumber[e.lower]
		}
		first := unicode.ToLower(runes[0])
		m.byFirst[first] = append(m.byFirst[first], e)
	}
	return m
}

// capitals is a word like TBD, which in small letters would be an ordinary word.
func capitals(w string) bool {
	letters := 0
	for _, r := range w {
		if unicode.IsLetter(r) {
			if !unicode.IsUpper(r) {
				return false
			}
			letters++
		}
	}
	return letters >= 2
}

func (m *matcher) find(text string, from, to int) []span {
	var out []span
	for i := from; i < to; {
		r, size := utf8.DecodeRuneInString(text[i:to])
		best := -1
		inWord := false
		if i > 0 {
			prev, _ := utf8.DecodeLastRuneInString(text[:i])
			inWord = isWordRune(prev) && isWordRune(r)
		}
		if !inWord {
			for _, e := range m.byFirst[unicode.ToLower(r)] {
				if end := e.matchAt(text, i, to); end > best {
					best = end
				}
			}
		}
		if best > i {
			out = append(out, span{i, best})
			i = best
			continue
		}
		i += size
	}
	return out
}

// germanEndings are what an adjective adds; after an e only its last letter.
var germanEndings, germanEndingsAfterE = []string{"en", "em", "er", "es", "e"}, []string{"n", "m", "r", "s"}

func (e *entry) matchAt(text string, i, to int) int {
	j := i
	for _, pr := range e.runes {
		if j >= to {
			return -1
		}
		r, size := utf8.DecodeRuneInString(text[j:to])
		if pr == ' ' {
			if !unicode.IsSpace(r) {
				return -1
			}
			for j < to {
				r, size = utf8.DecodeRuneInString(text[j:to])
				if !unicode.IsSpace(r) {
					break
				}
				j += size
			}
			continue
		}
		if r != pr && (e.exact || unicode.ToLower(r) != unicode.ToLower(pr)) {
			return -1
		}
		j += size
	}
	if last := e.runes[len(e.runes)-1]; isWordRune(last) {
		end := -1
		if e.inflects {
			endings := germanEndings
			if unicode.ToLower(last) == 'e' {
				endings = germanEndingsAfterE
			}
			for _, ending := range endings {
				if k := j + len(ending); k <= to && strings.EqualFold(text[j:k], ending) && boundary(text, k, to) {
					end = k
					break
				}
			}
		}
		if end < 0 && !boundary(text, j, to) {
			return -1
		}
		j = max(j, end)
	}
	if (e.beforeNumber && !numberFollows(text, j, to)) || (e.notBeforeNumber && numberFollows(text, j, to)) {
		return -1
	}
	return j
}

func boundary(text string, j, to int) bool {
	if j >= to {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[j:to])
	return !isWordRune(r)
}

func numberFollows(text string, j, to int) bool {
	for j < to {
		r, size := utf8.DecodeRuneInString(text[j:to])
		if !unicode.IsSpace(r) {
			return unicode.IsDigit(r)
		}
		j += size
	}
	return false
}

// longestFirst keeps, of spans that overlap, the one that starts first and then the longest.
func longestFirst(spans []span) []span {
	slices.SortFunc(spans, func(a, b span) int {
		if a.from != b.from {
			return a.from - b.from
		}
		return b.to - a.to
	})
	out := spans[:0]
	end := -1
	for _, s := range spans {
		if s.from >= end {
			out = append(out, s)
			end = s.to
		}
	}
	return out
}
