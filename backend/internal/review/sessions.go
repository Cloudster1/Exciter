package review

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// Review sessions (#204): one review of many requirements, in rounds, as docs/api-contract-m8.md agrees.

// Topics a session writes to the outbox, for those who take part.
const (
	TopicSessionOpened   = "review_session.opened"
	TopicSessionRound    = "review_session.round_started"
	TopicSessionClosed   = "review_session.closed"
	TopicSessionReminder = "review_session.reminder"
)

var (
	// ErrSessionNotFound is a session that is not there, or of a project the caller may not view.
	ErrSessionNotFound = errors.New("review session not found")
	// ErrNotEditor refuses opening a session to anybody who does not edit the project.
	ErrNotEditor = errors.New("only the project's editors open review sessions")
	// ErrNotModerator refuses changing a session to anybody but its moderator and the project's administrators.
	ErrNotModerator = errors.New("only the session's moderator changes it")
	// ErrSessionClosed refuses any change of a closed session.
	ErrSessionClosed = errors.New("the review session is closed")
	// ErrNothingRevised refuses a round when nothing was revised since its pin.
	ErrNothingRevised = errors.New("nothing was revised since the last round")
	// ErrTooManyRounds refuses a round past MaxSessionRounds.
	ErrTooManyRounds = errors.New("the session has had every round it takes")
)

// SessionLimits bound a page of sessions.
const (
	SessionDefaultLimit = 20
	SessionMaxLimit     = 100
)

func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > MaxSessionTitleLength {
		return "", &FieldError{Field: "title", Message: fmt.Sprintf("Name the session in at most %d characters, such as Release 2.0 review.", MaxSessionTitleLength)}
	}
	return title, nil
}

func checkDeadline(deadline *time.Time, now time.Time) error {
	if deadline == nil {
		return nil
	}
	if deadline.Before(now.Add(MinDeadline)) || deadline.After(now.Add(MaxDeadline)) {
		return &FieldError{Field: "deadline", Message: "Set the deadline at least an hour and at most a year ahead, or leave it out."}
	}
	return nil
}

