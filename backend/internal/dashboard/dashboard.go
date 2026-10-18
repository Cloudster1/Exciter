// Package dashboard is a project's dashboards (#212): widgets over its requirements, each read live as the reader,
// adapted from Armature's internal/report.
package dashboard

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// Agreed limits of dashboards (#242).
const (
	MaxNameLength  = 80
	MaxTitleLength = 80
	// MaxDashboards bounds the dashboards one person keeps in a project, shared ones included.
	MaxDashboards = 50
	MaxWidgets    = 30
	// MaxWidth is the columns a dashboard is laid out in.
	MaxWidth = 3
	// MaxBuckets bounds the slices of a breakdown; the rest are counted as one.
	MaxBuckets      = 20
	DefaultListRows = 10
	MaxListRows     = 50
	DefaultDays     = 90
	MaxDays         = 730
)

// Kind is what a widget shows.
type Kind string

const (
	// Breakdown counts the requirements by a field or an attribute.
	Breakdown Kind = "breakdown"
	// Count is how many requirements the source keeps.
	Count Kind = "count"
	// List is the first requirements the source keeps, in its order.
	List Kind = "list"
	// Coverage is how far the requirements are decomposed, implemented and verified.
	Coverage Kind = "coverage"
	// Review is how many requirements are approved, waiting, outdated or never reviewed.
	Review Kind = "review"
	// Suspects is how many requirements have suspect links, and the ones with most.
	Suspects Kind = "suspects"
	// Changes is how many requirements were created, revised or moved to another status, over time.
	Changes Kind = "changes"
	// Quality is how the requirements score against the writing rules (#207).
	Quality Kind = "quality"
)

// Kinds lists every Kind, in the order the chooser offers them.
var Kinds = []Kind{Breakdown, Count, List, Coverage, Review, Suspects, Changes, Quality}

