package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

var (
	// ErrUnavailable is a transition that does not leave the requirement's status, which somebody moved meanwhile.
	ErrUnavailable = errors.New("the transition does not leave the requirement's status")
	// ErrNotYours is a transition whose conditions keep the actor from taking it.
	ErrNotYours = errors.New("the transition is not the actor's to take")
	// ErrNoTransition is a transition the requirement's workflow does not have.
	ErrNoTransition = errors.New("no such transition")
)

// leaves says whether a transition may be taken from status: one from it, or one offered from every status
// that leads somewhere else.
func leaves(t Transition, status *uuid.UUID) bool {
	if t.FromStatusID == nil {
		return status == nil || t.ToStatusID != *status
	}
	return status != nil && *t.FromStatusID == *status
}

func rulesOf(t Transition, kind RuleKind) []Rule {
	var out []Rule
	for _, r := range t.Rules {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b Rule) int { return a.Position - b.Position })
	return out
}

// allowed says whether every condition of a transition lets the actor take it.
func allowed(t Transition, s Subject, a Actor) bool {
	for _, r := range rulesOf(t, KindCondition) {
		known, ok := lookup(r.Kind, r.Type)
		if !ok || !known.allow(r.Config, s, a) {
			return false
		}
	}
	return true
}

// Offer answers the transitions the actor may take from the subject's status, in the workflow's order; one
// whose conditions fail is left out rather than refused, which is what a condition is for.
func Offer(w *Workflow, s Subject, a Actor) []Available {
	out := []Available{}
	for _, t := range w.Transitions {
		if !leaves(t, s.StatusID) || !allowed(t, s, a) {
			continue
		}
		to, _ := w.Status(t.ToStatusID)
		needs := slices.ContainsFunc(t.Rules, func(r Rule) bool { return r.Type == "validator.comment_required" })
		out = append(out, Available{ID: t.ID, Name: t.Name, ToStatus: to, NeedsComment: needs})
	}
	return out
}

// Decide weighs a chosen transition, conditions then validators, and answers what its post-functions ask for;
// the caller changes the status and applies the effect in one transaction.
func Decide(w *Workflow, id uuid.UUID, s Subject, a Actor, in Input) (*Transition, *Effect, error) {
	i := slices.IndexFunc(w.Transitions, func(t Transition) bool { return t.ID == id })
	if i < 0 {
		return nil, nil, ErrNoTransition
	}
	t := w.Transitions[i]
	if !leaves(t, s.StatusID) {
		return nil, nil, ErrUnavailable
	}
	if !allowed(t, s, a) {
		return nil, nil, ErrNotYours
	}
	in.Comment = strings.TrimSpace(in.Comment)
	for _, r := range rulesOf(t, KindValidator) {
		known, ok := lookup(r.Kind, r.Type)
		if !ok {
			return nil, nil, fmt.Errorf("transition %q has a validator this server does not know: %s", t.Name, r.Type)
		}
		if err := known.validate(s, in); err != nil {
			return nil, nil, err
		}
	}
	effect := &Effect{}
	for _, r := range rulesOf(t, KindPostFunction) {
		if known, ok := lookup(r.Kind, r.Type); ok {
			known.apply(s, a, effect)
		}
	}
	return &t, effect, nil
}

// Status finds a status of the workflow by id.
func (w *Workflow) Status(id uuid.UUID) (Status, bool) {
	for _, s := range w.Steps {
		if s.Status.ID == id {
			return s.Status, true
		}
	}
	return Status{}, false
}

// Has says whether a status is one of the workflow's.
func (w *Workflow) Has(id uuid.UUID) bool {
	_, ok := w.Status(id)
	return ok
}

// Initial is the status requirements of the workflow start in.
func (w *Workflow) Initial() (Status, bool) {
	for _, s := range w.Steps {
		if s.IsInitial {
			return s.Status, true
		}
	}
	return Status{}, false
}
