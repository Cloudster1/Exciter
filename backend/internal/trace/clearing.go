package trace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// TopicLinkCleared is a link, or an Armature link, confirmed against the revisions it joins.
const TopicLinkCleared = "link.cleared"

// ErrRevisionMovedOn is a clearance naming a revision older than the end has now, which the clearer did not read.
var ErrRevisionMovedOn = errors.New("the requirement has a newer revision than the one named")

// CheckClearComment trims a clearance's comment and holds it to its length.
func CheckClearComment(comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" || utf8.RuneCountInString(comment) > MaxClearCommentLength {
		return "", &FieldError{"comment", fmt.Sprintf("Say in at most %d characters why the link still holds.", MaxClearCommentLength)}
	}
	return comment, nil
}

// CheckNamedRevision holds a revision the clearer names to the one the end has: a newer one is not there yet, an
// older one is ErrRevisionMovedOn.
func CheckNamedRevision(field, key string, named, current int) error {
	switch {
	case named < 1 || named > current:
		return &FieldError{field, fmt.Sprintf("%s is at revision %d. Name the revision you read.", key, current)}
	case named < current:
		return ErrRevisionMovedOn
	}
	return nil
}

// ClearRefusal names what a clearing function refused in the service's words.
func ClearRefusal(err error, notFound error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.ConstraintName == "trace_link_clear_current":
		return ErrRevisionMovedOn
	case pgErr.ConstraintName == "trace_link_clear_trashed":
		return requirement.ErrTrashed
	case pgErr.ConstraintName == "trace_link_clearance_comment":
		_, err := CheckClearComment("")
		return err
	case pgErr.Code == "P0002":
		return notFound
	case pgErr.Code == "42501":
		return requirement.ErrForbidden
	}
	return err
}

// Clear confirms a link against its ends as the actor read them, whoever may edit either end, and answers it as its
// source sees it.
func (s *Service) Clear(ctx context.Context, actor, id uuid.UUID, in ClearInput) (*Link, db.LSN, error) {
	comment, err := CheckClearComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	var out *Link
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			source, target          requirement.Ref
			sourceNumber, targetNum int
			editable, archived      bool
		)
		err := tx.QueryRow(ctx, `
			SELECT s.id, sp.key, s.number, s.revision, s.trashed_at IS NOT NULL, t.id, tp.key, t.number, t.revision,
			       t.trashed_at IS NOT NULL,
			       perm_requirement_editable(s.id, $2) OR perm_requirement_editable(t.id, $2),
			       sp.archived_at IS NOT NULL AND tp.archived_at IS NOT NULL
			FROM trace_link l
			JOIN requirement s ON s.id = l.source_id JOIN project sp ON sp.id = s.project_id
			JOIN requirement t ON t.id = l.target_id JOIN project tp ON tp.id = t.project_id
			WHERE l.id = $1 AND l.deleted_at IS NULL`, id, actor).
			Scan(&source.ID, &source.ProjectKey, &sourceNumber, &source.Revision, &source.Trashed, &target.ID, &target.ProjectKey,
				&targetNum, &target.Revision, &target.Trashed, &editable, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return err
		}
		source.Key, target.Key = requirement.Key(source.ProjectKey, sourceNumber), requirement.Key(target.ProjectKey, targetNum)
		switch {
		case !editable && archived:
			return project.ErrArchived
		case !editable:
			return requirement.ErrForbidden
		case source.Trashed || target.Trashed:
			return requirement.ErrTrashed
		}
		if err := CheckNamedRevision("sourceRevision", source.Key, in.SourceRevision, source.Revision); err != nil {
			return err
		}
		if err := CheckNamedRevision("targetRevision", target.Key, in.TargetRevision, target.Revision); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT trace_link_clear($1, $2, $3, $4)`, id, in.SourceRevision, in.TargetRevision, comment); err != nil {
			return ClearRefusal(err, ErrLinkNotFound)
		}
		if err := events.Emit(ctx, tx, TopicLinkCleared, map[string]any{"actorId": actor, "linkId": id, "requirementId": source.ID}); err != nil {
			return err
		}
		links, err := readLinks(ctx, tx, source.ID, &id)
		if err != nil {
			return err
		}
		if len(links) == 0 {
			return ErrLinkNotFound
		}
		out = &links[0]
		return nil
	})
	return out, lsn, err
}

// Clearances answers every confirmation of a link whose ends the reader sees, newest first.
func (s *Service) Clearances(ctx context.Context, reader, id uuid.UUID) ([]Clearance, error) {
	out := []Clearance{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var seen bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM trace_link WHERE id = $1)`, id).Scan(&seen); err != nil {
			return err
		}
		if !seen {
			return ErrLinkNotFound
		}
		rows, err := tx.Query(ctx, `
			SELECT c.id, c.source_rev, c.target_rev, c.comment, COALESCE(u.name, ''), c.cleared_at
			FROM trace_link_clearance c LEFT JOIN app_user u ON u.id = c.cleared_by
			WHERE c.link_id = $1 ORDER BY c.cleared_at DESC, c.id DESC`, id)
		if err != nil {
			return fmt.Errorf("read the clearances: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Clearance, error) {
			var c Clearance
			err := row.Scan(&c.ID, &c.SourceRevision, &c.TargetRevision, &c.Comment, &c.ClearedByName, &c.ClearedAt)
			return c, err
		})
		if found != nil {
			out = found
		}
		return err
	})
	return out, err
}

