package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/rank"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// walkTree numbers a project's tree out of the trash: each node's place among its siblings, by rank, makes
// its path, and the path is the outline and the reading order. $1 is the project.
const walkTree = `
WITH RECURSIVE live AS (
	SELECT id, parent_id, row_number() OVER (PARTITION BY parent_id ORDER BY rank, number) AS pos
	FROM requirement WHERE project_id = $1 AND trashed_at IS NULL
), walk AS (
	SELECT id, ARRAY[pos]::int[] AS path FROM live WHERE parent_id IS NULL
	UNION ALL
	SELECT l.id, w.path || l.pos::int FROM live l JOIN walk w ON l.parent_id = w.id
)`

// outlineOf writes a path as the reader numbers it, such as 2.3.1.
func outlineOf(path []int32) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(int(p))
	}
	return strings.Join(parts, ".")
}

// outline answers one requirement's outline number, empty when it is in the trash.
func outline(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (string, error) {
	var path []int32
	err := tx.QueryRow(ctx, walkTree+` SELECT path FROM walk WHERE id = $2`, projectID, id).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return outlineOf(path), err
}

const selectNodes = walkTree + `
SELECT r.id, r.number, r.kind, r.title, r.parent_id, cardinality(w.path) - 1, w.path, r.type_id, r.revision,
       (SELECT count(*)::int FROM requirement c WHERE c.parent_id = r.id AND c.trashed_at IS NULL),
       r.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''), ` + QualityScore + `
FROM walk w JOIN requirement r ON r.id = w.id LEFT JOIN req_status st ON st.id = r.status_id` + QualityScores + `
ORDER BY w.path`

func scanNode(projectKey string) pgx.RowToFunc[TreeNode] {
	return func(row pgx.CollectableRow) (TreeNode, error) {
		var (
			n        TreeNode
			number   int
			path     []int32
			statusID *uuid.UUID
			status   StatusRef
		)
		err := row.Scan(&n.ID, &number, &n.Kind, &n.Title, &n.ParentID, &n.Depth, &path, &n.TypeID, &n.Revision, &n.Children,
			&statusID, &status.Name, &status.Category, &n.Quality)
		if statusID != nil {
			status.ID = *statusID
			n.Status = &status
		}
		n.Key = Key(projectKey, number)
		n.Outline = outlineOf(path)
		return n, err
	}
}

// Tree answers a project's whole tree out of the trash, in reading order, without content.
func (s *Service) Tree(ctx context.Context, reader uuid.UUID, projectKey string) ([]TreeNode, error) {
	out := []TreeNode{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectNodes, p.ID)
		if err != nil {
			return fmt.Errorf("read the tree: %w", err)
		}
		nodes, err := pgx.CollectRows(rows, scanNode(p.Key))
		if nodes != nil {
			out = nodes
		}
		return err
	})
	return out, err
}

// SpecFilter narrows the specification to the requirements in some statuses or status categories (#169).
type SpecFilter struct {
	Statuses   []uuid.UUID
	Categories []string
}

func (f SpecFilter) narrows() bool { return len(f.Statuses) > 0 || len(f.Categories) > 0 }

