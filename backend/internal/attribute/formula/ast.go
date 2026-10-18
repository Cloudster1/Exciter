package formula

import (
	"github.com/google/uuid"
)

// node is one part of a formula, with the characters it spans.
type node interface {
	span() (from, to int)
}

type at struct{ from, to int }

func (a at) span() (int, int) { return a.from, a.to }

type numberLit struct {
	at
	value float64
}

type textLit struct {
	at
	value string
}

type boolLit struct {
	at
	value bool
}

type nullLit struct{ at }

// fieldRef is one of a requirement's own fields, such as status.
type fieldRef struct {
	at
	name string
}

// attrRef is an attribute, named as written until it is checked, then by id.
type attrRef struct {
	at
	name string
	id   uuid.UUID
}

type unary struct {
	at
	op string
	x  node
}

type binary struct {
	at
	op   string
	x, y node
}

type call struct {
	at
	fn   string
	args []node
}

// set is what an aggregate reads: the requirement's children, or the requirements its links of a type lead to.
type set struct {
	at
	children bool
	typeKey  string
	typeID   uuid.UUID
	out      bool
}

// aggregate reads many requirements: value is what it sums or compares, cond what an item must meet, and for any and
// all the condition itself.
type aggregate struct {
	at
	fn    string
	over  set
	value node
	where node
}
