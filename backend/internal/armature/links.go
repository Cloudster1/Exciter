package armature

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Requirements traced to Armature issues (#27): the links a requirement has, made and removed as the person.

var (
	// ErrLinkNotFound is a link that is gone, or one on a requirement the reader does not view.
	ErrLinkNotFound = errors.New("Armature link not found")
	// ErrSection is a section asked to take an issue, which only requirements do.
	ErrSection = errors.New("sections take no Armature issues")
	// ErrLinkExists is the same issue linked again with the same relation.
	ErrLinkExists = errors.New("the issue is linked with that relation already")
	// ErrReadOnlyToken is a token of Armature's read scope asked to link, which Armature would refuse.
	ErrReadOnlyToken = errors.New("the Armature token may only read")
)

// selectLinks reads requirement $1's links with how far each one's sync has come; one with no record yet is
// waiting for the worker.
const selectLinks = `
SELECT l.id, l.relation, l.issue_key, COALESCE(l.status_category, ''), l.status_stale,
       COALESCE(rl.state, 'pending'), COALESCE(rl.error, ''), l.broken_at, l.detached_at, COALESCE(u.name, ''), l.created_at
FROM armature_issue_link l
LEFT JOIN armature_remote_link rl ON rl.org_id = l.org_id AND rl.requirement_id = l.requirement_id AND rl.issue_key = l.issue_key
LEFT JOIN app_user u ON u.id = l.created_by
WHERE l.org_id = current_org_id() AND l.requirement_id = $1`

const linkOrder = ` ORDER BY array_position(ARRAY['implements', 'verifies', 'relates'], l.relation), l.issue_key, l.created_at`

func readLinks(ctx context.Context, tx db.DBTX, requirementID uuid.UUID, only *uuid.UUID) ([]Link, error) {
	query, args := selectLinks+linkOrder, []any{requirementID}
	if only != nil {
		query, args = selectLinks+` AND l.id = $2`, []any{requirementID, *only}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the Armature links: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Link, error) {
		var l Link
		err := row.Scan(&l.ID, &l.Relation, &l.IssueKey, &l.StatusCategory, &l.StatusStale, &l.Sync, &l.Error,
			&l.BrokenAt, &l.DetachedAt, &l.CreatedByName, &l.CreatedAt)
		return l, err
	})
}

func actorOf(ctx context.Context) (uuid.UUID, error) {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return uuid.Nil, requirement.ErrForbidden
	}
	return actor, nil
}

// Links answers the issues a requirement the reader views is traced to; none while the organization has no
// connection, since there is nothing to open them in.
func (s *Service) Links(ctx context.Context, reqKey string) ([]Link, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	out := []Link{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if e, err := s.endpoint(ctx, tx); err != nil || e == nil {
			return err
		}
		links, err := readLinks(ctx, tx, r.ID, nil)
		if links != nil {
			out = links
		}
		return err
	})
	return out, err
}

// linkable is the requirement a link would hang off, refused when the actor may not link it.
func linkable(ctx context.Context, tx db.DBTX, actor uuid.UUID, reqKey string) (*requirement.Endpoint, error) {
	r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
	switch {
	case err != nil:
		return nil, err
	case r.Kind == requirement.KindSection:
		return nil, ErrSection
	case r.Trashed:
		return nil, requirement.ErrTrashed
	case !r.Linkable:
		return nil, requirement.ErrForbidden
	}
	return r, nil
}

func checkLinkInput(in *LinkInput) (string, error) {
	if !slices.Contains(Relations, in.Relation) {
		return "", &FieldError{Field: "relation", Message: "Say whether the issue implements, verifies or relates to the requirement."}
	}
	key, ok := NormalizeKey(in.IssueKey)
	if !ok {
		return "", &FieldError{Field: "issueKey", Message: "Write the issue's key: its project key, a hyphen and its number, such as CP-12."}
	}
	return key, nil
}

// Link traces a requirement to an issue the actor may see in Armature, asked as them now, so nobody links what
// they cannot open. The worker then puts the requirement on the issue.
func (s *Service) Link(ctx context.Context, reqKey string, in LinkInput) (*Link, db.LSN, error) {
	key, err := checkLinkInput(&in)
	if err != nil {
		return nil, 0, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := linkable(ctx, tx, actor, reqKey)
		return err
	}); err != nil {
		return nil, 0, err
	}
	issue, err := s.issueToLink(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	var out *Link
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := linkable(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO armature_issue_link (org_id, requirement_id, relation, issue_key, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4) RETURNING id`, r.ID, in.Relation, issue.Key, actor).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "armature_issue_link_once" {
			return ErrLinkExists
		}
		if err != nil {
			return fmt.Errorf("link the Armature issue: %w", err)
		}
		links, err := readLinks(ctx, tx, r.ID, &id)
		if err != nil || len(links) == 0 {
			return errors.Join(err, ErrLinkNotFound)
		}
		out = &links[0]
		return nil
	})
	if err != nil {
		return nil, lsn, err
	}
	s.noteStatus(ctx, map[string]*Issue{issue.Key: issue})
	out.StatusCategory = issue.Status.Category
	return out, lsn, nil
}

// issueToLink asks Armature for the issue as the actor, past the cache, and names an issue moved since by the key
// it has now.
func (s *Service) issueToLink(ctx context.Context, key string) (*Issue, error) {
	v, status, err := s.Viewer(ctx)
	switch {
	case err != nil:
		return nil, err
	case status == StatusNotConfigured:
		return nil, ErrNotConfigured
	case status == StatusNotConnected:
		return nil, ErrNotConnected
	case v == nil:
		return nil, ErrRejected
	case v.ReadOnly:
		return nil, ErrReadOnlyToken
	}
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	issue, err := s.fetchIssue(callCtx, v, key)
	if err != nil {
		if s.failed(ctx, v, err) == StatusRejected {
			return nil, ErrRejected
		}
		return nil, err
	}
	if issue == nil {
		return nil, &FieldError{Field: "issueKey", Message: "Armature has no issue " + key + " that you may see. Check the key, or ask for access to its project in Armature."}
	}
	s.cache.PutIssue(ctx, v.OrgID, v.TokenID, key, issue)
	return issue, nil
}

// Unlink removes a trace to an issue; the worker takes the link off the issue.
func (s *Service) Unlink(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var editable bool
		err := tx.QueryRow(ctx, `
			SELECT perm_requirement_editable(requirement_id, $2) FROM armature_issue_link
			WHERE org_id = current_org_id() AND id = $1`, id, actor).Scan(&editable)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return err
		}
		if !editable {
			return requirement.ErrForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM armature_issue_link WHERE org_id = current_org_id() AND id = $1`, id)
		return err
	})
}
