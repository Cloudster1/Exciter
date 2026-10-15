// Package rql is the query language for requirements (#47): what a person types to narrow a project's grid, parsed
// and compiled to SQL with every value bound and every identifier ours. Adapted from Armature's internal/nql.
package rql

import "fmt"

// Error says what is wrong with a query and where. Msg is a sentence that
// names what to do; Pos lets the client point at the character.
type Error struct {
	// Pos is the 1-based character the trouble starts at.
	Pos int
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errAt(pos int, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}
