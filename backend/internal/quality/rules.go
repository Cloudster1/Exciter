package quality

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func bothFields() []Field { return []Field{FieldTitle, FieldStatement} }

// Rules is every rule with how it starts out in a project, in the order of RuleIDs.
var Rules = []RuleInfo{
	{ID: RuleVague, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(), Words: true,
		Label: "Vague words", Description: "Words that mean what the reader wants them to, such as user-friendly, fast or appropriate."},
	{ID: RuleUnbounded, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(), Words: true,
		Label: "Unbounded words", Description: "Words without an end a test can reach, such as all, any, etc. or and so on."},
	{ID: RuleEscape, DefaultSeverity: SeverityError, DefaultEnabled: true, Fields: bothFields(), Words: true,
		Label: "Escape clauses", Description: "Words that let the requirement be skipped, such as if possible or as far as practicable."},
	{ID: RulePlaceholder, DefaultSeverity: SeverityError, DefaultEnabled: true, Fields: bothFields(), Words: true,
		Label: "Placeholders", Description: "Text left open, such as TBD, TBC, [...] or <name>."},
	{ID: RuleAndOr, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(),
		Label: "And/or", Description: "And/or, which leaves open whether both or one is meant."},
	{ID: RuleMultipleShall, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(),
		Label: "More than one shall", Description: "A second shall or must in one sentence or in a title, which makes it two requirements."},
	{ID: RuleNegative, DefaultSeverity: SeverityHint, DefaultEnabled: true, Fields: bothFields(),
		Label: "Negative statements", Description: "What shall not be, such as shall not or never, which a test cannot cover in full."},
	{ID: RulePassive, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(),
		Label: "Passive voice", Description: "The passive voice without who does it, such as shall be encrypted."},
	{ID: RulePronoun, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(),
		Label: "Pronouns", Description: "A pronoun without the thing it stands for before it, such as It shall at the start."},
	{ID: RuleEARS, DefaultSeverity: SeverityHint, DefaultEnabled: true, Fields: []Field{FieldStatement},
		Label: "EARS shapes", Description: "A sentence with a shall that fits none of the EARS shapes, or a statement without a shall."},
	{ID: RuleAvoidTerm, DefaultSeverity: SeverityWarning, DefaultEnabled: true, Fields: bothFields(),
		Label: "Words to avoid", Description: "A word the glossary says not to use, and the term to use instead."},
}

// RuleOf is the catalog's entry for a rule.
func RuleOf(id RuleID) (RuleInfo, bool) {
	i := slices.IndexFunc(Rules, func(r RuleInfo) bool { return r.ID == id })
	if i < 0 {
		return RuleInfo{}, false
	}
	return Rules[i], true
}

// AvoidTerm is a word a glossary says not to use, and the term to use instead.
type AvoidTerm struct {
	Word string
	Use  string
}

// The shortest words a participle's shape is read from; shorter ones, such as red, erst or geht, are other words.
const (
	minParticipleEN  = 4
	minWeakDE        = 5
	minIertDE        = 6
	minInseparableDE = 6
	// maxPlaceholder bounds what a bracket left open holds, so a bracketed sentence is not one.
	maxPlaceholder = 40
)

// bracketed is a placeholder in brackets: [...], [value] or <name>.
var bracketed = regexp.MustCompile(`\[(?:\.\.\.|\x{2026}|[\p{L}_][^\[\]\n]{0,` + strconv.Itoa(maxPlaceholder-1) + `})\]|<[\p{L}_][^<>\n]{0,` + strconv.Itoa(maxPlaceholder-1) + `}>`)

// structural rules find the shape of a sentence, so their keys name the sentence rather than the words.
var structural = set(string(RulePassive), string(RulePronoun), string(RuleEARS), string(RuleMultipleShall),
	string(RuleNegative))

// raw is what a rule found, in bytes of its block's text.
type raw struct {
	rule  RuleID
	field Field
	block int
	span
	// unit is the sentence it was found in, which names a structural rule's key.
	unit *unit
}

// checker holds a language's rules and a project's word lists for one check.
type checker struct {
	lang     Language
	g        *grammar
	words    map[RuleID]*matcher
	andOr    *matcher
	negative *matcher
	avoid    *matcher
}

var andOrWords = []string{"and/or", "and / or", "und/oder", "und / oder"}

func newChecker(lang Language, words map[RuleID]Words, avoid []AvoidTerm) *checker {
	c := &checker{lang: lang, g: grammars[lang], words: map[RuleID]*matcher{}}
	german := lang == German
	for _, rule := range WordRules {
		c.words[rule] = newMatcher(wordList(lang, rule, words[rule]), german, c.g)
	}
	c.andOr = newMatcher(andOrWords, false, nil)
	c.negative = newMatcher(c.g.negatives, false, nil)
	var avoided []string
	for _, a := range avoid {
		avoided = append(avoided, a.Word)
	}
	c.avoid = newMatcher(avoided, false, nil)
	return c
}

