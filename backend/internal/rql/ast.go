package rql

import (
	"strconv"
	"strings"
)

// Expr is a condition on a requirement. String gives the normalized text, which is
// what the tests compare and what a client may show back.
type Expr interface {
	String() string
}

// Field names something about a requirement. A quoted name is an attribute's.
type Field struct {
	Name   string
	Custom bool
	Pos    int
}

func (f Field) String() string {
	if f.Custom {
		return strconv.Quote(f.Name)
	}
	return f.Name
}

// ValueKind is the shape a literal took in the text, which decides what it
// may be compared with.
type ValueKind int

const (
	VWord ValueKind = iota
	VString
	VNumber
	VDate
	VDuration
	VFunction
)

// Value is one literal or function call.
type Value struct {
	Kind ValueKind
	Text string
	Pos  int
	// Arg is a function's duration argument, when it has one.
	Arg *Value
}

func (v Value) String() string {
	switch v.Kind {
	case VString:
		return strconv.Quote(v.Text)
	case VFunction:
		if v.Arg != nil {
			return v.Text + "(" + v.Arg.String() + ")"
		}
		return v.Text + "()"
	default:
		return v.Text
	}
}

// Compare is field op value, for = != < <= > >= ~ and !~.
type Compare struct {
	Field Field
	Op    string
	Value Value
}

func (c Compare) String() string { return c.Field.String() + " " + c.Op + " " + c.Value.String() }

// In is field IN (values), or NOT IN.
type In struct {
	Field  Field
	Values []Value
	Not    bool
}

func (in In) String() string {
	parts := make([]string, len(in.Values))
	for i, v := range in.Values {
		parts[i] = v.String()
	}
	op := " IN ("
	if in.Not {
		op = " NOT IN ("
	}
	return in.Field.String() + op + strings.Join(parts, ", ") + ")"
}

// Empty is field IS EMPTY, or IS NOT EMPTY.
type Empty struct {
	Field Field
	Not   bool
}

func (e Empty) String() string {
	if e.Not {
		return e.Field.String() + " IS NOT EMPTY"
	}
	return e.Field.String() + " IS EMPTY"
}

type And struct{ Left, Right Expr }

func (a And) String() string { return "(" + a.Left.String() + " AND " + a.Right.String() + ")" }

type Or struct{ Left, Right Expr }

func (o Or) String() string { return "(" + o.Left.String() + " OR " + o.Right.String() + ")" }

type Not struct{ Expr Expr }

func (n Not) String() string { return "NOT " + n.Expr.String() }

// OrderBy is one sort key.
type OrderBy struct {
	Field Field
	Desc  bool
}

// Query is a parsed query: an optional condition and an optional ordering.
type Query struct {
	Where Expr
	Order []OrderBy
}

func (q *Query) String() string {
	var b strings.Builder
	if q.Where != nil {
		b.WriteString(q.Where.String())
	}
	if len(q.Order) > 0 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("ORDER BY ")
		for i, o := range q.Order {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(o.Field.String())
			if o.Desc {
				b.WriteString(" DESC")
			}
		}
	}
	return b.String()
}
