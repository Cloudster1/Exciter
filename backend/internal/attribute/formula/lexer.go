package formula

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNumber
	// tText is words in single quotes, 'approved'.
	tText
	// tName is an attribute named in double quotes, "Weight".
	tName
	tIdent
	tOp
	tLParen
	tRParen
	tComma
)

type token struct {
	kind tokenKind
	text string
	// from and to are the 1-based characters the token spans.
	from, to int
}

// errorAt is a mistake between two characters, said as a sentence.
func errorAt(from, to int, format string, args ...any) *Error {
	return &Error{From: from, To: to, Message: fmt.Sprintf(format, args...)}
}

// lex splits a formula into tokens, counting characters rather than bytes, since a person reads them under the text.
func lex(text string) ([]token, *Error) {
	runes := []rune(text)
	var out []token
	for i := 0; i < len(runes); {
		r := runes[i]
		pos := i + 1
		peek := rune(0)
		if i+1 < len(runes) {
			peek = runes[i+1]
		}
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			out = append(out, token{tLParen, "(", pos, pos})
			i++
		case r == ')':
			out = append(out, token{tRParen, ")", pos, pos})
			i++
		case r == ',':
			out = append(out, token{tComma, ",", pos, pos})
			i++
		case strings.ContainsRune("+-*/=", r):
			out = append(out, token{tOp, string(r), pos, pos})
			i++
		case r == '!' && peek == '=', r == '<' && peek == '=', r == '>' && peek == '=':
			out = append(out, token{tOp, string(r) + "=", pos, pos + 1})
			i += 2
		case r == '<' || r == '>':
			out = append(out, token{tOp, string(r), pos, pos})
			i++
		case r == '\'' || r == '"':
			var b strings.Builder
			j := i + 1
			for ; j < len(runes) && runes[j] != r; j++ {
				if runes[j] == '\\' && j+1 < len(runes) {
					j++
				}
				b.WriteRune(runes[j])
			}
			if j >= len(runes) {
				return nil, errorAt(pos, len(runes), "The quote opened here is never closed. Close it with %c.", r)
			}
			kind := tText
			if r == '"' {
				kind = tName
			}
			out = append(out, token{kind, b.String(), pos, j + 1})
			i = j + 1
		case unicode.IsDigit(r) || (r == '.' && unicode.IsDigit(peek)):
			j := i
			dot := false
			for j < len(runes) && (unicode.IsDigit(runes[j]) || (runes[j] == '.' && !dot)) {
				dot = dot || runes[j] == '.'
				j++
			}
			out = append(out, token{tNumber, string(runes[i:j]), pos, j})
			i = j
		case unicode.IsLetter(r) || r == '_':
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_') {
				j++
			}
			out = append(out, token{tIdent, string(runes[i:j]), pos, j})
			i = j
		default:
			return nil, errorAt(pos, pos, "%q has no meaning in a formula. Take it out.", string(r))
		}
	}
	return append(out, token{tEOF, "", len(runes) + 1, len(runes) + 1}), nil
}
