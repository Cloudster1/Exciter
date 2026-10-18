// Package quality checks a requirement's title and statement against writing rules (#207), without a network or a
// language model, and keeps what it found with each revision.
package quality

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Agreed limits of the writing rules (#233).
const (
	// Version names the rules as built; a new one checks every project's requirements again.
	Version = 1
	// MaxCustomWords bounds the words a project adds to or excepts from one rule's list, MaxWordLength each.
	MaxCustomWords = 200
	MaxWordLength  = 60
	MaxReasonChars = 500
	// MaxMatchChars bounds the text a finding quotes.
	MaxMatchChars = 200
	// RecheckBatch is how many requirements one transaction of a recheck takes.
	RecheckBatch = 200
	// DefaultLimit and MaxLimit window the project's quality list.
	DefaultLimit = 50
	MaxLimit     = 500
)

// The score starts full and loses each finding's weight, at most twice its weight per rule.
const (
	FullScore     = 100
	WeightError   = 20
	WeightWarning = 7
	WeightHint    = 2
)

// Language is the language a project's requirements are written in, which picks the rules' word lists.
type Language string

const (
	English Language = "en"
	German  Language = "de"
)

// Languages lists every Language, for the API document.
var Languages = []Language{English, German}

// Severity is how much a finding weighs.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityHint    Severity = "hint"
)

// Severities lists every Severity, for the API document.
var Severities = []Severity{SeverityError, SeverityWarning, SeverityHint}

// RuleID names a writing rule.
type RuleID string

const (
	RuleVague         RuleID = "vague"
	RuleUnbounded     RuleID = "unbounded"
	RuleEscape        RuleID = "escape"
	RulePlaceholder   RuleID = "placeholder"
	RuleAndOr         RuleID = "and_or"
	RuleMultipleShall RuleID = "multiple_shall"
	RuleNegative      RuleID = "negative"
	RulePassive       RuleID = "passive"
	RulePronoun       RuleID = "pronoun"
	RuleEARS          RuleID = "ears"
	// RuleAvoidTerm is a word the glossary says not to use (#210).
	RuleAvoidTerm RuleID = "avoid_term"
)

// RuleIDs lists every RuleID, for the API document.
var RuleIDs = []RuleID{RuleVague, RuleUnbounded, RuleEscape, RulePlaceholder, RuleAndOr, RuleMultipleShall, RuleNegative,
	RulePassive, RulePronoun, RuleEARS, RuleAvoidTerm}

// WordRules are the rules whose word lists a project may add to and except from.
var WordRules = []RuleID{RuleVague, RuleUnbounded, RuleEscape, RulePlaceholder}

// Field is the part of a requirement a finding is in.
type Field string

const (
	FieldTitle     Field = "title"
	FieldStatement Field = "statement"
)

// Fields lists every Field, for the API document.
var Fields = []Field{FieldTitle, FieldStatement}

// Pattern is the shape of a sentence that says what shall be.
type Pattern string

const (
	PatternUbiquitous Pattern = "ubiquitous"
	PatternEvent      Pattern = "event"
	PatternState      Pattern = "state"
	PatternUnwanted   Pattern = "unwanted"
	PatternOptional   Pattern = "optional"
	PatternComplex    Pattern = "complex"
	PatternNone       Pattern = "none"
)

// Patterns lists every Pattern, for the API document.
var Patterns = []Pattern{PatternUbiquitous, PatternEvent, PatternState, PatternUnwanted, PatternOptional, PatternComplex, PatternNone}

// Span is where in a requirement something was found: the field, its text block in document order, and the
// offsets within that block as the editor counts them. The title is block 0.
type Span struct {
	Field Field `json:"field"`
	Block int   `json:"block"`
	From  int   `json:"from"`
	To    int   `json:"to"`
}

// Person is somebody a finding names.
type Person struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Acceptance is an author's word that a finding stays, kept with the revision it was given for.
type Acceptance struct {
	Reason string `json:"reason"`
	By     Person `json:"by"`
	// Revision is the revision it holds for; CarriedFrom the one it was first given for, when it came forward.
	Revision    int       `json:"revision"`
	CarriedFrom *int      `json:"carriedFrom"`
	At          time.Time `json:"at"`
}

