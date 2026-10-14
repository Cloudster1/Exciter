package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Topics a review writes to the outbox.
const (
	TopicRequested = "review.requested"
	TopicDecided   = "review.decided"
	TopicClosed    = "review.closed"
)

var (
	// ErrNotFound is also the answer for a review of a requirement the caller may not view.
	ErrNotFound = errors.New("review not found")
	// ErrRuleNotFound answers a rule that is not there, or of a project the caller may not view.
	ErrRuleNotFound = errors.New("approval rule not found")
	// ErrNotAdministrator refuses changing a project's rules to anybody who does not administer it.
	ErrNotAdministrator = errors.New("only the project's administrators set its approval rules")
	// ErrNoRule refuses a review of a requirement no rule covers.
	ErrNoRule = errors.New("no approval rule covers this requirement")
	// ErrPending refuses a second review while one waits.
	ErrPending = errors.New("a review of this requirement is pending")
	// ErrRevisionMovedOn refuses asking or deciding on a revision that is no longer the one in question.
	ErrRevisionMovedOn = errors.New("the requirement's revision moved on")
	// ErrSection refuses a review of a section, which says nothing to approve.
	ErrSection = errors.New("a section is not reviewed")
	// ErrClosed refuses acting on a review no longer pending.
	ErrClosed = errors.New("that review is no longer pending")
	// ErrNotApprover refuses a decision by somebody the review does not name.
	ErrNotApprover = errors.New("only an approver the review names decides it")
	// ErrOwnRevision refuses the author of the revision deciding on it when the rule does not allow it.
	ErrOwnRevision = errors.New("the author of the revision may not decide on it")
	// ErrNotRequester refuses withdrawing a review to anybody but whoever asked and the project's administrators.
	ErrNotRequester = errors.New("only whoever asked, or the project's administrators, withdraw a review")
	// ErrSignedOut refuses a caller nobody signed in as.
	ErrSignedOut = errors.New("reviews need somebody signed in")
)

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Service keeps approval rules and the reviews of requirement revisions; the database holds every promise it makes.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

func actorOf(ctx context.Context) (uuid.UUID, error) {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return uuid.Nil, ErrSignedOut
	}
	return actor, nil
}

func cleanComment(comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > MaxCommentLength {
		return "", &FieldError{Field: "comment", Message: fmt.Sprintf("Keep the comment to %d characters.", MaxCommentLength)}
	}
	return comment, nil
}

// subjects reads the approvers a table lists for each owner id, by the owner column named.
func subjects(ctx context.Context, tx db.DBTX, table, owner string, ids []uuid.UUID) (map[uuid.UUID][]Subject, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.`+owner+`, s.subject_type, s.user_id, s.group_id, COALESCE(u.name, g.name, '')
		FROM `+table+` s LEFT JOIN app_user u ON u.id = s.user_id LEFT JOIN groups g ON g.id = s.group_id
		WHERE s.`+owner+` = ANY($1)
		ORDER BY s.subject_type DESC, lower(COALESCE(u.name, g.name, '')), s.user_id, s.group_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the approvers: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]Subject{}
	for rows.Next() {
		var (
			id uuid.UUID
			s  Subject
		)
		if err := rows.Scan(&id, &s.Type, &s.UserID, &s.GroupID, &s.Name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], s)
	}
	return out, rows.Err()
}

const selectRules = `
SELECT a.id, a.type_id, COALESCE(t.name, ''), a.required, a.author_may_approve, a.updated_at
FROM approval_rule a LEFT JOIN requirement_type t ON t.id = a.type_id`

