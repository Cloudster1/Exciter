// Package reqifio moves requirements in and out of ReqIF files (#44), the exchange format between requirements
// tools; docs/api-contract-backlog.md says how.
package reqifio

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
)

// megabyte is what the limits below are counted in.
const megabyte = 1 << 20

const (
	// MaxImportMB bounds an uploaded file, a .reqif or a .reqifz with its images.
	MaxImportMB    = 64
	MaxImportBytes = MaxImportMB * megabyte
	// MaxDocumentMB bounds the .reqif document once unpacked, which an import keeps whole until it runs.
	MaxDocumentMB    = 32
	MaxDocumentBytes = MaxDocumentMB * megabyte
	// MaxUnpackedMB bounds everything one archive unpacks into together, whatever its directory claims.
	MaxUnpackedMB    = 128
	MaxUnpackedBytes = MaxUnpackedMB * megabyte
	// MaxImageMB bounds one embedded image, an attachment's default limit.
	MaxImageMB    = 50
	MaxImageBytes = MaxImageMB * megabyte
	MaxObjects    = 5000
	MaxRelations  = 20000
	// MaxDepth bounds how deep a specification's hierarchy nests.
	MaxDepth = 64
	// MaxXHTMLDepth bounds how deep one rich text value nests before the rest is read as its words.
	MaxXHTMLDepth = 40
	// MaxLosses bounds the losses an import lists; LostCount stays whole.
	MaxLosses = 200
	// MaxExportRows bounds the requirements one export carries.
	MaxExportRows = 20000
	// MaxExportImageMB bounds the images one .reqifz export packs; past it the rest are left out.
	MaxExportImageMB    = 128
	MaxExportImageBytes = MaxExportImageMB * megabyte
)

// Format is how an import's file came: a ReqIF document, or an archive of one with its images.
type Format string

const (
	FormatReqIF  Format = "reqif"
	FormatReqIFZ Format = "reqifz"
)

// Formats lists every Format, for the API document.
var Formats = []Format{FormatReqIF, FormatReqIFZ}

// State is where an import stands.
type State string

const (
	// StateUploaded waits for its schema to be confirmed.
	StateUploaded State = "uploaded"
	StateQueued   State = "queued"
	StateRunning  State = "running"
	StateDone     State = "done"
	StateFailed   State = "failed"
	// StateCancelled was stopped before or while it ran.
	StateCancelled State = "cancelled"
)

// States lists every State, for the API document.
var States = []State{StateUploaded, StateQueued, StateRunning, StateDone, StateFailed, StateCancelled}

// SchemaKind is what in the file a schema item is.
type SchemaKind string

const (
	// SchemaType is a SPEC-OBJECT-TYPE, which becomes a requirement type.
	SchemaType SchemaKind = "type"
	// SchemaAttribute is an ATTRIBUTE-DEFINITION, which becomes an attribute or fills a field.
	SchemaAttribute SchemaKind = "attribute"
	// SchemaLinkType is a SPEC-RELATION-TYPE, which becomes a link type.
	SchemaLinkType SchemaKind = "linkType"
)

// SchemaKinds lists every SchemaKind, for the API document.
var SchemaKinds = []SchemaKind{SchemaType, SchemaAttribute, SchemaLinkType}

// MatchBy says how a schema item found what it becomes.
type MatchBy string

const (
	// ByIdentifier is an item an earlier import or export of this project already tied to what it is.
	ByIdentifier MatchBy = "identifier"
	// ByName is an item that has the name of something the project has.
	ByName MatchBy = "name"
	// ByChoice is an item somebody mapped.
	ByChoice MatchBy = "choice"
	// ByField is an attribute that fills a field every requirement has, such as its statement.
	ByField MatchBy = "field"
	// ByCreate is an item the import makes in the project.
	ByCreate MatchBy = "create"
	// BySkip is an attribute somebody chose to leave out.
	BySkip MatchBy = "skip"
	// BySection is the type of the file's headings, which become sections, and its attributes, which they do not keep.
	BySection MatchBy = "section"
	// ByNone is an item that becomes nothing yet; a problem says why.
	ByNone MatchBy = "none"
)

// MatchBys lists every MatchBy, for the API document.
var MatchBys = []MatchBy{ByIdentifier, ByName, ByChoice, ByField, ByCreate, BySkip, BySection, ByNone}

// Choices a schema item may be given besides the id of something the project has.
const (
	ChoiceCreate = "create"
	// ChoiceSkip leaves an attribute's values out of the import.
	ChoiceSkip = "skip"
)

