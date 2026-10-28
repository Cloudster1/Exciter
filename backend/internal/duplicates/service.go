package duplicates

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

var (
	// ErrForbidden is a decision or a setting by somebody who may not make it.
	ErrForbidden = errors.New("not allowed to decide about duplicates here")
	// ErrStale is a decision against a revision of either side that is no longer current.
	ErrStale = errors.New("a side changed since it was read")
)

// FieldError is a field that does not hold, with the sentence that says how to fix it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service finds similar requirements and keeps what was decided about pairs.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// thresholdOf is how alike two requirements of a project must be to be listed.
func thresholdOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (float64, *time.Time, error) {
	var (
		threshold float64
		at        time.Time
	)
	err := tx.QueryRow(ctx, `SELECT threshold, updated_at FROM duplicate_settings WHERE project_id = $1`, projectID).Scan(&threshold, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultThreshold, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("read the threshold: %w", err)
	}
	return threshold, &at, nil
}

// textOf is what of a requirement is compared, as the database's similarity_text holds it.
func textOf(title string, statement document.Node) string {
	text := title + " " + document.PlainText(statement)
	if utf8.RuneCountInString(text) > MaxTextChars {
		text = string([]rune(text)[:MaxTextChars])
	}
	return text
}

// similar are the requirements alike text above the threshold, of the project and of libraries the reader may read,
// most alike first; exclude is left out, and a pair of it settled by a decision too.
func similar(ctx context.Context, tx db.DBTX, projectID uuid.UUID, text string, exclude uuid.UUID, threshold float64) ([]Similar, error) {
	out := []Similar{}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold', $1, true)`, strconv.FormatFloat(threshold, 'f', 2, 64)); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT r.id, p.key, r.number, r.title, r.revision, r.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''),
		       similarity(r.similarity_text, $1), r.project_id <> $2
		FROM requirement r JOIN project p ON p.id = r.project_id LEFT JOIN req_status st ON st.id = r.status_id
		WHERE r.kind = 'requirement' AND r.trashed_at IS NULL AND r.id <> $3
		  AND (r.project_id = $2 OR COALESCE((p.settings ->> 'library')::boolean, false))
		  AND r.similarity_text % $1
		  AND NOT (r.project_id = $2 AND $3 <> '00000000-0000-0000-0000-000000000000'::uuid
		           AND duplicate_pair_settled(least(r.id, $3), greatest(r.id, $3)))
		ORDER BY similarity(r.similarity_text, $1) DESC, r.id
		LIMIT $4`, text, projectID, exclude, MaxSimilar)
	if err != nil {
		return nil, fmt.Errorf("find similar requirements: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Similar, error) {
		var (
			s        Similar
			number   int
			statusID *uuid.UUID
			status   requirement.StatusRef
		)
		ref := &s.Requirement
		if err := row.Scan(&ref.ID, &ref.ProjectKey, &number, &ref.Title, &ref.Revision, &statusID, &status.Name, &status.Category,
			&s.Score, &s.Library); err != nil {
			return s, err
		}
		ref.Kind, ref.Key = requirement.KindRequirement, requirement.Key(ref.ProjectKey, number)
		if statusID != nil {
			status.ID = *statusID
			ref.Status = &status
		}
		return s, nil
	})
	return append(out, found...), err
}

// Draft is what a draft is like as it is written.
func (s *Service) Draft(ctx context.Context, reader uuid.UUID, projectKey string, in DraftInput) ([]Similar, float64, error) {
	var (
		out       []Similar
		threshold float64
	)
	statement := document.Node{Type: "doc"}
	if len(in.Statement) > 0 && string(in.Statement) != "null" {
		n, err := document.Parse(in.Statement)
		if err != nil {
			var invalid *document.InvalidError
			if errors.As(err, &invalid) {
				return nil, 0, &FieldError{"statement", invalid.Message}
			}
			return nil, 0, err
		}
		statement = n
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return ErrForbidden
		}
		exclude := uuid.Nil
		if key := strings.TrimSpace(in.ExcludeKey); key != "" {
			r, err := requirement.FindEndpoint(ctx, tx, reader, key)
			if err != nil {
				return err
			}
			exclude = r.ID
		}
		if threshold, _, err = thresholdOf(ctx, tx, p.ID); err != nil {
			return err
		}
		out, err = similar(ctx, tx, p.ID, textOf(in.Title, statement), exclude, threshold)
		return err
	})
	return out, threshold, err
}

// Of is what a saved requirement is like.
func (s *Service) Of(ctx context.Context, reader uuid.UUID, key string) ([]Similar, float64, error) {
	var (
		out       []Similar
		threshold float64
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		if threshold, _, err = thresholdOf(ctx, tx, r.ProjectID); err != nil {
			return err
		}
		if r.Kind != requirement.KindRequirement || r.Trashed {
			out = []Similar{}
			return nil
		}
		var text string
		if err := tx.QueryRow(ctx, `SELECT similarity_text FROM requirement WHERE id = $1`, r.ID).Scan(&text); err != nil {
			return err
		}
		out, err = similar(ctx, tx, r.ProjectID, text, r.ID, threshold)
		return err
	})
	return out, threshold, err
}

// PairsQuery narrows a project's pairs: by types, a section either side is below, and a query either side matches.
type PairsQuery struct {
	Types   []uuid.UUID
	Section string
	Query   string
	Cursor  string
	Limit   int
}

// Pairs are a project's likely duplicates above its threshold, most alike first, a page at a time; a pair whose
// decision still holds stays away.
func (s *Service) Pairs(ctx context.Context, reader uuid.UUID, projectKey string, q PairsQuery) ([]Pair, string, float64, error) {
	out, next, threshold := []Pair{}, "", 0.0
	offset := 0
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 0 {
			return nil, "", 0, &FieldError{"cursor", "Give the cursor the page before answered as next."}
		}
		offset = n
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if threshold, _, err = thresholdOf(ctx, tx, p.ID); err != nil {
			return err
		}
		var kept []uuid.UUID
		narrowed := len(q.Types) > 0 || strings.TrimSpace(q.Query) != "" || strings.TrimSpace(q.Section) != ""
		if narrowed {
			if kept, err = keptBy(ctx, tx, reader, p, q); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `
			SELECT s.score, a.id, a.number, a.title, a.revision, b.id, b.number, b.title, b.revision
			FROM requirement_similarity s
			JOIN requirement a ON a.id = s.low_id JOIN requirement b ON b.id = s.high_id
			WHERE s.project_id = $1 AND s.score >= $2 AND a.trashed_at IS NULL AND b.trashed_at IS NULL
			  AND ($3 = false OR s.low_id = ANY ($4) OR s.high_id = ANY ($4))
			  AND NOT duplicate_pair_settled(s.low_id, s.high_id)
			ORDER BY s.score DESC, s.low_id, s.high_id
			LIMIT $5 OFFSET $6`, p.ID, threshold, narrowed, kept, q.Limit+1, offset)
		if err != nil {
			return fmt.Errorf("list the pairs: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Pair, error) {
			var (
				pair   Pair
				na, nb int
			)
			if err := row.Scan(&pair.Score, &pair.A.ID, &na, &pair.A.Title, &pair.A.Revision, &pair.B.ID, &nb, &pair.B.Title, &pair.B.Revision); err != nil {
				return pair, err
			}
			pair.A.ProjectKey, pair.B.ProjectKey = p.Key, p.Key
			pair.A.Kind, pair.B.Kind = requirement.KindRequirement, requirement.KindRequirement
			pair.A.Key, pair.B.Key = requirement.Key(p.Key, na), requirement.Key(p.Key, nb)
			return pair, nil
		})
		if err != nil {
			return err
		}
		if len(found) > q.Limit {
			found, next = found[:q.Limit], strconv.Itoa(offset+q.Limit)
		}
		out = append(out, found...)
		return nil
	})
	return out, next, threshold, err
}

// keptBy are the requirements the narrowing keeps.
func keptBy(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, q PairsQuery) ([]uuid.UUID, error) {
	kept, err := search.Keep(ctx, tx, reader, p, search.Filter{Types: q.Types, Query: q.Query}, search.Sort{})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(kept))
	for _, k := range kept {
		ids = append(ids, k.Ref.ID)
	}
	key := strings.TrimSpace(q.Section)
	if key == "" {
		return ids, nil
	}
	section, err := requirement.FindEndpoint(ctx, tx, reader, key)
	if err != nil || section.ProjectID != p.ID {
		return nil, &FieldError{"section", "Name a section of this project by its key."}
	}
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE below AS (
			SELECT id FROM requirement WHERE id = $1
			UNION ALL SELECT r.id FROM requirement r JOIN below b ON r.parent_id = b.id WHERE r.trashed_at IS NULL)
		SELECT id FROM below WHERE id = ANY ($2)`, section.ID, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// side is one requirement of a pair, found as the actor sees it and held to the revision they read.
type side struct {
	*requirement.Endpoint
}

func sidesOf(ctx context.Context, tx db.DBTX, actor uuid.UUID, p *project.Project, a, b Side, fa, fb string) (side, side, error) {
	read := func(s Side, field string) (side, error) {
		e, err := requirement.FindEndpoint(ctx, tx, actor, s.Key)
		if errors.Is(err, requirement.ErrNotFound) || (err == nil && (e.ProjectID != p.ID || e.Kind != requirement.KindRequirement)) {
			return side{}, &FieldError{field, "Name a requirement of this project by its key."}
		}
		if err != nil {
			return side{}, err
		}
		if e.Revision != s.Revision {
			return side{}, ErrStale
		}
		return side{e}, nil
	}
	x, err := read(a, fa)
	if err != nil {
		return side{}, side{}, err
	}
	y, err := read(b, fb)
	if err != nil {
		return side{}, side{}, err
	}
	if x.ID == y.ID {
		return side{}, side{}, &FieldError{fb, "Name two requirements; a requirement is no duplicate of itself."}
	}
	return x, y, nil
}

// decide writes a decision about a pair, ordered as the database keeps pairs.
func decide(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectID uuid.UUID, x, y side, kind Kind, reason string, link, kept *uuid.UUID) (*Decision, error) {
	low, high := x, y
	if strings.Compare(y.ID.String(), x.ID.String()) < 0 {
		low, high = y, x
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO duplicate_decision (project_id, low_id, high_id, low_rev, high_rev, kind, reason, link_id, kept_id, decided_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		projectID, low.ID, high.ID, low.Revision, high.Revision, string(kind), reason, link, kept, actor).Scan(&id); err != nil {
		return nil, fmt.Errorf("keep the decision: %w", err)
	}
	return decisionByID(ctx, tx, id)
}

func decisionByID(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Decision, error) {
	var (
		d          Decision
		kind       string
		pk         string
		la, lb     int
		keptNumber *int
	)
	err := tx.QueryRow(ctx, `
		SELECT d.id, d.kind, p.key, a.number, d.low_rev, b.number, d.high_rev, d.reason, d.link_id, k.number,
		       COALESCE(u.id, '00000000-0000-0000-0000-000000000000'), COALESCE(u.name, ''), d.decided_at
		FROM duplicate_decision d JOIN project p ON p.id = d.project_id
		JOIN requirement a ON a.id = d.low_id JOIN requirement b ON b.id = d.high_id
		LEFT JOIN requirement k ON k.id = d.kept_id LEFT JOIN app_user u ON u.id = d.decided_by
		WHERE d.id = $1`, id).Scan(&d.ID, &kind, &pk, &la, &d.A.Revision, &lb, &d.B.Revision, &d.Reason, &d.LinkID, &keptNumber,
		&d.DecidedBy.ID, &d.DecidedBy.Name, &d.DecidedAt)
	if err != nil {
		return nil, err
	}
	d.Kind, d.A.Key, d.B.Key = Kind(kind), requirement.Key(pk, la), requirement.Key(pk, lb)
	if keptNumber != nil {
		d.KeptKey = requirement.Key(pk, *keptNumber)
	}
	return &d, nil
}

// deciding reads the project a decision is made in, which the actor edits while it is open.
func deciding(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case !p.Can.Edit:
		return nil, ErrForbidden
	}
	return p, nil
}

// Dismiss says a pair does not say the same thing, until either side is revised.
func (s *Service) Dismiss(ctx context.Context, actor uuid.UUID, projectKey string, in DismissInput) (*Decision, db.LSN, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > MaxReasonChars {
		return nil, 0, &FieldError{"reason", fmt.Sprintf("Say in at most %d characters why the two are not the same.", MaxReasonChars)}
	}
	var out *Decision
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := deciding(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		x, y, err := sidesOf(ctx, tx, actor, p, in.A, in.B, "a", "b")
		if err != nil {
			return err
		}
		out, err = decide(ctx, tx, actor, p.ID, x, y, Dismissed, reason, nil, nil)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// Link links the two sides of a pair, which settles it while the link lasts.
func (s *Service) Link(ctx context.Context, actor uuid.UUID, projectKey string, in LinkInput) (*Decision, db.LSN, error) {
	if strings.TrimSpace(in.LinkTypeKey) == "" {
		return nil, 0, &FieldError{"linkTypeKey", "Choose one of the project's link types."}
	}
	var out *Decision
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := deciding(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		x, y, err := sidesOf(ctx, tx, actor, p, in.Source, in.Target, "source", "target")
		if err != nil {
			return err
		}
		link, err := trace.LinkIn(ctx, tx, actor, x.Key, trace.LinkInput{TypeKey: in.LinkTypeKey, OtherKey: y.Key})
		// The trace links name the type by its id; this request names it by its key, where the refusal belongs.
		var field *trace.FieldError
		if errors.As(err, &field) && field.Field == "typeId" {
			return &FieldError{"linkTypeKey", field.Message}
		}
		if err != nil {
			return err
		}
		out, err = decide(ctx, tx, actor, p.ID, x, y, Linked, "", &link.ID, nil)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// Merge keeps one side of a pair and puts the other in the trash, its links made again on the side kept, in one
// transaction; the trashed side keeps its own links, so restoring it brings them back.
func (s *Service) Merge(ctx context.Context, actor uuid.UUID, projectKey string, in MergeInput) (*Merge, db.LSN, error) {
	comment := strings.TrimSpace(in.Comment)
	if utf8.RuneCountInString(comment) > MaxReasonChars {
		return nil, 0, &FieldError{"comment", fmt.Sprintf("Keep the comment to at most %d characters.", MaxReasonChars)}
	}
	var out *Merge
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := deciding(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		keep, gone, err := sidesOf(ctx, tx, actor, p, in.Keep, in.Trash, "keep", "trash")
		if err != nil {
			return err
		}
		if !keep.Linkable || !gone.Linkable {
			return ErrForbidden
		}
		links, err := moveLinks(ctx, tx, actor, keep, gone)
		if err != nil {
			return err
		}
		if err := requirement.TrashIn(ctx, tx, actor, gone.Key); err != nil {
			return err
		}
		kept := keep.ID
		decision, err := decide(ctx, tx, actor, p.ID, keep, gone, Merged, comment, nil, &kept)
		if err != nil {
			return err
		}
		out = &Merge{Decision: *decision, Links: links}
		return nil
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// moveLinks makes each live link of the side that goes again on the side kept, each in a savepoint, so a link the
// database refuses there is skipped with the reason rather than undoing the merge.
func moveLinks(ctx context.Context, tx db.DBTX, actor uuid.UUID, keep, gone side) ([]MovedLink, error) {
	type link struct {
		typeID          uuid.UUID
		typeKey         string
		retired         bool
		source, target  uuid.UUID
		otherKey        string
		outgoing, twice bool
	}
	rows, err := tx.Query(ctx, `
		SELECT l.type_id, lt.key, lt.retired, l.source_id, l.target_id, op.key || '-R' || o.number, l.source_id = $1,
		       EXISTS (SELECT 1 FROM trace_link x WHERE x.deleted_at IS NULL AND x.type_id = l.type_id
		               AND x.source_id = CASE WHEN l.source_id = $1 THEN $2 ELSE l.source_id END
		               AND x.target_id = CASE WHEN l.target_id = $1 THEN $2 ELSE l.target_id END)
		FROM trace_link l JOIN link_type lt ON lt.id = l.type_id
		JOIN requirement o ON o.id = CASE WHEN l.source_id = $1 THEN l.target_id ELSE l.source_id END
		JOIN project op ON op.id = o.project_id
		WHERE l.deleted_at IS NULL AND (l.source_id = $1 OR l.target_id = $1)
		ORDER BY l.created_at`, gone.ID, keep.ID)
	if err != nil {
		return nil, fmt.Errorf("read the links to move: %w", err)
	}
	links, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (link, error) {
		var l link
		return l, row.Scan(&l.typeID, &l.typeKey, &l.retired, &l.source, &l.target, &l.otherKey, &l.outgoing, &l.twice)
	})
	if err != nil {
		return nil, err
	}
	out := []MovedLink{}
	for _, l := range links {
		moved := MovedLink{LinkTypeKey: l.typeKey, OtherKey: l.otherKey, Outgoing: l.outgoing}
		source, target := l.source, l.target
		if source == gone.ID {
			source = keep.ID
		}
		if target == gone.ID {
			target = keep.ID
		}
		switch {
		case source == target:
			moved.Skipped = SkipSelf
		case l.twice:
			moved.Skipped = SkipDuplicate
		case l.retired:
			moved.Skipped = SkipRetired
		default:
			moved.Skipped, err = insertLink(ctx, tx, actor, l.typeID, source, target)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, moved)
	}
	return out, nil
}

// insertLink writes one link in a savepoint and says the database refused it, empty when it did not.
func insertLink(ctx context.Context, tx db.DBTX, actor, typeID, source, target uuid.UUID) (SkipReason, error) {
	if _, err := tx.Exec(ctx, `SAVEPOINT duplicate_link`); err != nil {
		return "", err
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO trace_link (type_id, source_id, target_id, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		typeID, source, target, actor).Scan(&id)
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT duplicate_link`); err != nil {
			return "", err
		}
		return SkipRefused, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT duplicate_link`); err != nil {
		return "", err
	}
	return "", requirement.EmitLink(ctx, tx, requirement.TopicLinkCreated, actor, id, source, target)
}

// Settings are how alike two requirements of a project must be to be listed.
func (s *Service) Settings(ctx context.Context, reader uuid.UUID, projectKey string) (*Settings, error) {
	var out *Settings
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		threshold, at, err := thresholdOf(ctx, tx, p.ID)
		out = &Settings{Threshold: threshold, UpdatedAt: at}
		return err
	})
	return out, err
}

// SaveSettings sets the threshold; the project's administrators'.
func (s *Service) SaveSettings(ctx context.Context, actor uuid.UUID, projectKey string, in SettingsInput) (*Settings, db.LSN, error) {
	if in.Threshold < MinThreshold || in.Threshold > MaxThreshold {
		return nil, 0, &FieldError{"threshold", fmt.Sprintf("Choose a similarity from %.2f to %.2f.", MinThreshold, MaxThreshold)}
	}
	var out *Settings
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		switch {
		case err != nil:
			return err
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Administer:
			return ErrForbidden
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO duplicate_settings (project_id, threshold, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (project_id) DO UPDATE SET threshold = EXCLUDED.threshold, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			p.ID, in.Threshold, actor); err != nil {
			return fmt.Errorf("set the threshold: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionDuplicateSettingsSaved, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"threshold": in.Threshold}}); err != nil {
			return err
		}
		threshold, at, err := thresholdOf(ctx, tx, p.ID)
		out = &Settings{Threshold: threshold, UpdatedAt: at}
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}
