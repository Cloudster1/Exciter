package reqifio

import (
	"bufio"
	"encoding/xml"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Version is the REQ-IF-VERSION every ReqIF document since 1.0 carries.
const Version = "1.0"

// stampLayout is xsd:dateTime in UTC with milliseconds, as most tools write it.
const stampLayout = "2006-01-02T15:04:05.000Z"

var dataElements = map[DataKind]string{
	KindString: "STRING", KindInteger: "INTEGER", KindReal: "REAL", KindBoolean: "BOOLEAN",
	KindDate: "DATE", KindXHTML: "XHTML", KindEnum: "ENUMERATION",
}

var typeElements = map[TypeKind]string{
	TypeObject: "SPEC-OBJECT-TYPE", TypeSpecification: "SPECIFICATION-TYPE",
	TypeRelation: "SPEC-RELATION-TYPE", TypeGroup: "RELATION-GROUP-TYPE",
}

// Write writes a file as a ReqIF document.
func Write(w io.Writer, f *File) error {
	x := &writer{w: bufio.NewWriter(w)}
	ix := indexOf(f)
	x.raw(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	x.open("REQ-IF", "xmlns", Namespace, "xmlns:xhtml", XHTMLNamespace)
	x.open("THE-HEADER")
	x.open("REQ-IF-HEADER", "IDENTIFIER", f.Identifier)
	if f.Comment != "" {
		x.leaf("COMMENT", f.Comment)
	}
	x.leaf("CREATION-TIME", stamp(f.Created))
	x.leaf("REQ-IF-TOOL-ID", f.ToolID)
	x.leaf("REQ-IF-VERSION", Version)
	x.leaf("SOURCE-TOOL-ID", f.SourceTool)
	x.leaf("TITLE", f.Title)
	x.close("REQ-IF-HEADER")
	x.close("THE-HEADER")
	x.open("CORE-CONTENT")
	x.open("REQ-IF-CONTENT")
	if len(f.DataTypes) > 0 {
		x.open("DATATYPES")
		for _, d := range f.DataTypes {
			x.dataType(d)
		}
		x.close("DATATYPES")
	}
	if len(f.Types) > 0 {
		x.open("SPEC-TYPES")
		for _, t := range f.Types {
			x.specType(t)
		}
		x.close("SPEC-TYPES")
	}
	if len(f.Objects) > 0 {
		x.open("SPEC-OBJECTS")
		for _, o := range f.Objects {
			x.object(o, ix)
		}
		x.close("SPEC-OBJECTS")
	}
	if len(f.Relations) > 0 {
		x.open("SPEC-RELATIONS")
		for _, r := range f.Relations {
			x.open("SPEC-RELATION", identity(r.Identity)...)
			x.ref("SOURCE", "SPEC-OBJECT-REF", r.Source)
			x.ref("TARGET", "SPEC-OBJECT-REF", r.Target)
			x.ref("TYPE", "SPEC-RELATION-TYPE-REF", r.Type)
			x.close("SPEC-RELATION")
		}
		x.close("SPEC-RELATIONS")
	}
	if len(f.Specifications) > 0 {
		x.open("SPECIFICATIONS")
		for _, s := range f.Specifications {
			x.open("SPECIFICATION", identity(s.Identity)...)
			x.ref("TYPE", "SPECIFICATION-TYPE-REF", s.Type)
			x.hierarchy(s.Children)
			x.close("SPECIFICATION")
		}
		x.close("SPECIFICATIONS")
	}
	x.close("REQ-IF-CONTENT")
	x.close("CORE-CONTENT")
	x.close("REQ-IF")
	if x.err != nil {
		return x.err
	}
	return x.w.Flush()
}

func stamp(t time.Time) string {
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	return t.UTC().Format(stampLayout)
}

// identity is the attributes every identifiable element carries, the name only when it has one.
func identity(id Identity) []string {
	attrs := []string{"IDENTIFIER", id.Identifier, "LAST-CHANGE", stamp(id.Changed)}
	if id.Name != "" {
		attrs = append(attrs, "LONG-NAME", id.Name)
	}
	return attrs
}

func (x *writer) dataType(d *DataType) {
	name := "DATATYPE-DEFINITION-" + dataElements[d.Kind]
	attrs := identity(d.Identity)
	switch d.Kind {
	case KindString:
		attrs = append(attrs, "MAX-LENGTH", strconv.Itoa(d.MaxLength))
	case KindInteger:
		attrs = append(attrs, "MAX", d.Max, "MIN", d.Min)
	case KindReal:
		attrs = append(attrs, "ACCURACY", strconv.Itoa(d.Accuracy), "MAX", d.Max, "MIN", d.Min)
	}
	if d.Kind != KindEnum {
		x.empty(name, attrs...)
		return
	}
	x.open(name, attrs...)
	x.open("SPECIFIED-VALUES")
	for _, v := range d.Values {
		x.open("ENUM-VALUE", identity(v.Identity)...)
		x.open("PROPERTIES")
		x.empty("EMBEDDED-VALUE", "KEY", strconv.Itoa(v.Key), "OTHER-CONTENT", v.Other)
		x.close("PROPERTIES")
		x.close("ENUM-VALUE")
	}
	x.close("SPECIFIED-VALUES")
	x.close(name)
}

func (x *writer) specType(t *SpecType) {
	name := typeElements[t.Kind]
	if len(t.Attributes) == 0 {
		x.empty(name, identity(t.Identity)...)
		return
	}
	x.open(name, identity(t.Identity)...)
	x.open("SPEC-ATTRIBUTES")
	for _, a := range t.Attributes {
		kind := dataElements[a.Kind]
		attrs := identity(a.Identity)
		if a.Kind == KindEnum {
			attrs = append(attrs, "MULTI-VALUED", strconv.FormatBool(a.Multi))
		}
		x.open("ATTRIBUTE-DEFINITION-"+kind, attrs...)
		x.ref("TYPE", "DATATYPE-DEFINITION-"+kind+"-REF", a.DataType)
		x.close("ATTRIBUTE-DEFINITION-" + kind)
	}
	x.close("SPEC-ATTRIBUTES")
	x.close(name)
}

func (x *writer) object(o *Object, ix *index) {
	x.open("SPEC-OBJECT", identity(o.Identity)...)
	x.ref("TYPE", "SPEC-OBJECT-TYPE-REF", o.Type)
	var values []Value
	for _, v := range o.Values {
		if ix.defs[v.Def] != nil {
			values = append(values, v)
		}
	}
	if len(values) > 0 {
		x.open("VALUES")
		for _, v := range values {
			kind := dataElements[ix.defs[v.Def].Kind]
			name := "ATTRIBUTE-VALUE-" + kind
			defRef := "ATTRIBUTE-DEFINITION-" + kind + "-REF"
			switch ix.defs[v.Def].Kind {
			case KindEnum:
				x.open(name)
				x.ref("DEFINITION", defRef, v.Def)
				x.open("VALUES")
				for _, e := range v.Enum {
					x.leaf("ENUM-VALUE-REF", e)
				}
				x.close("VALUES")
				x.close(name)
			case KindXHTML:
				x.open(name)
				x.ref("DEFINITION", defRef, v.Def)
				x.open("THE-VALUE")
				if v.XHTML != nil {
					x.xhtml(*v.XHTML)
				}
				x.close("THE-VALUE")
				x.close(name)
			default:
				x.open(name, "THE-VALUE", v.Text)
				x.ref("DEFINITION", defRef, v.Def)
				x.close(name)
			}
		}
		x.close("VALUES")
	}
	x.close("SPEC-OBJECT")
}

func (x *writer) hierarchy(nodes []*Node) {
	if len(nodes) == 0 {
		return
	}
	x.open("CHILDREN")
	for _, n := range nodes {
		x.open("SPEC-HIERARCHY", identity(n.Identity)...)
		x.ref("OBJECT", "SPEC-OBJECT-REF", n.Object)
		x.hierarchy(n.Children)
		x.close("SPEC-HIERARCHY")
	}
	x.close("CHILDREN")
}

// voidElements are the XHTML elements that hold nothing.
var voidElements = map[string]bool{"br": true, "hr": true, "col": true, "param": true}

func (x *writer) xhtml(n XNode) {
	if n.Name == "" {
		if n.Text != "" {
			x.text(n.Text)
		}
		for _, c := range n.Children {
			x.xhtml(c)
		}
		return
	}
	name := "xhtml:" + n.Name
	var attrs []string
	for _, k := range slices.Sorted(maps.Keys(n.Attrs)) {
		attrs = append(attrs, k, n.Attrs[k])
	}
	if voidElements[n.Name] {
		x.empty(name, attrs...)
		return
	}
	x.open(name, attrs...)
	for _, c := range n.Children {
		x.xhtml(c)
	}
	x.close(name)
}

// writer writes XML by hand, so the XHTML keeps its xhtml prefix; the first error stops it.
type writer struct {
	w   *bufio.Writer
	err error
}

func (x *writer) raw(s string) {
	if x.err == nil {
		_, x.err = x.w.WriteString(s)
	}
}

func (x *writer) text(s string) {
	if x.err == nil {
		x.err = xml.EscapeText(x.w, []byte(s))
	}
}

func (x *writer) start(name string, attrs []string) {
	x.raw("<" + name)
	for i := 0; i+1 < len(attrs); i += 2 {
		x.raw(" " + attrs[i] + `="`)
		x.text(attrs[i+1])
		x.raw(`"`)
	}
}

func (x *writer) open(name string, attrs ...string) {
	x.start(name, attrs)
	x.raw(">")
}

func (x *writer) empty(name string, attrs ...string) {
	x.start(name, attrs)
	x.raw("/>")
}

func (x *writer) close(name string) { x.raw("</" + name + ">") }

func (x *writer) leaf(name, text string) {
	x.open(name)
	x.text(text)
	x.close(name)
}

// ref writes an element holding one reference, such as TYPE holding SPEC-OBJECT-TYPE-REF.
func (x *writer) ref(outer, inner, id string) {
	x.open(outer)
	x.leaf(inner, strings.TrimSpace(id))
	x.close(outer)
}
