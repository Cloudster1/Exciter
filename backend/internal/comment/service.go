package comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/markdown"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// The discussion on requirements (#33), Stator's comment service over requirements: whether the actor may read or
// write a comment is decided on its requirement, and the database's policies hold it to the same.

// Topics a comment writes to the outbox.
const (
	TopicCreated  = "comment.created"
	TopicEdited   = "comment.edited"
	TopicResolved = "thread.resolved"
	TopicReopened = "thread.reopened"
)

var (
	// ErrNotFound is also the answer for a comment on a requirement the caller may not view, and for one whose
	// whole thread is deleted.
	ErrNotFound = errors.New("comment not found")
	// ErrNotYours refuses rewriting somebody else's comment, or deleting it without administering the project.
	ErrNotYours = errors.New("that comment is somebody else's")
	// ErrMayNotComment refuses a reader who holds no comment right in the project.
	ErrMayNotComment = errors.New("the caller may read the requirement but not comment on it")
)

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Service keeps the comments on requirements.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// onRequirement is what a comment needs to know of its requirement for one actor.
type onRequirement struct {
	*requirement.Endpoint
	comment, moderate bool
}

func loadRequirement(ctx context.Context, tx db.DBTX, actor uuid.UUID, key string) (*onRequirement, error) {
	r, err := requirement.FindEndpoint(ctx, tx, actor, key)
	if err != nil {
		return nil, err
	}
	return withRights(ctx, tx, actor, r)
}

func withRights(ctx context.Context, tx db.DBTX, actor uuid.UUID, r *requirement.Endpoint) (*onRequirement, error) {
	out := onRequirement{Endpoint: r}
	err := tx.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'comment'), perm_project_holds($1, $2, 'administer')`,
		r.ProjectID, actor).Scan(&out.comment, &out.moderate)
	return &out, err
}

// may is whether the actor may write on the requirement now.
func (r *onRequirement) may() bool { return r.comment && !r.Trashed && !r.Archived }

// canComment says why the actor may not write on the requirement, or nil.
func (r *onRequirement) canComment() error {
	switch {
	case r.Archived:
		return project.ErrArchived
	case r.Trashed:
		return requirement.ErrTrashed
	case !r.comment:
		return ErrMayNotComment
	}
	return nil
}

func actorOf(ctx context.Context) (uuid.UUID, error) {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return uuid.Nil, ErrMayNotComment
	}
	return actor, nil
}

// body is what a comment says, sent as a document or as Markdown; a refusal of Markdown names its field.
func (s *Service) body(ctx context.Context, in Input) (json.RawMessage, error) {
	if in.BodyMarkdown == nil {
		return cleanBody(in.Body)
	}
	if in.Body != nil {
		return nil, &FieldError{Field: "bodyMarkdown", Message: "Send the comment as a document or as Markdown, not both."}
	}
	var root document.Node
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		root, err = markdown.Convert(*in.BodyMarkdown, markdown.Resolve{Person: markdown.Members(ctx, tx)})
		return err
	})
	var refusal *markdown.Refusal
	if errors.As(err, &refusal) {
		return nil, &FieldError{Field: "bodyMarkdown", Message: refusal.Message}
	}
	if err != nil {
		return nil, err
	}
	raw, err := cleanBody(root)
	var invalid *FieldError
	if errors.As(err, &invalid) {
		invalid.Field = "bodyMarkdown"
	}
	return raw, err
}

// WithMarkdown adds each comment's body as Markdown.
func (t *Thread) WithMarkdown() {
	for i := range t.Comments {
		t.Comments[i].WithMarkdown()
	}
}

// WithMarkdown adds the body as Markdown.
func (c *Comment) WithMarkdown() {
	if raw, err := json.Marshal(c.Body); err == nil {
		c.BodyMarkdown = markdown.Of(raw)
	}
}

// cleanBody holds a body to the comment allowlist and to saying something.
func cleanBody(body any) (json.RawMessage, error) {
	empty := &FieldError{Field: "body", Message: "Write something before you post the comment."}
	raw, err := json.Marshal(body)
	if err != nil || body == nil {
		return nil, empty
	}
	root, err := document.ParseComment(raw)
	if err != nil {
		return nil, &FieldError{Field: "body", Message: err.Error()}
	}
	if strings.TrimSpace(document.PlainText(root)) == "" {
		return nil, empty
	}
	return raw, nil
}

var selectComments = `
SELECT t.id, t.requirement_id, t.resolved_at, COALESCE(r.name, ''),
       c.id, c.author_id, COALESCE(u.name, ''), c.revision, c.body, c.deleted_at IS NOT NULL, c.created_at, c.edited_at, ` + via.Column("c") + `
