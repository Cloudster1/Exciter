package openapi

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Validate checks a decoded JSON value against a schema in the document, understanding exactly what Builder
// produces: a response that does not fit is a server that does not do what its own document says.
func (d *Document) Validate(s *Schema, value any) error {
	return d.validate(s, value, "$")
}

// ValidateJSON decodes raw JSON and validates it.
func (d *Document) ValidateJSON(s *Schema, raw []byte) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("body is not JSON: %w", err)
	}
	return d.Validate(s, value)
}

// Resolve follows a reference to its component.
func (d *Document) Resolve(s *Schema) (*Schema, error) {
	for s != nil && s.Ref != "" {
		name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		target, ok := d.Components.Schemas[name]
		if !ok {
			return nil, fmt.Errorf("unknown component %q", name)
		}
		s = target
	}
	return s, nil
}

func (d *Document) validate(s *Schema, value any, at string) error {
	if s == nil {
		return nil
	}
	s, err := d.Resolve(s)
	if err != nil {
		return err
	}
	// Each alternative is tried in turn, which is anyOf's reading; the builder only writes oneOf for alternatives that
	// cannot both match.
	if alternatives := slices.Concat(s.OneOf, s.AnyOf); len(alternatives) > 0 {
		var reasons []string
		for _, option := range alternatives {
			if err := d.validate(option, value, at); err == nil {
				return nil
			} else {
				reasons = append(reasons, err.Error())
			}
		}
		return fmt.Errorf("%s matches none of the alternatives: %s", at, strings.Join(reasons, "; "))
	}
	types := s.Types()
	if len(types) == 0 {
		return nil // any JSON value
	}
	actual := jsonType(value)
	fits := false
	for _, t := range types {
		if t == actual || (t == "number" && actual == "integer") || (t == "integer" && actual == "number" && isIntegral(value)) {
			fits = true
			break
		}
	}
	if !fits {
		return fmt.Errorf("%s is %s, want %s", at, actual, strings.Join(types, " or "))
	}
	if actual == "null" {
		return nil
	}
	if len(s.Enum) > 0 {
		str, _ := value.(string)
		found := false
		for _, allowed := range s.Enum {
			if allowed == str {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s is %q, want one of %s", at, str, strings.Join(s.Enum, ", "))
		}
	}
	switch actual {
	case "object":
		obj := value.(map[string]any)
		for _, name := range s.Required {
			if _, ok := obj[name]; !ok {
				return fmt.Errorf("%s lacks required property %q", at, name)
			}
		}
		for name, prop := range s.Properties {
			if v, ok := obj[name]; ok {
				if err := d.validate(prop, v, at+"."+name); err != nil {
					return err
				}
			}
		}
		if s.AdditionalProperties != nil {
			for name, v := range obj {
				if _, declared := s.Properties[name]; declared {
					continue
				}
				if err := d.validate(s.AdditionalProperties, v, at+"."+name); err != nil {
					return err
				}
			}
		}
	case "array":
		for i, item := range value.([]any) {
			if err := d.validate(s.Items, item, fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonType(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if isIntegral(t) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func isIntegral(v any) bool {
	f, ok := v.(float64)
	return ok && f == math.Trunc(f) && !math.IsInf(f, 0)
}
