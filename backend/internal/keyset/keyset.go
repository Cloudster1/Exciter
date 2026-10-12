// Package keyset writes the opaque cursor a client sends back for the next
// window, so a list ordered by time neither skips nor repeats rows as they arrive.
package keyset

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrBadCursor is a cursor this package did not write.
var ErrBadCursor = errors.New("bad cursor")

// Cursor is a row's place in an order by time, then id: descending for the
// lists newest first, ascending for the stale report.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// Encode writes the cursor as URL safe text.
func (c Cursor) Encode() string {
	raw := c.At.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// Decode reads a cursor; an empty one is the start, nil.
func Decode(text string) (*Cursor, error) {
	if text == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, ErrBadCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, ErrBadCursor
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrBadCursor
	}
	return &Cursor{At: t, ID: u}, nil
}

// Args are the cursor as the two SQL parameters a keyset query takes, both
// null at the start.
func (c *Cursor) Args() (*time.Time, *uuid.UUID) {
	if c == nil {
		return nil, nil
	}
	return &c.At, &c.ID
}

// Next is the cursor after a window read with one row more than limit: the
// last row kept when there was more, otherwise nil.
func Next(limit, read int, last Cursor) *string {
	if read <= limit {
		return nil
	}
	next := last.Encode()
	return &next
}