// checkParticipants holds participants to members, and groups of members, who may do what their role asks in the
// project, each named once in one role, with at least one approver.
func checkParticipants(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in []Participant) error {
	if len(in) == 0 || len(in) > MaxSessionParticipants {
		return &FieldError{Field: "participants", Message: fmt.Sprintf("Name 1 to %d people or groups who take part.", MaxSessionParticipants)}
	}
	seen := map[string]bool{}
	approvers := 0
	for _, p := range in {
		if p.Role != RoleReviewer && p.Role != RoleApprover {
			return &FieldError{Field: "participants", Message: "Each participant reviews or approves."}
		}
		var (
			id    *uuid.UUID
			query string
		)
		switch p.Type {
		case SubjectUser:
			id, query = p.UserID, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL AND is_active)
				AND perm_project_holds($2, $1, CASE WHEN $3 THEN 'approve' ELSE 'comment' END)`
		case SubjectGroup:
			// The contract asks that everybody named can do what the session asks, and every member of an approver
			// group counts towards the quorum, so one member without the right is enough to refuse the group. Who is
			// in it may change later, which review_decidable answers when somebody decides. A member whose account
			// cannot be read is held to the right too, so a hidden row never lets a group through.
			id, query = p.GroupID, `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1) AND NOT EXISTS (
				SELECT 1 FROM group_member m
				WHERE m.group_id = $1
				  AND NOT EXISTS (SELECT 1 FROM app_user u WHERE u.id = m.user_id AND (u.role IS NULL OR NOT u.is_active))
				  AND NOT perm_project_holds($2, m.user_id, CASE WHEN $3 THEN 'approve' ELSE 'comment' END))`
		default:
			return &FieldError{Field: "participants", Message: "A participant is a person or a group."}
		}
		if id == nil {
			return &FieldError{Field: "participants", Message: "Name each participant by the id of the person or the group."}
		}
		key := string(p.Type) + id.String()
		if seen[key] {
			return &FieldError{Field: "participants", Message: "Name each participant once."}
		}
		seen[key] = true
		if p.Role == RoleApprover {
			approvers++
		}
		var ok bool
		if err := tx.QueryRow(ctx, query, *id, projectID, p.Role == RoleApprover).Scan(&ok); err != nil {
			return err
		}
		if !ok && p.Type == SubjectGroup {
			return &FieldError{Field: "participants", Message: "A group may not take part while one of its members may not: a reviewer comments in the project, and an approver approves in it. Pick another group, or ask an administrator to grant its members that right."}
		}
		if !ok {
			return &FieldError{Field: "participants", Message: "A participant may not take part: a reviewer comments in the project, and an approver approves in it. Pick them again."}
		}
	}
	if approvers == 0 {
		return &FieldError{Field: "participants", Message: "Name at least one person or group who approves."}
	}
	return nil
}

// scoped is a requirement a session's scope holds, with its key, revision and place.
type scoped struct {
	id       uuid.UUID
	key      string
	revision int
	path     []int32
}

// scope resolves what a session opens over, in outline order, and what of it the session leaves out and why.
func scope(ctx context.Context, tx db.DBTX, actor uuid.UUID, p *project.Project, sc Scope, since *Since) ([]scoped, []Excluded, error) {
	var (
		ids []uuid.UUID
		err error
	)
	switch sc.Kind {
	case ScopeSection:
		root, ferr := requirement.FindEndpoint(ctx, tx, actor, strings.TrimSpace(sc.RootKey))
		if ferr != nil || root.ProjectID != p.ID || root.Kind != requirement.KindSection || root.Trashed {
			return nil, nil, &FieldError{Field: "scope", Message: fmt.Sprintf("Pick a section of %s out of the trash.", p.Key)}
		}
		err = pgxIDs(ctx, tx, &ids, `
			WITH RECURSIVE sub AS (SELECT id FROM requirement WHERE id = $1
				UNION ALL SELECT c.id FROM requirement c JOIN sub s ON c.parent_id = s.id WHERE c.trashed_at IS NULL)
			SELECT id FROM sub`, root.ID)
	case ScopeBaseline:
		if sc.BaselineID == nil {
			return nil, nil, &FieldError{Field: "scope", Message: "Pick a baseline of the project."}
		}
		err = pgxIDs(ctx, tx, &ids, `
			SELECT i.requirement_id FROM baseline_item i JOIN baseline b ON b.id = i.baseline_id
			WHERE b.id = $1 AND b.project_id = $2`, *sc.BaselineID, p.ID)
		if err == nil && len(ids) == 0 {
			return nil, nil, &FieldError{Field: "scope", Message: "Pick a baseline of the project that holds requirements."}
		}
	case ScopeQuery:
		kept, kerr := search.Keep(ctx, tx, actor, p, search.Filter{Query: sc.Query}, search.Sort{})
		if kerr != nil {
			return nil, nil, kerr
		}
		for _, k := range kept {
			ids = append(ids, k.Ref.ID)
		}
	default:
		return nil, nil, &FieldError{Field: "scope", Message: "Review a section, a baseline or a query."}
	}
	if err != nil {
		return nil, nil, err
	}
	sinceBase, sinceBaseline := "", (*uuid.UUID)(nil)
	if since != nil {
		sinceBase, sinceBaseline = since.Base, since.BaselineID
		if sinceBase != "approved" && (sinceBase != "baseline" || sinceBaseline == nil) {
			return nil, nil, &FieldError{Field: "since", Message: "Keep what changed since the last approval, or since a baseline of the project."}
		}
	}
	rows, err := tx.Query(ctx, requirement.WalkTree+`
		SELECT r.id, r.number, r.revision, w.path,
		       EXISTS (SELECT 1 FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'pending'),
		       CASE $3
		           WHEN 'approved' THEN requirement_approved(r.id)
		           WHEN 'baseline' THEN EXISTS (SELECT 1 FROM baseline_item i JOIN baseline b ON b.id = i.baseline_id
		                                        WHERE b.id = $4 AND b.project_id = $1 AND i.requirement_id = r.id AND i.revision = r.revision)
		           ELSE false
		       END
		FROM walk w JOIN requirement r ON r.id = w.id
		WHERE r.kind = 'requirement' AND r.id = ANY($2)
		ORDER BY w.path`, p.ID, ids, sinceBase, sinceBaseline)
	if err != nil {
		return nil, nil, fmt.Errorf("read the scope: %w", err)
	}
	defer rows.Close()
	var (
		kept     []scoped
		excluded = []Excluded{}
	)
	for rows.Next() {
		var (
			s                  scoped
			number             int
			pending, unchanged bool
		)
		if err := rows.Scan(&s.id, &number, &s.revision, &s.path, &pending, &unchanged); err != nil {
			return nil, nil, err
		}
		s.key = requirement.Key(p.Key, number)
		switch {
		case unchanged:
		case pending:
			excluded = append(excluded, Excluded{Key: s.key, Reason: "review_pending"})
		default:
			kept = append(kept, s)
		}
	}
	return kept, excluded, rows.Err()
}

func pgxIDs(ctx context.Context, tx db.DBTX, out *[]uuid.UUID, sql string, args ...any) error {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	*out, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return err
}

// approversReached counts the people a session's approvers reach, groups by their members.
func approversReached(ctx context.Context, tx db.DBTX, in []Participant) (int, error) {
	var users, groups []uuid.UUID
	for _, p := range in {
		if p.Role != RoleApprover {
			continue
		}
		if p.Type == SubjectUser {
			users = append(users, *p.UserID)
		} else {
			groups = append(groups, *p.GroupID)
		}
	}
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT person) FROM (
			SELECT unnest($1::uuid[]) AS person UNION SELECT m.user_id FROM group_member m WHERE m.group_id = ANY($2)) people`,
		users, groups).Scan(&n)
	return n, err
}

