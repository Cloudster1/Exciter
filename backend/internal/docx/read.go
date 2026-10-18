package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/ziplimit"
)

// The limits of reading a document, the ones a ReqIF archive is read against.
const (
	megabyte = 1 << 20
	// MaxPartMB bounds one XML part of the package, such as the document itself.
	MaxPartMB    = 32
	MaxPartBytes = MaxPartMB * megabyte
	// MaxMediaMB bounds one picture, an attachment's default limit.
	MaxMediaMB    = 50
	MaxMediaBytes = MaxMediaMB * megabyte
	// MaxUnpackedMB bounds everything the package unpacks into together.
	MaxUnpackedMB    = 128
	MaxUnpackedBytes = MaxUnpackedMB * megabyte
	// maxTreeDepth bounds how deep one block nests before the rest of it is passed over.
	maxTreeDepth = 256
)

// ErrNotWord is a file that is no Word document: no zip, or a zip without a document in it.
var ErrNotWord = errors.New("the file is not a Word document")

// TooLargeError is a document that unpacks into more than a reader takes.
type TooLargeError struct {
	Part  string
	Limit int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("the document's %s unpacks into more than %d MB", e.Part, e.Limit/megabyte)
}

// The namespaces a reader tells apart; others are matched by an element's local name alone.
const (
	nsWStrict   = "http://purl.oclc.org/ooxml/wordprocessingml/main"
	nsRStrict   = "http://purl.oclc.org/ooxml/officeDocument/relationships"
	nsMath      = "http://schemas.openxmlformats.org/officeDocument/2006/math"
	nsMarkup    = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	relDocument = "officeDocument"
)

// Read reads a .docx into its blocks, its styles and its pictures, as an import takes it.
func Read(data []byte) (*Source, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotWord
	}
	pkg := &pkg{b: ziplimit.Open(zr, MaxUnpackedBytes)}
	if pkg.b.Files["[Content_Types].xml"] == nil {
		return nil, ErrNotWord
	}
	if err := pkg.readContentTypes(); err != nil {
		return nil, err
	}
	main := pkg.mainPart()
	if main == "" {
		return nil, ErrNotWord
	}
	r := &reader{pkg: pkg, main: main, src: &Source{Styles: []StyleUse{}, Blocks: []Block{}, Media: []Media{}, Losses: []Loss{}},
		media: map[string]*Media{}, counters: map[string]*counter{}, styleCount: map[string]int{}, lossSeen: map[string]bool{}}
	if r.rels, err = pkg.relationships(main); err != nil {
		return nil, err
	}
	if err := r.readStyles(); err != nil {
		return nil, err
	}
	if err := r.readNumbering(); err != nil {
		return nil, err
	}
	if err := r.readMeta(); err != nil {
		return nil, err
	}
	doc, err := pkg.part(main)
	if err != nil {
		return nil, err
	}
	if err := r.readBody(doc); err != nil {
		return nil, err
	}
	r.finish()
	return r.src, nil
}

// pkg is the package of parts a document is, read against the limits.
type pkg struct {
	b *ziplimit.Bundle
	// types are the content types by extension, and by part name with its leading slash.
	defaults  map[string]string
	overrides map[string]string
}

// part unpacks a part by its name in the package; a part the package lacks is nil.
func (p *pkg) part(name string) ([]byte, error) {
	f := p.b.Files[name]
	if f == nil {
		return nil, nil
	}
	data, err := p.b.ReadFile(f, name, MaxPartBytes)
	return data, readError(err, name)
}

func readError(err error, name string) error {
	var large *ziplimit.TooLargeError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &large):
		return &TooLargeError{Part: name, Limit: large.Limit}
	}
	return ErrNotWord
}

func (p *pkg) readContentTypes() error {
	data, err := p.part("[Content_Types].xml")
	if err != nil {
		return err
	}
	root, err := parse(data)
	if err != nil {
		return ErrNotWord
	}
	p.defaults, p.overrides = map[string]string{}, map[string]string{}
	for _, n := range root.kids {
		switch n.local() {
		case "Default":
			p.defaults[strings.ToLower(n.attr("Extension"))] = n.attr("ContentType")
		case "Override":
			p.overrides[strings.TrimPrefix(n.attr("PartName"), "/")] = n.attr("ContentType")
		}
	}
	return nil
}

// contentType is what the package says a part is.
func (p *pkg) contentType(name string) string {
	if t, ok := p.overrides[name]; ok {
		return t
	}
	return p.defaults[strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))]
}

// mainPart is the document the package's relationships name, or the one Word always writes.
func (p *pkg) mainPart() string {
	rels, err := p.relationships("")
	if err == nil {
		for _, r := range rels {
			if strings.HasSuffix(r.kind, "/"+relDocument) && !r.external && p.b.Files[r.target] != nil {
				return r.target
			}
		}
	}
	if p.b.Files["word/document.xml"] != nil {
		return "word/document.xml"
	}
	return ""
}

// rel is one relationship of a part: what its id names, a part of the package or an address outside it.
type rel struct {
	kind, target string
	external     bool
}

// relationships reads a part's relationships by id, their targets resolved against the part.
func (p *pkg) relationships(part string) (map[string]rel, error) {
	dir, file := path.Split(part)
	data, err := p.part(dir + "_rels/" + file + ".rels")
	if err != nil || data == nil {
		return map[string]rel{}, err
	}
	root, err := parse(data)
	if err != nil {
		return map[string]rel{}, nil
	}
	out := map[string]rel{}
	for _, n := range root.kids {
		if n.local() != "Relationship" {
			continue
		}
		r := rel{kind: n.attr("Type"), target: n.attr("Target"), external: strings.EqualFold(n.attr("TargetMode"), "External")}
		if !r.external {
			if strings.HasPrefix(r.target, "/") {
				r.target = strings.TrimPrefix(r.target, "/")
			} else {
				r.target = path.Clean(path.Join(dir, r.target))
			}
		}
		out[n.attr("Id")] = r
	}
	return out, nil
}

