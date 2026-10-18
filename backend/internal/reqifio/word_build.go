package reqifio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/cloudster1/exciter/backend/internal/docx"
)

// The spec types and attribute definitions a Word document's parts are built with, each filling a field.
const (
	wordSourceTool   = "Word"
	wordSpec         = prefixProject + "word"
	wordTitleDef     = prefixField + "word-" + fieldTitle
	wordStatementDef = prefixField + "word-" + fieldStatement
	wordRationaleDef = prefixField + "word-" + fieldRationale
	wordChapterDef   = prefixField + "wordsection-" + fieldChapter
	wordSectionText  = prefixField + "wordsection-" + fieldStatement
	wordTablePrefix  = "word-table:"
	wordColumnPrefix = "word-column:"
	wordFrontPath    = "~front"
)

// The bounds of the parts of a Word identity, so the whole fits the 255 characters an identity keeps.
const (
	wordNameLength    = 40
	wordNumberLength  = 32
	wordPathLength    = 80
	wordHashLength    = 16
	wordSegmentLength = 40
)

// wordBuild is a Word document built with the choices made into the File a ReqIF document becomes, and what
// the preview tells of it beside the plans.
type wordBuild struct {
	src      *docx.Source
	choices  wordChoices
	ns       string
	file     *File
	nodes    []wordNode
	parts    map[string]*wordPart
	losses   []docx.Loss
	problems []Problem
	seen     map[string]int
	types    map[string]*wordTable
}

// wordPart is what an object of the build was made of: how an earlier import finds it again, and what it lost.
type wordPart struct {
	ids    wordIDs
	losses []docx.Loss
}

// wordIDs are the ways a part is found again, strongest first; empty ones it has not.
type wordIDs struct{ key, number, paragraph, path string }

// wordNode is one entry of the preview's tree; ref names the object it is, empty for text that makes none.
type wordNode struct {
	depth  int
	kind   NodeKind
	title  string
	source string
	ref    string
	losses int
}

// wordSection is a heading with the blocks below it up to the next heading, and the headings below it.
type wordSection struct {
	heading *docx.Block
	level   int
	kids    []*wordSection
	body    []*docx.Block
	path    string
}

// buildWord builds a document as the choices say, read again whenever they change; place is the section it goes
// below, or the project, which an identity names so the same document elsewhere is another import.
func buildWord(src *docx.Source, choices map[string]string, lang, fileName, place string) *wordBuild {
	c, problems := readWordChoices(choices, lang, src.Tables)
	title := strings.TrimSpace(src.Title)
	if title == "" {
		title = strings.TrimSuffix(path.Base(fileName), path.Ext(fileName))
	}
	b := &wordBuild{src: src, choices: c, ns: "word:" + wordName(src.DocID, fileName) + "@" + place + ":", parts: map[string]*wordPart{},
		losses: append([]docx.Loss{}, src.Losses...), problems: problems, seen: map[string]int{}, types: map[string]*wordTable{}}
	b.file = &File{Identifier: b.ns, Title: title, SourceTool: wordSourceTool, Types: []*SpecType{
		{Identity: Identity{Identifier: sectionType, Name: "Section"}, Kind: TypeObject, Attributes: []*AttrDef{
			{Identity: Identity{Identifier: wordChapterDef, Name: "Heading"}, Kind: KindString},
			{Identity: Identity{Identifier: wordSectionText, Name: "Text"}, Kind: KindXHTML}}},
		{Identity: Identity{Identifier: untypedType, Name: "Requirement"}, Kind: TypeObject, Attributes: []*AttrDef{
			{Identity: Identity{Identifier: wordTitleDef, Name: "Title"}, Kind: KindString},
			{Identity: Identity{Identifier: wordStatementDef, Name: "Text"}, Kind: KindXHTML},
			{Identity: Identity{Identifier: wordRationaleDef, Name: "Rationale"}, Kind: KindXHTML}}},
	}}
	for _, m := range src.Media {
		b.file.Images = append(b.file.Images, Image{Path: m.Path, ContentType: m.ContentType, Size: m.Size})
	}
	root := sectionsOf(src)
	spec := &Specification{Identity: Identity{Identifier: wordSpec, Name: title}, Type: specificationType}
	b.front(root, title, &spec.Children)
	for _, s := range root.kids {
		b.section(s, 0, &spec.Children)
	}
	b.file.Specifications = []*Specification{spec}
	if n := len(b.file.Objects); n > MaxObjects {
		b.problems = append(b.problems, Problem{Where: "file", Message: fmt.Sprintf("The document makes %d requirements and sections, more than the %d one import takes. Split it, and import the parts one after the other.", n, MaxObjects)})
	}
	return b
}