// Suspects answers a window of the suspect links touching a project's requirements whose ends the reader sees, the
// longest waiting first, and how many there are.
func (s *Service) Suspects(ctx context.Context, reader uuid.UUID, projectKey string, limit, offset int) ([]SuspectLink, int, error) {
	out := []SuspectLink{}
	total := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		const touching = `
			FROM trace_link_state v
			JOIN requirement s ON s.id = v.source_id JOIN project sp ON sp.id = s.project_id
			JOIN requirement t ON t.id = v.target_id JOIN project tp ON tp.id = t.project_id
			LEFT JOIN req_status ss ON ss.id = s.status_id
			LEFT JOIN req_status ts ON ts.id = t.status_id
			WHERE v.suspect AND (s.project_id = $1 OR t.project_id = $1)`
		if err := tx.QueryRow(ctx, `SELECT count(*)::int`+touching, p.ID).Scan(&total); err != nil {
			return fmt.Errorf("count the suspect links: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT v.id, v.type_id, v.source_reviewed_rev, v.target_reviewed_rev, v.suspect_since,
			       s.id, sp.key, s.number, s.kind, s.title, s.revision, ss.id, COALESCE(ss.name, ''), COALESCE(ss.category, ''),
			       t.id, tp.key, t.number, t.kind, t.title, t.revision, ts.id, COALESCE(ts.name, ''), COALESCE(ts.category, '')`+
			touching+` ORDER BY v.suspect_since, v.id LIMIT $2 OFFSET $3`, p.ID, limit, offset)
		if err != nil {
			return fmt.Errorf("list the suspect links: %w", err)
		}
		type suspect struct {
			link   SuspectLink
			typeID uuid.UUID
			rev    Revisions
		}
		var all []suspect
		ids := []uuid.UUID{}
		for rows.Next() {
			var (
				f    suspect
				ends [2]refScan
			)
			if err := rows.Scan(&f.link.ID, &f.typeID, &f.rev.SourceReviewed, &f.rev.TargetReviewed, &f.link.Since,
				&ends[0].ref.ID, &ends[0].ref.ProjectKey, &ends[0].number, &ends[0].ref.Kind, &ends[0].ref.Title, &ends[0].ref.Revision,
				&ends[0].statusID, &ends[0].status.Name, &ends[0].status.Category,
				&ends[1].ref.ID, &ends[1].ref.ProjectKey, &ends[1].number, &ends[1].ref.Kind, &ends[1].ref.Title, &ends[1].ref.Revision,
				&ends[1].statusID, &ends[1].status.Name, &ends[1].status.Category); err != nil {
				rows.Close()
				return err
			}
			f.link.Source, f.link.Target = ends[0].done(), ends[1].done()
			f.rev.SourceCurrent, f.rev.TargetCurrent = f.link.Source.Revision, f.link.Target.Revision
			all = append(all, f)
			ids = append(ids, f.typeID)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		types, err := linkTypes(ctx, tx, `t.id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		for _, f := range all {
			t := pick(types, f.typeID, nil)
			if t == nil {
				continue
			}
			f.link.Type, f.link.Ends = *t, Moved(t.SuspectEnd, f.rev)
			out = append(out, f.link)
		}
		return nil
	})
	return out, total, err
}

// refScan is one end of a link as a row holds it.
type refScan struct {
	ref      requirement.Ref
	number   int
	statusID *uuid.UUID
	status   requirement.StatusRef
}

func (r refScan) done() requirement.Ref {
	r.ref.Key = requirement.Key(r.ref.ProjectKey, r.number)
	if r.statusID != nil {
		r.status.ID = *r.statusID
		r.ref.Status = &r.status
	}
	return r.ref
}
