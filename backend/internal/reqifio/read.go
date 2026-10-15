package reqifio

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Namespace is the one ReqIF 1.0.1, 1.1 and 1.2 documents share.
const Namespace = "http://www.omg.org/spec/ReqIF/20110401/reqif.xsd"

// XHTMLNamespace is the namespace of rich text in a ReqIF document.
const XHTMLNamespace = "http://www.w3.org/1999/xhtml"

// zipMark opens every zip archive, which a .reqifz is.
var zipMark = []byte("PK\x03\x04")

// FileError is a file an import cannot read, with the sentence to show.
type FileError struct{ Message string }

func (e *FileError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &FileError{fmt.Sprintf(format, args...)} }

// Embedded is a file of an archive the rich text shows, with its bytes, until the import stores them.
type Embedded struct {
	Image
	Data []byte
}

// Read reads an uploaded file, a ReqIF document or a .reqifz archive of one, into what it holds and the files
// its rich text shows.
func Read(data []byte) (*File, Format, []Embedded, error) {
	if bytes.HasPrefix(data, zipMark) {
		f, embedded, err := readArchive(data)
		return f, FormatReqIFZ, embedded, err
	}
	if len(data) > MaxDocumentBytes {
		return nil, "", nil, invalid("The ReqIF document is larger than %d MB, more than one import takes. Export the specifications in several files, and import them one after the other.", MaxDocumentMB)
	}
	f, err := readDocument(data)
	return f, FormatReqIF, nil, err
}

// The elements of a document an import reads, by the element they sit in; every other element is passed over.
var containers = map[string]bool{
	"THE-HEADER": true, "CORE-CONTENT": true, "REQ-IF-CONTENT": true, "DATATYPES": true, "SPEC-TYPES": true,
	"SPEC-OBJECTS": true, "SPEC-RELATIONS": true, "SPECIFICATIONS": true,
}

var dataKinds = map[string]DataKind{
	"DATATYPE-DEFINITION-STRING": KindString, "DATATYPE-DEFINITION-INTEGER": KindInteger,
	"DATATYPE-DEFINITION-REAL": KindReal, "DATATYPE-DEFINITION-BOOLEAN": KindBoolean,
	"DATATYPE-DEFINITION-DATE": KindDate, "DATATYPE-DEFINITION-XHTML": KindXHTML,
	"DATATYPE-DEFINITION-ENUMERATION": KindEnum,
}

var typeKinds = map[string]TypeKind{
	"SPEC-OBJECT-TYPE": TypeObject, "SPECIFICATION-TYPE": TypeSpecification,
	"SPEC-RELATION-TYPE": TypeRelation, "RELATION-GROUP-TYPE": TypeGroup,
}

// suffixKinds read the kind of an attribute definition or value from the end of its element's name.
var suffixKinds = map[string]DataKind{
	"STRING": KindString, "INTEGER": KindInteger, "REAL": KindReal, "BOOLEAN": KindBoolean,
	"DATE": KindDate, "XHTML": KindXHTML, "ENUMERATION": KindEnum,
}

func kindAfter(local, prefix string) (DataKind, bool) {
	rest, ok := strings.CutPrefix(local, prefix)
	if !ok {
		return "", false
	}
	k, ok := suffixKinds[rest]
	return k, ok
}