// wordName names the document in its identities: the identifier Word keeps for it, else its file's name.
func wordName(docID, fileName string) string {
	if docID != "" {
		return strings.ToLower(truncate(docID, wordNameLength))
	}
	return truncate(normalWords(strings.TrimSuffix(path.Base(fileName), path.Ext(fileName))), wordNameLength)
}

// normalWords is text as an identity compares it: lower case, its words joined by hyphens.
func normalWords(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), "-")
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:wordHashLength]
}

// sectionsOf nests the headings by level, each below the nearest heading before it of a higher one.
func sectionsOf(src *docx.Source) *wordSection {
	root := &wordSection{}
	stack := []*wordSection{root}
	for i := range src.Blocks {
		blk := &src.Blocks[i]
		if blk.Kind != docx.BlockHeading {
			stack[len(stack)-1].body = append(stack[len(stack)-1].body, blk)
			continue
		}
		for len(stack) > 1 && stack[len(stack)-1].level >= blk.Level {
			stack = stack[:len(stack)-1]
		}
		s := &wordSection{heading: blk, level: blk.Level}
		parent := stack[len(stack)-1]
		parent.kids = append(parent.kids, s)
		stack = append(stack, s)
	}
	var paths func(s *wordSection)
	paths = func(s *wordSection) {
		taken := map[string]int{}
		for _, k := range s.kids {
			segment := truncate(normalWords(k.heading.Text), wordSegmentLength)
			if taken[segment]++; taken[segment] > 1 {
				segment += "~" + strconv.Itoa(taken[segment])
			}
			k.path = strings.TrimPrefix(s.path+"/"+segment, "/")
			paths(k)
		}
	}
	paths(root)
	return root
}

// identifier is a part's identity in the file: the document and its place, then every way it is found again.
func (b *wordBuild) identifier(ids wordIDs) (string, wordIDs) {
	ids.number = strings.ReplaceAll(truncate(ids.number, wordNumberLength), "|", "/")
	if len(ids.path) > wordPathLength {
		ids.path = "#" + shortHash(ids.path)
	}
	var parts []string
	for _, p := range []struct {
		kind  WordMatch
		value string
	}{{WordMatchKey, ids.key}, {WordMatchNumber, ids.number}, {WordMatchParagraph, ids.paragraph}, {WordMatchPath, ids.path}} {
		if p.value != "" {
			parts = append(parts, string(p.kind)+":"+p.value)
		}
	}
	id := b.ns + strings.Join(parts, "|")
	if b.seen[id]++; b.seen[id] > 1 {
		id += "|n:" + strconv.Itoa(b.seen[id])
	}
	return id, ids
}

// parseWordIdentity reads the ways an identity of the document at ns finds its part.
func parseWordIdentity(identifier, ns string) (wordIDs, bool) {
	rest, ok := strings.CutPrefix(identifier, ns)
	if !ok {
		return wordIDs{}, false
	}
	var ids wordIDs
	for _, part := range strings.Split(rest, "|") {
		kind, value, _ := strings.Cut(part, ":")
		switch WordMatch(kind) {
		case WordMatchKey:
			ids.key = value
		case WordMatchNumber:
			ids.number = value
		case WordMatchParagraph:
			ids.paragraph = value
		case WordMatchPath:
			ids.path = value
		}
	}
	return ids, true
}

