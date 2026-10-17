package requirement

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// ChangeBase is what the specification's entries are compared with (#170).
type ChangeBase string

const (
	// BasePrevious compares each requirement with its revision before the latest.
	BasePrevious ChangeBase = "previous"
	// BaseApproved compares each with the latest revision approved.
	BaseApproved ChangeBase = "approved"
	// BaseBaseline compares each with the revision and place a baseline holds.
	BaseBaseline ChangeBase = "baseline"
)

// ChangeBases lists every ChangeBase, for the API document.
var ChangeBases = []ChangeBase{BasePrevious, BaseApproved, BaseBaseline}

// ChangeKind is how a requirement differs from its base.
type ChangeKind string

const (
	ChangeChanged   ChangeKind = "changed"
	ChangeUnchanged ChangeKind = "unchanged"
	// ChangeAdded has no earlier side: its first revision, never approved, or not in the baseline.
	ChangeAdded ChangeKind = "added"
	// ChangeMoved is the baseline's revision in another place.
	ChangeMoved ChangeKind = "moved"
)

// ChangeKinds lists every ChangeKind, for the API document.
var ChangeKinds = []ChangeKind{ChangeChanged, ChangeUnchanged, ChangeAdded, ChangeMoved}

// SpecChange is how one requirement of the specification differs from the base asked for.
type SpecChange struct {
	Kind ChangeKind `json:"kind"`
	// From and To are the revisions compared; From is 0 for an added one.
	From int `json:"from"`
	To   int `json:"to"`
	// MovedFrom is its outline in the baseline when it stood elsewhere there, otherwise null.
	MovedFrom *string `json:"movedFrom"`
	// Comparison is what the two revisions differ in, null when they are the same revision.
	Comparison *Comparison `json:"comparison"`
}

// SpecRemoved is a requirement a baseline held that the document no longer has, as the baseline held it.
type SpecRemoved struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Outline  string `json:"outline"`
	Revision int    `json:"revision"`
}

// SpecChanges sums up the changes of the whole document against its base.
type SpecChanges struct {
	Base       ChangeBase `json:"base"`
	BaselineID *uuid.UUID `json:"baselineId"`
	// Changed counts the changed, added and moved requirements of the whole document.
	Changed int `json:"changed"`
	// Removed is the baseline's requirements no longer in the document, on the window at offset 0 only.
	Removed []SpecRemoved `json:"removed"`
}

// SpecQuery is what a read of the specification is narrowed to and compared with.
type SpecQuery struct {
	Filter SpecFilter
	// Changes is the base to compare with, empty for none.
	Changes  ChangeBase
	Baseline uuid.UUID
	// OnlyChanged keeps the requirements that differ from their base, and the sections above them.
	OnlyChanged bool
}

// check refuses a base there is none of, a baseline left out, and only changed without a base.
func (q SpecQuery) check() error {
	switch q.Changes {
	case "":
		if q.OnlyChanged {
			return &FieldError{"onlyChanged", "Choose what to compare with before keeping only what changed."}
		}
	case BasePrevious, BaseApproved:
	case BaseBaseline:
		if q.Baseline == uuid.Nil {
			return &FieldError{"baseline", "Choose the baseline to compare with."}
		}
	default:
		return &FieldError{"changes", "Compare with the previous revision, the last approval or a baseline."}
	}
	return nil
}

// params numbers a query's parameters in the order its text asks for them, so none goes unused.
type params struct{ values []any }

func (p *params) add(v any) string {
	p.values = append(p.values, v)
	return "$" + strconv.Itoa(len(p.values))
}

// changesCTE follows walkTree: each requirement of the project ($1) with the revision it is compared from, its kind
// and, against a baseline, where it stood when it was moved since. Empty without a base.
func changesCTE(q SpecQuery, p *params) string {
	live := `FROM requirement r %s WHERE r.project_id = $1 AND r.trashed_at IS NULL AND r.kind = 'requirement'`
	switch q.Changes {
	case BasePrevious:
		return `, chg AS (SELECT r.id, r.revision - 1 AS from_rev,
	       CASE WHEN r.revision = 1 THEN 'added' ELSE 'changed' END AS kind, NULL::int[] AS moved_from
	` + fmt.Sprintf(live, "") + `)`
	case BaseApproved:
		return `, chg AS (SELECT r.id, COALESCE(a.rev, 0) AS from_rev,
	       CASE WHEN a.rev IS NULL THEN 'added' WHEN a.rev = r.revision THEN 'unchanged' ELSE 'changed' END AS kind,
	       NULL::int[] AS moved_from
	` + fmt.Sprintf(live, `LEFT JOIN LATERAL (SELECT max(v.revision) AS rev FROM requirement_review v
	       WHERE v.requirement_id = r.id AND v.state = 'approved') a ON true`) + `)`
	case BaseBaseline:
		moved := `(bi.parent_id IS DISTINCT FROM r.parent_id OR bi.rank <> r.rank)`
		return `, chg AS (SELECT r.id, COALESCE(bi.revision, 0) AS from_rev,
	       CASE WHEN bi.requirement_id IS NULL THEN 'added' WHEN bi.revision <> r.revision THEN 'changed'
	            WHEN ` + moved + ` THEN 'moved' ELSE 'unchanged' END AS kind,
	       CASE WHEN ` + moved + ` THEN bi.path END AS moved_from
	` + fmt.Sprintf(live, `LEFT JOIN baseline_item bi ON bi.baseline_id = `+p.add(q.Baseline)+`::uuid AND bi.requirement_id = r.id`) + `)`
	}
	return `, chg AS (SELECT NULL::uuid AS id, 0 AS from_rev, ''::text AS kind, NULL::int[] AS moved_from WHERE false)`
}