// OpenSession opens a review session over a scope, pinning each requirement it holds and asking its review of the
// session's approvers, all in one transaction: either the session opens with every review, or nothing is written.
func (s *Service) OpenSession(ctx context.Context, projectKey string, in SessionInput) (*Session, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	title, err := checkTitle(in.Title)
	if err != nil {
		return nil, 0, err
	}
	description, err := cleanComment(in.Description)
	if err != nil {
		return nil, 0, &FieldError{Field: "description", Message: fmt.Sprintf("Keep the description to %d characters.", MaxCommentLength)}
	}
	if err := checkDeadline(in.Deadline, time.Now()); err != nil {
		return nil, 0, err
	}
	quorum := cmp.Or(in.Quorum, DefaultQuorum)
	if quorum < 1 || quorum > 100 {
		return nil, 0, &FieldError{Field: "quorum", Message: "Ask for 1 to 100 percent of the approvers."}
	}
	var out *Session
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		switch {
		case err != nil:
			return err
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Edit:
			return ErrNotEditor
		}
		if err := checkParticipants(ctx, tx, p.ID, in.Participants); err != nil {
			return err
		}
		if reached, err := approversReached(ctx, tx, in.Participants); err != nil {
			return err
		} else if reached > MaxSessionApprovers {
			return &FieldError{Field: "participants", Message: fmt.Sprintf("The approvers reach %d people; a session takes at most %d.", reached, MaxSessionApprovers)}
		}
		kept, excluded, err := scope(ctx, tx, actor, p, in.Scope, in.Since)
		if err != nil {
			return err
		}
		if len(kept) == 0 {
			return &FieldError{Field: "scope", Message: "Nothing in this scope needs a review. Widen it, or drop since."}
		}
		if len(kept) > MaxSessionItems {
			return &FieldError{Field: "scope", Message: fmt.Sprintf("The scope holds %d requirements; a session reviews at most %d. Narrow it.", len(kept), MaxSessionItems)}
		}
		var root *uuid.UUID
		if in.Scope.Kind == ScopeSection {
			r, err := requirement.FindEndpoint(ctx, tx, actor, strings.TrimSpace(in.Scope.RootKey))
			if err != nil {
				return err
			}
			root = &r.ID
		}
		var sinceBase *string
		var sinceBaseline *uuid.UUID
		if in.Since != nil {
			sinceBase, sinceBaseline = &in.Since.Base, in.Since.BaselineID
		}
		left, err := json.Marshal(excluded)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO review_session (project_id, title, description, scope_kind, scope_root, scope_baseline, scope_query,
			                            since_base, since_baseline, moderator_id, deadline, quorum, excluded)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
			p.ID, title, description, in.Scope.Kind, root, in.Scope.BaselineID, strings.TrimSpace(in.Scope.Query), sinceBase, sinceBaseline,
			actor, in.Deadline, quorum, left).Scan(&id); err != nil {
			return fmt.Errorf("open the session: %w", err)
		}
		for _, part := range in.Participants {
			if _, err := tx.Exec(ctx, `INSERT INTO review_session_participant (session_id, subject_type, user_id, group_id, role) VALUES ($1, $2, $3, $4, $5)`,
				id, part.Type, part.UserID, part.GroupID, part.Role); err != nil {
				return fmt.Errorf("name who takes part: %w", err)
			}
		}
		if err := pin(ctx, tx, actor, id, 1, "", kept); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionReviewSessionOpened, TargetType: "review_session", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": p.Key, "title": title, "items": len(kept), "excluded": len(excluded)}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicSessionOpened, map[string]any{"actorId": actor, "sessionId": id, "projectId": p.ID}); err != nil {
			return err
		}
		out, err = readSession(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// pin starts a round and pins each requirement in it.
func pin(ctx context.Context, tx db.DBTX, actor, session uuid.UUID, round int, comment string, items []scoped) error {
	if _, err := tx.Exec(ctx, `INSERT INTO review_session_round (session_id, number, comment, started_by) VALUES ($1, $2, $3, $4)`,
		session, round, comment, actor); err != nil {
		return fmt.Errorf("start round %d: %w", round, err)
	}
	for _, it := range items {
		if _, err := tx.Exec(ctx, `SELECT review_session_pin($1, $2)`, session, it.id); err != nil {
			return fmt.Errorf("pin %s: %w", it.key, err)
		}
	}
	return nil
}

// sessionRow is a session as its table holds it, with what the reader may do to it.
type sessionRow struct {
	Session
	projectID uuid.UUID
	moderated bool
	archived  bool
}

const selectSession = `
SELECT s.id, p.key, s.project_id, s.title, s.description, s.scope_kind, COALESCE(rt.number, 0), s.scope_baseline, s.scope_query,
       s.since_base, s.since_baseline, s.state, s.round, s.quorum, s.deadline, COALESCE(u.name, ''), s.baseline_id, s.excluded,
       s.opened_at, s.closed_at, review_session_moderated(s.id, $1::uuid), p.archived_at IS NOT NULL
FROM review_session s
JOIN project p ON p.id = s.project_id
LEFT JOIN requirement rt ON rt.id = s.scope_root
LEFT JOIN app_user u ON u.id = s.moderator_id`

func scanSession(row pgx.Row) (*sessionRow, error) {
	var (
		r          sessionRow
		rootNumber int
		sinceBase  *string
		sinceBL    *uuid.UUID
		excluded   []byte
	)
	s := &r.Session
	err := row.Scan(&s.ID, &s.ProjectKey, &r.projectID, &s.Title, &s.Description, &s.Scope.Kind, &rootNumber, &s.Scope.BaselineID, &s.Scope.Query,
		&sinceBase, &sinceBL, &s.State, &s.Round, &s.Quorum, &s.Deadline, &s.ModeratorName, &s.BaselineID, &excluded,
		&s.OpenedAt, &s.ClosedAt, &r.moderated, &r.archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if rootNumber > 0 {
		s.Scope.RootKey = requirement.Key(s.ProjectKey, rootNumber)
	}
	if sinceBase != nil {
		s.Since = &Since{Base: *sinceBase, BaselineID: sinceBL}
	}
	if err := json.Unmarshal(excluded, &s.Excluded); err != nil {
		return nil, fmt.Errorf("read what the session left out: %w", err)
	}
	open := s.State == SessionOpen && !r.archived
	s.Can = SessionCan{Edit: r.moderated && open, Round: r.moderated && open && s.Round < MaxSessionRounds, Close: r.moderated && open}
	return &r, nil
}

// pinned is one pin of a requirement in a round.
type pinned struct {
	requirementID, reviewID uuid.UUID
	round, revision         int
	outline                 string
}

// pins reads a session's pins in outline order, every round's when all, otherwise the latest of each requirement.
func pins(ctx context.Context, tx db.DBTX, r *sessionRow, all bool) ([]pinned, error) {
	latest := ` AND i.round = (SELECT max(j.round) FROM review_session_item j WHERE j.session_id = i.session_id AND j.requirement_id = i.requirement_id)`
	if all {
		latest = ""
	}
	rows, err := tx.Query(ctx, requirement.WalkTree+`
		SELECT i.requirement_id, i.review_id, i.round, i.revision, COALESCE(w.path, '{}')
		FROM review_session_item i LEFT JOIN walk w ON w.id = i.requirement_id
		WHERE i.session_id = $2`+latest+`
		ORDER BY w.path NULLS LAST, i.requirement_id, i.round`, r.projectID, r.ID)
	if err != nil {
		return nil, fmt.Errorf("read the session's requirements: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (pinned, error) {
		var (
			p    pinned
			path []int32
		)
		err := row.Scan(&p.requirementID, &p.reviewID, &p.round, &p.revision, &path)
		p.outline = requirement.OutlineOf(path)
		return p, err
	})
}

// readSession reads a session with its participants, its requirements' reviews as they stand, and each person's
// progress.
func readSession(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*Session, error) {
	r, err := scanSession(tx.QueryRow(ctx, selectSession+` WHERE s.id = $2`, actor, id))
	if err != nil {
		return nil, err
	}
	s := &r.Session
	if s.Participants, err = participants(ctx, tx, id); err != nil {
		return nil, err
	}
	current, err := pins(ctx, tx, r, false)
	if err != nil {
		return nil, err
	}
	reviewed, err := reviewsByID(ctx, tx, actor, current)
	if err != nil {
		return nil, err
	}
	s.Items = make([]SessionItem, 0, len(current))
	ids := make([]uuid.UUID, 0, len(current))
	for _, p := range current {
		v, ok := reviewed[p.reviewID]
		if !ok {
			continue
		}
		item := SessionItem{Requirement: v.Requirement, Outline: p.outline, Revision: p.revision, Round: p.round, Review: v}
		s.Items = append(s.Items, item)
		ids = append(ids, p.requirementID)
		switch v.State {
		case StateApproved:
			s.Counts.Approved++
		case StateRejected:
			s.Counts.Rejected++
		case StatePending:
			s.Counts.Pending++
		}
	}
	s.Counts.Total = len(s.Items)
	if err := openThreads(ctx, tx, s.Items, ids); err != nil {
		return nil, err
	}
	s.Progress, err = progress(ctx, tx, s, current)
	return s, err
}

func reviewsByID(ctx context.Context, tx db.DBTX, actor uuid.UUID, ps []pinned) (map[uuid.UUID]Review, error) {
	ids := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		ids[i] = p.reviewID
	}
	out := map[uuid.UUID]Review{}
	if len(ids) == 0 {
		return out, nil
	}
	all, err := reviews(ctx, tx, actor, ` WHERE v.id = ANY($2)`, ids)
	for _, v := range all {
		out[v.ID] = v
	}
	return out, err
}

func openThreads(ctx context.Context, tx db.DBTX, items []SessionItem, ids []uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT requirement_id, count(*)::int FROM comment_thread WHERE requirement_id = ANY($1) AND resolved_at IS NULL GROUP BY requirement_id`, ids)
	if err != nil {
		return err
	}
	open, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct {
		id uuid.UUID
		n  int
	}, error) {
		var v struct {
			id uuid.UUID
			n  int
		}
		err := row.Scan(&v.id, &v.n)
		return v, err
	})
	for _, o := range open {
		for i := range items {
			if items[i].Requirement.ID == o.id {
				items[i].OpenThreads = o.n
			}
		}
	}
	return err
}

func participants(ctx context.Context, tx db.DBTX, id uuid.UUID) ([]Participant, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.subject_type, p.user_id, p.group_id, COALESCE(u.name, g.name, ''), p.role
		FROM review_session_participant p LEFT JOIN app_user u ON u.id = p.user_id LEFT JOIN groups g ON g.id = p.group_id
		WHERE p.session_id = $1 ORDER BY p.role, p.subject_type DESC, lower(COALESCE(u.name, g.name, ''))`, id)
	if err != nil {
		return nil, fmt.Errorf("read who takes part: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Participant, error) {
		var p Participant
		err := row.Scan(&p.Type, &p.UserID, &p.GroupID, &p.Name, &p.Role)
		return p, err
	})
}

