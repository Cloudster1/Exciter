package openapi

import "strings"

const componentsPrefix = "#/components/schemas/"
const defsPrefix = "#/$defs/"

// SelfContained copies a schema so that it can leave the document: every
// component it reaches is carried along under $defs and the references follow.
func (b *Builder) SelfContained(s *Schema) *Schema {
	if s == nil {
		return nil
	}
	defs := map[string]*Schema{}
	out := b.rewrite(s, defs)
	if len(defs) > 0 {
		out.Defs = defs
	}
	return out
}

// rewrite returns a copy of s with references pointing into defs, filling defs
// with the components met on the way.
func (b *Builder) rewrite(s *Schema, defs map[string]*Schema) *Schema {
	if s == nil {
		return nil
	}
	copied := *s
	if name, ok := strings.CutPrefix(s.Ref, componentsPrefix); ok {
		copied.Ref = defsPrefix + name
		if _, seen := defs[name]; !seen {
			// Reserve the slot before descending, so a type that refers to
			// itself ends the walk.
			defs[name] = nil
			defs[name] = b.rewrite(b.components[name], defs)
		}
		return &copied
	}
	if len(s.Properties) > 0 {
		copied.Properties = make(map[string]*Schema, len(s.Properties))
		for key, each := range s.Properties {
			copied.Properties[key] = b.rewrite(each, defs)
		}
	}
	copied.Items = b.rewrite(s.Items, defs)
	copied.AdditionalProperties = b.rewrite(s.AdditionalProperties, defs)
	if len(s.OneOf) > 0 {
		copied.OneOf = make([]*Schema, len(s.OneOf))
		for i, each := range s.OneOf {
			copied.OneOf[i] = b.rewrite(each, defs)
		}
	}
	return &copied
}