// SchemaItem is a type, an attribute or a link type of the file, and what it becomes in the project.
type SchemaItem struct {
	Kind       SchemaKind `json:"kind"`
	Identifier string     `json:"identifier"`
	Name       string     `json:"name"`
	// Becomes is the kind of attribute an attribute is or would be made as; empty for the other kinds.
	Becomes attribute.Kind `json:"becomes,omitempty"`
	// Field is the field every requirement has that the attribute fills, such as statement; empty for the others.
	Field string `json:"field,omitempty"`
	// Fits are the kinds of attribute an attribute's values fit, which it may be mapped to.
	Fits []attribute.Kind `json:"fits,omitempty"`
	// Means is what the project has that it becomes, null while it is to be made or left out.
	Means     *uuid.UUID `json:"means"`
	MeansName string     `json:"meansName"`
	By        MatchBy    `json:"by"`
	// Count is how many objects or relations of the file use it.
	Count int `json:"count"`
}

// LossKind says what an import cannot carry over.
type LossKind string

const (
	// LossFormatting is rich text the editor has no form for, such as underlining, kept as its words.
	LossFormatting LossKind = "formatting"
	// LossImage is an image the file shows and the import cannot keep.
	LossImage LossKind = "image"
	// LossValue is a value that fits no attribute or field it maps to.
	LossValue LossKind = "value"
	// LossRelation is a relation whose ends are not both requirements of the project after the import.
	LossRelation LossKind = "relation"
	// LossPlace is an object the hierarchy names twice, or never.
	LossPlace LossKind = "place"
	// LossStatus is a status a requirement that exists keeps, since a status moves by transitions.
	LossStatus LossKind = "status"
	// LossPerson is somebody the file names who is no member of the project.
	LossPerson LossKind = "person"
)

// LossKinds lists every LossKind, for the API document.
var LossKinds = []LossKind{LossFormatting, LossImage, LossValue, LossRelation, LossPlace, LossStatus, LossPerson}

// Loss is something of the file the import leaves behind, which does not keep it from starting.
type Loss struct {
	Kind LossKind `json:"kind"`
	// Where names the object, type or relation of the file, empty for the file as a whole.
	Where   string `json:"where"`
	Message string `json:"message"`
}

// Problem is something about the file that keeps the import from starting.
type Problem struct {
	Where   string `json:"where"`
	Message string `json:"message"`
}

// Preview is what the file holds and what the schema as chosen would make of it.
type Preview struct {
	Title      string `json:"title"`
	SourceTool string `json:"sourceTool"`
	// Specifications are the names of the file's specifications, each a tree of objects.
	Specifications []string `json:"specifications"`
	Objects        int      `json:"objects"`
	Sections       int      `json:"sections"`
	Relations      int      `json:"relations"`
	Images         int      `json:"images"`
	// New and Updates count the objects that make a requirement, and those that change one the project has.
	New       int          `json:"new"`
	Updates   int          `json:"updates"`
	Schema    []SchemaItem `json:"schema"`
	Losses    []Loss       `json:"losses"`
	LostCount int          `json:"lostCount"`
	Problems  []Problem    `json:"problems"`
	// Tree is each part of the file as the import would make it, in order, at most MaxPreviewNodes (#209).
	Tree []PreviewNode `json:"tree"`
	// Styles are the paragraph styles of a Word document and how often each is used, for its choices.
	Styles []StyleUse `json:"styles"`
	// Tables counts a Word document's tables, which its choices name by number from 1.
	Tables int `json:"tables"`
}

// Refusal is an object or a relation the worker could not write, and why.
type Refusal struct {
	Where  string `json:"where"`
	Reason string `json:"reason"`
}

// Import is a ReqIF file on its way into a project.
type Import struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"projectKey"`
	FileName   string    `json:"fileName"`
	Format     Format    `json:"format"`
	State      State     `json:"state"`
	// Choices map a schema item's identifier to the id of what it becomes, create or skip.
	Choices  map[string]string `json:"choices"`
	Preview  Preview           `json:"preview"`
	Done     int               `json:"done"`
	Total    int               `json:"total"`
	Imported []string          `json:"imported"`
	Updated  []string          `json:"updated"`
	// Unchanged counts the requirements the file holds as they already were.
	Unchanged int       `json:"unchanged"`
	Refused   []Refusal `json:"refused"`
	// Error says why an import failed as a whole.
	Error    string     `json:"error"`
	ByName   string     `json:"byName"`
	Created  time.Time  `json:"createdAt"`
	Finished *time.Time `json:"finishedAt"`
}

// ChoicesInput maps the file's types, attributes and link types; every answer is sent at once.
type ChoicesInput struct {
	Choices map[string]string `json:"choices"`
}
