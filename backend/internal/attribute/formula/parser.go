package formula

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Fields are a requirement's own fields a formula reads, and the kind of each.
var Fields = map[string]Kind{
	"title": Text, "status": Text, "category": Text, "type": Text, "owner": Text, "priority": Text,
	"revision": Number, "created": Date, "updated": Date, "quality": Number, "testResult": Text,
}

// functions are the calls that read one requirement, and how many arguments each takes, -1 for any number.
var functions = map[string][2]int{
	"if": {3, 3}, "coalesce": {1, -1}, "round": {1, 2}, "floor": {1, 1}, "ceil": {1, 1}, "abs": {1, 1},
	"min": {1, -1}, "max": {1, -1}, "concat": {1, -1}, "lower": {1, 1}, "upper": {1, 1}, "len": {1, 1},
	"today": {0, 0}, "days": {2, 2},
}

// keywords are words a formula reserves.
var keywords = []string{"and", "or", "not", "where", "true", "false", "null", "children", "links", "out", "in"}

type parser struct {
	tokens []token
	at     int
	depth  int
	// inAggregate refuses an aggregate inside another, whose items' items the worker does not read.
	inAggregate bool
}

func (p *parser) peek() token { return p.tokens[p.at] }

func (p *parser) next() token {
	t := p.tokens[p.at]
	if t.kind != tEOF {
		p.at++
	}
	return t
}

func (p *parser) word(w string) bool {
	t := p.peek()
	return t.kind == tIdent && strings.EqualFold(t.text, w)
}

func (p *parser) op(ops ...string) bool {
	t := p.peek()
	return t.kind == tOp && slices.Contains(ops, t.text)
}

func (p *parser) expect(kind tokenKind, what string) (token, *Error) {
	t := p.next()
	if t.kind != kind {
		return t, errorAt(t.from, t.to, "Write %s here.", what)
	}
	return t, nil
}

// parse reads a formula into its tree.
func parse(text string) (node, *Error) {
	if strings.TrimSpace(text) == "" {
		return nil, errorAt(1, 1, "Write a formula, such as \"Weight\" * 2.")
	}
	if n := utf8.RuneCountInString(text); n > MaxLength {
		return nil, errorAt(MaxLength+1, n, "Keep a formula to %d characters.", MaxLength)
	}
	tokens, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, errorAt(t.from, t.to, "The formula should end before %q. Join the parts with an operator, or take this out.", t.text)
	}
	return n, nil
}

func (p *parser) deeper(from int) *Error {
	p.depth++
	if p.depth > MaxDepth {
		return errorAt(from, from, "This formula nests too deeply. Split it into computed attributes of their own.")
	}
	return nil
}

func spanOf(a, b node) at {
	from, _ := a.span()
	_, to := b.span()
	return at{from, to}
}

func (p *parser) or() (node, *Error) {
	x, err := p.and()
	for err == nil && p.word("or") {
		p.next()
		var y node
		if y, err = p.and(); err == nil {
			x = binary{spanOf(x, y), "or", x, y}
		}
	}
	return x, err
}

func (p *parser) and() (node, *Error) {
	x, err := p.not()
	for err == nil && p.word("and") {
		p.next()
		var y node
		if y, err = p.not(); err == nil {
			x = binary{spanOf(x, y), "and", x, y}
		}
	}
	return x, err
}

func (p *parser) not() (node, *Error) {
	if p.word("not") {
		t := p.next()
		if err := p.deeper(t.from); err != nil {
			return nil, err
		}
		x, err := p.not()
		p.depth--
		if err != nil {
			return nil, err
		}
		_, to := x.span()
		return unary{at{t.from, to}, "not", x}, nil
	}
	return p.compare()
}

func (p *parser) compare() (node, *Error) {
	x, err := p.sum()
	if err != nil {
		return nil, err
	}
	if p.op("=", "!=", "<", "<=", ">", ">=") {
		op := p.next().text
		y, err := p.sum()
		if err != nil {
			return nil, err
		}
		x = binary{spanOf(x, y), op, x, y}
	}
	return x, nil
}

func (p *parser) sum() (node, *Error) {
	x, err := p.product()
	for err == nil && p.op("+", "-") {
		op := p.next().text
		var y node
		if y, err = p.product(); err == nil {
			x = binary{spanOf(x, y), op, x, y}
		}
	}
	return x, err
}

func (p *parser) product() (node, *Error) {
	x, err := p.negation()
	for err == nil && p.op("*", "/") {
		op := p.next().text
		var y node
		if y, err = p.negation(); err == nil {
			x = binary{spanOf(x, y), op, x, y}
		}
	}
	return x, err
}

func (p *parser) negation() (node, *Error) {
	if p.op("-") {
		t := p.next()
		x, err := p.negation()
		if err != nil {
			return nil, err
		}
		_, to := x.span()
		return unary{at{t.from, to}, "-", x}, nil
	}
	return p.primary()
}

func (p *parser) primary() (node, *Error) {
	t := p.next()
	switch t.kind {
	case tNumber:
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, errorAt(t.from, t.to, "%s is not a number. Write it as 12 or 2.5.", t.text)
		}
		return numberLit{at{t.from, t.to}, v}, nil
	case tText:
		return textLit{at{t.from, t.to}, t.text}, nil
	case tName:
		return attrRef{at: at{t.from, t.to}, name: t.text}, nil
	case tLParen:
		if err := p.deeper(t.from); err != nil {
			return nil, err
		}
		x, err := p.or()
		p.depth--
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "a closing parenthesis"); err != nil {
			return nil, err
		}
		return x, nil
	case tIdent:
		return p.ident(t)
	case tEOF:
		return nil, errorAt(t.from, t.to, "The formula ends too early. Finish it with a value.")
	}
	return nil, errorAt(t.from, t.to, "%q cannot start a value. Write a number, 'text', a field or an \"attribute\" here.", t.text)
}

