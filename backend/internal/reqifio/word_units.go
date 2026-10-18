package reqifio

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/docx"
)

// wordUnit is blocks that become one requirement or one piece of informative text: a paragraph with the list
// its colon introduces, or a picture with its caption.
type wordUnit struct {
	blocks      []*docx.Block
	requirement bool
	number      string
	// rows is a table each row of which is a requirement.
	rows bool
}

// units builds a section's blocks into requirements below it, and answers the informative text its statement
// takes.
func (b *wordBuild) units(s *wordSection, depth int, children *[]*Node) []*docx.Block {
	var informative []*docx.Block
	for i, u := range b.group(s.body) {
		at := s.path + "/" + strconv.Itoa(i+1)
		first := u.blocks[0]
		source := blockSource(first)
		if u.rows && b.rows(first, at, depth, children) {
			continue
		}
		title := unitTitle(u)
		if u.requirement || u.rows {
			o := b.object(untypedType, wordIDs{number: u.number, paragraph: first.ParaID, path: at}, requirementValues(title, u.blocks, nil), u.blocks)
			*children = append(*children, &Node{Object: o.Identifier})
			b.node(wordNode{depth: depth, kind: NodeRequirement, title: title, source: source, ref: o.Identifier})
			continue
		}
		informative = append(informative, u.blocks...)
		losses := 0
		for _, blk := range u.blocks {
			losses += len(blk.Losses)
		}
		b.node(wordNode{depth: depth, kind: NodeInformative, title: title, source: source, losses: losses})
	}
	return informative
}

func blockSource(blk *docx.Block) string {
	if blk.Kind == docx.BlockTable {
		return "table " + strconv.Itoa(blk.Table)
	}
	return "paragraph " + strconv.Itoa(blk.Index)
}

// unitTitle is a unit's first words, a table's own title first.
func unitTitle(u *wordUnit) string {
	for _, blk := range u.blocks {
		if blk.TableTitle != "" {
			return truncate(blk.TableTitle, wordTitleLength)
		}
		if text := strings.ReplaceAll(blk.Text, " | ", ", "); strings.TrimSpace(text) != "" {
			return wordTitle(blk.Label, text)
		}
	}
	return "Picture"
}

// introduces says a paragraph ends with a colon, so what follows it belongs to it.
func introduces(blk *docx.Block) bool {
	return blk.Kind == docx.BlockParagraph && strings.HasSuffix(strings.TrimSpace(blk.Text), ":")
}

// group gathers a section's blocks into units and says which are requirements.
func (b *wordBuild) group(body []*docx.Block) []*wordUnit {
	var out []*wordUnit
	for i := 0; i < len(body); i++ {
		blk := body[i]
		switch blk.Role {
		case docx.RoleNote, docx.RoleCover, docx.RoleGlossary, docx.RoleRationale:
			continue
		case docx.RoleProperties, docx.RoleLinks:
			b.exported(body[i : i+1])
			for blk.Role == docx.RoleLinks && i+1 < len(body) && body[i+1].Kind == docx.BlockList {
				i++
			}
			continue
		}
		var last *wordUnit
		if len(out) > 0 && !out[len(out)-1].rows {
			last = out[len(out)-1]
		}
		switch {
		case blk.Kind == docx.BlockTable && b.choices.tables[blk.Table] == TableRows:
			out = append(out, &wordUnit{blocks: []*docx.Block{blk}, rows: true})
		case last != nil && (blk.Caption || blk.Kind == docx.BlockPicture):
			last.blocks = append(last.blocks, blk)
		case last != nil && (blk.Kind == docx.BlockList || blk.Kind == docx.BlockTable) && introduces(last.blocks[len(last.blocks)-1]):
			last.blocks = append(last.blocks, blk)
		default:
			out = append(out, &wordUnit{blocks: []*docx.Block{blk}})
		}
	}
	for _, u := range out {
		if u.rows {
			continue
		}
		texts := make([]string, 0, len(u.blocks))
		for _, blk := range u.blocks {
			texts = append(texts, blk.Text)
		}
		u.requirement, u.number = b.choices.classify(u.blocks[0], strings.Join(texts, "\n"))
		if u.blocks[0].Kind == docx.BlockTable {
			u.requirement = true
		}
	}
	return out
}

// columnFields are the names of a table's columns that fill a field every requirement has, in both languages.
var columnFields = map[string]string{
	"title": fieldTitle, "titel": fieldTitle, "name": fieldTitle, "heading": fieldTitle, "überschrift": fieldTitle,
	"statement": fieldStatement, "text": fieldStatement, "requirement": fieldStatement, "description": fieldStatement,
	"anforderung": fieldStatement, "anforderungstext": fieldStatement, "beschreibung": fieldStatement,
	"rationale": fieldRationale, "reason": fieldRationale, "begründung": fieldRationale,
	"id": fieldKey, "identifier": fieldKey, "number": fieldKey, "no": fieldKey, "no.": fieldKey, "nr": fieldKey,
	"nr.": fieldKey, "nummer": fieldKey, "kennung": fieldKey, "req id": fieldKey, "requirement id": fieldKey,
}

// wordTable is the spec type a table's header makes, shared by every table with the same header.
type wordTable struct {
	spec    *SpecType
	columns map[string]*AttrDef
	// fields are the columns that fill a field, by the field.
	fields map[string]string
}