func readRules(ctx context.Context, tx db.DBTX, where string, args ...any) ([]Rule, error) {
	rows, err := tx.Query(ctx, selectRules+` WHERE `+where+` ORDER BY a.type_id IS NOT NULL, lower(COALESCE(t.name, '')), a.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the approval rules: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Rule, error) {
		var r Rule
		err := row.Scan(&r.ID, &r.TypeID, &r.TypeName, &r.Required, &r.AuthorMayApprove, &r.UpdatedAt)
		return r, err
	})
	if err != nil || len(out) == 0 {
		return []Rule{}, err
	}
	ids := make([]uuid.UUID, len(out))
	for i, r := range out {
		ids[i] = r.ID
	}
	named, err := subjects(ctx, tx, "approval_rule_subject", "rule_id", ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Approvers = named[out[i].ID]
		if out[i].Approvers == nil {
			out[i].Approvers = []Subject{}
		}
	}
	return out, nil
}

// Rules are who approves a project's requirements, the rule for every other type first.
func (s *Service) Rules(ctx context.Context, projectKey string) ([]Rule, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []Rule
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		out, err = readRules(ctx, tx, `a.project_id = $1`, p.ID)
		return err
	})
	return out, err
}

// validRule checks a rule's count and approvers, each a person who is a member or a group, named once.
func validRule(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in RuleInput) error {
	if in.Required < MinRequired || in.Required > MaxRequired {
		return &FieldError{Field: "required", Message: fmt.Sprintf("Ask for %d to %d approvals.", MinRequired, MaxRequired)}
	}
	if in.TypeID != nil {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement_type WHERE id = $1 AND project_id = $2)`,
			*in.TypeID, projectID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return &FieldError{Field: "typeId", Message: "That type is not one of this project's. Pick one from its types, or leave it out for every other type."}
		}
	}
	if len(in.Approvers) == 0 {
		return &FieldError{Field: "approvers", Message: "Name at least one person or group who approves."}
	}
	people := 0
	seen := map[string]bool{}
	for _, a := range in.Approvers {
		var (
			id    *uuid.UUID
			query string
		)
		switch a.Type {
		case SubjectUser:
			id, query = a.UserID, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL AND is_active)`
			people++
		case SubjectGroup:
			id, query = a.GroupID, `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1)`
		default:
			return &FieldError{Field: "approvers", Message: "An approver is a person or a group; everyone cannot approve."}
		}
		if id == nil {
			return &FieldError{Field: "approvers", Message: "Name each approver by the id of the person or the group."}
		}
		key := string(a.Type) + id.String()
		if seen[key] {
			return &FieldError{Field: "approvers", Message: "Name each approver once."}
		}
		seen[key] = true
		var found bool
		if err := tx.QueryRow(ctx, query, *id).Scan(&found); err != nil {
			return err
		}
		if !found {
			return &FieldError{Field: "approvers", Message: "One of the approvers is no member or group here. Pick them again from the list."}
		}
	}
	// A group may grow; people alone can never give more approvals than there are of them.
	if people == len(in.Approvers) && in.Required > people {
		return &FieldError{Field: "required", Message: fmt.Sprintf("Ask for at most %d approvals, one per person named, or name a group.", people)}
	}
	return nil
}

// SaveRule sets the rule of one type, or the project's rule for every other type; reviews already asked keep theirs.
func (s *Service) SaveRule(ctx context.Context, projectKey string, in RuleInput) (*Rule, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Rule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if !p.Can.Administer {
			return ErrNotAdministrator
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		if err := validRule(ctx, tx, p.ID, in); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO approval_rule (project_id, type_id, required, author_may_approve, updated_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (project_id, type_id) DO UPDATE
			SET required = EXCLUDED.required, author_may_approve = EXCLUDED.author_may_approve,
			    updated_by = EXCLUDED.updated_by, updated_at = now()
			RETURNING id`, p.ID, in.TypeID, in.Required, in.AuthorMayApprove, actor).Scan(&id); err != nil {
			return fmt.Errorf("save the approval rule: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM approval_rule_subject WHERE rule_id = $1`, id); err != nil {
			return err
		}
		for _, a := range in.Approvers {
			if _, err := tx.Exec(ctx, `INSERT INTO approval_rule_subject (rule_id, subject_type, user_id, group_id) VALUES ($1, $2, $3, $4)`,
				id, a.Type, a.UserID, a.GroupID); err != nil {
				return fmt.Errorf("name the approvers: %w", err)
			}
		}
		rules, err := readRules(ctx, tx, `a.id = $1`, id)
		if err != nil {
			return err
		}
		out = &rules[0]
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionApprovalRuleSaved, TargetType: "approval_rule", TargetID: &id, Actor: actor,
			Data: map[string]any{"projectKey": p.Key, "type": out.TypeName, "required": out.Required,
				"authorMayApprove": out.AuthorMayApprove, "approvers": len(out.Approvers)},
		})
	})
	return out, lsn, err
}

