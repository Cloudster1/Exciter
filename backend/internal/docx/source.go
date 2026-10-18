package docx

// BlockKind is what a block of a Word document is.
type BlockKind string

const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
	BlockList      BlockKind = "list"
	BlockTable     BlockKind = "table"
	BlockPicture   BlockKind = "picture"
)

// Role is what a block of Exciter's own export is, beyond its kind.
type Role string

const (
	// RoleCover is the cover and the contents before the first requirement.
	RoleCover Role = "cover"
	// RoleRationale is the label the rationale follows, and RoleLinks the one the links follow.
	RoleRationale Role = "rationale"
	RoleLinks     Role = "links"
	// RoleProperties is a requirement's table of fields and attributes.
	RoleProperties Role = "properties"
	// RoleNote is a sentence the export added, such as that a picture could not be read.
	RoleNote Role = "note"
	// RoleGlossary is the chapter of the glossary's terms.
	RoleGlossary Role = "glossary"
)

// The kinds of loss a reader notes, named as an import names them.
const (
	LossFormatting = "formatting"
	LossImage      = "image"
	LossValue      = "value"
	LossPlace      = "place"
)

// Source is a Word document as an import keeps it: its blocks in order, without the bytes of its pictures.
type Source struct {
	Title string `json:"title"`
	// DocID is the identifier Word keeps for a document and its copies, empty when it wrote none.
	DocID  string     `json:"docId,omitempty"`
	Styles []StyleUse `json:"styles"`
	Blocks []Block    `json:"blocks"`
	Media  []Media    `json:"media"`
	Losses []Loss     `json:"losses"`
	// Exported says the document is Exciter's own Word export, whose headings carry their requirements' keys.
	Exported bool `json:"exported,omitempty"`
	// Tables counts the tables the blocks number.
	Tables int `json:"tables"`
}

// StyleUse is a paragraph style and how many paragraphs with words use it.
type StyleUse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Block is a heading, a paragraph, a list of consecutive list paragraphs, a table or a picture.
type Block struct {
	Kind BlockKind `json:"kind"`
	// Level is a heading's, from 1.
	Level     int    `json:"level,omitempty"`
	StyleID   string `json:"styleId,omitempty"`
	StyleName string `json:"styleName,omitempty"`
	// ParaID is the identifier Word keeps for the paragraph, or for a table's first.
	ParaID string `json:"paraId,omitempty"`
	// Label is the number Word's numbering shows before it, such as 3.2. or REQ-7.
	Label string `json:"label,omitempty"`
	// Index counts the paragraphs with words from 1, Table the tables, as a person finds them.
	Index int `json:"index,omitempty"`
	Table int `json:"table,omitempty"`
	// Text is the words alone; Rich is the content as rich text an import converts.
	Text string `json:"text"`
	Rich *XNode `json:"rich,omitempty"`
	// Rows are a table's cells by row, the first HeaderRows of them its header.
	Rows       [][]Cell `json:"rows,omitempty"`
	HeaderRows int      `json:"headerRows,omitempty"`
	// TableTitle is the title a table gives itself for whoever cannot see it.
	TableTitle string `json:"tableTitle,omitempty"`
	// Key is the requirement an Exciter export wrote below a heading.
	Key string `json:"key,omitempty"`
	// Caption says the paragraph is of the caption style, which belongs to the picture or table before it.
	Caption bool `json:"caption,omitempty"`
	// Role is set in Exciter's own export only.
	Role Role `json:"role,omitempty"`
	// Bookmark is the first bookmark the block holds.
	Bookmark string `json:"bookmark,omitempty"`
	Losses   []Loss `json:"losses,omitempty"`
}

// Cell is one cell of a table, at its column of the table's grid.
type Cell struct {
	Text    string `json:"text"`
	Rich    *XNode `json:"rich,omitempty"`
	Col     int    `json:"col"`
	ColSpan int    `json:"colSpan,omitempty"`
	RowSpan int    `json:"rowSpan,omitempty"`
	ParaID  string `json:"paraId,omitempty"`
}

// XNode is rich text in the shape an import reads it: an element by its name in lower case, or text.
type XNode struct {
	Name     string            `json:"n,omitempty"`
	Attrs    map[string]string `json:"a,omitempty"`
	Text     string            `json:"t,omitempty"`
	Children []XNode           `json:"c,omitempty"`
}

// Media is a picture of the document by its path in the package, with its bytes until they are stored.
type Media struct {
	Path        string `json:"path"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"`
}

// Loss is something of the document a reader cannot carry.
type Loss struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