func (p *parser) ident(t token) (node, *Error) {
	word := t.text
	lower := strings.ToLower(word)
	switch lower {
	case "true", "false":
		return boolLit{at{t.from, t.to}, lower == "true"}, nil
	case "null":
		return nullLit{at{t.from, t.to}}, nil
	}
	if p.peek().kind != tLParen {
		if _, ok := Fields[word]; ok {
			return fieldRef{at{t.from, t.to}, word}, nil
		}
		if slices.Contains(keywords, lower) {
			return nil, errorAt(t.from, t.to, "%s cannot stand here. Use it inside an aggregate, such as count(children).", word)
		}
		return nil, errorAt(t.from, t.to, "There is no field %s. Use %s, or an attribute in double quotes.", word, strings.Join(fieldNames(), ", "))
	}
	if aggregates(lower) && p.aggregateFollows() {
		return p.aggregate(t, lower)
	}
	arity, ok := functions[lower]
	if !ok {
		return nil, errorAt(t.from, t.to, "There is no function %s. Use %s.", word, strings.Join(functionNames(), ", "))
	}
	p.next()
	if err := p.deeper(t.from); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	var args []node
	for p.peek().kind != tRParen {
		if len(args) > 0 {
			if _, err := p.expect(tComma, "a comma between the arguments"); err != nil {
				return nil, err
			}
		}
		arg, err := p.or()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	end := p.next()
	if len(args) < arity[0] || (arity[1] >= 0 && len(args) > arity[1]) {
		return nil, errorAt(t.from, end.to, "%s takes %s.", lower, arityText(arity))
	}
	return call{at{t.from, end.to}, lower, args}, nil
}

func aggregates(word string) bool { return slices.Contains(Aggregates, word) }

// aggregateFollows says the call's first argument is a set, which makes min and max aggregates rather than functions.
func (p *parser) aggregateFollows() bool {
	next := p.tokens[min(p.at+1, len(p.tokens)-1)]
	return next.kind == tIdent && (strings.EqualFold(next.text, "children") || strings.EqualFold(next.text, "links"))
}

func (p *parser) aggregate(t token, fn string) (node, *Error) {
	if p.inAggregate {
		return nil, errorAt(t.from, t.to, "An aggregate cannot read inside another. Give the children a computed attribute of their own and read that.")
	}
	p.inAggregate = true
	defer func() { p.inAggregate = false }()
	p.next()
	over, err := p.set()
	if err != nil {
		return nil, err
	}
	a := aggregate{fn: fn, over: over}
	needsValue := fn != "count"
	if p.peek().kind == tComma {
		p.next()
		if a.value, err = p.or(); err != nil {
			return nil, err
		}
	} else if needsValue {
		next := p.peek()
		return nil, errorAt(next.from, next.to, "%s needs what to read of each, such as %s(children, \"Cost\").", fn, fn)
	}
	if !needsValue && a.value != nil {
		from, to := a.value.span()
		return nil, errorAt(from, to, "count counts the items; say which with where, such as count(children where category = 'approved').")
	}
	if p.word("where") {
		p.next()
		if a.where, err = p.or(); err != nil {
			return nil, err
		}
	}
	end, err := p.expect(tRParen, "a closing parenthesis")
	if err != nil {
		return nil, err
	}
	a.at = at{t.from, end.to}
	return a, nil
}

func (p *parser) set() (set, *Error) {
	t := p.next()
	if strings.EqualFold(t.text, "children") {
		return set{at: at{t.from, t.to}, children: true}, nil
	}
	if _, err := p.expect(tLParen, "an opening parenthesis after links"); err != nil {
		return set{}, err
	}
	key, err := p.expect(tText, "the key of a link type in single quotes, such as 'verifies'")
	if err != nil {
		return set{}, err
	}
	if _, err := p.expect(tComma, "a comma, then out or in"); err != nil {
		return set{}, err
	}
	way := p.next()
	if way.kind != tIdent || (!strings.EqualFold(way.text, "out") && !strings.EqualFold(way.text, "in")) {
		return set{}, errorAt(way.from, way.to, "Say out for the links this requirement is the source of, or in for those it is the target of.")
	}
	if _, err := p.expect(tRParen, "a closing parenthesis"); err != nil {
		return set{}, err
	}
	// The set spans the key, which the formula kept is written with by id.
	return set{at: at{key.from, key.to}, typeKey: key.text, out: strings.EqualFold(way.text, "out")}, nil
}

func fieldNames() []string {
	out := make([]string, 0, len(Fields))
	for k := range Fields {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func functionNames() []string {
	out := make([]string, 0, len(functions)+len(Aggregates))
	for k := range functions {
		out = append(out, k)
	}
	out = append(out, Aggregates...)
	slices.Sort(out)
	return slices.Compact(out)
}

func arityText(a [2]int) string {
	switch {
	case a[0] == 0 && a[1] == 0:
		return "no arguments"
	case a[0] == a[1] && a[0] == 1:
		return "one argument"
	case a[0] == a[1]:
		return strconv.Itoa(a[0]) + " arguments"
	case a[1] < 0:
		return "at least " + strconv.Itoa(a[0]) + " arguments"
	}
	return strconv.Itoa(a[0]) + " to " + strconv.Itoa(a[1]) + " arguments"
}
