package requirement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

const (
	// MaxBulk bounds the requirements one bulk read, transition or review request names (#167).
	MaxBulk = 100
	// MaxSpecChangesLimit bounds one window of the specification with changes, each entry carrying its comparison.
	MaxSpecChangesLimit = 50
)

// BulkOutcome says what a bulk operation did with one requirement.
type BulkOutcome string

const (
	BulkTaken     BulkOutcome = "taken"
	BulkRequested BulkOutcome = "requested"
	BulkRefused   BulkOutcome = "refused"
)

// BulkOutcomes lists every BulkOutcome, for the API document.
var BulkOutcomes = []BulkOutcome{BulkTaken, BulkRequested, BulkRefused}

// BulkRefusal is what the single operation would have answered for one requirement of a bulk one.
type BulkRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RequirementTransitions is what the caller may take now on one requirement of a bulk read.
type RequirementTransitions struct {
	Key         string               `json:"key"`
	Transitions []workflow.Available `json:"transitions"`
}

// BulkTransitionInput takes one transition on several requirements of a project, each on its own.
type BulkTransitionInput struct {
	TransitionID uuid.UUID `json:"transitionId"`
	Comment      string    `json:"comment,omitempty"`
	Keys         []string  `json:"keys"`
}

// BulkTransitionResult is one requirement's outcome: the requirement as it is now, or the refusal.
type BulkTransitionResult struct {
	Key         string       `json:"key"`
	Outcome     BulkOutcome  `json:"outcome"`
	Requirement *Requirement `json:"requirement"`
	Error       *BulkRefusal `json:"error"`
}

// BulkTransitioned is one requirement's outcome of a bulk transition: the requirement as it is now, or why not.
type BulkTransitioned struct {
	Key         string
	Requirement *Requirement
	Err         error
}

// CheckBulk refuses no key, more than MaxBulk and a key named twice, on field.
func CheckBulk(field string, keys []string) error {
	if len(keys) == 0 {
		return &FieldError{field, "Name at least one requirement."}
	}
	if len(keys) > MaxBulk {
		return &FieldError{field, fmt.Sprintf("Name at most %d requirements at once, and do the rest after.", MaxBulk)}
	}
	seen := map[string]bool{}
	for _, key := range keys {
		upper := strings.ToUpper(key)
		if seen[upper] {
			return &FieldError{field, fmt.Sprintf("%s is named twice. Name each requirement once.", key)}
		}
		seen[upper] = true
	}
	return nil
}

// TransitionsOf answers what the reader may take now on each requirement of a project that keys name; one the
// reader may not view, of another project, a section or in the trash is left out.
func (s *Service) TransitionsOf(ctx context.Context, reader uuid.UUID, projectKey string, keys []string) ([]RequirementTransitions, error) {
	if err := CheckBulk("key", keys); err != nil {
		return nil, err
	}
	out := []RequirementTransitions{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		for _, key := range keys {
			l, err := find(ctx, tx, reader, key)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if l.projectID != p.ID || l.Kind == KindSection || l.TrashedAt != nil {
				continue
			}
			offered, err := offer(ctx, tx, reader, l)
			if err != nil {
				return err
			}
			out = append(out, RequirementTransitions{Key: key, Transitions: offered})
		}
		return nil
	})
	return out, err
}

// BulkTransition takes one transition on each requirement keys name, each in its own transaction and in the order
// given, as Transition would, so one refused undoes nothing else. Only a request no requirement could pass is an error.
func (s *Service) BulkTransition(ctx context.Context, actor uuid.UUID, projectKey string, in BulkTransitionInput) ([]BulkTransitioned, db.LSN, error) {
	if err := CheckBulk("keys", in.Keys); err != nil {
		return nil, 0, err
	}
	if in.TransitionID == uuid.Nil {
		return nil, 0, &FieldError{"transitionId", "Choose the transition to take."}
	}
	if _, err := checkComment(in.Comment); err != nil {
		return nil, 0, err
	}
	var projectID uuid.UUID
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		projectID = p.ID
		return nil
	}); err != nil {
		return nil, 0, err
	}
	out := make([]BulkTransitioned, 0, len(in.Keys))
	var last db.LSN
	for _, key := range in.Keys {
		got, lsn, err := s.transition(ctx, actor, key, workflow.TransitionRequest{TransitionID: in.TransitionID, Comment: in.Comment}, projectID)
		last = max(last, lsn)
		out = append(out, BulkTransitioned{Key: key, Requirement: got, Err: err})
	}
	return out, last, nil
}
