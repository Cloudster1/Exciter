package workflow

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/quality"
)

// Subject is the requirement a transition is weighed against, as its rules read it.
type Subject struct {
	StatusID *uuid.UUID
	OwnerID  *uuid.UUID
	// LatestAuthor wrote the latest revision, whom not_author keeps from approving their own words.
	LatestAuthor *uuid.UUID
	// Approved says an approved review covers the current revision (#35).
	Approved bool
	// MissingRequired names a required attribute without a value, empty when none is missing.
	MissingRequired string
	MissingField    string
	// Key names it in a refusal. The trace below is read only when a transition is taken (#206).
	Key         string
	Links       []LinkCount
	Suspects    int
	OpenThreads int
	// Coverage is its level in each dimension, as the coverage report judges it.
	Coverage map[string]string
	// Quality is its score by the writing rules (#207), nil until it is checked.
	Quality *int
}

// LinkCount is how many live links of one type a requirement has from it and to it.
type LinkCount struct {
	TypeID             uuid.UUID
	TypeName           string
	Outgoing, Incoming int
}

// Actor is whoever takes the transition, with the project roles their grants give them.
type Actor struct {
	UserID uuid.UUID
	Roles  []string
}

// Input is what came with the transition.
type Input struct {
	Comment string
}

// Effect is what the post-functions ask for besides the new status; the caller applies it.
type Effect struct {
	// SetOwner makes Owner the requirement's owner, nil clearing it.
	SetOwner bool
	Owner    *uuid.UUID
}

// FieldError is a validator's refusal, on the field it is about.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// rule is one registered rule type: how it is described, how its config is checked, and what it does.
type rule struct {
	RuleType
	check func(json.RawMessage) error
	// allow is a condition's verdict; validate a validator's refusal; apply a post-function's effect.
	allow    func(json.RawMessage, Subject, Actor) bool
	validate func(json.RawMessage, Subject, Input) error
	apply    func(Subject, Actor, *Effect)
}

// Roles a role condition names, the project roles of #12.
var Roles = []string{"viewer", "reviewer", "editor", "admin"}

type rolesConfig struct {
	Roles []string `json:"roles"`
}

func readRoles(raw json.RawMessage) (rolesConfig, error) {
	var c rolesConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, fmt.Errorf("its roles are not a list of roles")
		}
	}
	if len(c.Roles) == 0 {
		return c, fmt.Errorf("it names no role; pick at least one")
	}
	for _, r := range c.Roles {
		if !slices.Contains(Roles, r) {
			return c, fmt.Errorf("%q is not a project role; pick viewer, reviewer, editor or admin", r)
		}
	}
	return c, nil
}

func noConfig(json.RawMessage) error { return nil }

// Directions are the ends a link is counted from; Dimensions the coverage a transition can ask for (#206).
const (
	Outgoing = "outgoing"
	Incoming = "incoming"
)

var (
	Directions = []string{Outgoing, Incoming}
	Dimensions = []string{"decomposed", "implemented", "verified"}
)

type linksConfig struct {
	LinkTypeID uuid.UUID `json:"linkTypeId"`
	Direction  string    `json:"direction"`
}

func readLinks(raw json.RawMessage) (linksConfig, error) {
	var c linksConfig
	if len(raw) > 0 && json.Unmarshal(raw, &c) != nil {
		return c, fmt.Errorf("its link type is not a link type's id")
	}
	if c.LinkTypeID == uuid.Nil {
		return c, fmt.Errorf("it names no link type; pick one")
	}
	if c.Direction == "" {
		c.Direction = Outgoing
	}
	if !slices.Contains(Directions, c.Direction) {
		return c, fmt.Errorf("pick outgoing or incoming as its direction")
	}
	return c, nil
}

func readDimension(raw json.RawMessage) (string, error) {
	var c struct {
		Dimension string `json:"dimension"`
	}
	if (len(raw) > 0 && json.Unmarshal(raw, &c) != nil) || !slices.Contains(Dimensions, c.Dimension) {
		return "", fmt.Errorf("pick decomposed, implemented or verified as its dimension")
	}
	return c.Dimension, nil
}

