package stator

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Requirements traced to Stator pages (#41): the links a requirement has, made and removed as the person, as its
// Armature links are.

var (
	// ErrLinkNotFound is a link that is gone, or one on a requirement the reader does not view.
	ErrLinkNotFound = errors.New("Stator link not found")
	// ErrSection is a section asked to take a page, which only requirements do.
	ErrSection = errors.New("sections take no Stator pages")
	// ErrLinkExists is the same page linked again with the same relation.
	ErrLinkExists = errors.New("the page is linked with that relation already")
)

// SuspectSQL says whether the page link aliased l of the requirement aliased r is suspect: its page has a newer
// version than the one confirmed, and neither is gone.
const SuspectSQL = `COALESCE(r.trashed_at IS NULL AND l.broken_at IS NULL AND l.detached_at IS NULL AND l.page_version > l.page_version_seen, false)`

// selectLinks reads requirement $1's page links; their remote links on the pages come with #42.
const selectLinks = `
SELECT l.id, l.relation, l.page_id, l.space_key, l.url, COALESCE(l.page_version, 0),
       COALESCE(l.page_version_seen, l.page_version, 0), ` + SuspectSQL + `, l.reviewed_rev,
       l.broken_at, l.detached_at, COALESCE(u.name, ''), l.created_at
FROM stator_page_link l
JOIN requirement r ON r.id = l.requirement_id
LEFT JOIN app_user u ON u.id = l.created_by
WHERE l.requirement_id = $1`

const linkOrder = ` ORDER BY array_position(ARRAY['specifies', 'describes', 'relates'], l.relation), l.created_at, l.id`

func readLinks(ctx context.Context, tx db.DBTX, requirementID uuid.UUID, only *uuid.UUID) ([]Link, error) {
	query, args := selectLinks+linkOrder, []any{requirementID}
	if only != nil {
		query, args = selectLinks+` AND l.id = $2`, []any{requirementID, *only}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the Stator links: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Link, error) {
		l := Link{Sync: SyncNone}
		err := row.Scan(&l.ID, &l.Relation, &l.PageID, &l.SpaceKey, &l.URL, &l.PageVersion, &l.PageVersionSeen, &l.Suspect,
			&l.ReviewedRevision, &l.BrokenAt, &l.DetachedAt, &l.CreatedByName, &l.CreatedAt)
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

// Links answers the pages a requirement the reader views is traced to; none while the installation has no
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

// Link traces a requirement to a page the actor may read in Stator, asked as them now, so nobody links what they
// cannot open. The link takes the page's version as the one seen.
func (s *Service) Link(ctx context.Context, reqKey string, in LinkInput) (*Link, db.LSN, error) {
	if !slices.Contains(Relations, in.Relation) {
		return nil, 0, &FieldError{Field: "relation", Message: "Say whether the page specifies, describes or relates to the requirement."}
	}
	if in.PageID == uuid.Nil {
		return nil, 0, &FieldError{Field: "pageId", Message: "Pick a page to link."}
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
	page, err := s.pageToLink(ctx, in.PageID)
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
			INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, r.ID, in.Relation, page.ID, page.SpaceKey, page.URL, actor).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "stator_page_link_once" {
			return ErrLinkExists
		}
		if err != nil {
			return fmt.Errorf("link the Stator page: %w", err)
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
	s.noteVersions(ctx, map[uuid.UUID]*Page{page.ID: page})
	out.PageVersion, out.PageVersionSeen = page.Version, page.Version
	return out, lsn, nil
}

// pageToLink asks Stator for the page as the actor, past the cache.
func (s *Service) pageToLink(ctx context.Context, id uuid.UUID) (*Page, error) {
	v, err := s.writer(ctx)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	page, err := s.fetchPage(callCtx, v, id)
	if err != nil {
		if s.failed(ctx, v, err) == StatusRejected {
			return nil, ErrRejected
		}
		return nil, err
	}
	if page == nil {
		return nil, &FieldError{Field: "pageId", Message: "Stator has no page you may read with that id. Pick the page again, or ask for access to its space in Stator."}
	}
	s.cache.PutPage(ctx, v.TokenID, id, page)
	return page, nil
}

// Unlink removes a trace to a page.
func (s *Service) Unlink(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var editable bool
		err := tx.QueryRow(ctx, `
			SELECT perm_requirement_editable(requirement_id, $2) FROM stator_page_link WHERE id = $1`, id, actor).Scan(&editable)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return err
		}
		if !editable {
			return requirement.ErrForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM stator_page_link WHERE id = $1`, id)
		return err
	})
}

// Clear confirms a trace to a page against the page's version the actor read; the requirement's editors do, and its
// revision now is recorded with it.
func (s *Service) Clear(ctx context.Context, id uuid.UUID, in ClearInput) (*Link, db.LSN, error) {
	comment, err := trace.CheckClearComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Link
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			requirementID      uuid.UUID
			version            int
			trashed            bool
			editable, archived bool
		)
		err := tx.QueryRow(ctx, `
			SELECT r.id, COALESCE(l.page_version, 0), r.trashed_at IS NOT NULL, perm_requirement_editable(r.id, $2),
			       p.archived_at IS NOT NULL
			FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id JOIN project p ON p.id = r.project_id
			WHERE l.id = $1`, id, actor).Scan(&requirementID, &version, &trashed, &editable, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case archived:
			return project.ErrArchived
		case !editable:
			return requirement.ErrForbidden
		case trashed:
			return requirement.ErrTrashed
		case in.PageVersion < 1 || in.PageVersion > version:
			return &FieldError{Field: "pageVersion", Message: fmt.Sprintf("The page is at version %d. Name the version you read.", version)}
		case in.PageVersion < version:
			return trace.ErrRevisionMovedOn
		}
		if _, err := tx.Exec(ctx, `SELECT stator_page_link_clear($1, $2, $3)`, id, in.PageVersion, comment); err != nil {
			return trace.ClearRefusal(err, ErrLinkNotFound)
		}
		if err := events.Emit(ctx, tx, trace.TopicLinkCleared, map[string]any{"actorId": actor, "statorLinkId": id, "requirementId": requirementID}); err != nil {
			return err
		}
		links, err := readLinks(ctx, tx, requirementID, &id)
		if err != nil || len(links) == 0 {
			return errors.Join(err, ErrLinkNotFound)
		}
		out = &links[0]
		return nil
	})
	return out, lsn, err
}
