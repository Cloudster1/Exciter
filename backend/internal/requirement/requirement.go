// Package requirement keeps requirements, their revisions, their tree and their
// trash (#13, #15, #16); docs/api-contract-m1.md says how.
package requirement

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Limits on what a person types, matching the web client's.
const (
	MaxTitleLength   = 300
	MaxCommentLength = 500
	// MaxDocumentBytes bounds a statement, a rationale or a rich text attribute.
	MaxDocumentBytes = 1 << 20
	// MaxBodyBytes bounds a request that writes one: a statement and a rationale, and room for the rest.
	MaxBodyBytes = 2*MaxDocumentBytes + 1<<16
	DefaultLimit = 20
	MaxLimit     = 100
	// MaxSpecLimit bounds one window of the specification view.
	MaxSpecLimit = 200
	// MaxCopy bounds the requirements one copy makes, children included.
	MaxCopy = 1000
)

// Kind says whether a node of the tree is a requirement or a section heading.
type Kind string

const (
	KindRequirement Kind = "requirement"
	// KindSection groups requirements under a heading; it has a title and an
	// optional statement, and no type, status, attributes or links.
	KindSection Kind = "section"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindRequirement, KindSection}

// Priority is how much a requirement matters; it is not normative.
type Priority string

const (
	PriorityMust   Priority = "must"
	PriorityShould Priority = "should"
	PriorityCould  Priority = "could"
	PriorityWont   Priority = "wont"
)

// Priorities lists every Priority, the highest first.
var Priorities = []Priority{PriorityMust, PriorityShould, PriorityCould, PriorityWont}

// PersonRef is somebody a requirement names.
type PersonRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// TypeRef is a requirement's type as a list shows it.
type TypeRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Icon   string    `json:"icon"`
	Colour string    `json:"colour"`
}

// StatusRef is a requirement's status as a list shows it.
type StatusRef struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Category string    `json:"category"`
}

// Ref names a requirement from somewhere else: a link, a tree, a search hit.
type Ref struct {
	ID         uuid.UUID  `json:"id"`
	Key        string     `json:"key"`
	ProjectKey string     `json:"projectKey"`
	Kind       Kind       `json:"kind"`
	Title      string     `json:"title"`
	Revision   int        `json:"revision"`
	Status     *StatusRef `json:"status"`
	// Trashed says it is in the trash, where links to it wait for it.
	Trashed bool `json:"trashed"`
}

// Can is what the caller may do to one requirement.
type Can struct {
	Edit       bool `json:"edit"`
	Transition bool `json:"transition"`
	Delete     bool `json:"delete"`
	Link       bool `json:"link"`
	Comment    bool `json:"comment"`
}

