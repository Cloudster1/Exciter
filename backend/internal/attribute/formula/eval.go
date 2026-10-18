package formula

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// dayLayout is how a date value is kept, as a date attribute keeps it.
const dayLayout = "2006-01-02"

// Value is what a formula or a part of it gives; Null is no value at all.
type Value struct {
	Kind Kind
	Null bool
	Num  float64
	Text string
	Bool bool
	Day  time.Time
}

// None is no value.
var None = Value{Null: true}

func NumberValue(n float64) Value { return Value{Kind: Number, Num: n} }
func TextValue(s string) Value    { return Value{Kind: Text, Text: s} }
func BoolValue(b bool) Value      { return Value{Kind: Boolean, Bool: b} }
func DayValue(d time.Time) Value  { return Value{Kind: Date, Day: d} }

// JSON is a value as an attribute keeps it, nil for none.
func (v Value) JSON() json.RawMessage {
	if v.Null {
		return nil
	}
	var out any
	switch v.Kind {
	case Number:
		if math.IsNaN(v.Num) || math.IsInf(v.Num, 0) {
			return nil
		}
		out = v.Num
	case Text:
		out = v.Text
	case Boolean:
		out = v.Bool
	case Date:
		out = v.Day.Format(dayLayout)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}

// FromJSON reads a kept value as a kind; what does not fit is none.
func FromJSON(kind Kind, raw json.RawMessage) Value {
	if len(raw) == 0 || string(raw) == "null" {
		return None
	}
	switch kind {
	case Number:
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			return NumberValue(n)
		}
	case Text:
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return TextValue(s)
		}
	case Boolean:
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return BoolValue(b)
		}
	case Date:
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if d, err := time.Parse(dayLayout, s); err == nil {
				return DayValue(d)
			}
		}
	}
	return None
}

// Item is one requirement as a formula reads it.
type Item interface {
	Field(name string) Value
	Attribute(id uuid.UUID) Value
}

// Scope is the requirement a formula is worked out for, with what its aggregates read.
type Scope interface {
	Item
	Children() []Item
	Linked(typeID uuid.UUID, out bool) []Item
	Today() time.Time
}

// Eval works the formula out for a requirement.
func (p *Program) Eval(s Scope) Value {
	e := &evaluator{p: p, scope: s}
	return e.eval(p.root, s)
}

type evaluator struct {
	p     *Program
	scope Scope
}

func truth(v Value) bool { return !v.Null && v.Kind == Boolean && v.Bool }

func (e *evaluator) eval(n node, item Item) Value {
	switch v := n.(type) {
	case numberLit:
		return NumberValue(v.value)
	case textLit:
		return TextValue(v.value)
	case boolLit:
		return BoolValue(v.value)
	case nullLit:
		return None
	case fieldRef:
		return item.Field(v.name)
	case attrRef:
		return item.Attribute(e.p.ids[v.from])
	case unary:
		x := e.eval(v.x, item)
		if x.Null {
			return None
		}
		if v.op == "not" {
			return BoolValue(!x.Bool)
		}
		return NumberValue(-x.Num)
	case binary:
		return e.binary(v, item)
	case call:
		return e.call(v, item)
	case aggregate:
		return e.aggregate(v)
	}
	return None
}

func (e *evaluator) binary(v binary, item Item) Value {
	switch v.op {
	case "and":
		return BoolValue(truth(e.eval(v.x, item)) && truth(e.eval(v.y, item)))
	case "or":
		return BoolValue(truth(e.eval(v.x, item)) || truth(e.eval(v.y, item)))
	}
	x, y := e.eval(v.x, item), e.eval(v.y, item)
	switch v.op {
	case "+", "-", "*", "/":
		if x.Null || y.Null {
			return None
		}
		switch v.op {
		case "+":
			return NumberValue(x.Num + y.Num)
		case "-":
			return NumberValue(x.Num - y.Num)
		case "*":
			return NumberValue(x.Num * y.Num)
		}
		if y.Num == 0 {
			return None
		}
		return NumberValue(x.Num / y.Num)
	}
	if x.Null || y.Null {
		// Only = and != compare a missing value, and only with null.
		switch v.op {
		case "=":
			return BoolValue(x.Null && y.Null)
		case "!=":
			return BoolValue(x.Null != y.Null)
		}
		return None
	}
	c := compare(x, y)
	switch v.op {
	case "=":
		return BoolValue(c == 0)
	case "!=":
		return BoolValue(c != 0)
	case "<":
		return BoolValue(c < 0)
	case "<=":
		return BoolValue(c <= 0)
	case ">":
		return BoolValue(c > 0)
	}
	return BoolValue(c >= 0)
}

