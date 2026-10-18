package rql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Env is what a query means by "me" and "now".
type Env struct {
	UserID uuid.UUID
	Now    time.Time
	// Location decides where a day starts; nil is UTC.
	Location *time.Location
}

// Order is one ORDER BY key, its SQL taken from the catalog and nothing else.
type Order struct {
	SQL  string
	Desc bool
}

// Compiled is a query ready to be joined to a WHERE clause over the requirement aliased r. Its parameters are
// numbered when it is placed, so it can follow other clauses.
type Compiled struct {
	where string
	args  []any
	// The sort keys bind their own arguments, since a list binds them after counting what the condition keeps.
	order     []Order
	orderArgs []any
}

// placeholder marks a bound argument until it is placed.
var placeholder = regexp.MustCompile(`\?(\d+)`)

// Place binds the condition's arguments through bind, which answers each one's placeholder, and returns the
// condition, empty for a query that only sorts.
func (c *Compiled) Place(bind func(any) string) string {
	if c == nil {
		return ""
	}
	return fill(c.where, c.args, bind)
}

// PlaceOrder binds the sort keys' arguments through bind and returns the keys.
func (c *Compiled) PlaceOrder(bind func(any) string) []Order {
	if c == nil {
		return nil
	}
	marks := make([]string, len(c.orderArgs))
	for i, a := range c.orderArgs {
		marks[i] = bind(a)
	}
	out := make([]Order, len(c.order))
	for i, o := range c.order {
		out[i] = Order{SQL: replace(o.SQL, marks), Desc: o.Desc}
	}
	return out
}

// fill binds each argument once and puts its placeholder wherever the SQL uses it.
func fill(sql string, args []any, bind func(any) string) string {
	marks := make([]string, len(args))
	for i, a := range args {
		marks[i] = bind(a)
	}
	return replace(sql, marks)
}

func replace(sql string, marks []string) string {
	return placeholder.ReplaceAllStringFunc(sql, func(m string) string {
		n, _ := strconv.Atoi(m[1:])
		return marks[n]
	})
}

// SQL returns the condition with its parameters numbered from first, and the arguments in that order.
func (c *Compiled) SQL(first int) (string, []any) {
	var args []any
	where := c.Place(func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(first+len(args)-1)
	})
	return where, args
}

// Sorts says the query has an ORDER BY of its own, which outranks the grid's.
func (c *Compiled) Sorts() bool { return c != nil && len(c.order) > 0 }

type compiler struct {
	cat  *Catalog
	env  Env
	args []any
}

// bind stores a value and returns its placeholder. Nothing typed ever reaches the SQL text by any other road.
func (c *compiler) bind(value any) string {
	c.args = append(c.args, value)
	return "?" + strconv.Itoa(len(c.args)-1)
}

// Compile checks every field, operator and value against the catalog and produces the SQL. A query that compiles
// cannot fail in the database for anything the person typed.
func (q *Query) Compile(cat *Catalog, env Env) (*Compiled, error) {
	if env.Location == nil {
		env.Location = time.UTC
	}
	if env.Now.IsZero() {
		env.Now = time.Now()
	}
	c := &compiler{cat: cat, env: env}
	out := &Compiled{}
	if q.Where != nil {
		where, err := c.expr(q.Where)
		if err != nil {
			return nil, err
		}
		out.where = where
	}
	out.args, c.args = c.args, nil
	for _, o := range q.Order {
		def, err := cat.lookup(o.Field)
		if err != nil {
			return nil, err
		}
		key := def.order
		if def.kind == fkAttribute {
			key = c.attributeOrder(def)
		}
		if key == "" {
			return nil, errAt(o.Field.Pos, "%s cannot be sorted by.", def.name)
		}
		out.order = append(out.order, Order{SQL: key, Desc: o.Desc})
	}
	out.orderArgs = c.args
	return out, nil
}