// Requirement is one requirement or section as its page shows it.
type Requirement struct {
	ID         uuid.UUID `json:"id"`
	Key        string    `json:"key"`
	ProjectKey string    `json:"projectKey"`
	Number     int       `json:"number"`
	Kind       Kind      `json:"kind"`
	// Title, Statement, Rationale, Type and Attributes are the normative
	// content: changing any of them writes the next revision.
	Title string `json:"title"`
	// Statement and Rationale are documents of the shared editor; an empty
	// rationale is null.
	Statement json.RawMessage `json:"statement"`
	Rationale json.RawMessage `json:"rationale"`
	Markdown
	Type *TypeRef `json:"type"`
	// Attributes are the values by attribute id, shaped by each attribute's kind.
	Attributes map[string]json.RawMessage `json:"attributes"`
	// Revision is the number of the latest revision, from 1.
	Revision int        `json:"revision"`
	Status   *StatusRef `json:"status"`
	Owner    *PersonRef `json:"owner"`
	Priority *Priority  `json:"priority"`
	Labels   []string   `json:"labels"`
	// ParentID is the requirement or section above, null at the top of the tree.
	ParentID *uuid.UUID `json:"parentId"`
	// Outline is the place in the tree, such as 2.3.1, as the reader numbers it.
	Outline string `json:"outline"`
	// FormerKeys are keys it had before a move or a key change, which still resolve.
	FormerKeys []string `json:"formerKeys"`
	// CopiedFrom is the requirement this one was copied from, while it exists.
	CopiedFrom *Ref `json:"copiedFrom"`
	// Reuse is the library requirement this one follows or branched from (#49), null for one of its own.
	Reuse         *Reuse     `json:"reuse"`
	CreatedAt     time.Time  `json:"createdAt"`
	CreatedByName string     `json:"createdByName"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	UpdatedByName string     `json:"updatedByName"`
	TrashedAt     *time.Time `json:"trashedAt"`
	Can           Can        `json:"can"`
	// Comments counts its discussion (#33).
	Comments CommentCounts `json:"comments"`
	// Watching is how the reader follows it (#34).
	Watching Watching `json:"watching"`
	// Approval is its review now open and the latest revision approved (#35).
	Approval Approval `json:"approval"`
}

// Approval is a requirement's standing for the workflow and the page: the review now open, and the latest revision
// approved, which counts only while it is the current one (#35).
type Approval struct {
	Pending          *uuid.UUID `json:"pendingReviewId"`
	ApprovedRevision *int       `json:"approvedRevision"`
	Current          bool       `json:"current"`
}

// approval reads Approval of the requirement aliased r, two columns.
const approval = `,
       (SELECT v.id FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'pending'),
       (SELECT max(v.revision) FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'approved')`

// Watching is how the reader follows a requirement: by itself, through its project, or not (#34).
type Watching struct {
	Requirement bool `json:"requirement"`
	Project     bool `json:"project"`
}

// watching reads Watching of the requirement aliased r for the reader $1, two columns.
const watching = `,
       EXISTS (SELECT 1 FROM watch w WHERE w.user_id = $1 AND w.requirement_id = r.id),
       EXISTS (SELECT 1 FROM watch w WHERE w.user_id = $1 AND w.project_id = r.project_id)`

// CommentCounts are a requirement's threads with a live comment, and those still open (#33).
type CommentCounts struct {
	Threads int `json:"threads"`
	Open    int `json:"open"`
}

// commentCounts reads CommentCounts of the requirement aliased r, two columns.
const commentCounts = `
       (SELECT count(*)::int FROM comment_thread ct WHERE ct.requirement_id = r.id
          AND EXISTS (SELECT 1 FROM comment cc WHERE cc.thread_id = ct.id AND cc.deleted_at IS NULL)),
       (SELECT count(*)::int FROM comment_thread ct WHERE ct.requirement_id = r.id AND ct.resolved_at IS NULL
          AND EXISTS (SELECT 1 FROM comment cc WHERE cc.thread_id = ct.id AND cc.deleted_at IS NULL))`

// SuspectLinks counts the suspect links of the requirement aliased r that the reader sees (#36), trace links,
// Armature links and Stator links (#41), one column; a Stator link watches its page, as stator.SuspectSQL says.
const SuspectLinks = `
       ((SELECT count(*) FROM trace_link_state sl WHERE sl.suspect AND (sl.source_id = r.id OR sl.target_id = r.id))
        + (SELECT count(*) FROM armature_issue_link al
           WHERE al.requirement_id = r.id AND r.trashed_at IS NULL AND r.revision > al.reviewed_rev)
        + (SELECT count(*) FROM stator_page_link sp
           WHERE sp.requirement_id = r.id AND r.trashed_at IS NULL AND sp.broken_at IS NULL AND sp.detached_at IS NULL
             AND sp.page_version > sp.page_version_seen))::int`

// Place is where a node goes in a tree: below a parent, after a sibling.
type Place struct {
	// ParentID is the requirement or section above, null for the top level.
	ParentID *uuid.UUID `json:"parentId"`
	// AfterID is the sibling it follows, null for the first place.
	AfterID *uuid.UUID `json:"afterId"`
}

// CreateInput is a new requirement or section.
type CreateInput struct {
	Kind      Kind            `json:"kind,omitempty"`
	Title     string          `json:"title"`
	Statement json.RawMessage `json:"statement,omitempty"`
	Rationale json.RawMessage `json:"rationale,omitempty"`
	// StatementMarkdown and RationaleMarkdown are the same written as Markdown, either form of each but not both (#186).
	StatementMarkdown *string                    `json:"statementMarkdown,omitempty"`
	RationaleMarkdown *string                    `json:"rationaleMarkdown,omitempty"`
	TypeID            *uuid.UUID                 `json:"typeId,omitempty"`
	Attributes        map[string]json.RawMessage `json:"attributes,omitempty"`
	OwnerID           *uuid.UUID                 `json:"ownerId,omitempty"`
	Priority          *Priority                  `json:"priority,omitempty"`
	Labels            []string                   `json:"labels,omitempty"`
	// Place is where it goes; left out, it goes last at the top level.
	Place *Place `json:"place,omitempty"`
	// Comment goes with revision 1.
	Comment string `json:"comment,omitempty"`
	// ImportedStatus is the status an import (#23) sets on what it makes; the API never takes one.
	ImportedStatus *uuid.UUID `json:"-"`
}

// UpdateInput changes a requirement; nil leaves a field alone. A change to
// the normative content needs BaseRevision; the status moves by transitions.
type UpdateInput struct {
	Title     *string         `json:"title,omitempty"`
	Statement json.RawMessage `json:"statement,omitempty"`
	// Rationale set to null clears it.
	Rationale json.RawMessage `json:"rationale,omitempty"`
	// StatementMarkdown and RationaleMarkdown are the same written as Markdown; a rationale of no words clears it.
	StatementMarkdown *string    `json:"statementMarkdown,omitempty"`
	RationaleMarkdown *string    `json:"rationaleMarkdown,omitempty"`
	TypeID            *uuid.UUID `json:"typeId,omitempty"`
	// Attributes merge: a key set to null clears that value, keys left out stay.
	Attributes map[string]json.RawMessage `json:"attributes,omitempty"`
	// OwnerID and Priority set to null clear them.
	OwnerID  json.RawMessage `json:"ownerId,omitempty"`
	Priority json.RawMessage `json:"priority,omitempty"`
	// BaseRevision is the revision the change was made from; a newer one refuses it.
	BaseRevision *int `json:"baseRevision,omitempty"`
	// Comment goes with the revision the save writes, if it writes one.
	Comment string `json:"comment,omitempty"`
}

// MoveInput puts a requirement, with everything below it, somewhere else.
type MoveInput struct {
	// ProjectKey is the project to move to, left out for the same one; a move
	// to another project gives every moved requirement a new key.
	ProjectKey string `json:"projectKey,omitempty"`
	Place      Place  `json:"place"`
	// StatusMapping maps statuses the target project's workflow lacks to ones it has.
	StatusMapping map[string]uuid.UUID `json:"statusMapping,omitempty"`
}

// CopyInput copies a requirement as new ones at their current revision.
type CopyInput struct {
	ProjectKey string `json:"projectKey,omitempty"`
	Place      Place  `json:"place"`
	// Children copies everything below it too.
	Children bool `json:"children"`
	// Links copies the links among the copied requirements and to the rest.
	Links bool `json:"links"`
}

// TreeNode is one requirement or section in a project's tree, in reading order.
type TreeNode struct {
	ID       uuid.UUID  `json:"id"`
	Key      string     `json:"key"`
	Kind     Kind       `json:"kind"`
	Title    string     `json:"title"`
	ParentID *uuid.UUID `json:"parentId"`
	Depth    int        `json:"depth"`
	Outline  string     `json:"outline"`
	TypeID   *uuid.UUID `json:"typeId"`
	Status   *StatusRef `json:"status"`
	Revision int        `json:"revision"`
	Children int        `json:"children"`
}

// SpecEntry is one node of the specification view with its content.
type SpecEntry struct {
	Node      TreeNode        `json:"node"`
	Statement json.RawMessage `json:"statement"`
	Rationale json.RawMessage `json:"rationale"`
	Markdown
	Attributes map[string]json.RawMessage `json:"attributes"`
	Labels     []string                   `json:"labels"`
	Can        Can                        `json:"can"`
	Comments   CommentCounts              `json:"comments"`
	// SuspectLinks counts its links waiting to be checked since an end moved on.
	SuspectLinks int `json:"suspectLinks"`
	// Reuse is what the entry follows or branched from (#49), null for one of its own.
	Reuse *Reuse `json:"reuse"`
	// Type, Owner and Priority are the requirement's, null when unset or for a section (#168).
	Type     *TypeRef   `json:"type"`
	Owner    *PersonRef `json:"owner"`
	Priority *Priority  `json:"priority"`
	// Approval is its review now open and the latest revision approved, as the requirement's page reads it.
	Approval Approval `json:"approval"`
	// Change is how it differs from the base asked for (#170), null for a section or when nothing was asked.
	Change *SpecChange `json:"change"`
	// Review is its pending review as its reader stands to it (#171), null without one.
	Review *SpecReview `json:"review"`
}

// SpecReview is a requirement's pending review as the document shows it to its reader.
type SpecReview struct {
	ID         uuid.UUID `json:"id"`
	Revision   int       `json:"revision"`
	Required   int       `json:"required"`
	Approvals  int       `json:"approvals"`
	Rejections int       `json:"rejections"`
	// Approver says the reader is one of its frozen approvers; MayDecide that they may decide it now, which the
	// revision's author may not unless the rule allows it.
	Approver  bool `json:"approver"`
	MayDecide bool `json:"mayDecide"`
	// Decision is the reader's standing one, approve or reject, null before they decide.
	Decision *string `json:"decision"`
}

// RevisionEntry is one revision as the history lists it.
type RevisionEntry struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Comment      string     `json:"comment"`
	AuthorID     *uuid.UUID `json:"authorId"`
	AuthorName   string     `json:"authorName"`
	CreatedAt    time.Time  `json:"createdAt"`
	RestoredFrom *int       `json:"restoredFrom"`
	// Hash is the SHA-256 of the normative content, which a revision that
	// changes nothing does not get past.
	Hash string `json:"hash"`
}

// Revision is one revision with its normative content, as it was.
type Revision struct {
	RevisionEntry
	Statement json.RawMessage `json:"statement"`
	Rationale json.RawMessage `json:"rationale"`
	Markdown
	Type       *TypeRef                   `json:"type"`
	Attributes map[string]json.RawMessage `json:"attributes"`
}

// RestoreInput writes an older revision's content as the next revision.
type RestoreInput struct {
	// BaseRevision is the latest revision as the caller saw it; a newer one refuses it.
	BaseRevision int `json:"baseRevision"`
	// Comment replaces the default naming the restored revision.
	Comment string `json:"comment,omitempty"`
}

// DiffChange is what happened to a block or an attribute between two revisions.
type DiffChange string

const (
	DiffEqual    DiffChange = "equal"
	DiffInserted DiffChange = "inserted"
	DiffDeleted  DiffChange = "deleted"
	DiffModified DiffChange = "modified"
)

// DiffChanges lists every DiffChange, for the API document.
var DiffChanges = []DiffChange{DiffEqual, DiffInserted, DiffDeleted, DiffModified}

// DiffBlock is one top-level block of a document in a comparison.
type DiffBlock struct {
	Change DiffChange `json:"change"`
	// Node is the block as a document node. A modified block is the newer
	// side's, with inline diffInsert and diffDelete marks on changed text.
	Node json.RawMessage `json:"node"`
}

// AttributeDiff is one attribute in a comparison.
type AttributeDiff struct {
	AttributeID uuid.UUID       `json:"attributeId"`
	Name        string          `json:"name"`
	Change      DiffChange      `json:"change"`
	From        json.RawMessage `json:"from"`
	To          json.RawMessage `json:"to"`
	// FromText and ToText are the values as a reader sees them, options by name.
	FromText string `json:"fromText"`
	ToText   string `json:"toText"`
}

// CompareSide is one side of a comparison; number 0 is the empty requirement.
type CompareSide struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Type       *TypeRef  `json:"type"`
	AuthorName string    `json:"authorName"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Comparison is what changed between two revisions of one requirement.
type Comparison struct {
	From CompareSide `json:"from"`
	To   CompareSide `json:"to"`
	// Title is the title as one paragraph, its changed words marked as the statement's are (#170).
	Title      []DiffBlock     `json:"title"`
	Statement  []DiffBlock     `json:"statement"`
	Rationale  []DiffBlock     `json:"rationale"`
	Attributes []AttributeDiff `json:"attributes"`
}

// ActivityKind says what an activity entry records.
type ActivityKind string

const (
	// ActivityRevision is a save that wrote a revision.
	ActivityRevision ActivityKind = "revision"
	// ActivityChange is a change that is logged but not revisioned.
	ActivityChange ActivityKind = "change"
)

// ActivityKinds lists every ActivityKind, for the API document.
var ActivityKinds = []ActivityKind{ActivityRevision, ActivityChange}

// ChangeField is what a logged change changed.
type ChangeField string

const (
	ChangeStatus     ChangeField = "status"
	ChangeOwner      ChangeField = "owner"
	ChangePriority   ChangeField = "priority"
	ChangeLabels     ChangeField = "labels"
	ChangePlace      ChangeField = "place"
	ChangeKey        ChangeField = "key"
	ChangeLink       ChangeField = "link"
	ChangeAttachment ChangeField = "attachment"
	ChangeTrash      ChangeField = "trash"
	// ChangeReuse is a reused requirement made a variant, or following its source again (#49).
	ChangeReuse ChangeField = "reuse"
)

// ChangeFields lists every ChangeField, for the API document.
var ChangeFields = []ChangeField{ChangeStatus, ChangeOwner, ChangePriority, ChangeLabels, ChangePlace, ChangeKey, ChangeLink, ChangeAttachment, ChangeTrash, ChangeReuse}

// Change is one logged change: the field, and what it was and became as a reader sees them.
type Change struct {
	Field ChangeField `json:"field"`
	From  string      `json:"from"`
	To    string      `json:"to"`
	// Reason says why when it was not the actor's own choice, such as a
	// revision taking an approved requirement back.
	Reason string `json:"reason"`
}

// ActivityEntry is one revision or logged change, newest first.
type ActivityEntry struct {
	ID        uuid.UUID      `json:"id"`
	Kind      ActivityKind   `json:"kind"`
	At        time.Time      `json:"at"`
	ActorName string         `json:"actorName"`
	Revision  *RevisionEntry `json:"revision"`
	Change    *Change        `json:"change"`
}

// TrashItem is one requirement the caller put in the trash, with what went with it.
type TrashItem struct {
	ID            uuid.UUID `json:"id"`
	Key           string    `json:"key"`
	Kind          Kind      `json:"kind"`
	Title         string    `json:"title"`
	TrashedAt     time.Time `json:"trashedAt"`
	TrashedByName string    `json:"trashedByName"`
	// Requirements counts the item, the one trashed and everything below it.
	Requirements int `json:"requirements"`
	// ParentInTree is false when its parent is gone, so a restore puts it at the top.
	ParentInTree bool `json:"parentInTree"`
}
