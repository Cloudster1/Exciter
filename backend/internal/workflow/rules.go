package workflow

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// Subject is the requirement a transition is weighed against, as its rules read it.
type Subject struct {
	StatusID *uuid.UUID
	OwnerID  *uuid.UUID
	// LatestAuthor wrote the latest revision, whom not_author keeps from approving their own words.
	LatestAuthor *uuid.UUID
	// MissingRequired names a required attribute without a value, empty when none is missing.
	MissingRequired string
	MissingField    string
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
	validate func(Subject, Input) error
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
		RuleType: RuleType{Kind: KindValidator, Type: "validator.comment_required", Label: "Comment required",
			Description: "It asks for a comment saying why."},
		check: noConfig,
		validate: func(_ Subject, in Input) error {
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
		validate: func(s Subject, _ Input) error {
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
		validate: func(s Subject, _ Input) error {
			if s.MissingRequired != "" {
				return &FieldError{s.MissingField, fmt.Sprintf("Give %s a value first; this transition asks for every required attribute.", s.MissingRequired)}
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