func (b *wordBuild) lose(kind, message string) {
	for _, l := range b.losses {
		if l.Message == message {
			return
		}
	}
	b.losses = append(b.losses, docx.Loss{Kind: kind, Message: message})
}

// object adds an object of the build, its losses those of its blocks.
func (b *wordBuild) object(typ string, ids wordIDs, values []Value, blocks []*docx.Block) *Object {
	id, ids := b.identifier(ids)
	part := &wordPart{ids: ids}
	for _, blk := range blocks {
		for _, l := range blk.Losses {
			if !containsLoss(part.losses, l) {
				part.losses = append(part.losses, l)
			}
		}
	}
	b.parts[id] = part
	o := &Object{Identity: Identity{Identifier: id}, Type: typ, Values: values}
	b.file.Objects = append(b.file.Objects, o)
	return o
}

func containsLoss(list []docx.Loss, l docx.Loss) bool {
	for _, have := range list {
		if have.Message == l.Message {
			return true
		}
	}
	return false
}

func (b *wordBuild) node(n wordNode) int {
	if n.ref != "" {
		n.losses = len(b.parts[n.ref].losses)
	}
	b.nodes = append(b.nodes, n)
	return len(b.nodes) - 1
}

// rich is blocks as one rich text value, nil when there are none.
func rich(blocks []*docx.Block) *XNode {
	root := &XNode{}
	for _, blk := range blocks {
		if blk.Rich != nil {
			root.Children = append(root.Children, fromWord(*blk.Rich))
		}
	}
	if len(root.Children) == 0 {
		return nil
	}
	return root
}

// fromWord is a Word document's rich text as the import's, the two of one shape.
func fromWord(x docx.XNode) XNode {
	out := XNode{Name: x.Name, Attrs: x.Attrs, Text: x.Text}
	for _, c := range x.Children {
		out.Children = append(out.Children, fromWord(c))
	}
	return out
}

func textValue(def, text string) Value { return Value{Def: def, Text: text} }

// sectionValues are a section's heading and the informative text that joins its statement.
func sectionValues(title string, informative []*docx.Block) []Value {
	values := []Value{textValue(wordChapterDef, title)}
	if x := rich(informative); x != nil {
		values = append(values, Value{Def: wordSectionText, XHTML: x})
	}
	return values
}

func requirementValues(title string, statement, rationale []*docx.Block) []Value {
	values := []Value{textValue(wordTitleDef, title)}
	if x := rich(statement); x != nil {
		values = append(values, Value{Def: wordStatementDef, XHTML: x})
	}
	if x := rich(rationale); x != nil {
		values = append(values, Value{Def: wordRationaleDef, XHTML: x})
	}
	return values
}

// headingSource names a heading by its style, as Word shows it.
func headingSource(h *docx.Block) string {
	name := h.StyleName
	if name == "" {
		return "Heading " + strconv.Itoa(h.Level)
	}
	r := []rune(name)
	return string(unicode.ToUpper(r[0])) + string(r[1:])
}

// front builds the text before the first heading: a section named after the document, or nothing.
func (b *wordBuild) front(root *wordSection, title string, nodes *[]*Node) {
	var cover, body []*docx.Block
	for _, blk := range root.body {
		if blk.Role == docx.RoleCover {
			cover = append(cover, blk)
		} else {
			body = append(body, blk)
		}
	}
	if len(cover) > 0 {
		b.node(wordNode{kind: NodeSkipped, title: "Cover and contents", source: "before the first requirement"})
	}
	if len(body) == 0 {
		return
	}
	if b.choices.front == FrontSkip {
		b.lose(docx.LossPlace, "The text before the first heading is left out.")
		b.node(wordNode{kind: NodeSkipped, title: wordTitle("", body[0].Text), source: "before the first heading"})
		return
	}
	b.container(&wordSection{body: body, path: wordFrontPath}, title, "before the first heading", wordIDs{path: wordFrontPath}, 0, nodes)
}

