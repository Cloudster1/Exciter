package search

import (
	"fmt"
	"strings"
	"unicode"
)

// ts_headline marks matches with these. They are private use characters, which
// no keyboard types, and are stripped from the text before it is marked, so a
// requirement cannot forge a match through them. ts_headline drops anything that
// reads as an HTML tag, so a requirement's own "<" stands in as openAngle meanwhile.
const (
	startSel  = "\uE000"
	stopSel   = "\uE001"
	openAngle = "\uE002"
	nameGap   = "\uE003"
)

// Snippet sizes, in words: about thirty around the matches, or the statement's
// first words when only the title matched.
const (
	snippetMinWords = 20
	snippetMaxWords = 32
)

var (
	headlineOptions = fmt.Sprintf("StartSel=%s, StopSel=%s, MinWords=%d, MaxWords=%d", startSel, stopSel, snippetMinWords, snippetMaxWords)
	titleOptions    = fmt.Sprintf("StartSel=%s, StopSel=%s, HighlightAll=true", startSel, stopSel)
)

// Split cuts text marked by ts_headline into plain runs, the matched ones
// flagged. Whitespace collapses to single spaces, a gap of only spaces
// between two matches joins them, and a stray delimiter is dropped.
func Split(marked string) []Segment {
	out := []Segment{}
	add := func(text string, match bool) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Match == match {
			out[n-1].Text += text
			return
		}
		out = append(out, Segment{Text: text, Match: match})
	}
	var run strings.Builder
	match := false
	flush := func() {
		add(run.String(), match)
		run.Reset()
	}
	space := false
	for _, r := range strings.TrimSpace(marked) {
		switch {
		case string(r) == startSel:
			if !match {
				flush()
				match = true
			}
		case string(r) == stopSel:
			if match {
				flush()
				match = false
			}
		case string(r) == openAngle:
			run.WriteByte('<')
		case string(r) == nameGap:
			continue
		case unicode.IsSpace(r):
			if !space {
				run.WriteByte(' ')
			}
			space = true
			continue
		default:
			run.WriteRune(r)
		}
		space = false
	}
	flush()
	return joinMatches(out)
}

// joinMatches makes "a" " " "b", two matches a space apart, one match, as
// a phrase reads.
func joinMatches(in []Segment) []Segment {
	out := make([]Segment, 0, len(in))
	for i := 0; i < len(in); i++ {
		s := in[i]
		if n := len(out); n > 0 && out[n-1].Match && s.Match {
			out[n-1].Text += s.Text
			continue
		}
		if n := len(out); n > 0 && out[n-1].Match && !s.Match && strings.TrimSpace(s.Text) == "" && i+1 < len(in) && in[i+1].Match {
			out[n-1].Text += s.Text + in[i+1].Text
			i++
			continue
		}
		out = append(out, s)
	}
	return out
}

// Plain is text as one unmatched segment, whitespace collapsed.
func Plain(text string) []Segment {
	return Split(strings.NewReplacer(startSel, "", stopSel, "", openAngle, "", nameGap, "").Replace(text))
}

// firstWords is the start of a statement, for a hit found by its filters alone.
func firstWords(text string, n int) string {
	words := strings.Fields(text)
	if len(words) > n {
		words = words[:n]
	}
	return strings.Join(words, " ")
}