func (c *compiler) expr(e Expr) (string, error) {
	switch e := e.(type) {
	case And:
		l, err := c.expr(e.Left)
		if err != nil {
			return "", err
		}
		r, err := c.expr(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " AND " + r + ")", nil
	case Or:
		l, err := c.expr(e.Left)
		if err != nil {
			return "", err
		}
		r, err := c.expr(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " OR " + r + ")", nil
	case Not:
		inner, err := c.expr(e.Expr)
		if err != nil {
			return "", err
		}
		return negate(inner), nil
	case Compare:
		return c.compare(e)
	case In:
		return c.in(e)
	case Empty:
		return c.empty(e)
	}
	return "", fmt.Errorf("rql: unknown expression %T", e)
}

func (c *compiler) compare(e Compare) (string, error) {
	def, err := c.cat.lookup(e.Field)
	if err != nil {
		return "", err
	}
	if !def.allows(e.Op) {
		return "", errAt(e.Field.Pos, "%q does not apply to %s. Use %s.", e.Op, def.name, describeOperators(def.operators()))
	}
	switch e.Op {
	case "=":
		return c.match(def, e.Field, []Value{e.Value})
	case "!=":
		positive, err := c.match(def, e.Field, []Value{e.Value})
		if err != nil {
			return "", err
		}
		return negate(positive), nil
	case "~", "!~":
		clause, err := c.contains(def, e.Value)
		if err != nil {
			return "", err
		}
		if e.Op == "!~" {
			return negate(clause), nil
		}
		return clause, nil
	default:
		return c.ordered(def, e.Op, e.Value)
	}
}

func (c *compiler) in(e In) (string, error) {
	def, err := c.cat.lookup(e.Field)
	if err != nil {
		return "", err
	}
	if !def.allows("in") {
		return "", errAt(e.Field.Pos, "IN does not apply to %s. Use %s.", def.name, describeOperators(def.operators()))
	}
	positive, err := c.match(def, e.Field, e.Values)
	if err != nil {
		return "", err
	}
	if e.Not {
		return negate(positive), nil
	}
	return positive, nil
}

// negate counts a requirement with nothing there as not matching. A positive test over an empty value is NULL, and
// NOT NULL would drop the requirement instead.
func negate(positive string) string {
	return "NOT COALESCE(" + positive + ", false)"
}

func (c *compiler) empty(e Empty) (string, error) {
	def, err := c.cat.lookup(e.Field)
	if err != nil {
		return "", err
	}
	if !def.allows("empty") {
		return "", errAt(e.Field.Pos, "%s is never empty, so IS EMPTY does not apply to it.", def.name)
	}
	var clause string
	switch def.kind {
	case fkLabels:
		clause = "NOT EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = r.id)"
	case fkEnumSet:
		clause = "(cardinality(" + def.column + ") = 0)"
	case fkTerm:
		clause = "NOT EXISTS (SELECT 1 FROM term_use tu WHERE tu.requirement_id = r.id)"
	case fkDocument:
		clause = "(COALESCE(btrim(requirement_plain_text(" + def.column + ")), '') = '')"
	case fkLink:
		clause = "NOT " + c.linked(def, "")
	case fkAttribute:
		clause = "NOT (" + Store(def.attr) + " ? " + c.bind(def.attr.ID.String()) + ")"
	default:
		clause = "(" + def.column + " IS NULL)"
	}
	// IS EMPTY is never NULL, so it needs no negate.
	if e.Not {
		return "NOT " + clause, nil
	}
	return clause, nil
}

// match is the positive form of = and IN: the requirement's value is one of these.
func (c *compiler) match(def fieldDef, f Field, values []Value) (string, error) {
	switch def.kind {
	case fkKey:
		ids, err := c.keys(values)
		if err != nil {
			return "", err
		}
		return "(r.id IN (" + ids + "))", nil
	case fkParent:
		ids, err := c.keys(values)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " IN (" + ids + "))", nil
	case fkLink:
		ids, err := c.keys(values)
		if err != nil {
			return "", err
		}
		return c.linked(def, ids), nil
	case fkTitle:
		names, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "(lower(" + def.column + ") = ANY(" + c.bind(names) + "::text[]))", nil
	case fkName:
		names, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " IN (SELECT x.id FROM " + def.table + " x WHERE x.project_id = r.project_id AND lower(x.name) = ANY(" + c.bind(names) + "::text[])))", nil
	case fkEnum, fkPriority:
		members, err := c.members(def, values)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " = ANY(" + c.bind(members) + "::text[]))", nil
	case fkEnumSet:
		members, err := c.members(def, values)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " && " + c.bind(members) + "::text[])", nil
	case fkUser:
		return c.users(def.column, def.name, values)
	case fkTerm:
		words, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		bound := c.bind(words)
		return "EXISTS (SELECT 1 FROM term_use tu JOIN glossary_term gt ON gt.id = tu.term_id WHERE tu.requirement_id = r.id" +
			" AND (lower(gt.name) = ANY(" + bound + "::text[]) OR EXISTS (SELECT 1 FROM unnest(gt.synonyms) sy WHERE lower(sy) = ANY(" + bound + "::text[]))))", nil
	case fkLabels:
		names, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = r.id AND l.name = ANY(" + c.bind(names) + "::text[]))", nil
	case fkNumber:
		var numbers []float64
		for _, v := range values {
			n, err := number(def.name, v)
			if err != nil {
				return "", err
			}
			numbers = append(numbers, n)
		}
		return "(" + def.column + " = ANY(" + c.bind(numbers) + "::numeric[]))", nil
	case fkTimestamp:
		var parts []string
		for _, v := range values {
			clause, err := c.ordered(def, "=", v)
			if err != nil {
				return "", err
			}
			parts = append(parts, clause)
		}
		return anyOf(parts), nil
	case fkAttribute:
		return c.attributeMatch(def, values)
	}
	return "", errAt(f.Pos, "%s cannot be compared with =.", def.name)
}

// linked is a link of the field's type at the field's end, its other end not in the trash and, when ids is not
// empty, one of those requirements.
func (c *compiler) linked(def fieldDef, ids string) string {
	var ends string
	switch def.dir {
	case dirForward:
		ends = "tl.source_id = r.id AND other.id = tl.target_id"
	case dirBackward:
		ends = "tl.target_id = r.id AND other.id = tl.source_id"
	default:
		ends = "((tl.source_id = r.id AND other.id = tl.target_id) OR (tl.target_id = r.id AND other.id = tl.source_id))"
	}
	clause := "EXISTS (SELECT 1 FROM trace_link tl JOIN requirement other ON " + ends + " AND other.trashed_at IS NULL" +
		" WHERE tl.type_id = " + c.bind(def.link) + "::uuid AND tl.deleted_at IS NULL"
	if ids != "" {
		clause += " AND other.id IN (" + ids + ")"
	}
	return clause + ")"
}

// contains is ~: the words appear somewhere in the text.
func (c *compiler) contains(def fieldDef, v Value) (string, error) {
	text, err := plainText(def.name, v)
	if def.kind == fkAttribute && (v.Kind == VDate || v.Kind == VDuration) {
		// Text may hold anything, so a date typed after ~ is just the characters to look for.
		text, err = v.Text, nil
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errAt(v.Pos, "Give %s some text to look for.", def.name)
	}
	switch def.kind {
	case fkTitle:
		return "(" + def.column + " ILIKE '%' || " + c.bind(escapeLike(text)) + " || '%')", nil
	case fkText:
		// The grid's words box searches the same way, so the two agree.
		return "(r.search_vector @@ websearch_to_tsquery('exciter_search'::regconfig, " + c.bind(text) + "))", nil
	case fkDocument:
		return "(to_tsvector('exciter_search'::regconfig, COALESCE(requirement_plain_text(" + def.column + "), '')) @@ plainto_tsquery('exciter_search'::regconfig, " + c.bind(text) + "))", nil
	case fkAttribute:
		return c.attributeContains(def, text), nil
	}
	return "", errAt(v.Pos, "%q does not apply to %s.", "~", def.name)
}

// ordered is < <= > >= and, for timestamps, = on a whole day.
func (c *compiler) ordered(def fieldDef, op string, v Value) (string, error) {
	switch def.kind {
	case fkPriority:
		member, err := c.member(def, v)
		if err != nil {
			return "", err
		}
		return "(" + priorityRank + " " + op + " array_position(ARRAY['wont', 'could', 'should', 'must'], " + c.bind(member) + "::text))", nil
	case fkNumber:
		n, err := number(def.name, v)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " " + op + " " + c.bind(n) + "::numeric)", nil
	case fkTimestamp:
		at, wholeDay, err := c.instant(v)
		if err != nil {
			return "", err
		}
		if !wholeDay {
			return "(" + def.column + " " + op + " " + c.bind(at) + "::timestamptz)", nil
		}
		// A day named without a time is the whole day: from its midnight up to the next, so "= 2026-10-15" and
		// "<= 2026-10-15" both mean what they say.
		next := at.AddDate(0, 0, 1)
		switch op {
		case ">=":
			return "(" + def.column + " >= " + c.bind(at) + "::timestamptz)", nil
		case ">":
			return "(" + def.column + " >= " + c.bind(next) + "::timestamptz)", nil
		case "<":
			return "(" + def.column + " < " + c.bind(at) + "::timestamptz)", nil
		case "<=":
			return "(" + def.column + " < " + c.bind(next) + "::timestamptz)", nil
		default:
			return "(" + def.column + " >= " + c.bind(at) + "::timestamptz AND " + def.column + " < " + c.bind(next) + "::timestamptz)", nil
		}
	case fkAttribute:
		return c.attributeOrdered(def, op, v)
	}
	return "", errAt(v.Pos, "%q does not apply to %s. Use %s.", op, def.name, describeOperators(def.operators()))
}

// users is a person field among the values: currentUser(), an email address or a name.
func (c *compiler) users(column, name string, values []Value) (string, error) {
	var ids []uuid.UUID
	var names []string
	for _, v := range values {
		if v.Kind == VFunction {
			if !strings.EqualFold(v.Text, "currentUser") {
				return "", errAt(v.Pos, "%s() does not name a person. Use currentUser(), an email address or a name.", v.Text)
			}
			ids = append(ids, c.env.UserID)
			continue
		}
		text, err := plainText(name, v)
		if err != nil {
			return "", err
		}
		names = append(names, strings.ToLower(text))
	}
	var parts []string
	if len(ids) > 0 {
		parts = append(parts, "("+column+" = ANY("+c.bind(ids)+"::uuid[]))")
	}
	if len(names) > 0 {
		list := c.bind(names)
		parts = append(parts, "("+column+" IN (SELECT u.id FROM app_user u WHERE lower(u.email::text) = ANY("+list+"::text[]) OR lower(u.name) = ANY("+list+"::text[])))")
	}
	return anyOf(parts), nil
}
