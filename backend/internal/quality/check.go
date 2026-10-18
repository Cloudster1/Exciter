package quality

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// Input is a requirement's words and what a project's settings add to the rules.
type Input struct {
	Language  Language
	Title     string
	Statement document.Node
	// Words are the project's own additions to and exceptions from the word rules' lists.
	Words map[RuleID]Words
	// Terms are the glossaries' terms, whose words the requirement may use or must avoid.
	Terms []Term
}

// Hit is what a rule found, before a project's settings judge it.
type Hit struct {
	Key  string
	Rule RuleID
	Span
	Match string
	// Use is the name of the term to write instead, for a word the glossary says not to use.
	Use string
}

// Result is every hit in field, block and position order, the shape of each sentence that says what shall be,
// and the terms used in field, block and position order.
type Result struct {
	Hits      []Hit
	Sentences []Sentence
	Uses      []TermUse
}

const (
	// maxKeyMatch bounds the bytes of words a key names, so a whole key stays under 200.
	maxKeyMatch = 160
	// sentenceHashBytes of a sentence's hash name it in a structural rule's key.
	sentenceHashBytes = 4
)

// wordRules find words; another word rule's longer find around them is the better finding.
var wordRules = set(string(RuleVague), string(RuleUnbounded), string(RuleEscape), string(RulePlaceholder))

// Examine runs every rule over a title and statement; it reads nothing else and is the same every time.
func Examine(in Input) Result {
	lang := language(in.Language)
	c := newChecker(lang, in.Words)
	g := newGlossary(lang, in.Terms)
	var raws []raw
	var uses []found
	if title := strings.TrimSpace(in.Title); title != "" {
		lead := strings.Index(in.Title, title)
		u := newUnit(FieldTitle, 0, in.Title, span{lead, lead + len(title)})
		raws = append(raws, c.sentenceHits(u, true, nil)...)
		r, f := g.find(u, nil)
		raws, uses = append(raws, r...), append(uses, f...)
	}
	blocks := document.TextBlocks(in.Statement)
	var units []*unit
	for b, block := range blocks {
		if block.Code {
			continue
		}
		for _, s := range splitSentences(block.Text) {
			units = append(units, newUnit(FieldStatement, b, block.Text, s))
		}
	}
	var sentences []Sentence
	position := func(u *unit, at int) int { return blocks[u.block].Position(at) }
	modal := false
	for i, u := range units {
		raws = append(raws, c.sentenceHits(u, i == 0, &blocks[u.block])...)
		r, f := g.find(u, &blocks[u.block])
		raws, uses = append(raws, r...), append(uses, f...)
		pattern, found, ok := c.ears(u)
		if !ok {
			continue
		}
		modal = true
		raws = append(raws, found...)
		sentences = append(sentences, Sentence{
			Span:    Span{Field: FieldStatement, Block: u.block, From: position(u, u.from), To: position(u, u.to)},
			Pattern: pattern,
		})
	}
	if !modal && len(units) > 0 {
		u := units[0]
		raws = append(raws, c.hit(u, RuleEARS, u.from, u.to))
	}
	return Result{Hits: hits(raws, in.Title, blocks), Sentences: sentences, Uses: g.uses(uses, in.Title, blocks)}
}

// sentenceHits runs the rules that read one sentence at a time, and drops what a person's name or a longer
// finding holds.
func (c *checker) sentenceHits(u *unit, first bool, block *document.TextBlock) []raw {
	found := c.wordHits(u)
	found = append(found, c.multipleShall(u)...)
	found = append(found, c.negatives(u)...)
	found = append(found, c.passive(u)...)
	found = append(found, c.pronouns(u, first)...)
	// Each word rule's finds are in order and do not overlap, so the one around a word is found by search.
	byRule := map[RuleID][]span{}
	for _, h := range found {
		if wordRules[string(h.rule)] {
			byRule[h.rule] = append(byRule[h.rule], h.span)
		}
	}
	inside := func(h raw) bool {
		for _, rule := range WordRules {
			spans := byRule[rule]
			if rule == h.rule || len(spans) == 0 {
				continue
			}
			i := sort.Search(len(spans), func(i int) bool { return spans[i].from > h.from }) - 1
			if i >= 0 && spans[i].to >= h.to && spans[i].to-spans[i].from > h.to-h.from {
				return true
			}
		}
		return false
	}
	out := found[:0:0]
	seen := map[raw]bool{}
	for _, h := range found {
		if seen[h] || (block != nil && block.Mentioned(h.from, h.to)) {
			continue
		}
		seen[h] = true
		if (wordRules[string(h.rule)] || h.rule == RulePronoun) && inside(h) {
			continue
		}
		out = append(out, h)
	}
	return out
}

func ruleIndex(r RuleID) int { return slices.Index(RuleIDs, r) }

func fieldIndex(f Field) int { return slices.Index(Fields, f) }

// hits orders what the rules found, counts it in editor offsets and gives each its key.
func hits(raws []raw, title string, blocks []document.TextBlock) []Hit {
	slices.SortStableFunc(raws, func(a, b raw) int {
		return cmp.Or(cmp.Compare(fieldIndex(a.field), fieldIndex(b.field)), cmp.Compare(a.block, b.block),
			cmp.Compare(a.from, b.from), cmp.Compare(ruleIndex(a.rule), ruleIndex(b.rule)), cmp.Compare(a.to, b.to))
	})
	out := make([]Hit, 0, len(raws))
	seen := map[string]int{}
	digests := map[*unit][]byte{}
	for _, r := range raws {
		text := title
		span := Span{Field: r.field, Block: r.block}
		if r.field == FieldTitle {
			span.From, span.To = utf16Len(title[:r.from]), utf16Len(title[:r.to])
		} else {
			text = blocks[r.block].Text
			span.From, span.To = blocks[r.block].Position(r.from), blocks[r.block].Position(r.to)
		}
		match := text[r.from:r.to]
		base := fmt.Sprintf("%s:%s:%s", r.rule, r.field, keyPart(r, match, digests))
		out = append(out, Hit{Key: fmt.Sprintf("%s:%d", base, seen[base]), Rule: r.rule, Span: span, Match: clip(match, MaxMatchChars), Use: r.use})
		seen[base]++
	}
	return out
}

// keyPart names the words a word rule found, and for a rule of shape its sentence too, so a key stays while
// other sentences change.
func keyPart(r raw, match string, digests map[*unit][]byte) string {
	words := normalized(match)
	if !structural[string(r.rule)] {
		return clipBytes(words, maxKeyMatch)
	}
	// A sentence is hashed once however much is found in it.
	digest, ok := digests[r.unit]
	if !ok {
		sum := sha256.Sum256([]byte(normalized(r.unit.sentence())))
		digest = sum[:]
		digests[r.unit] = digest
	}
	sum := sha256.Sum256(append(slices.Clone(digest), words...))
	return hex.EncodeToString(sum[:sentenceHashBytes])
}

func clip(s string, chars int) string {
	if utf8.RuneCountInString(s) <= chars {
		return s
	}
	return string([]rune(s)[:chars])
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
