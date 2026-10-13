package trace

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// KeyFromName is the key a type gets when its maker names none: its name in lower case, words joined by _.
func KeyFromName(name string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if gap && b.Len() > 0 {
				b.WriteByte('_')
			}
			gap = false
			b.WriteRune(r)
		default:
			gap = true
		}
	}
	key := strings.TrimLeft(b.String(), "0123456789_")
	if len(key) > MaxKeyLength {
		key = strings.TrimRight(key[:MaxKeyLength], "_")
	}
	return key
}

func checkText(field, value string, limit int, empty string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &FieldError{field, empty}
	}
	if utf8.RuneCountInString(value) > limit {
		return "", &FieldError{field, fmt.Sprintf("Keep it to %d characters.", limit)}
	}
	return value, nil
}

// applyType reads in onto t, checking each field; making a type needs a name and both phrases, and a key
// is taken from the name unless one is given. A type keeps its key.
func applyType(t *LinkType, in LinkTypeInput, creating bool) error {
	var err error
	if creating {
		if in.Name == nil {
			return &FieldError{"name", "Give the link type a name, such as Derives."}
		}
		if in.Forward == nil {
			return &FieldError{"forward", "Say how a link reads from its source, such as derives from."}
		}
		if in.Backward == nil {
			return &FieldError{"backward", "Say how a link reads from its target, such as is derived into."}
		}
		t.SuspectEnd, t.Counts = SuspectNone, CountsNothing
	}
	if in.Name != nil {
		if t.Name, err = checkText("name", *in.Name, MaxNameLength, "Give the link type a name, such as Derives."); err != nil {
			return err
		}
	}
	if in.Forward != nil {
		if t.Forward, err = checkText("forward", *in.Forward, MaxPhraseLength, "Say how a link reads from its source, such as derives from."); err != nil {
			return err
		}
	}
	if in.Backward != nil {
		if t.Backward, err = checkText("backward", *in.Backward, MaxPhraseLength, "Say how a link reads from its target, such as is derived into."); err != nil {
			return err
		}
	}
	key := strings.TrimSpace(in.Key)
	switch {
	case creating && key == "":
		if t.Key = KeyFromName(t.Name); !keyRe.MatchString(t.Key) {
			return &FieldError{"key", "Give the link type a key of letters, such as derives; its name has too few to make one."}
		}
	case creating:
		if !keyRe.MatchString(key) {
			return &FieldError{"key", "A key is 2 to 40 lower-case letters, digits or underscores and starts with a letter, such as derives."}
		}
		t.Key = key
	case key != "" && key != t.Key:
		return &FieldError{"key", "A link type keeps its key, which imports and scripts name it by. Make a new type for another key."}
	}
	if in.Acyclic != nil {
		t.Acyclic = *in.Acyclic
	}
	if in.SuspectEnd != nil {
		if !slices.Contains(SuspectEnds, *in.SuspectEnd) {
			return &FieldError{"suspectEnd", "Pick which end's change makes a link suspect: target, source, both or none."}
		}
		t.SuspectEnd = *in.SuspectEnd
	}
	if in.Counts != nil {
		if !slices.Contains(CountsValues, *in.Counts) {
			return &FieldError{"counts", "Pick what a link counts for: decomposition, verification or nothing."}
		}
		t.Counts = *in.Counts
	}
	if in.Retired != nil {
		t.Retired = *in.Retired
	}
	return nil
}

// checkRules drops repeated rules and refuses requirement types that are not the project's.
func checkRules(rules []TypeRule, types map[uuid.UUID]string) ([]TypeRule, error) {
	out := []TypeRule{}
	for _, rule := range rules {
		for _, end := range []*uuid.UUID{rule.SourceTypeID, rule.TargetTypeID} {
			if end != nil {
				if _, ok := types[*end]; !ok {
					return nil, &FieldError{"rules", "A rule names requirement types of this project. Reload the types and pick again."}
				}
			}
		}
		if !slices.ContainsFunc(out, func(each TypeRule) bool {
			return sameEnd(each.SourceTypeID, rule.SourceTypeID) && sameEnd(each.TargetTypeID, rule.TargetTypeID)
		}) {
			out = append(out, rule)
		}
	}
	if len(out) > MaxRules {
		return nil, &FieldError{"rules", fmt.Sprintf("A link type takes at most %d rules in a project. Leave some out, or allow any pair.", MaxRules)}
	}
	return out, nil
}

func sameEnd(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Allows says whether rules let a link join a source of one requirement type to a target of another; no rule
// is any pair, and a target in another project is not the rules' to judge, so only the source's type counts.
func Allows(rules []TypeRule, source, target *uuid.UUID, sameProject bool) bool {
	if len(rules) == 0 {
		return true
	}
	matches := func(end, typ *uuid.UUID) bool { return end == nil || (typ != nil && *end == *typ) }
	return slices.ContainsFunc(rules, func(rule TypeRule) bool {
		return matches(rule.SourceTypeID, source) && (!sameProject || matches(rule.TargetTypeID, target))
	})
}

// Symmetric is a type that reads the same from both ends, where B to A is the link A to B.
func (t LinkType) Symmetric() bool { return t.Forward == t.Backward }

// phrase is the type read from one end of a link.
func (t LinkType) phrase(d Direction) string {
	if d == Incoming {
		return t.Backward
	}
	return t.Forward
}

// cycleSentence says why a link would close a circle: the target already reaches the source through between.
func cycleSentence(t LinkType, targetKey, sourceKey string, between []string) string {
	through := ""
	if len(between) > 0 {
		through = " through " + strings.Join(between, ", ")
	}
	return fmt.Sprintf("%s already %s %s%s, so this link would close a circle. Link another requirement, or remove one of those links first.",
		targetKey, t.Forward, sourceKey, through)
}
