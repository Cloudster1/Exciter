package quality

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// glossary finds the glossaries' terms in a requirement: their names and synonyms as uses, their words to
// avoid as findings, each as stemmed words so inflections meet.
type glossary struct {
	lang  Language
	terms []Term
	byID  map[uuid.UUID]int
	root  *trieNode
}

// trieNode is where a run of stemmed words leads; ends are the terms whose words end here.
type trieNode struct {
	next map[string]*trieNode
	ends []termEnd
}

// termEnd is one term whose name or synonym, or word to avoid, ends at a node.
type termEnd struct {
	term  int
	avoid bool
}

// found is a term's words in a sentence, in bytes of its block's text.
type found struct {
	field    Field
	block    int
	term     int
	explicit bool
	span
}

func newGlossary(lang Language, terms []Term) *glossary {
	g := &glossary{lang: lang, terms: terms, byID: map[uuid.UUID]int{}, root: &trieNode{}}
	for i, t := range terms {
		if _, ok := g.byID[t.ID]; !ok {
			g.byID[t.ID] = i
		}
		for _, w := range append([]string{t.Name}, t.Synonyms...) {
			g.add(w, termEnd{term: i})
		}
		for _, w := range t.Avoid {
			g.add(w, termEnd{term: i, avoid: true})
		}
	}
	return g
}

func (g *glossary) add(words string, end termEnd) {
	toks := tokenize(words, 0, len(words))
	if len(toks) == 0 {
		return
	}
	node := g.root
	for _, t := range toks {
		key := Stem(g.lang, t.text)
		next := node.next[key]
		if next == nil {
			if node.next == nil {
				node.next = map[string]*trieNode{}
			}
			next = &trieNode{}
			node.next[key] = next
		}
		node = next
	}
	if !slices.Contains(node.ends, end) {
		node.ends = append(node.ends, end)
	}
}

// before says which of two terms sharing words wins: the first glossary, a use over a word to avoid, then the
// name, then the id, so a check is the same every time.
func (g *glossary) before(a, b termEnd) bool {
	ta, tb := g.terms[a.term], g.terms[b.term]
	return cmp.Or(cmp.Compare(ta.Rank, tb.Rank), compareBool(a.avoid, b.avoid), strings.Compare(ta.Name, tb.Name),
		strings.Compare(ta.ID.String(), tb.ID.String())) < 0
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// find is where a sentence uses a term's words, the longest at each place, and the words it should not use;
// words that touch a mention are a person's name.
func (g *glossary) find(u *unit, block *document.TextBlock) (avoided []raw, uses []found) {
	if len(g.terms) == 0 {
		return nil, nil
	}
	toks := u.tokens
	stems := make([]string, len(toks))
	for i, t := range toks {
		stems[i] = Stem(g.lang, t.text)
	}
	for i := 0; i < len(toks); {
		node, last := g.root, -1
		var best termEnd
		for j := i; j < len(toks); j++ {
			if j > i && !blank(u.text[toks[j-1].to:toks[j].from]) {
				break
			}
			if node = node.next[stems[j]]; node == nil {
				break
			}
			if len(node.ends) > 0 {
				last, best = j, node.ends[0]
				for _, e := range node.ends[1:] {
					if g.before(e, best) {
						best = e
					}
				}
			}
		}
		if last < 0 {
			i++
			continue
		}
		s := span{toks[i].from, toks[last].to}
		i = last + 1
		if block != nil && block.Mentioned(s.from, s.to) {
			continue
		}
		if best.avoid {
			h := raw{rule: RuleAvoidTerm, field: u.field, block: u.block, span: s, unit: u, use: g.terms[best.term].Name}
			avoided = append(avoided, h)
			continue
		}
		uses = append(uses, found{field: u.field, block: u.block, term: best.term, span: s})
	}
	return avoided, uses
}

func blank(s string) bool { return strings.TrimFunc(s, unicode.IsSpace) == "" }

// explicit is every range an author linked to a term the glossaries hold; a link to a term they do not hold,
// deleted or out of reach, is plain words.
func (g *glossary) explicit(blocks []document.TextBlock) []found {
	var out []found
	for b, block := range blocks {
		for _, m := range block.Marks {
			if m.Type != document.MarkTerm {
				continue
			}
			raw, _ := m.Attrs["termId"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				continue
			}
			if i, ok := g.byID[id]; ok {
				out = append(out, found{field: FieldStatement, block: b, term: i, explicit: true, span: span{m.From, m.To}})
			}
		}
	}
	return out
}

// uses orders the terms found, adds the explicit links, which stand for any match of their term inside them,
// and counts them in editor offsets.
func (g *glossary) uses(words []found, title string, blocks []document.TextBlock) []TermUse {
	links := g.explicit(blocks)
	all := slices.Clone(links)
	for _, w := range words {
		covered := slices.ContainsFunc(links, func(l found) bool {
			return l.block == w.block && l.term == w.term && w.field == FieldStatement && l.from <= w.from && w.to <= l.to
		})
		if !covered {
			all = append(all, w)
		}
	}
	slices.SortStableFunc(all, func(a, b found) int {
		return cmp.Or(cmp.Compare(fieldIndex(a.field), fieldIndex(b.field)), cmp.Compare(a.block, b.block),
			cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to),
			strings.Compare(g.terms[a.term].ID.String(), g.terms[b.term].ID.String()), compareBool(b.explicit, a.explicit))
	})
	out := make([]TermUse, 0, len(all))
	for i, f := range all {
		if i > 0 {
			p := all[i-1]
			if p.field == f.field && p.block == f.block && p.span == f.span && g.terms[p.term].ID == g.terms[f.term].ID {
				continue
			}
		}
		text := title
		use := TermUse{TermID: g.terms[f.term].ID, Span: Span{Field: f.field, Block: f.block}, Explicit: f.explicit}
		if f.field == FieldTitle {
			use.From, use.To = utf16Len(title[:f.from]), utf16Len(title[:f.to])
		} else {
			text = blocks[f.block].Text
			use.From, use.To = blocks[f.block].Position(f.from), blocks[f.block].Position(f.to)
		}
		use.Words = clip(text[f.from:f.to], MaxMatchChars)
		out = append(out, use)
	}
	return out
}
