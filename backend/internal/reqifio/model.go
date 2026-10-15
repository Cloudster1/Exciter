package reqifio

import "time"

// DataKind is the shape of a ReqIF datatype, and of the values of the attributes defined with it.
type DataKind string

const (
	KindString  DataKind = "string"
	KindInteger DataKind = "integer"
	KindReal    DataKind = "real"
	KindBoolean DataKind = "boolean"
	KindDate    DataKind = "date"
	KindXHTML   DataKind = "xhtml"
	KindEnum    DataKind = "enumeration"
)

// TypeKind is what a ReqIF spec type types.
type TypeKind string

const (
	TypeObject        TypeKind = "object"
	TypeSpecification TypeKind = "specification"
	TypeRelation      TypeKind = "relation"
	TypeGroup         TypeKind = "relationGroup"
)

// File is a ReqIF document as an import keeps it and an export writes it: everything the exchange carries,
// without the bytes of its images.
type File struct {
	Identifier string    `json:"identifier"`
	Title      string    `json:"title"`
	Comment    string    `json:"comment"`
	SourceTool string    `json:"sourceTool"`
	ToolID     string    `json:"toolId"`
	Created    time.Time `json:"created"`

	DataTypes      []*DataType      `json:"dataTypes"`
	Types          []*SpecType      `json:"types"`
	Objects        []*Object        `json:"objects"`
	Relations      []*Relation      `json:"relations"`
	Specifications []*Specification `json:"specifications"`
	// Images are the files of the archive the rich text shows, by their path in it.
	Images []Image `json:"images"`
}

// Identity is what every ReqIF element that can be named has.
type Identity struct {
	Identifier string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	Changed    time.Time `json:"changed"`
}

// DataType is a DATATYPE-DEFINITION.
type DataType struct {
	Identity
	Kind DataKind `json:"kind"`
	// MaxLength bounds a string; Min, Max and Accuracy are a number's, as the file writes them.
	MaxLength int         `json:"maxLength,omitempty"`
	Min       string      `json:"min,omitempty"`
	Max       string      `json:"max,omitempty"`
	Accuracy  int         `json:"accuracy,omitempty"`
	Values    []EnumValue `json:"values,omitempty"`
}

// EnumValue is one ENUM-VALUE of an enumeration.
type EnumValue struct {
	Identity
	Key   int    `json:"key"`
	Other string `json:"other,omitempty"`
}

// SpecType is a SPEC-OBJECT-TYPE, SPECIFICATION-TYPE, SPEC-RELATION-TYPE or RELATION-GROUP-TYPE.
type SpecType struct {
	Identity
	Kind       TypeKind   `json:"kind"`
	Attributes []*AttrDef `json:"attributes,omitempty"`
}

// AttrDef is one ATTRIBUTE-DEFINITION of a spec type.
type AttrDef struct {
	Identity
	Kind     DataKind `json:"kind"`
	DataType string   `json:"dataType"`
	Multi    bool     `json:"multi,omitempty"`
}

// Object is a SPEC-OBJECT: one requirement or heading.
type Object struct {
	Identity
	Type   string  `json:"type"`
	Values []Value `json:"values,omitempty"`
}

// Value is one ATTRIBUTE-VALUE: Text for the simple kinds as the file writes them, Enum for the ENUM-VALUEs
// chosen, XHTML for rich text.
type Value struct {
	Def   string   `json:"def"`
	Text  string   `json:"text,omitempty"`
	Enum  []string `json:"enum,omitempty"`
	XHTML *XNode   `json:"xhtml,omitempty"`
}

// Relation is a SPEC-RELATION from one object to another.
type Relation struct {
	Identity
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// Specification is a SPECIFICATION: a tree of objects.
type Specification struct {
	Identity
	Type     string  `json:"type"`
	Children []*Node `json:"children,omitempty"`
}

// Node is one SPEC-HIERARCHY: an object at its place in a specification.
type Node struct {
	Identity
	Object   string  `json:"object"`
	Children []*Node `json:"children,omitempty"`
}

// Image is a file of the archive that rich text shows.
type Image struct {
	Path        string `json:"path"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

// XNode is one node of a rich text value: an element by its local name in lower case, or text.
type XNode struct {
	Name     string            `json:"n,omitempty"`
	Attrs    map[string]string `json:"a,omitempty"`
	Text     string            `json:"t,omitempty"`
	Children []XNode           `json:"c,omitempty"`
}

// index finds a file's elements by their identifiers.
type index struct {
	dataTypes map[string]*DataType
	types     map[string]*SpecType
	defs      map[string]*AttrDef
	// defType is the spec type each attribute definition belongs to.
	defType map[string]*SpecType
	objects map[string]*Object
	enums   map[string]*EnumValue
}

func indexOf(f *File) *index {
	ix := &index{dataTypes: map[string]*DataType{}, types: map[string]*SpecType{}, defs: map[string]*AttrDef{},
		defType: map[string]*SpecType{}, objects: map[string]*Object{}, enums: map[string]*EnumValue{}}
	for _, d := range f.DataTypes {
		ix.dataTypes[d.Identifier] = d
		for i := range d.Values {
			ix.enums[d.Values[i].Identifier] = &d.Values[i]
		}
	}
	for _, t := range f.Types {
		ix.types[t.Identifier] = t
		for _, a := range t.Attributes {
			ix.defs[a.Identifier] = a
			ix.defType[a.Identifier] = t
		}
	}
	for _, o := range f.Objects {
		ix.objects[o.Identifier] = o
	}
	return ix
}

// value is the object's value of the definition def, nil when it has none.
func (o *Object) value(def string) *Value {
	for i := range o.Values {
		if o.Values[i].Def == def {
			return &o.Values[i]
		}
	}
	return nil
}