// checkFilter refuses a category there is none of, and a status the project does not have.
func checkFilter(ctx context.Context, tx db.DBTX, projectID uuid.UUID, f SpecFilter) error {
	for _, c := range f.Categories {
		if !slices.Contains(workflow.Categories, workflow.Category(c)) {
			return &FieldError{"category", "Choose draft, review, approved, done or closed as a status category."}
		}
	}
	if len(f.Statuses) == 0 {
		return nil
	}
	var known int
	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM req_status WHERE project_id = $1 AND id = ANY($2)`,
		projectID, f.Statuses).Scan(&known); err != nil {
		return err
	}
	asked := map[uuid.UUID]bool{}
	for _, id := range f.Statuses {
		asked[id] = true
	}
	if known != len(asked) {
		return &FieldError{"status", "One of those statuses is not this project's. Pick the statuses again from the filter."}
	}
	return nil
}

// Spec answers a window of a project's tree in reading order, with what each node says, narrowed and compared as
// the query asks; the summary of the changes is nil when it asks for none.
func (s *Service) Spec(ctx context.Context, reader uuid.UUID, projectKey string, q SpecQuery, limit, offset int) ([]SpecEntry, int, *SpecChanges, error) {
	if err := q.check(); err != nil {
		return nil, 0, nil, err
	}
	out := []SpecEntry{}
	total := 0
	var changes *SpecChanges
	q.reader = reader
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if err := checkFilter(ctx, tx, p.ID, q.Filter); err != nil {
			return err
		}
		if q.Changes == BaseBaseline {
			if err := checkBaseline(ctx, tx, p.ID, q.Baseline); err != nil {
				return err
			}
		}
		if q.Session != uuid.Nil {
			var mine bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM review_session WHERE id = $1 AND project_id = $2)`, q.Session, p.ID).Scan(&mine); err != nil {
				return err
			}
			if !mine {
				return &FieldError{"session", "That review session is not one of this project's. Open it from the project's sessions."}
			}
		}
		ctes, where, counted := specSQL(p.ID, q)
		if err := tx.QueryRow(ctx, walkTree+ctes+` SELECT count(*)::int FROM walk w`+where, counted.values...).Scan(&total); err != nil {
			return fmt.Errorf("count the specification: %w", err)
		}
		ctes, where, args := specSQL(p.ID, q)
		me := args.add(reader) + `::uuid`
		window := ` LIMIT ` + args.add(limit) + ` OFFSET ` + args.add(offset)
		rows, err := tx.Query(ctx, walkTree+ctes+`
			SELECT r.id, r.number, r.kind, r.title, r.parent_id, cardinality(w.path) - 1, w.path, r.type_id, r.revision,
			       (SELECT count(*)::int FROM requirement c WHERE c.parent_id = r.id AND c.trashed_at IS NULL),
			       r.statement, r.rationale, r.attributes,
			       ARRAY(SELECT l.name FROM requirement_label l WHERE l.requirement_id = r.id ORDER BY l.name),`+commentCounts+`,`+SuspectLinks+`,
			       COALESCE(t.name, ''), COALESCE(t.icon, ''), COALESCE(t.colour, ''), r.owner_id, COALESCE(o.name, ''), r.priority,
			       r.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''), c.kind, c.from_rev, c.moved_from,
			       pv.id, COALESCE(pv.revision, 0), COALESCE(pv.required, 0), COALESCE(review_approvals(pv.id), 0),
			       (SELECT count(*)::int FROM review_decision d WHERE d.review_id = pv.id AND d.decision = 'reject'),
			       COALESCE(review_names(pv.id, `+me+`), false), COALESCE(review_decidable(pv.id, `+me+`), false),
			       (SELECT d.decision FROM review_decision d WHERE d.review_id = pv.id AND d.reviewer_id = `+me+`), `+QualityScore+approval+reuseSQL+lockSQL+`,
			       COALESCE(t.role, 'requirement'), r.preconditions, r.steps, r.parameters,
			       CASE WHEN t.role = 'risk' AND r.kind = 'requirement' THEN r.risk END,
			       CASE WHEN t.role = 'risk' AND r.kind = 'requirement' THEN risk_view(r.project_id, r.risk, r.id) END
			FROM walk w JOIN requirement r ON r.id = w.id
			LEFT JOIN chg c ON c.id = r.id
			LEFT JOIN requirement_review pv ON pv.requirement_id = r.id AND pv.state = 'pending'
			LEFT JOIN requirement_type t ON t.id = r.type_id
			LEFT JOIN req_status st ON st.id = r.status_id
			LEFT JOIN app_user o ON o.id = r.owner_id`+reuseJoins+QualityScores+where+`
			ORDER BY w.path`+window, args.values...)
		if err != nil {
			return fmt.Errorf("read the specification: %w", err)
		}
		entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SpecEntry, error) {
			var (
				e         SpecEntry
				number    int
				path      []int32
				attrs     []byte
				reused    scannedReuse
				typ       TypeRef
				ownerID   *uuid.UUID
				ownerName string
				priority  *string
				statusID  *uuid.UUID
				status    StatusRef
				kind      *string
				fromRev   *int
				movedFrom []int32
				reviewID  *uuid.UUID
				review    SpecReview
				locked    scannedLock
				role      TypeRole
				pre       []byte
				steps     []byte
				params    []byte
				rating    []byte
				view      []byte
			)
			n := &e.Node
			err := row.Scan(append([]any{&n.ID, &number, &n.Kind, &n.Title, &n.ParentID, &n.Depth, &path, &n.TypeID, &n.Revision, &n.Children,
				&e.Statement, &e.Rationale, &attrs, &e.Labels, &e.Comments.Threads, &e.Comments.Open, &e.SuspectLinks,
				&typ.Name, &typ.Icon, &typ.Colour, &ownerID, &ownerName, &priority, &statusID, &status.Name, &status.Category,
				&kind, &fromRev, &movedFrom, &reviewID, &review.Revision, &review.Required, &review.Approvals, &review.Rejections,
				&review.Approver, &review.MayDecide, &review.Decision, &n.Quality, &e.Approval.Pending, &e.Approval.ApprovedRevision},
				append(append(reused.targets(), locked.targets()...), &role, &pre, &steps, &params, &rating, &view)...)...)
			if err != nil {
				return e, err
			}
			if role == RoleRisk && n.Kind == KindRequirement {
				r, err := scanRating(rating)
				if err != nil {
					return e, err
				}
				if e.Risk, err = riskView(r, view); err != nil {
					return e, err
				}
			}
			if role == RoleTestCase && n.Kind == KindRequirement {
				test, err := scanTestCase(pre, steps, params)
				if err != nil {
					return e, err
				}
				e.TestCase = &test
			}
			e.Reuse = reused.reuse()
			if n.TypeID != nil {
				typ.ID = *n.TypeID
				e.Type = &typ
			}
			if ownerID != nil {
				e.Owner = &PersonRef{ID: *ownerID, Name: ownerName}
			}
			if statusID != nil {
				status.ID = *statusID
				n.Status = &status
			}
			if reviewID != nil {
				review.ID = *reviewID
				e.Review = &review
			}
			if kind != nil && fromRev != nil {
				e.Change = &SpecChange{Kind: ChangeKind(*kind), From: *fromRev, To: n.Revision}
				if movedFrom != nil {
					outline := outlineOf(movedFrom)
					e.Change.MovedFrom = &outline
				}
			}
			if priority != nil {
				pr := Priority(*priority)
				e.Priority = &pr
			}
			e.Approval.Current = e.Approval.ApprovedRevision != nil && *e.Approval.ApprovedRevision == n.Revision
			n.Key = Key(p.Key, number)
			n.Outline = outlineOf(path)
			if len(e.Rationale) == 0 || string(e.Rationale) == "null" {
				e.Rationale = nil
			}
			if e.Labels == nil {
				e.Labels = []string{}
			}
			open := p.ArchivedAt == nil
			e.Lock = locked.lock(e.Node.Status)
			e.Can = Can{Edit: p.Can.Edit && open, Transition: (p.Can.Edit || p.Can.Approve) && open, Delete: p.Can.Edit && open,
				Link: p.Can.Edit && open, Comment: p.Can.Comment && open}.locked(e.Lock)
			return e, json.Unmarshal(attrs, &e.Attributes)
		})
		if err != nil {
			return err
		}
		if entries != nil {
			out = entries
		}
		if q.Changes == "" {
			return nil
		}
		if err := compareEntries(ctx, tx, p.ID, out); err != nil {
			return err
		}
		changes, err = summary(ctx, tx, p.ID, p.Key, q, offset)
		return err
	})
	return out, total, changes, err
}