// formatted are the elements that make a cell's words rich text rather than plain.
var formatted = []string{"b", "i", "s", "code", "a", "object", "ul", "ol", "br", "blockquote"}

func richCell(c docx.Cell) bool {
	if strings.Contains(c.Text, "\n") || utf8.RuneCountInString(c.Text) > attribute.MaxTextLength {
		return true
	}
	var found bool
	var walk func(docx.XNode)
	walk = func(x docx.XNode) {
		if slices.Contains(formatted, x.Name) {
			found = true
		}
		for _, k := range x.Children {
			walk(k)
		}
	}
	if c.Rich != nil {
		walk(*c.Rich)
	}
	return found
}

// tableType is the type a table of rows becomes, named by its header's columns, each column an attribute or a
// field.
func (b *wordBuild) tableType(blk *docx.Block, header int) (*wordTable, map[int]string) {
	names := map[int]string{}
	var cols []int
	for _, c := range blk.Rows[header-1] {
		names[c.Col] = strings.Join(strings.Fields(c.Text), " ")
		cols = append(cols, c.Col)
	}
	for _, row := range blk.Rows[header:] {
		for _, c := range row {
			if _, ok := names[c.Col]; !ok {
				names[c.Col] = ""
				cols = append(cols, c.Col)
			}
		}
	}
	slices.Sort(cols)
	var labels []string
	for _, col := range cols {
		if names[col] == "" {
			names[col] = "Column " + strconv.Itoa(col+1)
		}
		labels = append(labels, names[col])
	}
	hash := shortHash(strings.Join(labels, "|"))
	if t, ok := b.types[hash]; ok {
		return t, names
	}
	t := &wordTable{spec: &SpecType{Identity: Identity{Identifier: wordTablePrefix + hash, Name: truncate(strings.Join(labels, ", "), attribute.MaxNameLength)},
		Kind: TypeObject}, columns: map[string]*AttrDef{}, fields: map[string]string{}}
	taken := map[string]int{}
	for _, col := range cols {
		name := names[col]
		field := columnFields[strings.ToLower(strings.TrimSuffix(name, ":"))]
		d := &AttrDef{Identity: Identity{Name: name}, Kind: KindString}
		if _, used := t.fields[field]; field != "" && !used {
			d.Identifier = prefixField + "wordtable" + hash + "-" + field
			t.fields[field] = name
			if field == fieldStatement || field == fieldRationale {
				d.Kind = KindXHTML
			}
		} else {
			key := normalWords(name)
			if taken[key]++; taken[key] > 1 {
				key += "~" + strconv.Itoa(taken[key])
			}
			d.Identifier = wordColumnPrefix + hash + ":" + key
		}
		t.columns[name] = d
		t.spec.Attributes = append(t.spec.Attributes, d)
	}
	b.types[hash] = t
	b.file.Types = append(b.file.Types, t.spec)
	return t, names
}

// rows makes each body row of a table a requirement of its header's type; false is a table without body rows.
func (b *wordBuild) rows(blk *docx.Block, at string, depth int, children *[]*Node) bool {
	header := max(blk.HeaderRows, 1)
	if len(blk.Rows) <= header {
		return false
	}
	t, names := b.tableType(blk, header)
	for _, row := range blk.Rows[header:] {
		for _, c := range row {
			if d := t.columns[names[c.Col]]; d != nil && d.Kind == KindString && richCell(c) && fieldOf(d) != fieldTitle && fieldOf(d) != fieldKey {
				d.Kind = KindXHTML
			}
		}
	}
	made := 0
	for r, row := range blk.Rows[header:] {
		var values []Value
		var number, title, fallback string
		var texts []string
		for _, c := range row {
			d := t.columns[names[c.Col]]
			if d == nil || strings.TrimSpace(c.Text) == "" && (c.Rich == nil || len(c.Rich.Children) == 0) {
				continue
			}
			texts = append(texts, c.Text)
			switch field := fieldOf(d); {
			case field == fieldKey:
				number = strings.TrimSpace(c.Text)
			case field == fieldTitle:
				title = strings.TrimSpace(c.Text)
			case fallback == "":
				fallback = wordTitle("", c.Text)
			}
			if d.Kind == KindXHTML && c.Rich != nil {
				values = append(values, Value{Def: d.Identifier, XHTML: &XNode{Children: fromWord(*c.Rich).Children}})
			} else {
				values = append(values, textValue(d.Identifier, strings.TrimSpace(c.Text)))
			}
		}
		if len(values) == 0 {
			continue
		}
		if number == "" {
			number = b.choices.number(&docx.Block{Text: strings.Join(texts, " ")})
		}
		if title == "" {
			title = fallback
		}
		var paraID string
		if len(row) > 0 {
			paraID = row[0].ParaID
		}
		blocks := []*docx.Block{}
		if made == 0 {
			blocks = append(blocks, blk)
		}
		source := fmt.Sprintf("table %d row %d", blk.Table, r+1)
		o := b.object(t.spec.Identifier, wordIDs{number: number, paragraph: paraID, path: at + "/row" + strconv.Itoa(r+1)}, values, blocks)
		o.Name = title
		*children = append(*children, &Node{Object: o.Identifier})
		b.node(wordNode{depth: depth, kind: NodeRequirement, title: title, source: source, ref: o.Identifier})
		made++
	}
	return true
}
