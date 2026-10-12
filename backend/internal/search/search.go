// Package search finds requirements by their words and their fields, and keeps
// the views people save of a project's grid (#20); docs/api-contract-m1.md says how.
package search

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

const (
	MaxQueryLength    = 200
	DefaultLimit      = 20
	MaxLimit          = 100
	DefaultQuickLimit = 8
	MaxQuickLimit     = 20
	// MaxGridLimit bounds one window of the grid.
	MaxGridLimit = 500
	// MaxAttributeFilters bounds the attr conditions of one query.
	MaxAttributeFilters = 10
	MaxViewNameLength   = 100
	MaxColumns          = 30
)

// Segment is a run of text, matched or not; the client marks matched runs.
type Segment struct {
	Text  string `json:"text"`
	Match bool   `json:"match"`
}

// Hit is one requirement whose words matched.
type Hit struct {
	Requirement requirement.Ref `json:"requirement"`
	ProjectName string          `json:"projectName"`
	Title       []Segment       `json:"title"`
	// Snippet is the best passage of statement or rationale, about 30 words.
	Snippet       []Segment `json:"snippet"`
	Labels        []string  `json:"labels"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedByName string    `json:"updatedByName"`
}

// QuickHit is one requirement for the command palette.
type QuickHit struct {
	Requirement requirement.Ref `json:"requirement"`
	ProjectName string          `json:"projectName"`
	// Outline is its place in its project's tree, such as 2.3.1.
	Outline string `json:"outline"`
}

// Op is how an attribute condition compares.
type Op string

const (
	// OpIs matches a value equal to the one given, or a multi-select holding it.
	OpIs  Op = "is"
	OpNot Op = "not"
	// OpGte and OpLte compare numbers and dates.
	OpGte Op = "gte"
	OpLte Op = "lte"
	// OpContains matches text and rich text containing the words given.
	OpContains Op = "contains"
	OpSet      Op = "set"
	OpUnset    Op = "unset"
)

// Ops lists every Op, for the API document.
var Ops = []Op{OpIs, OpNot, OpGte, OpLte, OpContains, OpSet, OpUnset}

// AttributeCondition narrows by one attribute; the attr query parameter
// spells it attributeId:op:value.
type AttributeCondition struct {
	AttributeID uuid.UUID `json:"attributeId"`
	Op          Op        `json:"op"`
	Value       string    `json:"value"`
}

// Filter is what a view narrows the grid by; each list matches any of its values.
type Filter struct {
	Q          string               `json:"q"`
	Kinds      []requirement.Kind   `json:"kinds"`
	Types      []uuid.UUID          `json:"types"`
	Statuses   []uuid.UUID          `json:"statuses"`
	Categories []string             `json:"categories"`
	Labels     []string             `json:"labels"`
	Owners     []uuid.UUID          `json:"owners"`
	Priorities []string             `json:"priorities"`
	Attributes []AttributeCondition `json:"attributes"`
}

// Sort orders the grid by a column, outline order when the column is empty.
type Sort struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc"`
}

// Row is one requirement of the grid, with what its columns show.
type Row struct {
	requirement.Ref
	Outline    string                     `json:"outline"`
	Type       *requirement.TypeRef       `json:"type"`
	Owner      *requirement.PersonRef     `json:"owner"`
	Priority   *requirement.Priority      `json:"priority"`
	Labels     []string                   `json:"labels"`
	Attributes map[string]json.RawMessage `json:"attributes"`
	UpdatedAt  time.Time                  `json:"updatedAt"`
	Can        requirement.Can            `json:"can"`
}

// View is a saved grid: a filter, columns and an order.
type View struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"projectKey"`
	Name       string    `json:"name"`
	OwnerID    uuid.UUID `json:"ownerId"`
	OwnerName  string    `json:"ownerName"`
	// Shared views are offered to everybody who may view the project.
	Shared  bool     `json:"shared"`
	Filter  Filter   `json:"filter"`
	Columns []string `json:"columns"`
	Sort    Sort     `json:"sort"`
	// CanEdit is the owner's, and a project administrator's for a shared view.
	CanEdit   bool      `json:"canEdit"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ViewInput makes a view, or changes one where nil leaves a field alone.
type ViewInput struct {
	Name    *string   `json:"name,omitempty"`
	Shared  *bool     `json:"shared,omitempty"`
	Filter  *Filter   `json:"filter,omitempty"`
	Columns *[]string `json:"columns,omitempty"`
	Sort    *Sort     `json:"sort,omitempty"`
}