// readDocument reads a ReqIF document element by element, so only what an import keeps is ever held.
func readDocument(doc []byte) (*File, error) {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "utf8") {
			return input, nil
		}
		return nil, fmt.Errorf("the charset %s", charset)
	}
	f := &File{}
	var (
		stack []string
		root  bool
	)
	broken := func(err error) error {
		return invalid("The file is no XML an import reads (%v). Export it from its tool again as ReqIF, UTF-8 encoded, and upload the new file.", err)
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, broken(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !root {
				if t.Name.Local != "REQ-IF" || t.Name.Space != Namespace {
					return nil, invalid("This is no ReqIF file: its root is %s rather than REQ-IF of the ReqIF namespace. Export the requirements from their tool as ReqIF and upload that file.", t.Name.Local)
				}
				root = true
				stack = append(stack, t.Name.Local)
				continue
			}
			if err := f.readElement(dec, t, stack[len(stack)-1]); err != nil {
				var fileErr *FileError
				if errors.As(err, &fileErr) {
					return nil, err
				}
				return nil, broken(err)
			}
			if containers[t.Name.Local] {
				stack = append(stack, t.Name.Local)
			}
		case xml.EndElement:
			if len(stack) > 0 && stack[len(stack)-1] == t.Name.Local {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !root {
		return nil, invalid("The file is empty. Export the requirements from their tool as ReqIF and upload that file.")
	}
	return f, nil
}

// readElement reads one element below parent: something the file holds, or a container to go into, or
// anything else, which it skips.
func (f *File) readElement(dec *xml.Decoder, t xml.StartElement, parent string) error {
	local := t.Name.Local
	switch {
	case containers[local]:
		return nil
	case parent == "THE-HEADER" && local == "REQ-IF-HEADER":
		var h xHeader
		if err := dec.DecodeElement(&h, &t); err != nil {
			return err
		}
		f.Identifier, f.Title, f.Comment = h.Identifier, strings.TrimSpace(h.Title), strings.TrimSpace(h.Comment)
		f.SourceTool, f.ToolID = strings.TrimSpace(h.SourceTool), strings.TrimSpace(h.ToolID)
		f.Created = parseTime(h.Created)
		return nil
	case parent == "DATATYPES" && dataKinds[local] != "":
		var x xDataType
		if err := dec.DecodeElement(&x, &t); err != nil {
			return err
		}
		f.DataTypes = append(f.DataTypes, x.dataType(dataKinds[local]))
		return nil
	case parent == "SPEC-TYPES" && typeKinds[local] != "":
		var x xSpecType
		if err := dec.DecodeElement(&x, &t); err != nil {
			return err
		}
		f.Types = append(f.Types, x.specType(typeKinds[local]))
		return nil
	case parent == "SPEC-OBJECTS" && local == "SPEC-OBJECT":
		if len(f.Objects) >= MaxObjects {
			return invalid("The file holds more than %d objects, more than one import takes. Export the specifications in several files, and import them one after the other.", MaxObjects)
		}
		var x xObject
		if err := dec.DecodeElement(&x, &t); err != nil {
			return err
		}
		f.Objects = append(f.Objects, x.object())
		return nil
	case parent == "SPEC-RELATIONS" && local == "SPEC-RELATION":
		if len(f.Relations) >= MaxRelations {
			return invalid("The file holds more than %d relations, more than one import takes. Export the specifications in several files, and import them one after the other.", MaxRelations)
		}
		var x xRelation
		if err := dec.DecodeElement(&x, &t); err != nil {
			return err
		}
		f.Relations = append(f.Relations, &Relation{Identity: x.identity(), Type: x.Type.id(), Source: x.Source.id(), Target: x.Target.id()})
		return nil
	case parent == "SPECIFICATIONS" && local == "SPECIFICATION":
		var x xSpecification
		if err := dec.DecodeElement(&x, &t); err != nil {
			return err
		}
		children, err := nodes(x.Children, 1)
		if err != nil {
			return err
		}
		f.Specifications = append(f.Specifications, &Specification{Identity: x.identity(), Type: x.Type.id(), Children: children})
		return nil
	}
	return dec.Skip()
}

func nodes(in []xHierarchy, depth int) ([]*Node, error) {
	if len(in) > 0 && depth > MaxDepth {
		return nil, invalid("A specification nests more than %d levels deep, deeper than a project's tree goes. Flatten it in its tool and export it again.", MaxDepth)
	}
	out := make([]*Node, 0, len(in))
	for _, h := range in {
		children, err := nodes(h.Children, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, &Node{Identity: h.identity(), Object: h.Object.id(), Children: children})
	}
	return out, nil
}

// The shapes encoding/xml decodes a document's elements into.
type (
	xIdent struct {
		Identifier string `xml:"IDENTIFIER,attr"`
		LongName   string `xml:"LONG-NAME,attr"`
		LastChange string `xml:"LAST-CHANGE,attr"`
	}
	xHeader struct {
		xIdent
		Comment    string `xml:"COMMENT"`
		Created    string `xml:"CREATION-TIME"`
		ToolID     string `xml:"REQ-IF-TOOL-ID"`
		SourceTool string `xml:"SOURCE-TOOL-ID"`
		Title      string `xml:"TITLE"`
	}
	// xRef is an element that holds one reference element, such as TYPE holding SPEC-OBJECT-TYPE-REF.
	xRef struct {
		Refs []xText `xml:",any"`
	}
	xText struct {
		XMLName xml.Name
		Text    string `xml:",chardata"`
	}
	xDataType struct {
		xIdent
		MaxLength string       `xml:"MAX-LENGTH,attr"`
		Min       string       `xml:"MIN,attr"`
		Max       string       `xml:"MAX,attr"`
		Accuracy  string       `xml:"ACCURACY,attr"`
		Values    []xEnumValue `xml:"SPECIFIED-VALUES>ENUM-VALUE"`
	}
	xEnumValue struct {
		xIdent
		Embedded struct {
			Key   string `xml:"KEY,attr"`
			Other string `xml:"OTHER-CONTENT,attr"`
		} `xml:"PROPERTIES>EMBEDDED-VALUE"`
	}
	xSpecType struct {
		xIdent
		Attributes struct {
			Defs []xAttrDef `xml:",any"`
		} `xml:"SPEC-ATTRIBUTES"`
	}
	xAttrDef struct {
		XMLName xml.Name
		xIdent
		Multi string `xml:"MULTI-VALUED,attr"`
		Type  xRef   `xml:"TYPE"`
	}
	xObject struct {
		xIdent
		Type   xRef `xml:"TYPE"`
		Values struct {
			Values []xValue `xml:",any"`
		} `xml:"VALUES"`
	}
	xValue struct {
		XMLName xml.Name
		Attr    string      `xml:"THE-VALUE,attr"`
		Def     xRef        `xml:"DEFINITION"`
		Enum    []string    `xml:"VALUES>ENUM-VALUE-REF"`
		XHTML   *xhtmlValue `xml:"THE-VALUE"`
	}
	xRelation struct {
		xIdent
		Type   xRef `xml:"TYPE"`
		Source xRef `xml:"SOURCE"`
		Target xRef `xml:"TARGET"`
	}
	xSpecification struct {
		xIdent
		Type     xRef         `xml:"TYPE"`
		Children []xHierarchy `xml:"CHILDREN>SPEC-HIERARCHY"`
	}
	xHierarchy struct {
		xIdent
		Object   xRef         `xml:"OBJECT"`
		Children []xHierarchy `xml:"CHILDREN>SPEC-HIERARCHY"`
	}
)

func (x xIdent) identity() Identity {
	return Identity{Identifier: strings.TrimSpace(x.Identifier), Name: strings.TrimSpace(x.LongName), Changed: parseTime(x.LastChange)}
}

func (r xRef) id() string {
	if len(r.Refs) == 0 {
		return ""
	}
	return strings.TrimSpace(r.Refs[0].Text)
}

func (x xDataType) dataType(kind DataKind) *DataType {
	d := &DataType{Identity: x.identity(), Kind: kind, Min: x.Min, Max: x.Max}
	d.MaxLength, _ = strconv.Atoi(strings.TrimSpace(x.MaxLength))
	d.Accuracy, _ = strconv.Atoi(strings.TrimSpace(x.Accuracy))
	for _, v := range x.Values {
		key, _ := strconv.Atoi(strings.TrimSpace(v.Embedded.Key))
		d.Values = append(d.Values, EnumValue{Identity: v.identity(), Key: key, Other: v.Embedded.Other})
	}
	return d
}

func (x xSpecType) specType(kind TypeKind) *SpecType {
	t := &SpecType{Identity: x.identity(), Kind: kind}
	for _, a := range x.Attributes.Defs {
		k, ok := kindAfter(a.XMLName.Local, "ATTRIBUTE-DEFINITION-")
		if !ok {
			continue
		}
		t.Attributes = append(t.Attributes, &AttrDef{Identity: a.identity(), Kind: k, DataType: a.Type.id(), Multi: a.Multi == "true"})
	}
	return t
}

func (x xObject) object() *Object {
	o := &Object{Identity: x.identity(), Type: x.Type.id()}
	for _, v := range x.Values.Values {
		k, ok := kindAfter(v.XMLName.Local, "ATTRIBUTE-VALUE-")
		if !ok {
			continue
		}
		value := Value{Def: v.Def.id()}
		switch k {
		case KindEnum:
			for _, ref := range v.Enum {
				if ref = strings.TrimSpace(ref); ref != "" {
					value.Enum = append(value.Enum, ref)
				}
			}
		case KindXHTML:
			if v.XHTML == nil {
				continue
			}
			value.XHTML = &XNode{Children: v.XHTML.children}
		default:
			value.Text = v.Attr
		}
		o.Values = append(o.Values, value)
	}
	return o
}

// xhtmlValue is the content of THE-VALUE of a rich text value, read as XNodes.
type xhtmlValue struct {
	children []XNode
}

func (v *xhtmlValue) UnmarshalXML(dec *xml.Decoder, _ xml.StartElement) error {
	children, err := readXHTML(dec, 0)
	v.children = children
	return err
}

// readXHTML reads the content of an element up to its end; past MaxXHTMLDepth an element is kept as its words.
func readXHTML(dec *xml.Decoder, depth int) ([]XNode, error) {
	var out []XNode
	text := func(s string) {
		if s == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Name == "" {
			out[n-1].Text += s
			return
		}
		out = append(out, XNode{Text: s})
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth >= MaxXHTMLDepth {
				words, err := wordsUntilEnd(dec)
				if err != nil {
					return nil, err
				}
				text(words)
				continue
			}
			n := XNode{Name: strings.ToLower(t.Name.Local)}
			for _, a := range t.Attr {
				if a.Name.Space != "" && a.Name.Space != XHTMLNamespace {
					continue
				}
				if n.Attrs == nil {
					n.Attrs = map[string]string{}
				}
				n.Attrs[strings.ToLower(a.Name.Local)] = a.Value
			}
			if n.Children, err = readXHTML(dec, depth+1); err != nil {
				return nil, err
			}
			out = append(out, n)
		case xml.CharData:
			text(string(t))
		case xml.EndElement:
			return out, nil
		}
	}
}

// wordsUntilEnd reads what is left of an element as its text alone.
func wordsUntilEnd(dec *xml.Decoder) (string, error) {
	var b strings.Builder
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			b.Write(t)
		}
	}
	return b.String(), nil
}

// timeLayouts are the forms of xsd:dateTime tools write, with and without a zone and fractions.
var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02"}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
