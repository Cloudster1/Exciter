package quality

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// minStem is the shortest stem an ending is cut down to, so a short word such as bus or has keeps its letters.
const minStem = 3

// germanPasses is how often an ending is cut from a German word, as in sicheren, sicher, sich.
const germanPasses = 2

// germanSuffixes are the endings German words take, the longest first.
var germanSuffixes = []string{"nen", "ern", "en", "er", "es", "em", "e", "n", "s"}

// germanFolds are the letters a German word is written without, so Zug and Züge meet.
var germanFolds = strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss")

// pluralKeeps are English words ending in -ies that are not plurals of a -y.
var pluralKeeps = set("series", "species")

// Stem is a word cut down to what its inflections share, so a term and its plural, case or tense meet; it is
// crude on purpose, as terms are stemmed the same way. An acronym keeps its capitals, so ABS does not find abs.
func Stem(lang Language, word string) string {
	if capitals(word) {
		return word
	}
	if rest, ok := strings.CutSuffix(word, "s"); ok && capitals(rest) {
		return rest
	}
	w := strings.ToLower(word)
	if language(lang) == German {
		return stemDE(w)
	}
	return stemEN(w)
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// cutEnding takes an ending off when what is left is long enough.
func cutEnding(w, suffix, replace string) (string, bool) {
	rest, ok := strings.CutSuffix(w, suffix)
	if !ok || runes(rest+replace) < minStem {
		return w, false
	}
	return rest + replace, true
}

func stemEN(w string) string {
	w = strings.ReplaceAll(w, string(rightQuote), "'")
	if rest, ok := strings.CutSuffix(w, "'s"); ok {
		w = rest
	} else if rest, ok := strings.CutSuffix(w, "s'"); ok {
		w = rest + "s"
	}
	w = pluralEN(w)
	w = tenseEN(w)
	if rest, ok := cutEnding(w, "e", ""); ok {
		w = rest
	}
	return w
}

func pluralEN(w string) string {
	if strings.HasSuffix(w, "ies") && !pluralKeeps[w] {
		if rest, ok := cutEnding(w, "ies", "y"); ok && runes(w) > len("ies")+1 {
			return rest
		}
	}
	for _, after := range []string{"ss", "x", "z", "ch", "sh", "s"} {
		if strings.HasSuffix(w, after+"es") {
			if rest, ok := cutEnding(w, "es", ""); ok {
				return rest
			}
		}
	}
	for _, keep := range []string{"ss", "us", "is"} {
		if strings.HasSuffix(w, keep) {
			return w
		}
	}
	rest, _ := cutEnding(w, "s", "")
	return rest
}

func tenseEN(w string) string {
	if rest, ok := cutEnding(w, "ied", "y"); ok {
		return rest
	}
	for _, ending := range []string{"ing", "ed"} {
		if ending == "ed" && strings.HasSuffix(w, "eed") {
			continue
		}
		if rest, ok := cutEnding(w, ending, ""); ok {
			return undouble(rest)
		}
	}
	return w
}

// undouble takes back the consonant a tense doubled, as in stopped; a doubled l, s or z is the word's own.
func undouble(w string) string {
	n := len(w)
	if n < 2 || w[n-1] != w[n-2] || runes(w) <= minStem {
		return w
	}
	c := rune(w[n-1])
	if c >= utf8.RuneSelf || !unicode.IsLetter(c) || strings.ContainsRune("aeiouylsz", c) {
		return w
	}
	return w[:n-1]
}

func stemDE(w string) string {
	w = germanFolds.Replace(w)
	for range germanPasses {
		cutOne := false
		for _, suffix := range germanSuffixes {
			if rest, ok := cutEnding(w, suffix, ""); ok {
				w, cutOne = rest, true
				break
			}
		}
		if !cutOne {
			break
		}
	}
	return w
}