// wordList is a rule's built-in words less those the project excepts, and the words it adds.
func wordList(lang Language, rule RuleID, w Words) []string {
	except := map[string]bool{}
	for _, e := range w.Except {
		except[normalized(e)] = true
	}
	var out []string
	for _, word := range append(slices.Clone(builtIn[lang][rule]), w.Add...) {
		if !except[normalized(word)] {
			out = append(out, word)
		}
	}
	return out
}

func (c *checker) hit(u *unit, rule RuleID, from, to int) raw {
	return raw{rule: rule, field: u.field, block: u.block, span: span{from, to}, unit: u}
}

// wordHits are the findings of the word lists, of and/or and of the glossary's words to avoid.
func (c *checker) wordHits(u *unit) []raw {
	var out []raw
	for _, rule := range WordRules {
		found := c.words[rule].find(u.text, u.from, u.to)
		if rule == RulePlaceholder {
			for _, loc := range bracketed.FindAllStringIndex(u.sentence(), -1) {
				found = append(found, span{u.from + loc[0], u.from + loc[1]})
			}
			found = longestFirst(found)
		}
		for _, s := range found {
			out = append(out, c.hit(u, rule, s.from, s.to))
		}
	}
	for _, s := range c.andOr.find(u.text, u.from, u.to) {
		out = append(out, c.hit(u, RuleAndOr, s.from, s.to))
	}
	for _, s := range findAvoided(c.avoid, u) {
		out = append(out, c.hit(u, RuleAvoidTerm, s.from, s.to))
	}
	return out
}

// findAvoided is where a draft uses a word the glossary says not to; #210 matches inflections here.
func findAvoided(m *matcher, u *unit) []span { return m.find(u.text, u.from, u.to) }

func (c *checker) modal(t token) bool { return c.g.modals[t.lower] }

func (c *checker) firstModal(toks []token) int {
	return slices.IndexFunc(toks, c.modal)
}

// multipleShall finds every modal after a sentence's first.
func (c *checker) multipleShall(u *unit) []raw {
	var out []raw
	seen := false
	for _, t := range u.tokens {
		if c.modal(t) {
			if seen {
				out = append(out, c.hit(u, RuleMultipleShall, t.from, t.to))
			}
			seen = true
		}
	}
	return out
}

// negativeWordsDE say no in a sentence that says what shall be.
var negativeWordsDE = set("nicht", "kein", "keine", "keinen", "keinem", "keiner", "keines")

func (c *checker) negatives(u *unit) []raw {
	var out []raw
	found := c.negative.find(u.text, u.from, u.to)
	toks := u.tokens
	covered := map[int]bool{}
	for _, s := range found {
		next := tokenFrom(toks, s.to)
		// Nicht nur ... sondern auch says more, not less.
		if c.lang == German && next < len(toks) && toks[next].lower == "nur" {
			continue
		}
		for i := tokenFrom(toks, s.from); i < next; i++ {
			covered[i] = true
		}
		out = append(out, c.hit(u, RuleNegative, s.from, s.to))
	}
	if c.lang != German || c.firstModal(toks) < 0 {
		return out
	}
	// German puts its nicht where the verb leaves room, often far from the modal.
	for i, t := range toks {
		if negativeWordsDE[t.lower] && !covered[i] && (i+1 >= len(toks) || toks[i+1].lower != "nur") {
			out = append(out, c.hit(u, RuleNegative, t.from, t.to))
		}
	}
	return out
}

// lead is how many clauses before the modal's are the sentence's condition, which may be passive and say a
// pronoun's thing.
func (c *checker) lead(toks []token) int {
	if len(toks) == 0 {
		return 0
	}
	if _, ok := c.openerAt(toks, 0); !ok {
		return 0
	}
	if m := c.firstModal(toks); m >= 0 {
		return toks[m].clause
	}
	return 1
}

func (c *checker) openerAt(toks []token, i int) (opener, bool) {
	for _, o := range c.g.openers {
		if wordsAt(toks, i, o.words) {
			return o, true
		}
	}
	return opener{}, false
}

func wordsAt(toks []token, i int, words []string) bool {
	if i+len(words) > len(toks) {
		return false
	}
	for k, w := range words {
		if toks[i+k].lower != w {
			return false
		}
	}
	return true
}

func (c *checker) passive(u *unit) []raw {
	if c.lang == German {
		return c.passiveDE(u)
	}
	toks := u.tokens
	lead := c.lead(toks)
	// agent[i] says a by follows token i in its clause.
	agent := make([]bool, len(toks))
	for i := len(toks) - 2; i >= 0; i-- {
		agent[i] = toks[i+1].clause == toks[i].clause && (agent[i+1] || toks[i+1].lower == "by")
	}
	var out []raw
	for i := 0; i < len(toks); i++ {
		if toks[i].clause < lead || !beForms[toks[i].lower] {
			continue
		}
		j, adverbs := i+1, 0
		for j < len(toks) && toks[j].clause == toks[i].clause {
			w := toks[j].lower
			if beForms[w] {
				j++
				continue
			}
			if adverbs < 2 && (strings.HasSuffix(w, "ly") || passiveAdverbs[w]) {
				adverbs++
				j++
				continue
			}
			break
		}
		if j >= len(toks) || toks[j].clause != toks[i].clause || !participleEN(toks, j) {
			continue
		}
		if !agent[j] {
			out = append(out, c.hit(u, RulePassive, toks[i].from, toks[j].to))
		}
		i = j
	}
	return out
}