// specSQL is the common text of a specification read after walkTree: the changes, and what the filter and only
// changed keep, the requirements and the sections above them so the outline reads.
func specSQL(projectID uuid.UUID, q SpecQuery) (ctes, where string, p *params) {
	p = &params{}
	p.add(projectID)
	ctes = changesCTE(q, p)
	byStatus, onlyChanged := q.Filter.narrows(), q.OnlyChanged && q.Changes != ""
	if !byStatus && !onlyChanged {
		return ctes, "", p
	}
	statuses, categories := q.Filter.Statuses, q.Filter.Categories
	if statuses == nil {
		statuses = []uuid.UUID{}
	}
	if categories == nil {
		categories = []string{}
	}
	keep := ""
	if byStatus {
		keep += ` AND (r.status_id = ANY(` + p.add(statuses) + `::uuid[]) OR st.category = ANY(` + p.add(categories) + `::text[]))`
	}
	if onlyChanged {
		keep += ` AND c.kind <> 'unchanged'`
	}
	ctes += `, kept AS (
	SELECT r.id FROM requirement r LEFT JOIN req_status st ON st.id = r.status_id LEFT JOIN chg c ON c.id = r.id
	WHERE r.project_id = $1 AND r.trashed_at IS NULL AND r.kind = 'requirement'` + keep + `
), above AS (
	SELECT r.parent_id AS id FROM requirement r JOIN kept k ON k.id = r.id WHERE r.parent_id IS NOT NULL
	UNION
	SELECT r.parent_id FROM requirement r JOIN above a ON a.id = r.id WHERE r.parent_id IS NOT NULL
)`
	where = ` WHERE (w.id IN (SELECT id FROM kept)
	       OR w.id IN (SELECT a.id FROM above a JOIN requirement sc ON sc.id = a.id WHERE sc.kind = 'section'))`
	return ctes, where, p
}

// checkBaseline refuses a baseline that is not of the project, or one the reader may not see.
func checkBaseline(ctx context.Context, tx db.DBTX, projectID, baselineID uuid.UUID) error {
	var of uuid.UUID
	err := tx.QueryRow(ctx, `SELECT project_id FROM baseline WHERE id = $1`, baselineID).Scan(&of)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && of != projectID) {
		return &FieldError{"baseline", "That baseline is not one of this project's. Pick a baseline from the list."}
	}
	return err
}

// summary counts the document's changes against its base and, at offset 0 against a baseline, lists what it lost.
func summary(ctx context.Context, tx db.DBTX, projectID uuid.UUID, projectKey string, q SpecQuery, offset int) (*SpecChanges, error) {
	out := &SpecChanges{Base: q.Changes, Removed: []SpecRemoved{}}
	if q.Changes == BaseBaseline {
		id := q.Baseline
		out.BaselineID = &id
	}
	ctes, _, p := specSQL(projectID, SpecQuery{Changes: q.Changes, Baseline: q.Baseline})
	if err := tx.QueryRow(ctx, walkTree+ctes+` SELECT count(*)::int FROM chg WHERE kind <> 'unchanged'`, p.values...).Scan(&out.Changed); err != nil {
		return nil, fmt.Errorf("count the changes: %w", err)
	}
	if q.Changes != BaseBaseline || offset > 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT i.number, COALESCE(v.title, ''), i.path, i.revision
		FROM baseline_item i
		LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
		WHERE i.baseline_id = $1 AND i.kind = 'requirement'
		  AND NOT EXISTS (SELECT 1 FROM requirement r WHERE r.id = i.requirement_id AND r.project_id = $2 AND r.trashed_at IS NULL)
		ORDER BY i.path`, q.Baseline, projectID)
	if err != nil {
		return nil, fmt.Errorf("read what the baseline held that is gone: %w", err)
	}
	removed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SpecRemoved, error) {
		var (
			r      SpecRemoved
			number int
			path   []int32
		)
		err := row.Scan(&number, &r.Title, &path, &r.Revision)
		r.Key, r.Outline = Key(projectKey, number), outlineOf(path)
		return r, err
	})
	if err != nil {
		return nil, err
	}
	out.Removed = append(out.Removed, removed...)
	return out, nil
}
