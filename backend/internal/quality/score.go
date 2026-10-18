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

// ConfigHash names what a check depended on beyond the text: Version, language, word lists, avoid words. Hex sha256.
func ConfigHash(lang Language, words map[RuleID]Words, avoid []AvoidTerm) string {
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
	terms := make([]string, 0, len(avoid))
	for _, a := range avoid {
		terms = append(terms, fmt.Sprintf("%q %q", listed([]string{a.Word}, false), strings.TrimSpace(a.Use)))
	}
	slices.Sort(terms)
	for _, t := range terms {
		fmt.Fprintf(&b, "avoid %s\n", t)
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