// related is the part of the document's relationship of a kind, or the name Word gives it.
func (r *reader) related(kind, fallback string) string {
	for _, rel := range r.rels {
		if strings.HasSuffix(rel.kind, "/"+kind) && !rel.external {
			return rel.target
		}
	}
	return path.Join(path.Dir(r.main), fallback)
}

// readMeta reads the document's title and the identifier Word keeps for it.
func (r *reader) readMeta() error {
	data, err := r.pkg.part("docProps/core.xml")
	if err != nil {
		return err
	}
	if root, err := parse(data); err == nil && root != nil {
		for _, n := range root.kids {
			if n.local() == "title" {
				r.src.Title = strings.TrimSpace(n.words())
			}
		}
	}
	data, err = r.pkg.part(r.related("settings", "settings.xml"))
	if err != nil {
		return err
	}
	root, err := parse(data)
	if err != nil || root == nil {
		return nil
	}
	var w14 string
	for _, n := range root.kids {
		if n.local() != "docId" {
			continue
		}
		if strings.Contains(n.name.Space, "word/2012") {
			r.src.DocID = n.attr("val")
		} else if w14 == "" {
			w14 = n.attr("val")
		}
	}
	if r.src.DocID == "" {
		r.src.DocID = w14
	}
	r.src.DocID = strings.Trim(r.src.DocID, "{}")
	return nil
}

// picture reads a picture the document embeds by its relationship, once; nil is one it cannot carry.
func (r *reader) picture(id string) (*Media, string) {
	rel, ok := r.rels[id]
	if !ok {
		return nil, "A picture the document names is missing from it, and is left out."
	}
	if rel.external {
		return nil, "A picture the document links to outside itself is left out."
	}
	if m, seen := r.media[rel.target]; seen {
		if m == nil {
			return nil, "A picture could not be read and is left out."
		}
		return m, ""
	}
	f := r.pkg.b.Files[rel.target]
	var m *Media
	if f != nil {
		data, err := r.pkg.b.ReadFile(f, rel.target, MaxMediaBytes)
		var large *ziplimit.TooLargeError
		switch {
		case errors.As(err, &large) && large.Whole:
			r.err = &TooLargeError{Part: rel.target, Limit: large.Limit}
		case err == nil && len(data) > 0:
			contentType := r.pkg.contentType(rel.target)
			if contentType == "" || contentType == "application/octet-stream" {
				contentType = http.DetectContentType(data)
			}
			m = &Media{Path: rel.target, ContentType: contentType, Size: len(data), Data: data}
			r.src.Media = append(r.src.Media, *m)
		}
	}
	r.media[rel.target] = m
	if m == nil {
		return nil, "A picture could not be read and is left out."
	}
	return m, ""
}

// node is an element of a part with what is inside it, or text when its name is empty.
type node struct {
	name  xml.Name
	attrs []xml.Attr
	kids  []*node
	text  string
}

func (n *node) local() string { return n.name.Local }

// attr is an attribute by its local name, whatever its namespace.
func (n *node) attr(local string) string {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// relAttr is an attribute of the relationships' namespace, such as r:id, which w:id must not be taken for.
func (n *node) relAttr(local string) string {
	for _, a := range n.attrs {
		if a.Name.Local == local && (a.Name.Space == nsR || a.Name.Space == nsRStrict) {
			return a.Value
		}
	}
	return ""
}

// child is the first element of the local name directly inside n, nil when there is none.
func (n *node) child(local string) *node {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name.Local == local {
			return k
		}
	}
	return nil
}

// find is the first element of the local name anywhere below n.
func (n *node) find(local string) *node {
	for _, k := range n.kids {
		if k.name.Local == local {
			return k
		}
		if found := k.find(local); found != nil {
			return found
		}
	}
	return nil
}

// words are the text below n.
func (n *node) words() string {
	var b strings.Builder
	var walk func(*node)
	walk = func(x *node) {
		if x.name.Local == "" {
			b.WriteString(x.text)
		}
		for _, k := range x.kids {
			walk(k)
		}
	}
	walk(n)
	return b.String()
}

// on reads a toggle such as w:b: present and not switched off.
func on(n *node) bool {
	if n == nil {
		return false
	}
	switch strings.ToLower(n.attr("val")) {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

func newDecoder(data []byte) *xml.Decoder {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "utf8") {
			return input, nil
		}
		return nil, fmt.Errorf("the charset %s", charset)
	}
	return dec
}

// parse reads a whole part as a tree; a part the package lacks is nil.
func parse(data []byte) (*node, error) {
	if data == nil {
		return nil, nil
	}
	dec := newDecoder(data)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			return tree(dec, start)
		}
	}
}

// tree reads the element start opened and everything inside it; what nests past maxTreeDepth is passed over.
func tree(dec *xml.Decoder, start xml.StartElement) (*node, error) {
	root := &node{name: start.Name, attrs: start.Attr}
	stack := []*node{root}
	skip := 0
	for len(stack) > 0 {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			if skip > 0 || len(stack) >= maxTreeDepth {
				skip++
				continue
			}
			n := &node{name: t.Name, attrs: t.Attr}
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if skip == 0 {
				top.kids = append(top.kids, &node{text: string(t)})
			}
		}
	}
	return root, nil
}