// compareEntries fills in the comparison of every entry that differs from its base by a revision.
func compareEntries(ctx context.Context, tx db.DBTX, projectID uuid.UUID, entries []SpecEntry) error {
	defs, err := attribute.Defs(ctx, tx, projectID)
	if err != nil {
		return err
	}
	for i := range entries {
		c := entries[i].Change
		if c == nil || (c.Kind != ChangeChanged && c.Kind != ChangeAdded) {
			continue
		}
		if c.Comparison, err = comparison(ctx, tx, defs, entries[i].Node.ID, c.From, c.To); err != nil {
			return fmt.Errorf("compare %s: %w", entries[i].Node.Key, err)
		}
	}
	return nil
}

// placeError is the refusal of a place, on the field that named it.
func placeError(field, message string) error { return &FieldError{"place." + field, message} }

// noCycle is the sentence the tree guard's refusal is answered with.
const noCycle = "A requirement cannot go below itself or below one of the requirements under it."

// parentGone is the sentence a parent of another project, or in the trash, is refused with.
const parentGone = "That parent is not in this project, or it is in the trash. Pick another place."

// treeRefusal answers the database's refusal of a parent with the sentence the author reads.
func treeRefusal(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "requirement_tree_no_cycle", "requirement_parent_same_project":
		return placeError("parentId", noCycle)
	case "requirement_parent_live":
		// The parent went to the trash after the place was checked.
		return placeError("parentId", parentGone)
	}
	return err
}

