package rql

import (
	"strings"

	"github.com/cloudster1/exciter/backend/internal/attribute"
)

// An attribute's value lives in r.attributes under its id, in the shape its kind keeps (#14), a computed one's in
// requirement_computed (#213); these compare it as the grid's attribute conditions do, so the two agree.

// Store is where a requirement r keeps an attribute's values: its own attributes, or what was computed for it.
func Store(a *attribute.Def) string {
	if a.Computed() {
		return "requirement_computed_of(r.id)"
	}
	return "r.attributes"
}

// attributeMatch is the positive form of = and IN on an attribute.
func (c *compiler) attributeMatch(def fieldDef, values []Value) (string, error) {
	a := def.attr
	id := c.bind(a.ID.String())
	src := Store(a)
	switch a.Kind {
	case attribute.Text, attribute.URL:
		texts, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "(lower(" + src + " ->> " + id + ") = ANY(" + c.bind(texts) + "::text[]))", nil
	case attribute.Number:
		var numbers []float64
		for _, v := range values {
			n, err := number(a.Name, v)
			if err != nil {
				return "", err
			}
			numbers = append(numbers, n)
		}
		return "(" + numeric(src, id) + " = ANY(" + c.bind(numbers) + "::numeric[]))", nil
	case attribute.Date:
		var days []string
		for _, v := range values {
			day, err := c.day(a.Name, v)
			if err != nil {
				return "", err
			}
			days = append(days, day)
		}
		return "(" + src + " ->> " + id + " = ANY(" + c.bind(days) + "::text[]))", nil
	case attribute.Boolean:
		text, err := plainText(a.Name, values[0])
		if err != nil {
			return "", err
		}
		text = strings.ToLower(text)
		if text != "true" && text != "false" {
			return "", errAt(values[0].Pos, "%s is true or false, and %q is neither.", a.Name, values[0].Text)
		}
		return "(" + src + " -> " + id + " = to_jsonb(" + c.bind(text == "true") + "::boolean))", nil
	case attribute.Select, attribute.MultiSelect:
		ids, err := c.options(a, values)
		if err != nil {
			return "", err
		}
		if a.Kind == attribute.MultiSelect {
			return "((" + src + " -> " + id + ") ?| " + c.bind(ids) + "::text[])", nil
		}
		return "(" + src + " ->> " + id + " = ANY(" + c.bind(ids) + "::text[]))", nil
	case attribute.User:
		// A person is kept as their id; the guard leaves a value of another shape no person rather than an error.
		person := "(CASE WHEN " + src + " ->> " + id + " ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (" + src + " ->> " + id + ")::uuid END)"
		return c.users(person, a.Name, values)
	}
	return "", errAt(values[0].Pos, "%s cannot be compared with =. Use ~.", a.Name)
}

// numeric reads a number attribute, guarded so that a value of another shape is no number rather than an error.
func numeric(src, id string) string {
	return "(CASE WHEN jsonb_typeof(" + src + " -> " + id + ") = 'number' THEN (" + src + " ->> " + id + ")::numeric END)"
}

// textOf is the words of a text value, the plain text of a rich text one.
func textOf(src string, kind attribute.Kind, id string) string {
	if kind == attribute.RichText {
		return "COALESCE(requirement_plain_text(" + src + " -> " + id + "), '')"
	}
	return "COALESCE(" + src + " ->> " + id + ", '')"
}

// attributeContains is ~ on an attribute: a piece of the text, or words of rich text.
func (c *compiler) attributeContains(def fieldDef, text string) string {
	id := c.bind(def.attr.ID.String())
	src := Store(def.attr)
	if def.attr.Kind == attribute.RichText {
		return "(to_tsvector('exciter_search'::regconfig, " + textOf(src, def.attr.Kind, id) + ") @@ plainto_tsquery('exciter_search'::regconfig, " + c.bind(text) + "))"
	}
	return "(" + textOf(src, def.attr.Kind, id) + " ILIKE '%' || " + c.bind(escapeLike(text)) + " || '%')"
}

// attributeOrdered is < <= > >= on a number or date attribute.
func (c *compiler) attributeOrdered(def fieldDef, op string, v Value) (string, error) {
	a := def.attr
	src := Store(a)
	switch a.Kind {
	case attribute.Number:
		n, err := number(a.Name, v)
		if err != nil {
			return "", err
		}
		return "(" + numeric(src, c.bind(a.ID.String())) + " " + op + " " + c.bind(n) + "::numeric)", nil
	case attribute.Date:
		day, err := c.day(a.Name, v)
		if err != nil {
			return "", err
		}
		// Days are kept as 2026-10-15, which compare as text in calendar order.
		return "(" + src + " ->> " + c.bind(a.ID.String()) + " " + op + " " + c.bind(day) + "::text)", nil
	}
	return "", errAt(v.Pos, "%q does not apply to %s. Use %s.", op, a.Name, describeOperators(def.operators()))
}

// day reads a value as the calendar day a date attribute holds.
func (c *compiler) day(name string, v Value) (string, error) {
	if v.Kind != VDate && v.Kind != VDuration && v.Kind != VFunction {
		return "", errAt(v.Pos, "%s is a date, and %s is not one. Write 2026-09-04, -7d or startOfWeek().", name, v)
	}
	at, _, err := c.instant(v)
	if err != nil {
		return "", err
	}
	return at.In(c.env.Location).Format("2006-01-02"), nil
}

// options reads option names as the ids a select keeps.
func (c *compiler) options(a *attribute.Def, values []Value) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		text, err := plainText(a.Name, v)
		if err != nil {
			return nil, err
		}
		found := ""
		for _, o := range a.Options {
			if strings.EqualFold(strings.TrimSpace(o.Name), strings.TrimSpace(text)) {
				found = o.ID.String()
				break
			}
		}
		if found == "" {
			names := make([]string, len(a.Options))
			for i, o := range a.Options {
				names[i] = o.Name
			}
			if len(names) == 0 {
				return nil, errAt(v.Pos, "%s has no options yet.", a.Name)
			}
			return nil, errAt(v.Pos, "%q is not an option of %s. Use %s.", v.Text, a.Name, describeOperators(names))
		}
		out = append(out, found)
	}
	return out, nil
}

// attributeOrder is what an attribute sorts by: what a reader sees of it.
func (c *compiler) attributeOrder(def fieldDef) string {
	a := def.attr
	id := c.bind(a.ID.String())
	src := Store(a)
	switch a.Kind {
	case attribute.Number:
		return numeric(src, id)
	case attribute.Select:
		options := make([]string, len(a.Options))
		for i, o := range a.Options {
			options[i] = o.ID.String()
		}
		return "array_position(" + c.bind(options) + "::text[], " + src + " ->> " + id + ")"
	case attribute.User:
		return "(SELECT lower(u.name) FROM app_user u WHERE u.id::text = " + src + " ->> " + id + ")"
	case attribute.Text, attribute.URL, attribute.RichText:
		return "lower(NULLIF(" + textOf(src, a.Kind, id) + ", ''))"
	case attribute.Date:
		return "(" + src + " ->> " + id + ")"
	case attribute.Boolean:
		return "(" + src + " -> " + id + ")"
	}
	return ""
}