// The bounds of the score a transition may ask for (#207).
var (
	scoreFloor   = 0
	scoreCeiling = quality.FullScore
)

func readMinimum(raw json.RawMessage) (int, error) {
	var c struct {
		Minimum *int `json:"minimum"`
	}
	if (len(raw) > 0 && json.Unmarshal(raw, &c) != nil) || c.Minimum == nil || *c.Minimum < scoreFloor || *c.Minimum > scoreCeiling {
		return 0, fmt.Errorf("give the least score as a whole number from %d to %d", scoreFloor, scoreCeiling)
	}
	return *c.Minimum, nil
}

// count says how many of a thing, as a sentence names them.
func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// registry is every rule a transition may carry: Armature's registry, made for requirements.
var registry = []rule{
	{
		RuleType: RuleType{Kind: KindCondition, Type: "condition.project_role", Label: "Project role",
			Description: "Only people holding one of these roles in the project may take it.",
			Options:     []Option{{Name: "roles", Label: "Roles", Kind: OptionRoles, Required: true, Choices: Roles}}},
		check: func(raw json.RawMessage) error { _, err := readRoles(raw); return err },
		allow: func(raw json.RawMessage, _ Subject, a Actor) bool {
			c, err := readRoles(raw)
			return err == nil && slices.ContainsFunc(c.Roles, func(r string) bool { return slices.Contains(a.Roles, r) })
		},
	},
	{
		RuleType: RuleType{Kind: KindCondition, Type: "condition.is_owner", Label: "Owner only",
			Description: "Only the requirement's owner may take it."},
		check: noConfig,
		allow: func(_ json.RawMessage, s Subject, a Actor) bool { return s.OwnerID != nil && *s.OwnerID == a.UserID },
	},
	{
		RuleType: RuleType{Kind: KindCondition, Type: "condition.not_author", Label: "Not the author",
			Description: "Whoever wrote the latest revision may not take it, so nobody approves their own words."},
		check: noConfig,
		allow: func(_ json.RawMessage, s Subject, a Actor) bool {
			return s.LatestAuthor == nil || *s.LatestAuthor != a.UserID
		},
	},
	{
		RuleType: RuleType{Kind: KindCondition, Type: "condition.approved", Label: "Approved",
			Description: "Only a requirement whose current revision its approvers approved may take it."},
		check: noConfig,
		allow: func(_ json.RawMessage, s Subject, _ Actor) bool { return s.Approved },
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.comment_required", Label: "Comment required",
			Description: "It asks for a comment saying why."},
		check: noConfig,
		validate: func(_ json.RawMessage, _ Subject, in Input) error {
			if in.Comment == "" {
				return &FieldError{"comment", "Say why in a comment; this transition asks for one."}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.owner_required", Label: "Owner required",
			Description: "The requirement needs an owner first."},
		check: noConfig,
		validate: func(_ json.RawMessage, s Subject, _ Input) error {
			if s.OwnerID == nil {
				return &FieldError{"ownerId", "Give the requirement an owner first; this transition asks for one."}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.required_attributes", Label: "Required attributes",
			Description: "Every required attribute needs a value first."},
		check: noConfig,
		validate: func(_ json.RawMessage, s Subject, _ Input) error {
			if s.MissingRequired != "" {
				return &FieldError{s.MissingField, fmt.Sprintf("Give %s a value first; this transition asks for every required attribute.", s.MissingRequired)}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.links_exist", Label: "Links of a type",
			Description: "The requirement needs at least one link of a type, from it or to it, first.",
			Options: []Option{{Name: "linkTypeId", Label: "Link type", Kind: OptionLinkType, Required: true},
				{Name: "direction", Label: "Direction", Kind: OptionDirection, Required: true, Choices: Directions}}},
		check: func(raw json.RawMessage) error { _, err := readLinks(raw); return err },
		validate: func(raw json.RawMessage, s Subject, _ Input) error {
			c, err := readLinks(raw)
			if err != nil {
				return err
			}
			name := "that type"
			for _, l := range s.Links {
				if l.TypeID == c.LinkTypeID {
					name = l.TypeName
					if (c.Direction == Outgoing && l.Outgoing > 0) || (c.Direction == Incoming && l.Incoming > 0) {
						return nil
					}
				}
			}
			way := map[string]string{Outgoing: "from it", Incoming: "to it"}[c.Direction]
			return &FieldError{"transition", fmt.Sprintf("%s has no %s link %s yet. Link it, then take this transition.", s.Key, name, way)}
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.no_suspect", Label: "No suspect link",
			Description: "None of the requirement's links may be suspect."},
		check: noConfig,
		validate: func(_ json.RawMessage, s Subject, _ Input) error {
			if s.Suspects > 0 {
				return &FieldError{"transition", fmt.Sprintf("%s has %s. Confirm or change them, then take this transition.", s.Key, count(s.Suspects, "suspect link"))}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.coverage_full", Label: "Covered",
			Description: "The requirement needs full coverage in a dimension its type's coverage rule asks for.",
			Options:     []Option{{Name: "dimension", Label: "Dimension", Kind: OptionDimension, Required: true, Choices: Dimensions}}},
		check: func(raw json.RawMessage) error { _, err := readDimension(raw); return err },
		validate: func(raw json.RawMessage, s Subject, _ Input) error {
			dimension, err := readDimension(raw)
			if err != nil {
				return err
			}
			if level := s.Coverage[dimension]; level == "none" || level == "partial" {
				return &FieldError{"transition", fmt.Sprintf("%s is not fully %s yet. Cover it, then take this transition.", s.Key, dimension)}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.quality_min", Label: "Quality score",
			Description: "The requirement needs at least this score by the project's writing rules.",
			Options:     []Option{{Name: "minimum", Label: "Least score", Kind: OptionNumber, Required: true, Min: &scoreFloor, Max: &scoreCeiling}}},
		check: func(raw json.RawMessage) error { _, err := readMinimum(raw); return err },
		validate: func(raw json.RawMessage, s Subject, _ Input) error {
			least, err := readMinimum(raw)
			if err != nil {
				return err
			}
			switch {
			case s.Quality == nil:
				return &FieldError{"transition", fmt.Sprintf("%s is not checked yet. Wait a moment, then take the transition again.", s.Key)}
			case *s.Quality < least:
				return &FieldError{"transition", fmt.Sprintf("%s scores %d; this transition asks for %d. Fix or accept its findings, then take it again.",
					s.Key, *s.Quality, least)}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindValidator, Type: "validator.threads_resolved", Label: "Discussion resolved",
			Description: "Every comment thread on the requirement needs to be resolved first."},
		check: noConfig,
		validate: func(_ json.RawMessage, s Subject, _ Input) error {
			if s.OpenThreads > 0 {
				return &FieldError{"transition", fmt.Sprintf("%s has %s. Resolve them, then take this transition.", s.Key, count(s.OpenThreads, "open comment thread"))}
			}
			return nil
		},
	},
	{
		RuleType: RuleType{Kind: KindPostFunction, Type: "postfunction.assign_to_actor", Label: "Make the taker the owner",
			Description: "Whoever takes it becomes the requirement's owner."},
		check: noConfig,
		apply: func(_ Subject, a Actor, e *Effect) {
			id := a.UserID
			e.SetOwner, e.Owner = true, &id
		},
	},
	{
		RuleType: RuleType{Kind: KindPostFunction, Type: "postfunction.clear_owner", Label: "Clear the owner",
			Description: "The requirement is left without an owner."},
		check: noConfig,
		apply: func(_ Subject, _ Actor, e *Effect) { e.SetOwner, e.Owner = true, nil },
	},
}

func lookup(kind RuleKind, typ string) (rule, bool) {
	i := slices.IndexFunc(registry, func(r rule) bool { return r.Kind == kind && r.Type == typ })
	if i < 0 {
		return rule{}, false
	}
	return registry[i], true
}

// RuleTypes describes every rule a transition may carry, for the designer.
func RuleTypes() []RuleType {
	out := make([]RuleType, len(registry))
	for i, r := range registry {
		out[i] = r.RuleType
		if out[i].Options == nil {
			out[i].Options = []Option{}
		}
		for j := range out[i].Options {
			if out[i].Options[j].Choices == nil {
				out[i].Options[j].Choices = []string{}
			}
		}
	}
	return out
}
