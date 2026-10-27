package trace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// selectLinks reads the live links of requirement $1 the reader sees, each from that end, in the order of
// their types and directions.
var selectLinks = `
SELECT ` + via.Column("l") + `, l.id, l.type_id, l.source_id = $1, o.id, op.key, o.number, o.kind, o.title, o.revision,
       o.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''), o.trashed_at IS NOT NULL,
       l.source_reviewed_rev, l.target_reviewed_rev, l.created_at, COALESCE(u.name, ''), h.revision, h.trashed_at IS NOT NULL
FROM trace_link l
JOIN link_type lt ON lt.id = l.type_id
JOIN requirement h ON h.id = $1
JOIN requirement o ON o.id = CASE WHEN l.source_id = $1 THEN l.target_id ELSE l.source_id END
JOIN project op ON op.id = o.project_id
LEFT JOIN req_status st ON st.id = o.status_id
LEFT JOIN app_user u ON u.id = l.created_by
WHERE l.deleted_at IS NULL AND (l.source_id = $1 OR l.target_id = $1)`

const linkOrder = ` ORDER BY lt.project_id IS NOT NULL, lt.id, l.source_id = $1 DESC, op.key, o.number`

// found is a link as read, before its type is put in.
type found struct {
	link   Link
	typeID uuid.UUID
	// here is the requirement asked about, as its revision and trash stand.
	here        int
	hereTrashed bool
}

func readLinks(ctx context.Context, tx db.DBTX, id uuid.UUID, only *uuid.UUID) ([]Link, error) {
	query, args := selectLinks+linkOrder, []any{id}
	if only != nil {
		query, args = selectLinks+` AND l.id = $2`, []any{id, *only}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read links: %w", err)
	}
	var all []found
	for rows.Next() {
		var (
			f        found
			outgoing bool
			number   int
			statusID *uuid.UUID
			status   requirement.StatusRef
		)
		o := &f.link.Other
		if err := rows.Scan(&f.link.Via, &f.link.ID, &f.typeID, &outgoing, &o.ID, &o.ProjectKey, &number, &o.Kind, &o.Title, &o.Revision,
			&statusID, &status.Name, &status.Category, &o.Trashed,
			&f.link.SourceRevision, &f.link.TargetRevision, &f.link.CreatedAt, &f.link.CreatedByName, &f.here, &f.hereTrashed); err != nil {
			rows.Close()
			return nil, err
		}
		o.Key = requirement.Key(o.ProjectKey, number)
		if statusID != nil {
			status.ID = *statusID
			o.Status = &status
		}
		f.link.Direction = Incoming
		if outgoing {
			f.link.Direction = Outgoing
		}
		all = append(all, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(all))
	for _, f := range all {
		if !slices.Contains(ids, f.typeID) {
			ids = append(ids, f.typeID)
		}
	}
	types, err := linkTypes(ctx, tx, `t.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(all))
	for _, f := range all {
		t := pick(types, f.typeID, nil)
		if t == nil {
			continue
		}
		f.link.Type = *t
		f.link.Phrase = t.phrase(f.link.Direction)
		f.link.Suspect = Moved(t.SuspectEnd, f.revisions())
		out = append(out, f.link)
	}
	return out, nil
}

// revisions puts the requirement asked about and the other end in their places on the link.
func (f found) revisions() Revisions {
	r := Revisions{SourceReviewed: f.link.SourceRevision, TargetReviewed: f.link.TargetRevision, Trashed: f.hereTrashed || f.link.Other.Trashed}
	r.SourceCurrent, r.TargetCurrent = f.here, f.link.Other.Revision
	if f.link.Direction == Incoming {
		r.SourceCurrent, r.TargetCurrent = f.link.Other.Revision, f.here
	}
	return r
}

// Links answers a requirement's live links in both directions, to requirements the reader may view.
func (s *Service) Links(ctx context.Context, reader uuid.UUID, key string) ([]Link, error) {
	out := []Link{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, reader, key)
		if err != nil {
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

func checkLinkInput(in *LinkInput) error {
	in.OtherKey = strings.ToUpper(strings.TrimSpace(in.OtherKey))
	if in.OtherKey == "" {
		return &FieldError{"otherKey", "Name the requirement to link to, such as CHK-R2."}
	}
	if in.Direction == "" {
		in.Direction = Outgoing
	}
	if !slices.Contains(Directions, in.Direction) {
		return &FieldError{"direction", "Say whether this requirement is the link's source (outgoing) or its target (incoming)."}
	}
	if in.TypeID == nil && strings.TrimSpace(in.TypeKey) == "" {
		return &FieldError{"typeId", "Pick a link type, such as derives."}
	}
	return nil
}

// Link links the requirement key names to another, from it unless the direction says to it.
func (s *Service) Link(ctx context.Context, actor uuid.UUID, key string, in LinkInput) (*Link, db.LSN, error) {
	var out *Link
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = LinkIn(ctx, tx, actor, key, in)
		return err
	})
	return out, lsn, err
}

// LinkIn links a requirement in the caller's transaction, as linking a pair of duplicates does (#208).
func LinkIn(ctx context.Context, tx db.DBTX, actor uuid.UUID, key string, in LinkInput) (*Link, error) {
	if err := checkLinkInput(&in); err != nil {
		return nil, err
	}
	here, err := requirement.FindEndpoint(ctx, tx, actor, key)
	if err != nil {
		return nil, err
	}
	switch {
	case here.Kind == requirement.KindSection:
		return nil, ErrSection
	case here.Trashed:
		return nil, requirement.ErrTrashed
	case here.Archived:
		return nil, project.ErrArchived
	case !here.Linkable:
		return nil, requirement.ErrForbidden
	}
	other, err := linkEnd(ctx, tx, actor, here, in.OtherKey)
	if err != nil {
		return nil, err
	}
	source, target := here, other
	if in.Direction == Incoming {
		source, target = other, here
	}
	t, err := linkTypeFor(ctx, tx, source, target, in)
	if err != nil {
		return nil, err
	}
	if err := refuseCycle(ctx, tx, t, source, target); err != nil {
		return nil, err
	}
	if t.Counts == CountsMitigation {
		var fits bool
		if err := tx.QueryRow(ctx, `SELECT requirement_is_risk($2) AND NOT requirement_is_risk($1)`, source.ID, target.ID).Scan(&fits); err != nil {
			return nil, err
		}
		if !fits {
			return nil, errNotMitigation
		}
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO trace_link (type_id, source_id, target_id, created_by)
		VALUES ($1, $2, $3, $4) RETURNING id`, t.ID, source.ID, target.ID, actor).Scan(&id)
	if err != nil {
		return nil, linkRefusal(err, here, other)
	}
	if err := requirement.EmitLink(ctx, tx, requirement.TopicLinkCreated, actor, id, source.ID, target.ID); err != nil {
		return nil, err
	}
	links, err := readLinks(ctx, tx, here.ID, &id)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, ErrLinkNotFound
	}
	return &links[0], nil
}