// rankAt finds where a node goes: below parent, after the sibling after names (first when nil). moving is the
// node being moved, which is no sibling of itself and no parent of anything it lands under.
func rankAt(ctx context.Context, tx db.DBTX, projectID uuid.UUID, place Place, moving *uuid.UUID) (string, error) {
	if place.ParentID != nil {
		if moving != nil && *place.ParentID == *moving {
			return "", placeError("parentId", noCycle)
		}
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement WHERE project_id = $1 AND id = $2 AND trashed_at IS NULL)`,
			projectID, *place.ParentID).Scan(&live); err != nil {
			return "", err
		}
		if !live {
			return "", placeError("parentId", parentGone)
		}
	}
	siblings := `FROM requirement WHERE project_id = $1 AND parent_id IS NOT DISTINCT FROM $2
		AND trashed_at IS NULL AND ($3::uuid IS NULL OR id <> $3)`
	var before, after string
	if place.AfterID != nil {
		if moving != nil && *place.AfterID == *moving {
			return "", placeError("afterId", "A requirement cannot follow itself. Pick the sibling it goes after.")
		}
		var afterRank string
		var afterNumber int
		err := tx.QueryRow(ctx, `SELECT rank, number `+siblings+` AND id = $4`, projectID, place.ParentID, moving, *place.AfterID).Scan(&afterRank, &afterNumber)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", placeError("afterId", "The requirement it should follow is not below that parent. Pick the place again.")
		}
		if err != nil {
			return "", err
		}
		before = afterRank
		err = tx.QueryRow(ctx, `SELECT rank `+siblings+` AND (rank, number) > ($4, $5) ORDER BY rank, number LIMIT 1`,
			projectID, place.ParentID, moving, afterRank, afterNumber).Scan(&after)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	} else if err := tx.QueryRow(ctx, `SELECT rank `+siblings+` ORDER BY rank, number LIMIT 1`, projectID, place.ParentID, moving).Scan(&after); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// Two siblings of one rank, as rows written straight to the table may be, leave no room between them.
	if before != "" && before == after {
		after = ""
	}
	return rank.Between(before, after)
}

// lastAtTop is the rank after everything at the top of a project, where a node without a place goes.
func lastAtTop(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (string, error) {
	var last string
	// Ordering by parent_id as well lets the tree index hand the rows over last first, since IS NULL does not pin
	// the column for the planner; ordering by rank alone sorted every row of the project, each through its policy.
	err := tx.QueryRow(ctx, `SELECT rank FROM requirement WHERE project_id = $1 AND parent_id IS NULL
		AND trashed_at IS NULL ORDER BY parent_id DESC NULLS FIRST, rank DESC, number DESC LIMIT 1`, projectID).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return rank.Between(last, "")
}

// subtree lists a node and everything below it that is out of the trash, in reading order.
func subtree(ctx context.Context, tx db.DBTX, projectID, root uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE live AS (
			SELECT id, parent_id, row_number() OVER (PARTITION BY parent_id ORDER BY rank, number) AS pos
			FROM requirement WHERE project_id = $1 AND (trashed_at IS NULL OR id = $2)
		), below AS (
			SELECT id, ARRAY[0]::int[] AS path FROM live WHERE id = $2
			UNION ALL
			SELECT l.id, b.path || l.pos::int FROM live l JOIN below b ON l.parent_id = b.id
		)
		SELECT id FROM below ORDER BY path`, projectID, root)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// WalkTree is walkTree for the grid, which orders and numbers a project's requirements by it; $1 is the project.
const WalkTree = walkTree

// OutlineOf writes a path of WalkTree as the reader numbers it.
func OutlineOf(path []int32) string { return outlineOf(path) }

// Outline answers one requirement's outline number in its project, empty when it is in the trash.
func Outline(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (string, error) {
	return outline(ctx, tx, projectID, id)
}
