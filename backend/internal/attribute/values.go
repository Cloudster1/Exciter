package attribute

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Viewer says whether a person may view the project a value is given in, which a user value must.
type Viewer func(person uuid.UUID) (bool, error)

func valueError(def Def, format string, args ...any) error {
	return &FieldError{"attributes." + def.ID.String(), fmt.Sprintf(format, args...)}
}

// Shows says whether an attribute shows on requirements of a type; one naming no types shows on all of them.
func (d Def) Shows(typeID *uuid.UUID) bool {
	if len(d.TypeIDs) == 0 {
		return true
	}
	return typeID != nil && slices.Contains(d.TypeIDs, *typeID)
}

// Merge applies changes to a requirement's values; an empty one clears its key. A changed type drops the values
// of attributes that do not show on it, which its revisions keep.
func Merge(defs []Def, typeID *uuid.UUID, typeChanged bool, current, changes map[string]json.RawMessage, viewer Viewer) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(current)+len(changes))
	for key, raw := range current {
		out[key] = raw
	}
	byID := make(map[string]Def, len(defs))
	for _, d := range defs {
		byID[d.ID.String()] = d
	}
	if typeChanged {
		for key := range out {
			if d, ok := byID[key]; !ok || !d.Shows(typeID) {
				delete(out, key)
			}
		}
	}
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	// In order, so the same request is refused on the same attribute every time.
	slices.Sort(keys)
	for _, key := range keys {
		d, ok := byID[key]
		if !ok {
			return nil, &FieldError{"attributes." + key, "This project has no such attribute. Pick one from its attributes."}
		}
		if d.Computed() {
			return nil, valueError(d, "%s is worked out from its formula, so it takes no value. Leave it out.", d.Name)
		}
		value, err := normalize(d, changes[key], current[key], viewer)
		if err != nil {
			return nil, err
		}
		if value == nil {
			delete(out, key)
			continue
		}
		if !d.Shows(typeID) {
			return nil, valueError(d, "%s does not show on requirements of this type. Leave it out, or change the type first.", d.Name)
		}
		if d.Retired && !bytes.Equal(value, current[key]) {
			return nil, valueError(d, "%s is retired, so it takes no new value. Leave it as it is.", d.Name)
		}
		out[key] = value
	}
	if err := CheckChoices(defs, current, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Missing refuses values that leave out a required attribute showing on the type; a revision needs them all.
func Missing(defs []Def, typeID *uuid.UUID, values map[string]json.RawMessage) error {
	for _, d := range defs {
		if d.Required && !d.Retired && d.Shows(typeID) {
			if _, ok := values[d.ID.String()]; !ok {
				return valueError(d, "%s is required. Give it a value.", d.Name)
			}
		}
	}
	return nil
}

func isEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(`""`)) || bytes.Equal(trimmed, []byte("[]"))
}

func encode(v any) json.RawMessage {
	out, err := json.Marshal(v)
	if err != nil {
		// Only strings, numbers, booleans and lists of strings reach here; none can fail.
		return json.RawMessage("null")
	}
	return out
}

// option finds a choice by id; a retired one is taken only where the value already chose it.
func option(d Def, id string, previous []string) (Option, error) {
	for _, o := range d.Options {
		if o.ID.String() == id {
			if o.Retired && !slices.Contains(previous, id) {
				return Option{}, valueError(d, "The option %s of %s is retired. Pick another.", o.Name, d.Name)
			}
			return o, nil
		}
	}
	return Option{}, valueError(d, "%s takes one of its options. Pick one from the list.", d.Name)
}

func chosen(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	_ = json.Unmarshal(raw, &many)
	return many
}

// normalize checks a value against its attribute's kind and answers it in its stored form, nil to clear it.
func normalize(d Def, raw, previous json.RawMessage, viewer Viewer) (json.RawMessage, error) {
	if isEmpty(raw) {
		return nil, nil
	}
	switch d.Kind {
	case Text:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, valueError(d, "%s takes text.", d.Name)
		}
		if s = strings.TrimSpace(s); s == "" {
			return nil, nil
		}
		if utf8.RuneCountInString(s) > MaxTextLength {
			return nil, valueError(d, "%s takes at most %d characters. Put longer text in the statement.", d.Name, MaxTextLength)
		}
		return encode(s), nil
	case RichText:
		doc, err := document.Parse(raw)
		if err != nil {
			var invalid *document.InvalidError
			if errors.As(err, &invalid) {
				return nil, valueError(d, "%s", invalid.Message)
			}
			return nil, err
		}
		if len(doc.Content) == 0 || (len(doc.Content) == 1 && doc.Content[0].Type == "paragraph" && len(doc.Content[0].Content) == 0) {
			return nil, nil
		}
		return raw, nil
	case Number:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, valueError(d, "%s takes a number, such as 12 or 2.5.", d.Name)
		}
		return encode(n), nil
	case Select:
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return nil, valueError(d, "%s takes one of its options. Pick one from the list.", d.Name)
		}
		if _, err := option(d, id, chosen(previous)); err != nil {
			return nil, err
		}
		return encode(id), nil
	case MultiSelect:
		var ids []string
		if err := json.Unmarshal(raw, &ids); err != nil {
			return nil, valueError(d, "%s takes a list of its options. Pick them from the list.", d.Name)
		}
		if len(ids) > MaxMultiSelect {
			return nil, valueError(d, "%s takes at most %d options.", d.Name, MaxMultiSelect)
		}
		before := chosen(previous)
		picked := map[string]bool{}
		for _, id := range ids {
			if _, err := option(d, id, before); err != nil {
				return nil, err
			}
			picked[id] = true
		}
		// In the attribute's order, so the same choice is always stored, and hashed, the same way.
		ordered := []string{}
		for _, o := range d.Options {
			if picked[o.ID.String()] {
				ordered = append(ordered, o.ID.String())
			}
		}
		return encode(ordered), nil
	case Date:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, valueError(d, "%s takes a date such as 2026-01-31.", d.Name)
		}
		day, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
		if err != nil {
			return nil, valueError(d, "%s takes a date such as 2026-01-31.", d.Name)
		}
		return encode(day.Format(time.DateOnly)), nil
	case User:
		var id uuid.UUID
		if err := json.Unmarshal(raw, &id); err != nil {
			return nil, valueError(d, "%s takes a person. Pick one from the list.", d.Name)
		}
		views, err := viewer(id)
		if err != nil {
			return nil, err
		}
		if !views {
			return nil, valueError(d, "%s takes a member who may view this project. Pick one from the list.", d.Name)
		}
		return encode(id.String()), nil
	case Boolean:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, valueError(d, "%s is either true or false.", d.Name)
		}
		return encode(b), nil
	case URL:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, valueError(d, "%s takes a web address starting with http:// or https://.", d.Name)
		}
		s = strings.TrimSpace(s)
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, valueError(d, "%s takes a web address starting with http:// or https://.", d.Name)
		}
		return encode(s), nil
	}
	return nil, valueError(d, "%s is of a kind this server does not know.", d.Name)
}