// progress is how far each person a session reaches got: an approver by the decisions they gave on what is pinned
// now, a reviewer by the requirements they commented on since the session opened.
func progress(ctx context.Context, tx db.DBTX, s *Session, current []pinned) ([]Progress, error) {
	reviewIDs := make([]uuid.UUID, len(current))
	reqIDs := make([]uuid.UUID, len(current))
	for i, p := range current {
		reviewIDs[i], reqIDs[i] = p.reviewID, p.requirementID
	}
	rows, err := tx.Query(ctx, `
		WITH people AS (
			SELECT p.user_id AS person, p.role FROM review_session_participant p WHERE p.session_id = $1 AND p.user_id IS NOT NULL
			UNION
			SELECT m.user_id, p.role FROM review_session_participant p JOIN group_member m ON m.group_id = p.group_id WHERE p.session_id = $1
		), roles AS (
			SELECT person, CASE WHEN bool_or(role = 'approver') THEN 'approver' ELSE 'reviewer' END AS role FROM people GROUP BY person
		)
		SELECT r.person, COALESCE(u.name, ''), r.role,
		       (SELECT count(*)::int FROM review_decision d WHERE d.review_id = ANY($2) AND d.reviewer_id = r.person),
		       (SELECT count(DISTINCT t.requirement_id)::int FROM comment c JOIN comment_thread t ON t.id = c.thread_id
		        WHERE t.requirement_id = ANY($3) AND c.author_id = r.person AND c.created_at >= $4)
		FROM roles r JOIN app_user u ON u.id = r.person
		ORDER BY r.role, lower(u.name)`, s.ID, reviewIDs, reqIDs, s.OpenedAt)
	if err != nil {
		return nil, fmt.Errorf("read the progress: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Progress, error) {
		p := Progress{Of: len(current)}
		err := row.Scan(&p.Person.ID, &p.Person.Name, &p.Role, &p.Decided, &p.Commented)
		return p, err
	})
}