// errNotMitigation refuses a link that counts as mitigation between anything but a requirement and a risk.
var errNotMitigation = &FieldError{"typeId", "A mitigation joins a requirement that is no risk to the risk it mitigates. Link from such a requirement to a risk, or pick another link type."}

// linkEnd reads the far end of a new link, which must be another requirement the actor views, out of the trash.
func linkEnd(ctx context.Context, tx db.DBTX, actor uuid.UUID, here *requirement.Endpoint, key string) (*requirement.Endpoint, error) {
	other, err := requirement.FindEndpoint(ctx, tx, actor, key)
	switch {
	case errors.Is(err, requirement.ErrNotFound):
		return nil, &FieldError{"otherKey", fmt.Sprintf("There is no requirement %s you can see. Check the key.", key)}
	case err != nil:
		return nil, err
	case other.ID == here.ID:
		return nil, &FieldError{"otherKey", "A requirement cannot link to itself. Pick another."}
	case other.Kind == requirement.KindSection:
		return nil, &FieldError{"otherKey", fmt.Sprintf("%s is a section. Link a requirement in it instead.", other.Key)}
	case other.Trashed:
		return nil, &FieldError{"otherKey", fmt.Sprintf("%s is in the trash. Restore it first, or link another requirement.", other.Key)}
	}
	return other, nil
}

// linkTypeFor finds the type named among those the source's project may use, and holds the link to its rules.
func linkTypeFor(ctx context.Context, tx db.DBTX, source, target *requirement.Endpoint, in LinkInput) (*LinkType, error) {
	types, err := usable(ctx, tx, source.ProjectID)
	if err != nil {
		return nil, err
	}
	wanted := strings.ToLower(strings.TrimSpace(in.TypeKey))
	i := slices.IndexFunc(types, func(t LinkType) bool {
		return (in.TypeID != nil && t.ID == *in.TypeID) || (in.TypeID == nil && t.Key == wanted)
	})
	if i < 0 {
		return nil, &FieldError{"typeId", fmt.Sprintf("That link type is not one %s may use. Pick one of the types offered.", source.ProjectKey)}
	}
	t := &types[i]
	switch {
	case t.Retired:
		return nil, &FieldError{"typeId", fmt.Sprintf("The link type %s is retired and takes no new links. Pick another.", t.Name)}
	case !t.Enabled:
		return nil, &FieldError{"typeId", fmt.Sprintf("%s does not use the link type %s. Pick another, or ask an administrator of %s to enable it.",
			source.ProjectKey, t.Name, source.ProjectKey)}
	case !Allows(t.Rules, source.TypeID, target.TypeID, source.ProjectID == target.ProjectID):
		return nil, &FieldError{"typeId", fmt.Sprintf("In %s, %s links do not join %s to %s. Pick another link type, or ask an administrator of %s to allow it.",
			source.ProjectKey, t.Name, typeName(source), typeName(target), source.ProjectKey)}
	}
	return t, nil
}

