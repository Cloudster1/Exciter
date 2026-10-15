package rql

import (
	"regexp"
	"strings"
	"unicode"
)

type kind int

const (
	kEOF kind = iota
	kWord
	kString
	kNumber
	kDate
	kDuration
	kOp
	kLParen
	kRParen
	kComma
)

type token struct {
	kind kind
	text string
	// pos is the 1-based character the token starts at.
	pos int
}

var (
	datePattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}(:\d{2})?)?$`)
	durationPattern = regexp.MustCompile(`^[+-]?\d+[hdw]$`)
	numberPattern   = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)
)

func isWordStart(r rune) bool { return unicode.IsLetter(r) || r == '_' || r == '@' }

// isWordChar admits what keys, names, emails and times are made of.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_.-@:", r)
}

// lex splits a query into tokens. Positions are counted in characters, not
// bytes, because they are shown to a person under the text they typed.
func lex(text string) ([]token, error) {
	runes := []rune(text)
	var out []token
	for i := 0; i < len(runes); {
		r := runes[i]
		pos := i + 1
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			out = append(out, token{kLParen, "(", pos})
			i++
		case r == ')':
			out = append(out, token{kRParen, ")", pos})
			i++
		case r == ',':
			out = append(out, token{kComma, ",", pos})
			i++
		case r == '"' || r == '\'':
			var b strings.Builder
			j := i + 1
			closed := false
			for j < len(runes) {
				if runes[j] == '\\' && j+1 < len(runes) {
					b.WriteRune(runes[j+1])
					j += 2
					continue
				}
				if runes[j] == r {
					closed = true
					break
				}
				b.WriteRune(runes[j])
				j++
			}
			if !closed {
				return nil, errAt(pos, "The quote opened at character %d is never closed.", pos)
			}
			out = append(out, token{kString, b.String(), pos})
			i = j + 1
		case r == '=' || r == '~':
			out = append(out, token{kOp, string(r), pos})
			i++
		case r == '!':
			if next != '=' && next != '~' {
				return nil, errAt(pos, "\"!\" on its own means nothing. Write != or !~.")
			}
			out = append(out, token{kOp, string(r) + string(next), pos})
			i += 2
		case r == '<' || r == '>':
			if next == '=' {
				out = append(out, token{kOp, string(r) + "=", pos})
				i += 2
			} else {
				out = append(out, token{kOp, string(r), pos})
				i++
			}
		case isWordStart(r) || unicode.IsDigit(r) || ((r == '-' || r == '+') && unicode.IsDigit(next)):
			j := i + 1
			for j < len(runes) && isWordChar(runes[j]) {
				j++
			}
			word := string(runes[i:j])
			switch {
			case isWordStart(r):
				out = append(out, token{kWord, word, pos})
			case datePattern.MatchString(word):
				out = append(out, token{kDate, word, pos})
			case durationPattern.MatchString(word):
				out = append(out, token{kDuration, word, pos})
			case numberPattern.MatchString(word):
				out = append(out, token{kNumber, word, pos})
			default:
				return nil, errAt(pos, "%q is not a number, a date or a duration. Quote it if it is text.", word)
			}
			i = j
		default:
			return nil, errAt(pos, "%q cannot be used here.", string(r))
		}
	}
	out = append(out, token{kEOF, "", len(runes) + 1})
	return out, nil
}