// Finding is one place a rule found, and what to do about it.
type Finding struct {
	// Key stays the same across revisions while the rule finds the same words in the same field.
	Key      string   `json:"key"`
	Rule     RuleID   `json:"rule"`
	Severity Severity `json:"severity"`
	Span
	Match   string `json:"match"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
	// Active says it counts: its rule is on for the requirement's type and nobody accepted it.
	Active   bool        `json:"active"`
	Accepted *Acceptance `json:"accepted"`
}

// Sentence is a sentence that says what shall be, and its shape.
type Sentence struct {
	Span
	Pattern Pattern `json:"pattern"`
}

// TermMatch is a glossary term a draft uses (#210).
type TermMatch struct {
	TermID     uuid.UUID `json:"termId"`
	Name       string    `json:"name"`
	ProjectKey string    `json:"projectKey"`
	Span
	// Explicit says an author linked the words to the term.
	Explicit          bool   `json:"explicit"`
	DefinitionPreview string `json:"definitionPreview"`
}

// CheckInput is a draft to check as it is written, before it is saved.
type CheckInput struct {
	Title     string          `json:"title"`
	Statement json.RawMessage `json:"statement"`
	// TypeID picks the rules that apply; the project's default type when absent.
	TypeID *uuid.UUID `json:"typeId,omitempty"`
	// RequirementKey names the requirement being revised, whose accepted findings the answer carries.
	RequirementKey string `json:"requirementKey,omitempty"`
}

// Check is what the rules find in a draft.
type Check struct {
	Score     *int        `json:"score"`
	Language  Language    `json:"language"`
	Findings  []Finding   `json:"findings"`
	Sentences []Sentence  `json:"sentences"`
	Terms     []TermMatch `json:"terms"`
}

// Quality is a requirement's score as a list or a page shows it; Score is null for a section and until it is
// checked, and Stale says a recheck after a change of word lists has yet to reach it.
type Quality struct {
	Score    *int `json:"score"`
	Active   int  `json:"active"`
	Accepted int  `json:"accepted"`
	Stale    bool `json:"stale"`
}

// Findings are what the rules found in one revision of a requirement.
type Findings struct {
	Revision int `json:"revision"`
	Quality
	Findings  []Finding  `json:"findings"`
	Sentences []Sentence `json:"sentences"`
}

// AcceptInput accepts one finding of the current revision.
type AcceptInput struct {
	Revision int    `json:"revision"`
	Key      string `json:"key"`
	Reason   string `json:"reason"`
}

// RuleInfo is a rule as the catalog lists it.
type RuleInfo struct {
	ID              RuleID   `json:"id"`
	DefaultSeverity Severity `json:"defaultSeverity"`
	DefaultEnabled  bool     `json:"defaultEnabled"`
	Fields          []Field  `json:"fields"`
	// Words says a project may add to and except from its word list.
	Words       bool   `json:"words"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// RuleSetting is how a project uses one rule; empty TypeIDs is every type.
type RuleSetting struct {
	Rule     RuleID      `json:"rule"`
	Enabled  bool        `json:"enabled"`
	Severity Severity    `json:"severity"`
	TypeIDs  []uuid.UUID `json:"typeIds"`
}

// Words are what a project adds to a rule's built-in list, and the built-in words it does not want found.
type Words struct {
	Add    []string `json:"add"`
	Except []string `json:"except"`
}

// SettingsInput is every rule's setting and every word list, sent at once.
type SettingsInput struct {
	Rules []RuleSetting    `json:"rules"`
	Words map[RuleID]Words `json:"words"`
}

// Settings are a project's rules and word lists.
type Settings struct {
	SettingsInput
	Language  Language   `json:"language"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// Band counts the requirements whose score is from From to To.
type Band struct {
	From  int `json:"from"`
	To    int `json:"to"`
	Count int `json:"count"`
}

// RuleCount is how many active findings of one rule a project's requirements hold.
type RuleCount struct {
	Rule  RuleID `json:"rule"`
	Count int    `json:"count"`
}

// Scored is a requirement in the project's quality list.
type Scored struct {
	ID    uuid.UUID `json:"id"`
	Key   string    `json:"key"`
	Title string    `json:"title"`
	Quality
	Rules []RuleID `json:"rules"`
}

// Summary is a project's quality: its score bands and rules, and its requirements lowest first.
type Summary struct {
	Checked      int         `json:"checked"`
	Unchecked    int         `json:"unchecked"`
	Average      *float64    `json:"average"`
	Bands        []Band      `json:"bands"`
	Rules        []RuleCount `json:"rules"`
	Requirements []Scored    `json:"requirements"`
	Total        int         `json:"total"`
	Limit        int         `json:"limit"`
	Offset       int         `json:"offset"`
}