// KindInfo describes a kind for the chooser.
type KindInfo struct {
	Kind        Kind   `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Width is the columns the widget takes unless told otherwise.
	Width int `json:"width"`
}

// GroupBy is what a breakdown counts by.
type GroupBy string

const (
	ByStatus    GroupBy = "status"
	ByCategory  GroupBy = "category"
	ByType      GroupBy = "type"
	ByOwner     GroupBy = "owner"
	ByPriority  GroupBy = "priority"
	ByLabel     GroupBy = "label"
	ByAttribute GroupBy = "attribute"
)

// GroupBys lists every GroupBy, for the API document.
var GroupBys = []GroupBy{ByStatus, ByCategory, ByType, ByOwner, ByPriority, ByLabel, ByAttribute}

// Shape is how a breakdown is drawn.
type Shape string

const (
	Bars  Shape = "bars"
	Donut Shape = "donut"
)

// Shapes lists every Shape, for the API document.
var Shapes = []Shape{Bars, Donut}

// Interval is how a widget over time buckets it.
type Interval string

const (
	Week  Interval = "week"
	Month Interval = "month"
)

// Intervals lists every Interval, for the API document.
var Intervals = []Interval{Week, Month}

// Series is what a widget over time counts.
type Series string

const (
	Created Series = "created"
	Revised Series = "revised"
	// StatusChanged counts the requirements moved to another status.
	StatusChanged Series = "statusChanged"
)

// SeriesKinds lists every Series, for the API document.
var SeriesKinds = []Series{Created, Revised, StatusChanged}

// Source is what a widget reads: a query in the query language, or a shared saved view of the grid; the project's
// every requirement when both are empty.
type Source struct {
	Query  string     `json:"query,omitempty"`
	ViewID *uuid.UUID `json:"viewId,omitempty"`
}

// Config is how a widget is asked and drawn; each kind reads what it needs and leaves the rest.
type Config struct {
	Source      Source     `json:"source"`
	GroupBy     GroupBy    `json:"groupBy,omitempty"`
	AttributeID *uuid.UUID `json:"attributeId,omitempty"`
	Shape       Shape      `json:"shape,omitempty"`
	// Columns are the grid's columns a list shows, and Rows how many.
	Columns  []string `json:"columns,omitempty"`
	Rows     int      `json:"rows,omitempty"`
	Interval Interval `json:"interval,omitempty"`
	Days     int      `json:"days,omitempty"`
	Series   []Series `json:"series,omitempty"`
}

// Widget is one view on a dashboard.
type Widget struct {
	ID       uuid.UUID `json:"id"`
	Kind     Kind      `json:"kind"`
	Title    string    `json:"title"`
	Width    int       `json:"width"`
	Position int       `json:"position"`
	Config   Config    `json:"config"`
}

// Can says what the reader may do with a dashboard.
type Can struct {
	Edit  bool `json:"edit"`
	Share bool `json:"share"`
}

// Summary names a dashboard in a list.
type Summary struct {
	ID         uuid.UUID             `json:"id"`
	ProjectKey string                `json:"projectKey"`
	Name       string                `json:"name"`
	Shared     bool                  `json:"shared"`
	Owner      requirement.PersonRef `json:"owner"`
	// Home says the project's home shows it.
	Home      bool      `json:"home"`
	Can       Can       `json:"can"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Dashboard is a dashboard and its widgets, in order.
type Dashboard struct {
	Summary
	Widgets   []Widget  `json:"widgets"`
	CreatedAt time.Time `json:"createdAt"`
}

// Input makes a dashboard, empty or from a template.
type Input struct {
	Name     string `json:"name"`
	Shared   bool   `json:"shared"`
	Template string `json:"template,omitempty"`
}

// Patch renames a dashboard or shares it; nil leaves a field alone.
type Patch struct {
	Name   *string `json:"name,omitempty"`
	Shared *bool   `json:"shared,omitempty"`
}

// WidgetInput adds a widget at the end.
type WidgetInput struct {
	Kind   Kind   `json:"kind"`
	Title  string `json:"title"`
	Width  int    `json:"width,omitempty"`
	Config Config `json:"config"`
}

// WidgetPatch changes a widget; nil leaves a field alone.
type WidgetPatch struct {
	Title  *string `json:"title,omitempty"`
	Width  *int    `json:"width,omitempty"`
	Config *Config `json:"config,omitempty"`
}

// OrderInput is every widget of a dashboard, in the order it is laid out.
type OrderInput struct {
	Widgets []uuid.UUID `json:"widgets"`
}

// PreviewInput is a widget as it is being set up, read before it is saved.
type PreviewInput struct {
	Kind   Kind   `json:"kind"`
	Config Config `json:"config"`
}

// Bucket is one bar, slice or count, and the query that keeps its requirements, to open them in the grid.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	// Category is a status's category, so the page colours by meaning.
	Category string `json:"category,omitempty"`
	// Query is a condition in the query language, joined to the source's by AND; empty for what cannot be opened.
	Query string `json:"query"`
}

// Group is a set of buckets under one name, as coverage has one per dimension.
type Group struct {
	Name    string   `json:"name"`
	Buckets []Bucket `json:"buckets"`
	Total   int      `json:"total"`
}

// Point is one bucket of time.
type Point struct {
	Start time.Time `json:"start"`
	Count int       `json:"count"`
	Query string    `json:"query"`
}

// Line is one series over time.
type Line struct {
	Series Series  `json:"series"`
	Points []Point `json:"points"`
}

// Data is what a widget shows, read as the reader.
type Data struct {
	Kind Kind `json:"kind"`
	// Total is how many requirements the source keeps that the reader may see.
	Total  int     `json:"total"`
	Groups []Group `json:"groups"`
	Lines  []Line  `json:"lines"`
	// Rows are a list's requirements, or the suspects with most suspect links.
	Rows []search.Row `json:"rows"`
	// Average is the quality widget's mean score, null when nothing is scored.
	Average *float64 `json:"average"`
	// Query and ViewID are the source, so the page opens the grid with it.
	Query  string     `json:"query"`
	ViewID *uuid.UUID `json:"viewId"`
}

// Template is a first dashboard, with the widgets it starts with.
type Template struct {
	Key         string        `json:"key"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Widgets     []WidgetInput `json:"widgets"`
}