func participleEN(toks []token, j int) bool {
	w := toks[j].lower
	if adjectival[w] || (j+1 < len(toks) && toks[j+1].lower == "of") {
		return false
	}
	return irregular[w] || (strings.HasSuffix(w, "ed") && !strings.HasSuffix(w, "eed") && utf8.RuneCountInString(w) >= minParticipleEN)
}

func (c *checker) passiveDE(u *unit) []raw {
	toks := u.tokens
	lead := c.lead(toks)
	var out []raw
	for start := 0; start < len(toks); {
		end := start
		for end < len(toks) && toks[end].clause == toks[start].clause {
			end++
		}
		clause := toks[start:end]
		start = end
		if clause[0].clause < lead || slices.ContainsFunc(clause, func(t token) bool { return agentsDE[t.lower] }) {
			continue
		}
		w := slices.IndexFunc(clause, func(t token) bool { return werdenForms[t.lower] })
		if w < 0 {
			continue
		}
		p := -1
		for k := w + 1; k < len(clause) && p < 0; k++ {
			if participleDE(clause[k]) {
				p = k
			}
		}
		for k := w - 1; k >= 0 && p < 0; k-- {
			if participleDE(clause[k]) {
				p = k
			}
		}
		if p < 0 {
			continue
		}
		out = append(out, c.hit(u, RulePassive, clause[min(w, p)].from, clause[max(w, p)].to))
	}
	return out
}

// participleDE reads a German participle by its shape; a noun has a capital, so the word must not.
func participleDE(t token) bool {
	if r, _ := utf8.DecodeRuneInString(t.text); !unicode.IsLower(r) {
		return false
	}
	w := t.lower
	if notParticipleDE[w] {
		return false
	}
	if participleStem(w) {
		return true
	}
	for _, prefix := range separableDE {
		if rest, ok := strings.CutPrefix(w, prefix); ok && strings.HasPrefix(rest, "ge") && participleStem(rest) {
			return true
		}
	}
	return false
}

func participleStem(w string) bool {
	n := utf8.RuneCountInString(w)
	switch {
	case strongDE[w]:
		return true
	case strings.HasSuffix(w, "iert") && n >= minIertDE:
		return true
	case strings.HasPrefix(w, "ge") && strings.HasSuffix(w, "t") && n >= minWeakDE:
		return true
	}
	if !strings.HasSuffix(w, "t") || n < minInseparableDE {
		return false
	}
	for _, prefix := range inseparableDE {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

// expletiveDE are the verbs after which es stands for nothing, as in Es muss sichergestellt werden.
var expletiveDE = set("ist", "sind", "gibt", "wird", "werden")

// pronouns finds a pronoun whose thing the requirement has not named: at its start, right after a condition,
// and a demonstrative as a subject.
func (c *checker) pronouns(u *unit, first bool) []raw {
	toks := u.tokens
	var out []raw
	seen := map[int]bool{}
	flag := func(i int) {
		if !seen[i] {
			seen[i] = true
			out = append(out, c.hit(u, RulePronoun, toks[i].from, toks[i].to))
		}
	}
	auxAfter := func(i int) bool {
		return i+1 < len(toks) && (c.modal(toks[i+1]) || c.g.auxiliaries[toks[i+1].lower])
	}
	candidate := func(i int) bool {
		w := toks[i].lower
		if c.g.demonstratives[w] {
			return auxAfter(i)
		}
		if !c.g.pronouns[w] {
			return false
		}
		return !(c.lang == German && w == "es" && i+1 < len(toks) &&
			(c.modal(toks[i+1]) || expletiveDE[toks[i+1].lower]))
	}
	if first && len(toks) > 0 && candidate(0) {
		flag(0)
	}
	if lead := c.lead(toks); lead > 0 {
		i := slices.IndexFunc(toks, func(t token) bool { return t.clause >= lead })
		if i >= 0 && c.g.thenWords[toks[i].lower] {
			i++
		}
		// German puts the verb right after the condition, and the subject after it.
		if c.lang == German && i >= 0 && i < len(toks) && c.modal(toks[i]) {
			i++
		}
		if i >= 0 && i < len(toks) && candidate(i) {
			flag(i)
		}
	}
	for i, t := range toks {
		if c.g.demonstratives[t.lower] && auxAfter(i) {
			flag(i)
		}
	}
	return out
}
