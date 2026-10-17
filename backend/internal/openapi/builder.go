package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Builder turns Go types into schemas, naming struct types as components so
// that a type used in twenty responses is described once.
type Builder struct {
	// Overrides replace the derived schema for a type, for the request patch
	// types whose JSON shape is nothing like their Go shape.
	Overrides map[reflect.Type]*Schema
	// Enums lists the values a named string type takes.
	Enums map[reflect.Type][]string
	// Descriptions annotate a component.
	Descriptions map[reflect.Type]string
	// FieldOverrides replace one struct field's schema, keyed "Type.jsonName", for fields whose Go type says
	// less than the API promises, such as a raw JSON message that is always a rich text document.
	FieldOverrides map[string]*Schema
	// Names give a struct type its component name, for two types that share a
	// Go name in different packages; without one the builder refuses the pair.
	Names map[reflect.Type]string

	components map[string]*Schema
	names      map[reflect.Type]string
	taken      map[string]reflect.Type
}

func NewBuilder() *Builder {
	return &Builder{
		Overrides:      map[reflect.Type]*Schema{},
		Enums:          map[reflect.Type][]string{},
		Descriptions:   map[reflect.Type]string{},
		FieldOverrides: map[string]*Schema{},
		Names:          map[reflect.Type]string{},
		components:     map[string]*Schema{},
		names:          map[reflect.Type]string{},
		taken:          map[string]reflect.Type{},
	}
}

// Components is everything named so far.
func (b *Builder) Components() map[string]*Schema { return b.components }

var (
	timeType = reflect.TypeOf(time.Time{})
	rawType  = reflect.TypeOf(json.RawMessage{})
)

// isUUID recognises github.com/google/uuid.UUID without importing it.
func isUUID(t reflect.Type) bool {
	return t.Name() == "UUID" && strings.HasSuffix(t.PkgPath(), "google/uuid")
}

// SchemaOf describes a value's type; a nil value describes nothing.
func (b *Builder) SchemaOf(v any) *Schema {
	if v == nil {
		return nil
	}
	return b.Schema(reflect.TypeOf(v))
}

// Schema describes a type, naming structs as components and returning a
// reference to them.
func (b *Builder) Schema(t reflect.Type) *Schema {
	if s, ok := b.Overrides[t]; ok {
		copied := *s
		return &copied
	}
	if values, ok := b.Enums[t]; ok {
		return &Schema{Type: "string", Enum: values}
	}
	switch {
	case t == timeType:
		return &Schema{Type: "string", Format: "date-time"}
	case t == rawType:
		return &Schema{Description: "A JSON value."}
	case isUUID(t):
		return &Schema{Type: "string", Format: "uuid"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return Nullable(b.Schema(t.Elem()))
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: "string", Format: "byte"}
		}
		return &Schema{Type: "array", Items: b.Schema(t.Elem())}
	case reflect.Map:
		return &Schema{Type: "object", AdditionalProperties: b.Schema(t.Elem())}
	case reflect.Interface:
		return &Schema{Description: "A JSON value."}
	case reflect.Struct:
		if t.Name() == "" {
			return b.object(t)
		}
		return &Schema{Ref: "#/components/schemas/" + b.name(t)}
	}
	panic(fmt.Sprintf("openapi: no schema for %s", t))
}

// Inline describes a struct's fields in place rather than as a component, for a form whose fields sit beside its
// files.
func (b *Builder) Inline(v any) *Schema {
	return b.object(reflect.TypeOf(v))
}

// name registers a struct type as a component and returns its name. Two types sharing a name are refused,
// since keeping either one would document the other's routes with the wrong body.
func (b *Builder) name(t reflect.Type) string {
	if name, ok := b.names[t]; ok {
		return name
	}
	name := b.componentName(t)
	if other, clash := b.taken[name]; clash && other != t {
		panic(fmt.Sprintf("openapi: %s and %s both map to the schema name %s; "+
			"give one of them its own name in Builder.Names", qualifiedName(other), qualifiedName(t), name))
	}
	b.names[t] = name
	b.taken[name] = t
	// Register before descending so a type that refers to itself terminates.
	b.components[name] = &Schema{Type: "object"}
	s := b.object(t)
	s.Description = b.Descriptions[t]
	b.components[name] = s
	return name
}

// componentName is the name a struct type is documented under.
func (b *Builder) componentName(t reflect.Type) string {
	if name, ok := b.Names[t]; ok {
		return name
	}
	return exportedName(t)
}

func qualifiedName(t reflect.Type) string {
	return t.PkgPath() + "." + t.Name()
}

// object describes a struct the way encoding/json writes it: tag names,
// embedded structs flattened, omitempty optional, pointers nullable.
func (b *Builder) object(t reflect.Type) *Schema {
	s := &Schema{Type: "object", Properties: map[string]*Schema{}}
	b.fields(t, t, s)
	return s
}

// fields adds t's fields to into; owner is the struct being described, which
// an embedded struct's fields still belong to for the purpose of overrides.
func (b *Builder) fields(owner, t reflect.Type, into *Schema) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			embedded := f.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				b.fields(owner, embedded, into)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		omitempty := strings.Contains(","+opts+",", ",omitempty,")
		_, overridden := b.Overrides[f.Type]
		schema := b.Schema(f.Type)
		if f.Type.Kind() == reflect.Pointer && omitempty && !overridden {
			// Absent when nil rather than null, so the value itself is not nullable.
			schema = b.Schema(f.Type.Elem())
		}
		if s, ok := b.FieldOverrides[b.componentName(owner)+"."+name]; ok {
			copied := *s
			schema = &copied
		}
		into.Properties[name] = schema
		if !omitempty && !overridden {
			into.Required = append(into.Required, name)
		}
	}
}

// exportedName is a type's name capitalised: request types are unexported in
// the server, and a client wants them named like everything else.
func exportedName(t reflect.Type) string {
	if t.Name() == "" {
		return ""
	}
	return strings.ToUpper(t.Name()[:1]) + t.Name()[1:]
}