// section builds a heading and what is below it.
func (b *wordBuild) section(s *wordSection, depth int, nodes *[]*Node) {
	h := s.heading
	source := headingSource(h)
	if h.Role == docx.RoleGlossary {
		b.lose(docx.LossPlace, "The glossary of the document is left out; terms are not imported.")
		b.node(wordNode{depth: depth, kind: NodeSkipped, title: h.Text, source: source})
		return
	}
	ids := wordIDs{key: h.Key, number: b.choices.number(h), paragraph: h.ParaID, path: s.path}
	leaf := len(s.kids) == 0
	switch {
	case h.Key != "" && leaf:
		statement, rationale := b.exported(s.body)
		b.leaf(h.Text, statement, rationale, ids, source, depth, nodes)
	case b.choices.unit == UnitHeading && leaf && h.Key == "":
		b.leaf(h.Text, b.plain(s.body), nil, ids, source, depth, nodes)
	default:
		b.container(s, h.Text, source, ids, depth, nodes)
	}
}

// leaf makes a heading one requirement holding what is below it.
func (b *wordBuild) leaf(title string, statement, rationale []*docx.Block, ids wordIDs, source string, depth int, nodes *[]*Node) {
	o := b.object(untypedType, ids, requirementValues(title, statement, rationale), append(append([]*docx.Block{}, statement...), rationale...))
	*nodes = append(*nodes, &Node{Object: o.Identifier})
	b.node(wordNode{depth: depth, kind: NodeRequirement, title: title, source: source, ref: o.Identifier})
}

// container makes a heading a section: its requirements below it, and the rest its statement.
func (b *wordBuild) container(s *wordSection, title, source string, ids wordIDs, depth int, nodes *[]*Node) {
	at := b.node(wordNode{depth: depth, kind: NodeSection, title: title, source: source})
	self := &Node{}
	var informative []*docx.Block
	if s.heading != nil && s.heading.Key != "" {
		var rationale []*docx.Block
		informative, rationale = b.exported(s.body)
		if len(rationale) > 0 {
			b.lose(docx.LossValue, "Sections have no rationale, so the rationale of a section is left out.")
		}
	} else if b.choices.unit == UnitHeading {
		informative = b.plain(s.body)
	} else {
		informative = b.units(s, depth+1, &self.Children)
	}
	o := b.object(sectionType, ids, sectionValues(title, informative), informative)
	self.Object = o.Identifier
	b.nodes[at].ref, b.nodes[at].losses = o.Identifier, len(b.parts[o.Identifier].losses)
	*nodes = append(*nodes, self)
	for _, k := range s.kids {
		b.section(k, depth+1, &self.Children)
	}
}

// plain are a section's blocks without those an export added.
func (b *wordBuild) plain(body []*docx.Block) []*docx.Block {
	var out []*docx.Block
	for _, blk := range body {
		if blk.Role == "" {
			out = append(out, blk)
		}
	}
	return out
}

// exported reads an entry of Exciter's own export: its statement, then its rationale; its properties and links
// stay as the project has them.
func (b *wordBuild) exported(body []*docx.Block) (statement, rationale []*docx.Block) {
	target := &statement
	for i := 0; i < len(body); i++ {
		switch blk := body[i]; blk.Role {
		case docx.RoleNote, docx.RoleCover, docx.RoleGlossary:
		case docx.RoleProperties:
			b.lose(docx.LossValue, "The properties tables of the document are left out; requirements keep the type, status and attributes they have.")
		case docx.RoleRationale:
			target = &rationale
		case docx.RoleLinks:
			b.lose(string(LossRelation), "The links the document lists are left out; the project keeps its own.")
			for i+1 < len(body) && body[i+1].Kind == docx.BlockList {
				i++
			}
		default:
			*target = append(*target, blk)
		}
	}
	return statement, rationale
}
