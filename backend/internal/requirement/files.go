package requirement

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
)

// FindEndpointByID reads the requirement id names as the reader sees it, for a file found by its own id.
func FindEndpointByID(ctx context.Context, tx db.DBTX, reader, id uuid.UUID) (*Endpoint, error) {
	l, err := findByID(ctx, tx, reader, id)
	if err != nil {
		return nil, err
	}
	return FindEndpoint(ctx, tx, reader, l.Key)
}

// LogChange notes a change of what is on a requirement that writes no revision, as a file's upload does.
func LogChange(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, field ChangeField, from, to string) error {
	return logChange(ctx, tx, actor, id, field, from, to)
}

// FileCopier gives a copy the files of its original (#21), so the copy's content shows files of its own.
type FileCopier interface {
	// PlanCopy names a new id for each file of original the copy takes, before the copy's content is written.
	PlanCopy(ctx context.Context, tx db.DBTX, original uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	// CopyFiles writes the planned files, bytes included, on the copy.
	CopyFiles(ctx context.Context, tx db.DBTX, actor, copy uuid.UUID, ids map[uuid.UUID]uuid.UUID) error
}

// ObserveCopies has every copy bring its original's files along.
func (s *Service) ObserveCopies(files FileCopier) { s.files = files }

// pointAtFiles rewrites what content shows of the original's files to the copy's own.
func pointAtFiles(content *Normative, ids map[uuid.UUID]uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var err error
	if content.Statement, _, err = document.RewriteReferences(content.Statement, ids); err != nil {
		return err
	}
	if content.Rationale, _, err = document.RewriteReferences(content.Rationale, ids); err != nil {
		return err
	}
	values := make(map[string]json.RawMessage, len(content.Attributes))
	for id, raw := range content.Attributes {
		if values[id], _, err = document.RewriteReferences(raw, ids); err != nil {
			return err
		}
	}
	content.Attributes = values
	// A test case's steps show files as its statement does.
	test := content.Test
	if len(test.Preconditions) > 0 {
		if test.Preconditions, _, err = document.RewriteReferences(test.Preconditions, ids); err != nil {
			return err
		}
	}
	steps := make([]Step, len(test.Steps))
	for i, s := range test.Steps {
		steps[i] = s
		if steps[i].Action, _, err = document.RewriteReferences(s.Action, ids); err != nil {
			return err
		}
		if len(s.Expected) > 0 {
			if steps[i].Expected, _, err = document.RewriteReferences(s.Expected, ids); err != nil {
				return err
			}
		}
	}
	test.Steps = steps
	content.Test = test
	return nil
}