func typeName(e *requirement.Endpoint) string {
	if e.TypeName == "" {
		return "an untyped requirement"
	}
	return "a " + e.TypeName
}

// refuseCycle walks an acyclic type's links the actor sees from the new link's target, and refuses when they
// reach its source, naming the way round; the trigger refuses the same over the links nobody here sees.
func refuseCycle(ctx context.Context, tx db.DBTX, t *LinkType, source, target *requirement.Endpoint) error {
	if !t.Acyclic {
		return nil
	}
	var path []uuid.UUID
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE downstream AS (
			SELECT l.target_id, ARRAY[l.target_id] AS path
			FROM trace_link l
			WHERE l.type_id = $1 AND l.deleted_at IS NULL AND l.source_id = $2
			UNION ALL
			SELECT l.target_id, d.path || l.target_id
			FROM trace_link l
			JOIN downstream d ON l.source_id = d.target_id
			WHERE l.type_id = $1 AND l.deleted_at IS NULL AND array_length(d.path, 1) < $4 AND NOT l.target_id = ANY (d.path)
		)
		SELECT path FROM downstream WHERE target_id = $3 ORDER BY array_length(path, 1) LIMIT 1`,
		t.ID, target.ID, source.ID, MaxCycleDepth).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("walk the %s links: %w", t.Key, err)
	}
	between, err := keysOf(ctx, tx, path[:len(path)-1])
	if err != nil {
		return err
	}
	return &FieldError{"otherKey", cycleSentence(*t, target.Key, source.Key, between)}
}

// keysOf names requirements by id, in the order given.
func keysOf(ctx context.Context, tx db.DBTX, ids []uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.key, r.number
		FROM unnest($1::uuid[]) WITH ORDINALITY AS wanted(id, position)
		JOIN requirement r ON r.id = wanted.id
		JOIN project p ON p.id = r.project_id
		ORDER BY wanted.position`, ids)
	if err != nil {
		return nil, err
	}
	var (
		keys   []string
		key    string
		number int
	)
	_, err = pgx.ForEachRow(rows, []any{&key, &number}, func() error { keys = append(keys, requirement.Key(key, number)); return nil })
	return keys, err
}

// linkRefusal says what the database refused of a new link in the sentence the service would have used.
func linkRefusal(err error, here, other *requirement.Endpoint) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "trace_link_repeat", "trace_link_live_idx":
		return &FieldError{"otherKey", "Those requirements are already linked that way."}
	case "trace_link_limit":
		return &FieldError{"otherKey", fmt.Sprintf("%s or %s already holds %d links, the most a requirement holds. Remove some first.",
			here.Key, other.Key, MaxLinksPerRequirement)}
	case "trace_link_cycle":
		return &FieldError{"otherKey", fmt.Sprintf("Linking %s and %s that way would close a circle through requirements you cannot see. Link another requirement.",
			here.Key, other.Key)}
	case "trace_link_ends", "trace_link_type":
		return &FieldError{"otherKey", "One of the requirements or the link type changed meanwhile. Reload and try again."}
	case "trace_link_mitigates_risk":
		return errNotMitigation
	}
	if pgErr.Code == "42501" {
		return requirement.ErrForbidden
	}
	return err
}

// Unlink removes a live link from either end, leaving its tombstone.
func (s *Service) Unlink(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			source, target     uuid.UUID
			editable, archived bool
		)
		err := tx.QueryRow(ctx, `
			SELECT l.source_id, l.target_id,
			       perm_requirement_editable(l.source_id, $2) OR perm_requirement_editable(l.target_id, $2),
			       sp.archived_at IS NOT NULL AND tp.archived_at IS NOT NULL
			FROM trace_link l
			JOIN requirement s ON s.id = l.source_id JOIN project sp ON sp.id = s.project_id
			JOIN requirement t ON t.id = l.target_id JOIN project tp ON tp.id = t.project_id
			WHERE l.id = $1 AND l.deleted_at IS NULL`, id, actor).Scan(&source, &target, &editable, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return err
		}
		// As clearing does: a link whose ends are all archived waits for its project, not for a right.
		switch {
		case !editable && archived:
			return project.ErrArchived
		case !editable:
			return requirement.ErrForbidden
		}
		if _, err := tx.Exec(ctx, `UPDATE trace_link SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, id, actor); err != nil {
			return fmt.Errorf("remove the link: %w", err)
		}
		return requirement.EmitLink(ctx, tx, requirement.TopicLinkRemoved, actor, id, source, target)
	})
}