// DeleteRule removes an approval rule; reviews already asked keep their approvers.
func (s *Service) DeleteRule(ctx context.Context, ruleID uuid.UUID) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			projectID uuid.UUID
			typeName  string
		)
		err := tx.QueryRow(ctx, `
			SELECT a.project_id, COALESCE(t.name, '') FROM approval_rule a LEFT JOIN requirement_type t ON t.id = a.type_id
			WHERE a.id = $1`, ruleID).Scan(&projectID, &typeName)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRuleNotFound
		}
		if err != nil {
			return err
		}
		var administer, archived bool
		if err := tx.QueryRow(ctx, `SELECT perm_project_holds(id, $2, 'administer'), archived_at IS NOT NULL FROM project WHERE id = $1`,
			projectID, actor).Scan(&administer, &archived); err != nil {
			return err
		}
		switch {
		case !administer:
			return ErrNotAdministrator
		case archived:
			return project.ErrArchived
		}
		if _, err := tx.Exec(ctx, `DELETE FROM approval_rule WHERE id = $1`, ruleID); err != nil {
			return fmt.Errorf("remove the approval rule: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionApprovalRuleRemoved, TargetType: "approval_rule", TargetID: &ruleID, Actor: actor,
			Data: map[string]any{"projectId": projectID, "type": typeName},
		})
	})
}

// selectReviews reads reviews with their requirement as it is now, and what $1 may do to each.
const selectReviews = `
SELECT v.id, r.id, p.key, r.number, r.kind, r.title, r.revision, r.trashed_at IS NOT NULL, st.id, COALESCE(st.name, ''),
       COALESCE(st.category, ''), v.revision, v.state, v.required, review_approvals(v.id), v.comment, COALESCE(q.name, ''),
       v.requested_at, v.finished_at, COALESCE(a.name, ''), review_decidable(v.id, $1::uuid),
       v.state = 'pending' AND (v.requested_by = $1::uuid OR perm_project_holds(r.project_id, $1::uuid, 'administer'))
FROM requirement_review v
JOIN requirement r ON r.id = v.requirement_id
JOIN project p ON p.id = r.project_id
JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
LEFT JOIN req_status st ON st.id = r.status_id
LEFT JOIN app_user q ON q.id = v.requested_by
LEFT JOIN app_user a ON a.id = rv.created_by`

// reviews reads those matching the rest of the statement, $1 being the actor, with their approvers and decisions.
func reviews(ctx context.Context, tx db.DBTX, actor uuid.UUID, rest string, args ...any) ([]Review, error) {
	rows, err := tx.Query(ctx, selectReviews+rest, append([]any{actor}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("read the reviews: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Review, error) {
		var (
			v        Review
			number   int
			statusID *uuid.UUID
			status   requirement.StatusRef
		)
		ref := &v.Requirement
		err := row.Scan(&v.ID, &ref.ID, &ref.ProjectKey, &number, &ref.Kind, &ref.Title, &ref.Revision, &ref.Trashed, &statusID,
			&status.Name, &status.Category, &v.Revision, &v.State, &v.Required, &v.Approvals, &v.Comment, &v.RequestedBy,
			&v.RequestedAt, &v.FinishedAt, &v.AuthorName, &v.Can.Decide, &v.Can.Withdraw)
		ref.Key = requirement.Key(ref.ProjectKey, number)
		if statusID != nil {
			status.ID = *statusID
			ref.Status = &status
		}
		return v, err
	})
	if err != nil || len(out) == 0 {
		return []Review{}, err
	}
	ids := make([]uuid.UUID, len(out))
	at := map[uuid.UUID]int{}
	for i, v := range out {
		ids[i], at[v.ID] = v.ID, i
	}
	named, err := subjects(ctx, tx, "review_subject", "review_id", ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Approvers, out[i].Decisions = named[out[i].ID], []DecisionEntry{}
		if out[i].Approvers == nil {
			out[i].Approvers = []Subject{}
		}
	}
	rows, err = tx.Query(ctx, `
		SELECT d.review_id, d.reviewer_id, COALESCE(u.name, ''), d.decision, d.comment, d.decided_at
		FROM review_decision d LEFT JOIN app_user u ON u.id = d.reviewer_id
		WHERE d.review_id = ANY($1) AND d.reviewer_id IS NOT NULL ORDER BY d.decided_at, d.id`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the decisions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id uuid.UUID
			d  DecisionEntry
		)
		if err := rows.Scan(&id, &d.ReviewerID, &d.ReviewerName, &d.Decision, &d.Comment, &d.DecidedAt); err != nil {
			return nil, err
		}
		out[at[id]].Decisions = append(out[at[id]].Decisions, d)
	}
	return out, rows.Err()
}