// compare orders two values of one kind; text without regard to case, as the query language compares it.
func compare(x, y Value) int {
	switch x.Kind {
	case Number:
		switch {
		case x.Num < y.Num:
			return -1
		case x.Num > y.Num:
			return 1
		}
		return 0
	case Text:
		return strings.Compare(strings.ToLower(x.Text), strings.ToLower(y.Text))
	case Date:
		return x.Day.Compare(y.Day)
	case Boolean:
		switch {
		case x.Bool == y.Bool:
			return 0
		case x.Bool:
			return 1
		}
		return -1
	}
	return 0
}

func (e *evaluator) call(v call, item Item) Value {
	args := func() []Value {
		out := make([]Value, len(v.args))
		for i, a := range v.args {
			out[i] = e.eval(a, item)
		}
		return out
	}
	switch v.fn {
	case "if":
		if truth(e.eval(v.args[0], item)) {
			return e.eval(v.args[1], item)
		}
		return e.eval(v.args[2], item)
	case "coalesce":
		for _, a := range v.args {
			if x := e.eval(a, item); !x.Null {
				return x
			}
		}
		return None
	case "today":
		return DayValue(e.scope.Today())
	case "concat":
		var b strings.Builder
		for _, x := range args() {
			b.WriteString(textOf(x))
		}
		return TextValue(b.String())
	}
	all := args()
	for _, x := range all {
		if x.Null {
			return None
		}
	}
	switch v.fn {
	case "round":
		digits := 0.0
		if len(all) > 1 {
			digits = math.Round(all[1].Num)
		}
		scale := math.Pow(10, digits)
		return NumberValue(math.Round(all[0].Num*scale) / scale)
	case "floor":
		return NumberValue(math.Floor(all[0].Num))
	case "ceil":
		return NumberValue(math.Ceil(all[0].Num))
	case "abs":
		return NumberValue(math.Abs(all[0].Num))
	case "min", "max":
		best := all[0]
		for _, x := range all[1:] {
			if (v.fn == "min") == (x.Num < best.Num) && x.Num != best.Num {
				best = x
			}
		}
		return best
	case "lower":
		return TextValue(strings.ToLower(all[0].Text))
	case "upper":
		return TextValue(strings.ToUpper(all[0].Text))
	case "len":
		return NumberValue(float64(utf8.RuneCountInString(all[0].Text)))
	case "days":
		return NumberValue(math.Round(all[1].Day.Sub(all[0].Day).Hours() / 24))
	}
	return None
}

// textOf is a value as concat writes it.
func textOf(v Value) string {
	if v.Null {
		return ""
	}
	switch v.Kind {
	case Number:
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	case Boolean:
		return strconv.FormatBool(v.Bool)
	case Date:
		return v.Day.Format(dayLayout)
	}
	return v.Text
}

func (e *evaluator) aggregate(v aggregate) Value {
	var items []Item
	if v.over.children {
		items = e.scope.Children()
	} else {
		items = e.scope.Linked(e.p.ids[v.over.from], v.over.out)
	}
	var kept []Item
	for _, it := range items {
		if v.where == nil || truth(e.eval(v.where, it)) {
			kept = append(kept, it)
		}
	}
	switch v.fn {
	case "count":
		return NumberValue(float64(len(kept)))
	case "any", "all":
		for _, it := range kept {
			if truth(e.eval(v.value, it)) == (v.fn == "any") {
				return BoolValue(v.fn == "any")
			}
		}
		return BoolValue(v.fn == "all")
	}
	var values []Value
	for _, it := range kept {
		if x := e.eval(v.value, it); !x.Null {
			values = append(values, x)
		}
	}
	switch v.fn {
	case "sum":
		total := 0.0
		for _, x := range values {
			total += x.Num
		}
		return NumberValue(total)
	case "avg":
		if len(values) == 0 {
			return None
		}
		total := 0.0
		for _, x := range values {
			total += x.Num
		}
		return NumberValue(total / float64(len(values)))
	}
	if len(values) == 0 {
		return None
	}
	best := values[0]
	for _, x := range values[1:] {
		if c := compare(x, best); (v.fn == "min" && c < 0) || (v.fn == "max" && c > 0) {
			best = x
		}
	}
	return best
}