FROM comment_thread t
JOIN comment c ON c.thread_id = t.id
LEFT JOIN app_user u ON u.id = c.author_id
LEFT JOIN app_user r ON r.id = t.resolved_by`

// threads reads threads in order, leaving out those wholly deleted, and says what the actor may do to each.
func threads(ctx context.Context, tx db.DBTX, actor uuid.UUID, r *onRequirement, where string, args ...any) ([]Thread, error) {
	rows, err := tx.Query(ctx, selectComments+` WHERE `+where+` ORDER BY t.created_at, t.id, c.created_at, c.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the comments: %w", err)
	}
	defer rows.Close()
	out := []Thread{}
	for rows.Next() {
		var (
			threadID, requirementID uuid.UUID
			resolvedAt              *time.Time
			resolvedBy              string
			c                       Comment
			body                    []byte
		)
		if err := rows.Scan(&threadID, &requirementID, &resolvedAt, &resolvedBy,
			&c.ID, &c.AuthorID, &c.AuthorName, &c.Revision, &body, &c.Deleted, &c.CreatedAt, &c.EditedAt, &c.Via); err != nil {
			return nil, err
		}
		c.ThreadID = threadID
		if body != nil {
			c.Body = json.RawMessage(body)
		}
		mine := c.AuthorID != nil && *c.AuthorID == actor
		c.Can = Can{Edit: !c.Deleted && mine && r.may(), Delete: !c.Deleted && (mine || r.moderate)}
		if n := len(out); n == 0 || out[n-1].ID != threadID {
			out = append(out, Thread{
				ID: threadID, RequirementID: requirementID, Comments: []Comment{},
				Resolved: resolvedAt != nil, ResolvedAt: resolvedAt, ResolvedBy: resolvedBy,
				Can: ThreadCan{Reply: r.may(), Resolve: r.may()},
			})
		}
		last := &out[len(out)-1]
		last.Comments = append(last.Comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	live := out[:0]
	for _, t := range out {
		for _, c := range t.Comments {
			if !c.Deleted {
				live = append(live, t)
				break
			}
		}
	}
	return live, nil
}

// List is a requirement's threads, oldest first.
func (s *Service) List(ctx context.Context, reqKey string) ([]Thread, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []Thread
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := loadRequirement(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		out, err = threads(ctx, tx, actor, r, `t.requirement_id = $1`, r.ID)
		return err
	})
	return out, err
}

// held is one comment's place, read before acting on it.
type held struct {
	thread, requirement uuid.UUID
	author              *uuid.UUID
	deleted             bool
}

// locate finds a comment and its requirement as the actor sees them; a comment on a requirement they may not view
// is not found. lock takes the comment for writing.
func locate(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, lock bool) (*held, *onRequirement, error) {
	sql := `SELECT thread_id, requirement_id, author_id, deleted_at IS NOT NULL FROM comment WHERE id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	var h held
	err := tx.QueryRow(ctx, sql, id).Scan(&h.thread, &h.requirement, &h.author, &h.deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	r, err := requirement.FindEndpointByID(ctx, tx, actor, h.requirement)
	if errors.Is(err, requirement.ErrNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	on, err := withRights(ctx, tx, actor, r)
	return &h, on, err
}

// thread reads one thread, refusing one wholly deleted.
func thread(ctx context.Context, tx db.DBTX, actor uuid.UUID, r *onRequirement, id uuid.UUID) (*Thread, error) {
	found, err := threads(ctx, tx, actor, r, `t.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return &found[0], nil
}

// Get is the whole thread a comment belongs to.
func (s *Service) Get(ctx context.Context, commentID uuid.UUID) (*Thread, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	var out *Thread
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, r, err := locate(ctx, tx, actor, commentID, false)
		if err != nil {
			return err
		}
		out, err = thread(ctx, tx, actor, r, h.thread)
		return err
	})
	return out, err
}