func one(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*Review, error) {
	found, err := reviews(ctx, tx, actor, ` WHERE v.id = $2`, id)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return &found[0], nil
}

// Approval is a requirement's review now open and its latest revision approved.
func (s *Service) Approval(ctx context.Context, reqKey string) (Approval, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return Approval{}, err
	}
	var out Approval
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			SELECT (SELECT v.id FROM requirement_review v WHERE v.requirement_id = $1 AND v.state = 'pending'),
			       (SELECT max(v.revision) FROM requirement_review v WHERE v.requirement_id = $1 AND v.state = 'approved')`,
			r.ID).Scan(&out.Pending, &out.ApprovedRevision); err != nil {
			return err
		}
		out.Current = out.ApprovedRevision != nil && *out.ApprovedRevision == r.Revision
		return nil
	})
	return out, err
}

// List is a requirement's reviews, the newest first.
func (s *Service) List(ctx context.Context, reqKey string, limit, offset int) ([]Review, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var (
		out   []Review
		total int
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM requirement_review WHERE requirement_id = $1`, r.ID).Scan(&total); err != nil {
			return err
		}
		out, err = reviews(ctx, tx, actor, ` WHERE v.requirement_id = $2 ORDER BY v.requested_at DESC, v.id DESC LIMIT $3 OFFSET $4`,
			r.ID, limit, offset)
		return err
	})
	return out, total, err
}

// waiting is what waits for $1's decision: pending, theirs to decide, not decided by them yet.
const waiting = ` WHERE v.state = 'pending' AND review_decidable(v.id, $1::uuid)
  AND NOT EXISTS (SELECT 1 FROM review_decision d WHERE d.review_id = v.id AND d.reviewer_id = $1::uuid)`

