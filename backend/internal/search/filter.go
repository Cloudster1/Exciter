package search

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// FieldError is a query refused on one parameter, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Columns the grid sorts by besides an attribute's id, and the ones it only shows.
var (
	SortColumns = []string{"key", "title", "type", "status", "owner", "priority", "updated"}
	ShowColumns = []string{"labels", "outline"}
)

// ParseGrid reads the grid's narrowing and order from a query string, in the words the contract gives them;
// what needs the project, such as an attribute's kind, is checked when the grid is read.
func ParseGrid(v url.Values) (Filter, Sort, error) {
	f := Filter{Q: strings.TrimSpace(v.Get("q")), Categories: v["category"], Labels: v["label"], Priorities: v["priority"]}
	for _, k := range v["kind"] {
		f.Kinds = append(f.Kinds, requirement.Kind(k))
	}
	for name, into := range map[string]*[]uuid.UUID{"owner": &f.Owners, "type": &f.Types, "status": &f.Statuses} {
		found, err := ids(name, v[name], "That is not an id. Pick it from the list.")
		if err != nil {
			return f, Sort{}, err
		}
		*into = found
	}
	for _, raw := range v["attr"] {
		c, err := parseCondition(raw)
		if err != nil {
			return f, Sort{}, err
		}
		f.Attributes = append(f.Attributes, c)
	}
	switch v.Get("suspect") {
	case "", "false":
	case "true":
		f.Suspect = true
	default:
		return f, Sort{}, &FieldError{"suspect", "Say true to keep only requirements with a suspect link, or false."}
	}
	sort := Sort{Column: strings.TrimSpace(v.Get("sort"))}
	switch v.Get("desc") {
	case "", "false":
	case "true":
		sort.Desc = true
	default:
		return f, sort, &FieldError{"desc", "Say true to order backwards, or false."}
	}
	return f, sort, nil
}

// parseCondition reads attributeId:op:value; the value may itself hold colons.
func parseCondition(raw string) (AttributeCondition, error) {
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 2 {
		return AttributeCondition{}, &FieldError{"attr", "Write an attribute condition as attributeId:op:value, such as the id, is and an option's id."}
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return AttributeCondition{}, &FieldError{"attr", "That attribute is not one of this project's. Pick it from the list."}
	}
	c := AttributeCondition{AttributeID: id, Op: Op(parts[1])}
	if len(parts) == 3 {
		c.Value = parts[2]
	}
	return c, nil
}

// check holds a filter and an order to the contract, with the project's attributes; it answers the filter with
// its labels normalized.
func check(f Filter, sort Sort, defs []attribute.Def) (Filter, error) {
	if utf8.RuneCountInString(f.Q) > MaxQueryLength {
		return f, &FieldError{"q", fmt.Sprintf("Keep the search to %d characters.", MaxQueryLength)}
	}
	for _, k := range f.Kinds {
		if !slices.Contains(requirement.Kinds, k) {
			return f, &FieldError{"kind", "Narrow by requirement or section."}
		}
	}
	if _, err := categories(f.Categories); err != nil {
		return f, err
	}
	for _, p := range f.Priorities {
		if !slices.Contains(requirement.Priorities, requirement.Priority(p)) {
			return f, &FieldError{"priority", "Narrow by must, should, could or wont."}
		}
	}
	labels := []string{}
	for _, raw := range f.Labels {
		name, err := label.Normalize(raw)
		if err != nil {
			return f, &FieldError{"label", "Narrow by labels as they are written, such as safety-critical."}
		}
		labels = append(labels, name)
	}
	f.Labels = labels
	if len(f.Attributes) > MaxAttributeFilters {
		return f, &FieldError{"attr", fmt.Sprintf("Narrow by at most %d attribute conditions.", MaxAttributeFilters)}
	}
	for _, c := range f.Attributes {
		if _, err := condition(c, defs, func(any) string { return "" }); err != nil {
			return f, err
		}
	}
	if sort.Column != "" && !slices.Contains(SortColumns, sort.Column) && defOf(sort.Column, defs) == nil {
		return f, &FieldError{"sort", "Sort by key, title, type, status, owner, priority, updated or one of the project's attributes."}
	}
	return f, nil
}

func defOf(id string, defs []attribute.Def) *attribute.Def {
	for i := range defs {
		if defs[i].ID.String() == id {
			return &defs[i]
		}
	}
	return nil
}

// opsOf are the comparisons each kind takes.
var opsOf = map[attribute.Kind][]Op{
	attribute.Text:        {OpIs, OpNot, OpContains, OpSet, OpUnset},
	attribute.RichText:    {OpIs, OpNot, OpContains, OpSet, OpUnset},
	attribute.Number:      {OpIs, OpNot, OpGte, OpLte, OpSet, OpUnset},
	attribute.Select:      {OpIs, OpNot, OpSet, OpUnset},
	attribute.MultiSelect: {OpIs, OpNot, OpSet, OpUnset},
	attribute.Date:        {OpIs, OpNot, OpGte, OpLte, OpSet, OpUnset},
	attribute.User:        {OpIs, OpNot, OpSet, OpUnset},
	attribute.Boolean:     {OpIs, OpNot, OpSet, OpUnset},
	attribute.URL:         {OpIs, OpNot, OpSet, OpUnset},
}

