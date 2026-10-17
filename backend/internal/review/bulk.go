package review

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// BulkRequestItem names a requirement and the revision the caller saw, which its review pins.
type BulkRequestItem struct {
	Key      string `json:"key"`
	Revision int    `json:"revision"`
}

// BulkRequestInput asks for the review of several requirements of a project, each on its own (#171).
type BulkRequestInput struct {
	Items   []BulkRequestItem `json:"items"`
	Comment string            `json:"comment,omitempty"`
}

// BulkRequestResult is one requirement's outcome: the review asked for, or the refusal.
type BulkRequestResult struct {
	Key     string                   `json:"key"`
	Outcome requirement.BulkOutcome  `json:"outcome"`
	Review  *Review                  `json:"review"`
	Error   *requirement.BulkRefusal `json:"error"`
}

// BulkRequested is one requirement's outcome of a bulk request: the review asked for, or why not.
type BulkRequested struct {
	Key    string
	Review *Review
	Err    error
}

// BulkRequest asks for the review of each requirement the items name, each in its own transaction and in the order
// given, as Request would, so one refused undoes nothing else. Only a request no requirement could pass is an error.
func (s *Service) BulkRequest(ctx context.Context, projectKey string, in BulkRequestInput) ([]BulkRequested, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	keys := make([]string, len(in.Items))
	for i, item := range in.Items {
		keys[i] = item.Key
	}
	if err := requirement.CheckBulk("items", keys); err != nil {
		return nil, 0, err
	}
	if _, err := cleanComment(in.Comment); err != nil {
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
	out := make([]BulkRequested, 0, len(in.Items))
	var last db.LSN
	for _, item := range in.Items {
		got, lsn, err := s.request(ctx, item.Key, RequestInput{Revision: item.Revision, Comment: in.Comment}, projectID)
		last = max(last, lsn)
		out = append(out, BulkRequested{Key: item.Key, Review: got, Err: err})
	}
	return out, last, nil
}