// Waiting is the reviews waiting for the caller's decision across every project, the oldest first.
func (s *Service) Waiting(ctx context.Context, limit, offset int) ([]Review, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var (
		out   []Review
		total int
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM requirement_review v`+waiting, actor).Scan(&total); err != nil {
			return err
		}
		var err error
		out, err = reviews(ctx, tx, actor, waiting+` ORDER BY v.requested_at, v.id LIMIT $2 OFFSET $3`, limit, offset)
		return err
	})
	return out, total, err
}

// Get is one review with every approver's decision.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Review, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	var out *Review
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = one(ctx, tx, actor, id)
		return err
	})
	return out, err
}

// Request asks for the requirement's current revision to be approved, pinning it and freezing its rule's approvers.
func (s *Service) Request(ctx context.Context, reqKey string, in RequestInput) (*Review, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	var out *Review
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		var editable bool
		if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'edit')`, r.ProjectID, actor).Scan(&editable); err != nil {
			return err
		}
		switch {
		case r.Archived:
			return project.ErrArchived
		case !editable:
			return requirement.ErrForbidden
		case r.Trashed:
			return requirement.ErrTrashed
		case r.Kind == requirement.KindSection:
			return ErrSection
		case in.Revision != r.Revision:
			return ErrRevisionMovedOn
		}
		var (
			rule             *uuid.UUID
			required         int
			authorMayApprove bool
		)
		err = tx.QueryRow(ctx, `SELECT a.id, a.required, a.author_may_approve FROM approval_rule a WHERE a.id = approval_rule_of($1)`,
			r.ID).Scan(&rule, &required, &authorMayApprove)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoRule
		}
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO requirement_review (requirement_id, revision, requested_by, comment, required, author_may_approve)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, r.ID, r.Revision, actor, comment, required, authorMayApprove).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "requirement_review_pending_idx" {
			return ErrPending
		}
		if err != nil {
			return fmt.Errorf("ask for the review: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO review_subject (review_id, subject_type, user_id, group_id)
			SELECT $1, subject_type, user_id, group_id FROM approval_rule_subject WHERE rule_id = $2`, id, *rule); err != nil {
			return fmt.Errorf("freeze the approvers: %w", err)
		}
		if err := events.Emit(ctx, tx, TopicRequested, map[string]any{
			"actorId": actor, "reviewId": id, "requirementId": r.ID, "revision": r.Revision,
		}); err != nil {
			return err
		}
		out, err = one(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// held is a review as deciding or withdrawing it reads it, locked.
type held struct {
	requirement      uuid.UUID
	projectID        uuid.UUID
	revision         int
	state            State
	requestedBy      *uuid.UUID
	author           *uuid.UUID
	authorMayApprove bool
	required         int
}

func hold(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*held, error) {
	var h held
	err := tx.QueryRow(ctx, `
		SELECT v.requirement_id, r.project_id, v.revision, v.state, v.requested_by, rv.created_by, v.author_may_approve, v.required
		FROM requirement_review v
		JOIN requirement r ON r.id = v.requirement_id
		JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
		WHERE v.id = $1 AND perm_requirement_viewable(v.requirement_id, $2)
		FOR UPDATE OF v`, id, actor).Scan(&h.requirement, &h.projectID, &h.revision, &h.state, &h.requestedBy, &h.author,
		&h.authorMayApprove, &h.required)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &h, err
}

// Decide approves or rejects the revision under review as one of its approvers; one rejection closes the review,
// and so does the approval that reaches its count.
func (s *Service) Decide(ctx context.Context, id uuid.UUID, in DecisionInput) (*Review, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	if in.Decision != DecisionApprove && in.Decision != DecisionReject {
		return nil, 0, &FieldError{Field: "decision", Message: "Approve or reject the revision."}
	}
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	var out *Review
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, err := hold(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		var named bool
		if err := tx.QueryRow(ctx, `SELECT review_names($1, $2)`, id, actor).Scan(&named); err != nil {
			return err
		}
		switch {
		case h.state != StatePending:
			return ErrClosed
		case in.Revision != h.revision:
			return ErrRevisionMovedOn
		case !named:
			return ErrNotApprover
		case !h.authorMayApprove && h.author != nil && *h.author == actor:
			return ErrOwnRevision
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO review_decision (review_id, reviewer_id, decision, comment) VALUES ($1, $2, $3, $4)
			ON CONFLICT (review_id, reviewer_id) DO UPDATE
			SET decision = EXCLUDED.decision, comment = EXCLUDED.comment, decided_at = now()`,
			id, actor, in.Decision, comment); err != nil {
			return fmt.Errorf("record the decision: %w", err)
		}
		state := StatePending
		if in.Decision == DecisionReject {
			state = StateRejected
		} else {
			var approvals int
			if err := tx.QueryRow(ctx, `SELECT review_approvals($1)`, id).Scan(&approvals); err != nil {
				return err
			}
			if approvals >= h.required {
				state = StateApproved
			}
		}
		if state != StatePending {
			if _, err := tx.Exec(ctx, `UPDATE requirement_review SET state = $2, finished_at = now() WHERE id = $1`, id, state); err != nil {
				return fmt.Errorf("close the review: %w", err)
			}
		}
		if err := events.Emit(ctx, tx, TopicDecided, map[string]any{
			"actorId": actor, "reviewId": id, "requirementId": h.requirement, "decision": in.Decision, "state": state,
		}); err != nil {
			return err
		}
		out, err = one(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// Withdraw takes back a pending review, by whoever asked or an administrator of the project, whom the audit log keeps.
func (s *Service) Withdraw(ctx context.Context, id uuid.UUID) (*Review, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Review
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, err := hold(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		asked := h.requestedBy != nil && *h.requestedBy == actor
		var administer bool
		if err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer')`, h.projectID, actor).Scan(&administer); err != nil {
			return err
		}
		switch {
		case !asked && !administer:
			return ErrNotRequester
		case h.state != StatePending:
			return ErrClosed
		}
		if _, err := tx.Exec(ctx, `UPDATE requirement_review SET state = 'withdrawn', finished_at = now() WHERE id = $1`, id); err != nil {
			return fmt.Errorf("withdraw the review: %w", err)
		}
		if err := events.Emit(ctx, tx, TopicClosed, map[string]any{
			"actorId": actor, "reviewId": id, "requirementId": h.requirement, "state": StateWithdrawn,
		}); err != nil {
			return err
		}
		if !asked {
			if err := audit.Write(ctx, tx, audit.Entry{
				Action: audit.ActionReviewWithdrawn, TargetType: "review", TargetID: &id, Actor: actor,
				Data: map[string]any{"requirementId": h.requirement, "revision": h.revision, "requestedBy": h.requestedBy},
			}); err != nil {
				return err
			}
		}
		out, err = one(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}