// Session answers one review session.
func (s *Service) Session(ctx context.Context, id uuid.UUID) (*Session, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	var out *Session
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		out, err = readSession(ctx, tx, actor, id)
		return err
	})
	return out, err
}

// moderating reads a session for a change, refusing whoever does not moderate it, a session already closed and one
// of an archived project.
func moderating(ctx context.Context, tx db.DBTX, actor, id uuid.UUID) (*sessionRow, error) {
	r, err := scanSession(tx.QueryRow(ctx, selectSession+` WHERE s.id = $2`, actor, id))
	switch {
	case err != nil:
		return nil, err
	case !r.moderated:
		return nil, ErrNotModerator
	case r.State != SessionOpen:
		return nil, ErrSessionClosed
	case r.archived:
		return nil, project.ErrArchived
	}
	// Locked only now: a lock is held to the update policy, which would hide the session from whoever may not change it.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM review_session WHERE id = $1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	return r, nil
}

// pinState is a requirement of a session as a new round finds it.
type pinState struct {
	pinned
	now, number      int
	trashed, pending bool
}

// pinAgain is what a new round pins anew: each requirement revised since its pin, out of the trash and without a
// review of its own waiting. The rest keep the review and the decisions of the round that pinned them.
func pinAgain(states []pinState) []pinState {
	var out []pinState
	for _, st := range states {
		if st.now > st.revision && !st.trashed && !st.pending {
			out = append(out, st)
		}
	}
	return out
}

