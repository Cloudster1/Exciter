package quality

import "slices"

// conjunctions may join a second condition to the first, as in When X, and while Y.
var conjunctions = set("and", "or", "und", "oder")

// ears reads the shape of a sentence that says what shall be; ok is false when it holds no modal.
func (c *checker) ears(u *unit) (pattern Pattern, found []raw, ok bool) {
	toks := u.tokens
	m := c.firstModal(toks)
	if m < 0 {
		return PatternNone, nil, false
	}
	add := func(from, to int) { found = append(found, c.hit(u, RuleEARS, toks[from].from, toks[to].to)) }
	mainClause := toks[m].clause
	pattern = PatternUbiquitous
	first, opened := c.openerAt(toks, 0)
	switch {
	case m == 0:
		add(0, 0)
	case opened && first.pattern == PatternNone:
		add(0, len(first.words)-1)
	case opened:
		pattern = c.conditions(toks, m, add)
	case mainClause > 0:
		// Something other than a condition comes before the subject, such as For safety reasons.
		add(0, slices.IndexFunc(toks, func(t token) bool { return t.clause > 0 })-1)
	}
	c.triggersAfter(toks, m, add)
	if len(found) > 0 {
		pattern = PatternNone
	}
	return pattern, found, true
}

// conditions reads the clauses before the modal's: their shapes, an If without then, and in German a verb
// that does not follow them.
func (c *checker) conditions(toks []token, m int, add func(from, to int)) Pattern {
	mainClause := toks[m].clause
	kinds := map[Pattern]bool{}
	// then is where the last then before the modal stands.
	then := -1
	for i := m - 1; i >= 0 && then < 0; i-- {
		if c.g.thenWords[toks[i].lower] {
			then = i
		}
	}
	for i := 0; i < m; i++ {
		if (i > 0 && toks[i].clause == toks[i-1].clause) || toks[i].clause >= max(mainClause, 1) {
			continue
		}
		j := i
		if i > 0 && conjunctions[toks[j].lower] {
			j++
		}
		o, ok := c.openerAt(toks, j)
		if !ok {
			continue
		}
		if o.pattern == PatternNone {
			add(j, j+len(o.words)-1)
			continue
		}
		kinds[o.pattern] = true
		if o.needsThen && then <= j {
			add(j, j)
		}
	}
	if c.lang == German && mainClause > 0 {
		start := slices.IndexFunc(toks, func(t token) bool { return t.clause == mainClause })
		verb := start
		if c.g.thenWords[toks[verb].lower] {
			verb++
		}
		if verb != m {
			add(start, m)
		}
	}
	if len(kinds) > 1 {
		return PatternComplex
	}
	for _, p := range Patterns {
		if kinds[p] {
			return p
		}
	}
	return PatternUbiquitous
}

// triggersAfter finds a condition after the modal, where EARS wants it before; German sets it off with a comma.
func (c *checker) triggersAfter(toks []token, m int, add func(from, to int)) {
	for k := m + 1; k < len(toks); k++ {
		if c.lang == German && (toks[k].clause == toks[m].clause || toks[k].clause == toks[k-1].clause) {
			continue
		}
		for _, words := range c.g.triggers {
			if wordsAt(toks, k, words) {
				add(k, k+len(words)-1)
				break
			}
		}
	}
}