// add writes a comment in the actor's name and tells the outbox whom it mentions.
func add(ctx context.Context, tx db.DBTX, actor, id, threadID, requirementID uuid.UUID, body json.RawMessage, reply bool) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO comment (id, thread_id, requirement_id, revision, author_id, body)
		VALUES ($1, $2, $3, 0, $4, $5)`, id, threadID, requirementID, actor, []byte(body)); err != nil {
		return fmt.Errorf("add the comment: %w", err)
	}
	mentioned, err := toTell(ctx, tx, requirementID, nil, body)
	if err != nil {
		return err
	}
	return events.Emit(ctx, tx, TopicCreated, map[string]any{
		"commentId": id, "threadId": threadID, "requirementId": requirementID, "actorId": actor, "reply": reply, "mentioned": mentioned,
	})
}

// toTell are the people body names and before did not, narrowed to members who may view the requirement.
func toTell(ctx context.Context, tx db.DBTX, requirementID uuid.UUID, before, body json.RawMessage) ([]uuid.UUID, error) {
	named := document.MentionIDs(document.NewMentions(document.MentionsIn(before), document.MentionsIn(body)))
	out := []uuid.UUID{}
	if len(named) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id FROM unnest($1::uuid[]) WITH ORDINALITY AS m (id, at)
		JOIN app_user u ON u.id = m.id AND u.role IS NOT NULL AND u.is_active
		WHERE perm_requirement_viewable($2, u.id)
		ORDER BY m.at`, named, requirementID)
	if err != nil {
		return nil, fmt.Errorf("read whom the comment mentions: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	return append(out, ids...), nil
}

// Start begins a thread on a requirement.
func (s *Service) Start(ctx context.Context, reqKey string, in Input) (*Thread, db.LSN, error) {
	body, err := s.body(ctx, in)
	if err != nil {
		return nil, 0, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Thread
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := loadRequirement(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if err := r.canComment(); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO comment_thread (requirement_id, created_by) VALUES ($1, $2) RETURNING id`, r.ID, actor).Scan(&id); err != nil {
			return fmt.Errorf("start the thread: %w", err)
		}
		if err := add(ctx, tx, actor, id, id, r.ID, body, false); err != nil {
			return err
		}
		out, err = thread(ctx, tx, actor, r, id)
		return err
	})
	return out, lsn, err
}

// Reply adds a comment at the end of the thread the given comment is in, and reopens it if it was resolved.
func (s *Service) Reply(ctx context.Context, commentID uuid.UUID, in Input) (*Comment, *Thread, db.LSN, error) {
	body, err := s.body(ctx, in)
	if err != nil {
		return nil, nil, 0, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	var (
		made *Comment
		out  *Thread
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, r, err := locate(ctx, tx, actor, commentID, false)
		if err != nil {
			return err
		}
		if _, err := thread(ctx, tx, actor, r, h.thread); err != nil {
			return err
		}
		if err := r.canComment(); err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := add(ctx, tx, actor, id, h.thread, h.requirement, body, true); err != nil {
			return err
		}
		// The reply already tells the thread's writers, so reopening this way emits nothing more.
		if _, err := tx.Exec(ctx, `
			UPDATE comment_thread SET resolved_at = NULL, resolved_by = NULL
			WHERE id = $1 AND resolved_at IS NOT NULL`, h.thread); err != nil {
			return fmt.Errorf("reopen the thread: %w", err)
		}
		if out, err = thread(ctx, tx, actor, r, h.thread); err != nil {
			return err
		}
		made = find(out, id)
		return nil
	})
	return made, out, lsn, err
}

func find(t *Thread, id uuid.UUID) *Comment {
	for i := range t.Comments {
		if t.Comments[i].ID == id {
			return &t.Comments[i]
		}
	}
	return nil
}

// Edit rewrites the actor's own comment. Nobody rewrites another person's words, not even an administrator.
func (s *Service) Edit(ctx context.Context, commentID uuid.UUID, in Input) (*Comment, db.LSN, error) {
	body, err := s.body(ctx, in)
	if err != nil {
		return nil, 0, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Comment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, r, err := locate(ctx, tx, actor, commentID, true)
		if err != nil {
			return err
		}
		if h.deleted {
			return ErrNotFound
		}
		if h.author == nil || *h.author != actor {
			return ErrNotYours
		}
		if err := r.canComment(); err != nil {
			return err
		}
		var before []byte
		if err := tx.QueryRow(ctx, `SELECT body FROM comment WHERE id = $1`, commentID).Scan(&before); err != nil {
			return fmt.Errorf("read the comment: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE comment SET body = $2, edited_at = now() WHERE id = $1`, commentID, []byte(body)); err != nil {
			return fmt.Errorf("change the comment: %w", err)
		}
		mentioned, err := toTell(ctx, tx, h.requirement, before, body)
		if err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicEdited, map[string]any{
			"commentId": commentID, "threadId": h.thread, "requirementId": h.requirement, "actorId": actor, "mentioned": mentioned,
		}); err != nil {
			return err
		}
		t, err := thread(ctx, tx, actor, r, h.thread)
		if err != nil {
			return err
		}
		out = find(t, commentID)
		return nil
	})
	return out, lsn, err
}