// UpdateSession changes what a session says and when it ends.
func (s *Service) UpdateSession(ctx context.Context, id uuid.UUID, in SessionUpdate) (*Session, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Session
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := moderating(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		title, description, deadline := r.Title, r.Description, r.Deadline
		if in.Title != nil {
			if title, err = checkTitle(*in.Title); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if description, err = cleanComment(*in.Description); err != nil {
				return &FieldError{Field: "description", Message: fmt.Sprintf("Keep the description to %d characters.", MaxCommentLength)}
			}
		}
		if in.Deadline != nil {
			if err := checkDeadline(in.Deadline, time.Now()); err != nil {
				return err
			}
			deadline = in.Deadline
		}
		if _, err := tx.Exec(ctx, `UPDATE review_session SET title = $2, description = $3, deadline = $4,
			reminded_at = CASE WHEN deadline IS DISTINCT FROM $4 THEN NULL ELSE reminded_at END WHERE id = $1`,
			id, title, description, deadline); err != nil {
			return fmt.Errorf("change the session: %w", err)
		}
		out, err = readSession(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// StartRound pins anew what was revised since its pin, asking a new review of each; the rest keep their decisions.
func (s *Service) StartRound(ctx context.Context, id uuid.UUID, in RoundInput) (*Session, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return nil, 0, err
	}
	var out *Session
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := moderating(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if r.Round >= MaxSessionRounds {
			return ErrTooManyRounds
		}
		current, err := pins(ctx, tx, r, false)
		if err != nil {
			return err
		}
		states := make([]pinState, 0, len(current))
		for _, p := range current {
			st := pinState{pinned: p}
			err := tx.QueryRow(ctx, `
				SELECT r.revision, r.number, r.trashed_at IS NOT NULL,
				       EXISTS (SELECT 1 FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'pending')
				FROM requirement r WHERE r.id = $1`, p.requirementID).Scan(&st.now, &st.number, &st.trashed, &st.pending)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			states = append(states, st)
		}
		var revised []scoped
		for _, st := range pinAgain(states) {
			revised = append(revised, scoped{id: st.requirementID, key: requirement.Key(r.ProjectKey, st.number), revision: st.now})
		}
		if len(revised) == 0 {
			return ErrNothingRevised
		}
		next := r.Round + 1
		if _, err := tx.Exec(ctx, `UPDATE review_session SET round = $2 WHERE id = $1`, id, next); err != nil {
			return fmt.Errorf("move the session on: %w", err)
		}
		if err := pin(ctx, tx, actor, id, next, comment, revised); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionReviewSessionRoundStarted, TargetType: "review_session", TargetID: &id,
			Actor: actor, Data: map[string]any{"project": r.ProjectKey, "round": next, "items": len(revised)}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicSessionRound, map[string]any{"actorId": actor, "sessionId": id, "projectId": r.projectID, "round": next}); err != nil {
			return err
		}
		out, err = readSession(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// CloseSession withdraws what is still pending and closes the session, taking a baseline of what it approved when
// one is named.
func (s *Service) CloseSession(ctx context.Context, id uuid.UUID, in CloseInput) (*Session, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Session
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := moderating(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		current, err := pins(ctx, tx, r, false)
		if err != nil {
			return err
		}
		reviewIDs := make([]uuid.UUID, len(current))
		for i, p := range current {
			reviewIDs[i] = p.reviewID
		}
		if _, err := tx.Exec(ctx, `UPDATE requirement_review SET state = 'withdrawn', finished_at = now()
			WHERE id = ANY($1) AND state = 'pending'`, reviewIDs); err != nil {
			return fmt.Errorf("withdraw what is still pending: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE review_session SET state = 'closed', closed_at = now() WHERE id = $1`, id); err != nil {
			return fmt.Errorf("close the session: %w", err)
		}
		var baselineID *uuid.UUID
		if in.Baseline != nil {
			approved, err := approvedNow(ctx, tx, reviewIDs)
			if err != nil {
				return err
			}
			made, err := baseline.TakeOfSession(ctx, tx, actor, r.projectID, id, in.Baseline.Name, in.Baseline.Description, approved)
			if err != nil {
				return err
			}
			baselineID = &made
			if _, err := tx.Exec(ctx, `UPDATE review_session SET baseline_id = $2 WHERE id = $1`, id, made); err != nil {
				return fmt.Errorf("keep the session's baseline: %w", err)
			}
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionReviewSessionClosed, TargetType: "review_session", TargetID: &id, Actor: actor,
			Data: map[string]any{"project": r.ProjectKey, "rounds": r.Round, "items": len(current), "baseline": baselineID}}); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicSessionClosed, map[string]any{"actorId": actor, "sessionId": id, "projectId": r.projectID}); err != nil {
			return err
		}
		out, err = readSession(ctx, tx, actor, id)
		return err
	})
	return out, lsn, err
}

// approvedNow is the requirements whose review the session approved at the revision they have now.
func approvedNow(ctx context.Context, tx db.DBTX, reviewIDs []uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := pgxIDs(ctx, tx, &out, `
		SELECT v.requirement_id FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id
		WHERE v.id = ANY($1) AND v.state = 'approved' AND v.revision = r.revision AND r.trashed_at IS NULL`, reviewIDs)
	return out, err
}

// Report answers a session as closed, or as it stands while open: every requirement's review in every round.
func (s *Service) Report(ctx context.Context, id uuid.UUID) (*Report, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	var out *Report
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		session, err := readSession(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out = &Report{Session: *session, Rounds: []RoundEntry{}, Items: []ReportItem{}}
		rows, err := tx.Query(ctx, `SELECT number, comment, started_at FROM review_session_round WHERE session_id = $1 ORDER BY number`, id)
		if err != nil {
			return err
		}
		if out.Rounds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (RoundEntry, error) {
			var e RoundEntry
			err := row.Scan(&e.Number, &e.Comment, &e.StartedAt)
			return e, err
		}); err != nil {
			return err
		}
		r := &sessionRow{Session: *session}
		if err := tx.QueryRow(ctx, `SELECT project_id FROM review_session WHERE id = $1`, id).Scan(&r.projectID); err != nil {
			return err
		}
		every, err := pins(ctx, tx, r, true)
		if err != nil {
			return err
		}
		reviewed, err := reviewsByID(ctx, tx, actor, every)
		if err != nil {
			return err
		}
		for _, p := range every {
			v, ok := reviewed[p.reviewID]
			if !ok {
				continue
			}
			at := slices.IndexFunc(out.Items, func(i ReportItem) bool { return i.Requirement.ID == p.requirementID })
			if at < 0 {
				out.Items = append(out.Items, ReportItem{Requirement: v.Requirement, Outline: p.outline, Rounds: []ReportRound{}})
				at = len(out.Items) - 1
			}
			out.Items[at].Rounds = append(out.Items[at].Rounds, ReportRound{Round: p.round, Revision: p.revision, State: v.State, Decisions: v.Decisions})
		}
		for i := range out.Items {
			for _, it := range session.Items {
				if it.Requirement.ID == out.Items[i].Requirement.ID {
					out.Items[i].OpenThreads = it.OpenThreads
				}
			}
		}
		if session.BaselineID != nil {
			if err := tx.QueryRow(ctx, `SELECT name FROM baseline WHERE id = $1`, *session.BaselineID).Scan(&out.BaselineName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	return out, err
}

// summaries reads sessions as a list shows them, $1 being the reader, with what is still the reader's to do.
const selectSummaries = `
SELECT s.id, p.key, s.title, s.state, s.round, s.deadline, COALESCE(u.name, ''), s.opened_at, s.closed_at,
       count(*) FILTER (WHERE v.state = 'approved')::int, count(*) FILTER (WHERE v.state = 'rejected')::int,
       count(*) FILTER (WHERE v.state = 'pending')::int, count(v.id)::int,
       count(*) FILTER (WHERE v.state = 'pending' AND review_decidable(v.id, $1::uuid)
                        AND NOT EXISTS (SELECT 1 FROM review_decision d WHERE d.review_id = v.id AND d.reviewer_id = $1::uuid))::int
FROM review_session s
JOIN project p ON p.id = s.project_id
LEFT JOIN app_user u ON u.id = s.moderator_id
LEFT JOIN review_session_item i ON i.session_id = s.id
     AND i.round = (SELECT max(j.round) FROM review_session_item j WHERE j.session_id = s.id AND j.requirement_id = i.requirement_id)
LEFT JOIN requirement_review v ON v.id = i.review_id`

func summaries(ctx context.Context, tx db.DBTX, actor uuid.UUID, where, order string, limit, offset int, args ...any) ([]SessionSummary, int, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM review_session s JOIN project p ON p.id = s.project_id WHERE $1::uuid IS NOT NULL AND `+where,
		append([]any{actor}, args...)...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count the sessions: %w", err)
	}
	n := len(args) + 1
	rows, err := tx.Query(ctx, selectSummaries+` WHERE `+where+` GROUP BY s.id, p.key, u.name ORDER BY `+order+
		fmt.Sprintf(` LIMIT $%d OFFSET $%d`, n+1, n+2), append(append([]any{actor}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("read the sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SessionSummary, error) {
		var v SessionSummary
		err := row.Scan(&v.ID, &v.ProjectKey, &v.Title, &v.State, &v.Round, &v.Deadline, &v.ModeratorName, &v.OpenedAt, &v.ClosedAt,
			&v.Counts.Approved, &v.Counts.Rejected, &v.Counts.Pending, &v.Counts.Total, &v.Waiting)
		return v, err
	})
	if out == nil {
		out = []SessionSummary{}
	}
	return out, total, err
}

// Sessions answers a project's sessions, open first, then newest first; state keeps one of them.
func (s *Service) Sessions(ctx context.Context, projectKey string, state SessionState, limit, offset int) ([]SessionSummary, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	if state != "" && state != SessionOpen && state != SessionClosed {
		return nil, 0, &FieldError{Field: "state", Message: "Keep the open sessions or the closed ones."}
	}
	var (
		out   []SessionSummary
		total int
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		out, total, err = summaries(ctx, tx, actor, `s.project_id = $2 AND ($3::text = '' OR s.state = $3::text)`,
			`s.state = 'open' DESC, s.opened_at DESC, s.id DESC`, limit, offset, p.ID, string(state))
		return err
	})
	return out, total, err
}

// MySessions answers the open sessions the caller takes part in, across projects, nearest deadline first.
func (s *Service) MySessions(ctx context.Context, limit, offset int) ([]SessionSummary, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var (
		out   []SessionSummary
		total int
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		out, total, err = summaries(ctx, tx, actor, `s.state = 'open' AND review_session_takes_part(s.id, $1::uuid, NULL)`,
			`s.deadline NULLS LAST, s.opened_at, s.id`, limit, offset)
		return err
	})
	return out, total, err
}
