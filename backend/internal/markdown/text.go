package markdown

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/cloudster1/exciter/backend/internal/document"
)

func span(kind string, attrs [][2]string, inner string) string {
	var b strings.Builder
	b.WriteString("<span " + exciterAttr + `="` + kind + `"`)
	for _, a := range attrs {
		b.WriteString(" " + a[0] + `="` + htmlText(a[1]) + `"`)
	}
	return b.String() + ">" + inner + "</span>"
}

// htmlText escapes text for an HTML element on one line: a raw line break would end the HTML block it stands in.
func htmlText(s string) string {
	return strings.NewReplacer("\n", "&#10;", "\r", "&#13;").Replace(html.EscapeString(s))
}

var entityLike = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

var lineStartList = regexp.MustCompile(`^[0-9]{1,9}[.)]`)

// escape keeps text from being read as Markdown: what only means something at the start of a line is escaped
// there, the rest wherever it could mean it.
func escape(s string, mode escMode, atStart bool) string {
	var b strings.Builder
	if atStart {
		if m := lineStartList.FindString(s); m != "" {
			b.WriteString(m[:len(m)-1] + `\` + m[len(m)-1:])
			s = s[len(m):]
			atStart = false
		}
	}
	runes := []rune(s)
	for i, c := range runes {
		switch c {
		case '\\', '`', '*', '[', ']', '~':
			b.WriteRune('\\')
		case '_':
			if i == 0 || i == len(runes)-1 || !isWord(runes[i-1]) || !isWord(runes[i+1]) {
				b.WriteRune('\\')
			}
		case '<':
			if i+1 < len(runes) && (unicode.IsLetter(runes[i+1]) || strings.ContainsRune("/!?", runes[i+1])) {
				b.WriteRune('\\')
			}
		case '&':
			if entityLike.MatchString(string(runes[i:min(len(runes), i+40)])) {
				b.WriteRune('\\')
			}
		case '|':
			if mode == escTable {
				b.WriteRune('\\')
			}
		case '#':
			if mode == escHeading || (i == 0 && atStart) {
				b.WriteRune('\\')
			}
		case '-', '+', '=', '>':
			if i == 0 && atStart {
				b.WriteRune('\\')
			}
		}
		b.WriteRune(c)
	}
	return b.String()
}

func isWord(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }

// codeSpan fences inline code with one backtick more than the longest run inside it, padded where a backtick or
// space would otherwise be lost.
func codeSpan(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	fence := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") ||
		(strings.HasPrefix(s, " ") && strings.HasSuffix(s, " ") && strings.TrimSpace(s) != "") {
		s = " " + s + " "
	}
	return fence + s + fence
}

func codeFence(text, language string) string {
	fence := strings.Repeat("`", max(3, longestRun(text, '`')+1))
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return fence + language + "\n" + fence
	}
	return fence + language + "\n" + text + "\n" + fence
}

func longestRun(s string, c rune) int {
	best, run := 0, 0
	for _, r := range s {
		if r == c {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

// destination writes a link target Markdown reads back whole: in angle brackets when it holds a space or a
// parenthesis.
func destination(href string) string {
	if !strings.ContainsAny(href, " ()<>") {
		return href
	}
	return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(href) + ">"
}

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}
	return strings.Join(lines, "\n")
}

// prefixLines puts first before the first line and rest before every other line that is not blank.
func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		switch {
		case i == 0:
			lines[i] = first + line
		case line != "":
			lines[i] = rest + line
		}
	}
	return strings.Join(lines, "\n")
}

func textOf(n document.Node) string {
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func stringAttr(n document.Node, name string) string {
	s, _ := n.Attrs[name].(string)
	return s
}

func intAttr(n document.Node, name string, fallback int) int {
	switch v := n.Attrs[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

// Words writes text as Markdown that reads as those words on one line, wherever in a line it stands.
func Words(s string) string {
	return escape(strings.Join(strings.Fields(s), " "), escText, false)
}

// Heading writes text as a heading of the level, at most 6, that reads as those words alone.
func Heading(level int, s string) string {
	return strings.Repeat("#", max(1, min(level, maxHeadingLevel))) + " " + escape(strings.Join(strings.Fields(s), " "), escHeading, false)
}

// Paragraphs writes plain text as Markdown that reads as it, each of its lines a paragraph.
func Paragraphs(s string) string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, escape(line, escText, true))
		}
	}
	return strings.Join(out, "\n\n")
}

// maxHeadingLevel is the deepest heading Markdown writes with hashes.
const maxHeadingLevel = 6
