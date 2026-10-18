package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// ruleCap is how many times its weight one rule takes from the score at most.
const ruleCap = 2

// Judged is a hit with the project's verdict on it.
type Judged struct {
	Rule     RuleID
	Severity Severity
	Active   bool
}

// Weight is what one active finding of a severity takes from the score.
func Weight(s Severity) int {
	switch s {
	case SeverityError:
		return WeightError
	case SeverityWarning:
		return WeightWarning
	case SeverityHint:
		return WeightHint
	}
	return 0
}

// Score is FullScore less each rule's active weights, capped at twice the rule's weight per rule, and 0 at least.
func Score(findings []Judged) int {
	taken, most := map[RuleID]int{}, map[RuleID]int{}
	for _, f := range findings {
		if !f.Active {
			continue
		}
		w := Weight(f.Severity)
		taken[f.Rule] += w
		most[f.Rule] = max(most[f.Rule], ruleCap*w)
	}
	score := FullScore
	for rule, w := range taken {
		score -= min(w, most[rule])
	}
	return max(score, 0)
}

// ConfigHash names what a check depended on beyond the text: Version, language, word lists and the glossaries'
// terms, in any order. Hex sha256.
func ConfigHash(lang Language, words map[RuleID]Words, terms []Term) string {
	var b strings.Builder
	fmt.Fprintf(&b, "version %d\nlanguage %s\n", Version, language(lang))
	rules := make([]RuleID, 0, len(words))
	for rule := range words {
		rules = append(rules, rule)
	}
	slices.Sort(rules)
	for _, rule := range rules {
		add, except := listed(words[rule].Add, false), listed(words[rule].Except, true)
		if len(add)+len(except) > 0 {
			fmt.Fprintf(&b, "rule %q add %q except %q\n", rule, add, except)
		}
	}
	// A use names its term by id, and the rank picks the term two glossaries share a word with.
	lines := make([]string, 0, len(terms))
	for _, t := range terms {
		lines = append(lines, fmt.Sprintf("term %s rank %d name %q synonyms %q avoid %q", t.ID, t.Rank,
			strings.Join(strings.Fields(t.Name), " "), listed(t.Synonyms, false), listed(t.Avoid, false)))
	}
	slices.Sort(lines)
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// listed is a word list as the rules read it: spaces collapsed, sorted and once each; an exception ignores case.
func listed(words []string, fold bool) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if fold {
			w = normalized(w)
		} else {
			w = strings.Join(strings.Fields(w), " ")
		}
		if w != "" {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
