package reqifio

// A Word document comes in through the same import as a ReqIF file (#209): read into the same plan, previewed, run in
// the worker and tied to what it made, so a second import of it updates rather than copies.

// FormatDocx is a Word document; its import is a ReqIF import in every other respect.
const FormatDocx Format = "docx"

// MaxPreviewNodes bounds the tree a preview shows; the counts stay whole.
const MaxPreviewNodes = 2000

// WordForm is the upload's field beside the file: the section to import below, the project's top when empty.
type WordForm struct {
	ParentKey string `json:"parentKey,omitempty"`
}

// Formats a Word import is, beside the ReqIF ones.
var WordFormats = []Format{FormatDocx}

// The choices a Word import takes beside the schema's, as keys of ChoicesInput.Choices.
const (
	// ChoiceWordUnit is paragraph or heading: one requirement per paragraph, or one per heading with what is below it.
	ChoiceWordUnit = "word.unit"
	// ChoiceWordStyles names the paragraph styles that make requirements, separated by commas.
	ChoiceWordStyles = "word.styles"
	// ChoiceWordPattern is a pattern a requirement's number or first words match, such as REQ-\d+.
	ChoiceWordPattern = "word.pattern"
	// ChoiceWordKeywords are words that make a paragraph a requirement, such as shall, separated by commas.
	ChoiceWordKeywords = "word.keywords"
	// ChoiceWordTable prefixes a table's number: one for one requirement, rows for one per row.
	ChoiceWordTable = "word.table."
	// ChoiceWordFront is section or skip: what becomes of the text before the first heading.
	ChoiceWordFront = "word.front"
)

// The values the Word choices take.
const (
	UnitParagraph = "paragraph"
	UnitHeading   = "heading"
	TableOne      = "one"
	TableRows     = "rows"
	FrontSection  = "section"
	FrontSkip     = "skip"
)

// WordMatch says how a Word import found what an earlier import of the same document made.
type WordMatch string

const (
	// WordMatchKey is a requirement's key in Exciter's own Word export.
	WordMatchKey WordMatch = "key"
	// WordMatchNumber is an author's number that the pattern matched.
	WordMatchNumber WordMatch = "number"
	// WordMatchParagraph is the identifier Word gives a paragraph.
	WordMatchParagraph WordMatch = "paragraph"
	// WordMatchPath is the headings above it and its place among their units.
	WordMatchPath WordMatch = "path"
)

// WordMatches lists every WordMatch, for the API document.
var WordMatches = []WordMatch{WordMatchKey, WordMatchNumber, WordMatchParagraph, WordMatchPath}

// NodeKind is what a part of the document becomes.
type NodeKind string

const (
	NodeSection     NodeKind = "section"
	NodeRequirement NodeKind = "requirement"
	// NodeInformative is text that joins the statement of the section above it.
	NodeInformative NodeKind = "informative"
	NodeSkipped     NodeKind = "skipped"
)

// NodeKinds lists every NodeKind, for the API document.
var NodeKinds = []NodeKind{NodeSection, NodeRequirement, NodeInformative, NodeSkipped}

// StyleUse is a paragraph style of a Word document and how many paragraphs use it.
type StyleUse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// PreviewNode is one part of the document as the import would make it, in the order of the document.
type PreviewNode struct {
	Depth int      `json:"depth"`
	Kind  NodeKind `json:"kind"`
	Title string   `json:"title"`
	// Source says where in the document it is, such as its heading, paragraph or table row.
	Source string `json:"source"`
	// MatchedBy says how it found what an earlier import made; ExistingKey is that requirement.
	MatchedBy   WordMatch `json:"matchedBy,omitempty"`
	ExistingKey string    `json:"existingKey,omitempty"`
	Losses      int       `json:"losses"`
}