// Delete takes a comment's words away and leaves its place in the thread. Its author may while they may view the
// requirement; the project's administrators may for anybody's, which is audited. A deleted one is no change.
func (s *Service) Delete(ctx context.Context, commentID uuid.UUID) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, r, err := locate(ctx, tx, actor, commentID, true)
		if err != nil {
			return err
		}
		mine := h.author != nil && *h.author == actor
		if !mine && !r.moderate {
			return ErrNotYours
		}
		if r.Archived {
			return project.ErrArchived
		}
		if h.deleted {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, commentID, actor); err != nil {
			return fmt.Errorf("delete the comment: %w", err)
		}
		if mine {
			return nil
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionCommentDeleted, TargetType: "comment", TargetID: &commentID, Actor: actor,
			Data: map[string]any{"requirementKey": r.Key, "threadId": h.thread, "authorId": h.author},
		})
	})
}

// Resolve marks a thread resolved, or reopens it, in the actor's name; a thread already so is no change.
func (s *Service) Resolve(ctx context.Context, threadID uuid.UUID, resolved bool) (*Thread, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Thread
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, r, err := locate(ctx, tx, actor, threadID, false)
		if err != nil {
			return err
		}
		if h.thread != threadID {
			return ErrNotFound
		}
		if _, err := thread(ctx, tx, actor, r, threadID); err != nil {
			return err
		}
		if err := r.canComment(); err != nil {
			return err
		}
		sql, args, topic := `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1 AND resolved_at IS NULL`,
			[]any{threadID, actor}, TopicResolved
		if !resolved {
			sql, args, topic = `UPDATE comment_thread SET resolved_at = NULL, resolved_by = NULL WHERE id = $1 AND resolved_at IS NOT NULL`,
				[]any{threadID}, TopicReopened
		}
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("resolve the thread: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if err := events.Emit(ctx, tx, topic, map[string]any{"threadId": threadID, "requirementId": h.requirement, "actorId": actor}); err != nil {
				return err
			}
		}
		out, err = thread(ctx, tx, actor, r, threadID)
		return err
	})
	return out, lsn, err
}

// Mentionable is who a comment on the requirement may mention: members whose name or email starts with q, each
// saying whether they may read the requirement.
func (s *Service) Mentionable(ctx context.Context, reqKey, q string, limit int) ([]Mentionable, error) {
	if limit < 1 || limit > MentionableMaxLimit {
		return nil, &FieldError{Field: "limit", Message: fmt.Sprintf("Ask for 1 to %d people.", MentionableMaxLimit)}
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	out := []Mentionable{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := loadRequirement(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if !r.comment {
			return ErrMayNotComment
		}
		prefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(q)) + "%"
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email::text, perm_requirement_viewable($3, u.id) FROM app_user u
			WHERE u.role IS NOT NULL AND u.is_active AND (u.name ILIKE $1 OR u.name ILIKE '% ' || $1 OR u.email::text ILIKE $1)
			ORDER BY lower(u.name), u.email LIMIT $2`, prefix, limit, r.ID)
		if err != nil {
			return err
		}
		people, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Mentionable])
		if err == nil {
			out = append(out, people...)
		}
		return err
	})
	return out, err
}
