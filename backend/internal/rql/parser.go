package rql

import (
	"strings"
	"unicode/utf8"
)

// keywords cannot be field names or bare values; a status called "In" has to
// be quoted, which is the price of a language without a reserved-word table.
var keywords = map[string]bool{
	"AND": true, "OR": true, "NOT": true, "IN": true, "IS": true, "EMPTY": true,
	"ORDER": true, "BY": true, "ASC": true, "DESC": true,
}

// Limits on a query, which Armature's language leaves unbounded: the text a person types, how deep brackets and
// NOT nest, and how many conditions one query holds.
const (
	MaxLength = 2000
	MaxDepth  = 32
	MaxTerms  = 100
)

type parser struct {
	toks  []token
	i     int
	depth int
	terms int
}

// Parse reads a query. The text is only ever inspected here; the compiler
// works on what this returns.
func Parse(text string) (*Query, error) {
	if n := utf8.RuneCountInString(text); n > MaxLength {
		return nil, errAt(MaxLength+1, "Keep the query to %d characters; this one has %d.", MaxLength, n)
	}
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q := &Query{}
	if !p.at(kEOF) && !p.keyword("ORDER") {
		q.Where, err = p.or()
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("ORDER") {
		p.next()
		if !p.keyword("BY") {
			return nil, errAt(p.peek().pos, "ORDER is followed by BY, then the field to sort by.")
		}
		p.next()
		for {
			field, err := p.field()
			if err != nil {
				return nil, err
			}
			o := OrderBy{Field: field}
			if p.keyword("DESC") {
				o.Desc = true
				p.next()
			} else if p.keyword("ASC") {
				p.next()
			}
			q.Order = append(q.Order, o)
			if !p.at(kComma) {
				break
			}
			p.next()
		}
	}
	if !p.at(kEOF) {
		return nil, errAt(p.peek().pos, "%q was not expected here. Join conditions with AND or OR.", p.peek().text)
	}
	return q, nil
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) at(k kind) bool { return p.peek().kind == k }

func (p *parser) keyword(word string) bool {
	t := p.peek()
	return t.kind == kWord && strings.EqualFold(t.text, word)
}

func (p *parser) or() (Expr, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.keyword("OR") {
		p.next()
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		left = Or{left, right}
	}
	return left, nil
}

func (p *parser) and() (Expr, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.keyword("AND") {
		p.next()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		left = And{left, right}
	}
	return left, nil
}

func (p *parser) unary() (Expr, error) {
	if p.depth++; p.depth > MaxDepth {
		return nil, errAt(p.peek().pos, "Brackets and NOT nest at most %d deep here. Write the query flatter.", MaxDepth)
	}
	defer func() { p.depth-- }()
	if p.keyword("NOT") {
		p.next()
		inner, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Not{inner}, nil
	}
	if p.at(kLParen) {
		open := p.next()
		inner, err := p.or()
		if err != nil {
			return nil, err
		}
		if !p.at(kRParen) {
			return nil, errAt(open.pos, "The bracket opened at character %d is never closed.", open.pos)
		}
		p.next()
		return inner, nil
	}
	return p.predicate()
}

func (p *parser) predicate() (Expr, error) {
	if p.terms++; p.terms > MaxTerms {
		return nil, errAt(p.peek().pos, "A query holds at most %d conditions. Split it, or use IN for a list.", MaxTerms)
	}
	field, err := p.field()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	switch {
	case t.kind == kOp:
		p.next()
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		// "= EMPTY" reads as well as "IS EMPTY", and means the same.
		if value.Kind == VWord && strings.EqualFold(value.Text, "EMPTY") && (t.text == "=" || t.text == "!=") {
			return Empty{Field: field, Not: t.text == "!="}, nil
		}
		return Compare{Field: field, Op: t.text, Value: value}, nil
	case p.keyword("IN"), p.keyword("NOT"):
		in := In{Field: field}
		if p.keyword("NOT") {
			p.next()
			in.Not = true
			if !p.keyword("IN") {
				return nil, errAt(p.peek().pos, "NOT after a field is followed by IN.")
			}
		}
		p.next()
		if !p.at(kLParen) {
			return nil, errAt(p.peek().pos, "IN is followed by a list in brackets, such as IN (a, b).")
		}
		p.next()
		for {
			value, err := p.value()
			if err != nil {
				return nil, err
			}
			in.Values = append(in.Values, value)
			if p.at(kComma) {
				p.next()
				continue
			}
			break
		}
		if !p.at(kRParen) {
			return nil, errAt(p.peek().pos, "The list is missing its closing bracket.")
		}
		p.next()
		return in, nil
	case p.keyword("IS"):
		p.next()
		e := Empty{Field: field}
		if p.keyword("NOT") {
			p.next()
			e.Not = true
		}
		if !p.keyword("EMPTY") {
			return nil, errAt(p.peek().pos, "IS is followed by EMPTY or NOT EMPTY.")
		}
		p.next()
		return e, nil
	case t.kind == kEOF:
		return nil, errAt(t.pos, "The query ends where an operator was expected after %s.", field)
	default:
		return nil, errAt(t.pos, "%s needs an operator after it, such as = or IN.", field)
	}
}

func (p *parser) field() (Field, error) {
	t := p.peek()
	switch {
	case t.kind == kString:
		p.next()
		return Field{Name: t.text, Custom: true, Pos: t.pos}, nil
	case t.kind == kWord && !keywords[strings.ToUpper(t.text)]:
		p.next()
		return Field{Name: t.text, Pos: t.pos}, nil
	case t.kind == kEOF:
		return Field{}, errAt(t.pos, "The query ends where a field name was expected.")
	default:
		return Field{}, errAt(t.pos, "A field name was expected, but %q was found.", t.text)
	}
}

func (p *parser) value() (Value, error) {
	t := p.peek()
	switch t.kind {
	case kString:
		p.next()
		return Value{Kind: VString, Text: t.text, Pos: t.pos}, nil
	case kNumber:
		p.next()
		return Value{Kind: VNumber, Text: t.text, Pos: t.pos}, nil
	case kDate:
		p.next()
		return Value{Kind: VDate, Text: t.text, Pos: t.pos}, nil
	case kDuration:
		p.next()
		return Value{Kind: VDuration, Text: t.text, Pos: t.pos}, nil
	case kWord:
		if keywords[strings.ToUpper(t.text)] && !strings.EqualFold(t.text, "EMPTY") {
			return Value{}, errAt(t.pos, "A value was expected before %q.", t.text)
		}
		p.next()
		if !p.at(kLParen) {
			return Value{Kind: VWord, Text: t.text, Pos: t.pos}, nil
		}
		p.next()
		fn := Value{Kind: VFunction, Text: t.text, Pos: t.pos}
		if p.at(kDuration) {
			arg := p.next()
			fn.Arg = &Value{Kind: VDuration, Text: arg.text, Pos: arg.pos}
		}
		if !p.at(kRParen) {
			return Value{}, errAt(p.peek().pos, "%s() takes at most a duration such as -1w, then a closing bracket.", t.text)
		}
		p.next()
		return fn, nil
	case kEOF:
		return Value{}, errAt(t.pos, "The query ends where a value was expected.")
	default:
		return Value{}, errAt(t.pos, "A value was expected, but %q was found.", t.text)
	}
}