// condition is the SQL of one attribute condition over requirement r, binding what it compares through bind.
func condition(c AttributeCondition, defs []attribute.Def, bind func(any) string) (string, error) {
	d := defOf(c.AttributeID.String(), defs)
	if d == nil {
		return "", &FieldError{"attr", "That attribute is not one of this project's. Pick it from the list."}
	}
	if !slices.Contains(opsOf[d.Kind], c.Op) {
		return "", &FieldError{"attr", fmt.Sprintf("%s does not compare by %s. Pick another comparison.", d.Name, c.Op)}
	}
	value := strings.TrimSpace(c.Value)
	if (c.Op == OpSet || c.Op == OpUnset) != (value == "") {
		if value == "" {
			return "", &FieldError{"attr", fmt.Sprintf("Say which value of %s to compare with.", d.Name)}
		}
		return "", &FieldError{"attr", fmt.Sprintf("Whether %s is set takes no value. Leave it empty.", d.Name)}
	}
	id := bind(d.ID.String())
	switch c.Op {
	case OpSet:
		return `r.attributes ? ` + id, nil
	case OpUnset:
		return `NOT (r.attributes ? ` + id + `)`, nil
	case OpContains:
		return `to_tsvector('exciter_search'::regconfig, ` + textOf(d.Kind, id) + `) @@ plainto_tsquery('exciter_search'::regconfig, ` + bind(value) + `)`, nil
	}
	compared, err := comparable(d, value)
	if err != nil {
		return "", err
	}
	v := bind(compared)
	var equal string
	switch d.Kind {
	case attribute.Text, attribute.URL, attribute.RichText:
		equal = `lower(` + textOf(d.Kind, id) + `) = lower(` + v + `)`
	case attribute.Number:
		if c.Op == OpGte || c.Op == OpLte {
			return `(r.attributes ->> ` + id + `)::numeric ` + map[Op]string{OpGte: ">=", OpLte: "<="}[c.Op] + ` ` + v + `::numeric`, nil
		}
		equal = `(r.attributes ->> ` + id + `)::numeric = ` + v + `::numeric`
	case attribute.Date:
		if c.Op == OpGte || c.Op == OpLte {
			return `r.attributes ->> ` + id + ` ` + map[Op]string{OpGte: ">=", OpLte: "<="}[c.Op] + ` ` + v, nil
		}
		equal = `r.attributes ->> ` + id + ` = ` + v
	case attribute.MultiSelect:
		equal = `(r.attributes -> ` + id + `) ? ` + v
	case attribute.Boolean:
		equal = `r.attributes -> ` + id + ` = to_jsonb(` + v + `::boolean)`
	default:
		equal = `r.attributes ->> ` + id + ` = ` + v
	}
	if c.Op == OpNot {
		// Not equal takes in what has no value at all.
		return `NOT COALESCE(` + equal + `, false)`, nil
	}
	return equal, nil
}

// textOf is the words of a text value, the plain text of a rich text one.
func textOf(kind attribute.Kind, id string) string {
	if kind == attribute.RichText {
		return `COALESCE(requirement_plain_text(r.attributes -> ` + id + `), '')`
	}
	return `COALESCE(r.attributes ->> ` + id + `, '')`
}

// comparable checks a value has the shape its attribute's kind compares, and answers it as SQL compares it.
func comparable(d *attribute.Def, value string) (any, error) {
	refuse := func(shape string) error {
		return &FieldError{"attr", fmt.Sprintf("%s compares with %s.", d.Name, shape)}
	}
	switch d.Kind {
	case attribute.Number:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, refuse("a number, such as 12 or 2.5")
		}
		return n, nil
	case attribute.Date:
		day, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return nil, refuse("a day, such as 2026-01-31")
		}
		return day.Format(time.DateOnly), nil
	case attribute.Boolean:
		if value != "true" && value != "false" {
			return nil, refuse("true or false")
		}
		return value == "true", nil
	case attribute.Select, attribute.MultiSelect:
		id, err := uuid.Parse(value)
		if err != nil || !slices.ContainsFunc(d.Options, func(o attribute.Option) bool { return o.ID == id }) {
			return nil, refuse("one of its options")
		}
		return id.String(), nil
	case attribute.User:
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, refuse("a person; pick one from the list")
		}
		return id.String(), nil
	}
	return value, nil
}

// sortKey is what the grid orders by for a column, as SQL over requirement r with its type t, status st and
// owner o; an attribute orders by what a reader sees of it.
func sortKey(column string, defs []attribute.Def, bind func(any) string) string {
	switch column {
	case "key":
		return `r.number`
	case "title":
		return `lower(r.title)`
	case "type":
		return `lower(t.name)`
	case "status":
		return `st.position`
	case "owner":
		return `lower(o.name)`
	case "priority":
		return `array_position(ARRAY['must', 'should', 'could', 'wont'], r.priority)`
	case "updated":
		return `r.updated_at`
	}
	d := defOf(column, defs)
	if d == nil {
		return ``
	}
	id := bind(d.ID.String())
	switch d.Kind {
	case attribute.Number:
		return `(r.attributes ->> ` + id + `)::numeric`
	case attribute.Select:
		options := make([]string, len(d.Options))
		for i, o := range d.Options {
			options[i] = o.ID.String()
		}
		return `array_position(` + bind(options) + `::text[], r.attributes ->> ` + id + `)`
	case attribute.User:
		return `(SELECT lower(u.name) FROM app_user u WHERE u.id::text = r.attributes ->> ` + id + `)`
	case attribute.Text, attribute.URL, attribute.RichText:
		return `lower(NULLIF(` + textOf(d.Kind, id) + `, ''))`
	}
	return `r.attributes -> ` + id
}
