package trace

import (
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// The graph of what a requirement traces to (#211), agreed in docs/api-contract-m11.md.

// Agreed limits of the graph (#242).
const (
	GraphDefaultDepth = 2
	GraphMaxDepth     = MaxLevels
	// MaxGraphNodes bounds one answer, since every node can fan out at every level.
	MaxGraphNodes = 500
)

// GraphDirection says which way the graph follows links from the requirement it is centred on.
type GraphDirection string

const (
	// GraphUp follows links whose source a node is, towards what it derives from, refines, satisfies or verifies.
	GraphUp GraphDirection = "up"
	// GraphDown follows links whose target a node is, and reaches the issues, pages, tests and code below it.
	GraphDown GraphDirection = "down"
	GraphBoth GraphDirection = "both"
)

// GraphDirections lists every GraphDirection, for the API document.
var GraphDirections = []GraphDirection{GraphUp, GraphDown, GraphBoth}

// NodeKind says what a node of the graph is.
type NodeKind string

const (
	NodeRequirement NodeKind = "requirement"
	NodeIssue       NodeKind = "issue"
	NodePage        NodeKind = "page"
	NodeTest        NodeKind = "test"
	NodeCode        NodeKind = "code"
)

// NodeKinds lists every NodeKind, for the API document.
var NodeKinds = []NodeKind{NodeRequirement, NodeIssue, NodePage, NodeTest, NodeCode}

// GraphQuery is what the graph is asked: which way, how far, and through which link types and relations.
type GraphQuery struct {
	Direction GraphDirection
	Depth     int
	// TypeKeys are the link types followed, every type the project uses when empty.
	TypeKeys []string
	// Relations are the Armature, Stator, test and code relations reached, every one when empty.
	Relations []string
	// Branch is the evidence branch of the centre's project, its counting branch when empty.
	Branch string
}

// GraphNode is one thing the walk reached, at its level: below zero up, above zero down, zero the centre.
type GraphNode struct {
	// ID is the requirement's id, or the kind and the item's own name, unique within one graph.
	ID          string               `json:"id"`
	Level       int                  `json:"level"`
	Kind        NodeKind             `json:"kind"`
	Requirement *requirement.Ref     `json:"requirement"`
	Type        *requirement.TypeRef `json:"type"`
	IssueKey    string               `json:"issueKey,omitempty"`
	Page        *MatrixPage          `json:"page"`
	Test        *ChainTest           `json:"test,omitempty"`
	Code        *ChainCode           `json:"code,omitempty"`
	// Gaps are the coverage dimensions its project's rules ask of it and find missing or partial.
	Gaps []Dimension `json:"gaps"`
	// Broken says the item was deleted or unlinked on its side, or the requirement is in the trash.
	Broken bool `json:"broken"`
	// Hidden counts its links, of the types followed, to requirements the reader may not see; they are never named.
	Hidden int `json:"hidden"`
	// Risk marks a risk with its level and state (#261).
	Risk *requirement.RiskMark `json:"risk,omitempty"`
}

// GraphEdge is a link between two nodes, read from its source; From is the link's source for a trace link, and the
// requirement for an issue, page, test or code.
type GraphEdge struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	LinkID   *uuid.UUID `json:"linkId"`
	TypeKey  string     `json:"typeKey,omitempty"`
	Phrase   string     `json:"phrase,omitempty"`
	Relation string     `json:"relation,omitempty"`
	Suspect  bool       `json:"suspect"`
	Broken   bool       `json:"broken"`
}

// Graph is everything the walk reached from one requirement, nearest first.
type Graph struct {
	Root      string         `json:"root"`
	Direction GraphDirection `json:"direction"`
	Depth     int            `json:"depth"`
	Nodes     []GraphNode    `json:"nodes"`
	Edges     []GraphEdge    `json:"edges"`
	// Hidden is the sum of the nodes' hidden links.
	Hidden int `json:"hidden"`
	// Truncated says the walk stopped at MaxGraphNodes before its depth.
	Truncated bool `json:"truncated"`
}
