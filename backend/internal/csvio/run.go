package csvio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// errStopped is an import somebody cancelled while it ran.
var errStopped = errors.New("the import was cancelled")

// made is a row the first pass wrote: the requirement it is now.
type made struct {
	ID  uuid.UUID
	Key string
}

// Run carries out a started import as the actor the context names, its maker: the rows first, each in its own
// transaction, then the tree and the links. An import that is not queued is left alone.
func (s *Service) Run(ctx context.Context, id uuid.UUID) error {
	var claimed bool
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE import_job SET state = 'running', started_at = now() WHERE id = $1 AND state = 'queued'`, id)
		claimed = err == nil && tag.RowsAffected() == 1
		return err
	}); err != nil || !claimed {
		return err
	}
	j, plans, problems, err := s.prepare(ctx, id)
	if err != nil {
		return s.fail(ctx, id, err)
	}
	if msg, ok := problems[0]; ok {
		return s.fail(ctx, id, errors.New(msg))
	}
	refs := map[string]made{}
	comment := "Imported from " + j.FileName
	for i, p := range plans {
		var note progress
		if msg, refused := problems[p.Line]; refused {
			note.refused = &Refusal{Row: p.Line, Reason: msg}
		} else if r, updated, err := s.write(ctx, j, p, comment); err != nil {
			note.refused = &Refusal{Row: p.Line, Reason: reason(err, p.Line)}
		} else {
			if p.Ref != "" {
				refs[p.Ref] = made{ID: r.ID, Key: r.Key}
			}
			note.key, note.updated = r.Key, updated
		}
		if err := s.advance(ctx, id, i+1, note); err != nil {
			return s.end(ctx, id, err)
		}
	}
	for _, refusal := range s.connect(ctx, j, plans, refs) {
		if err := s.advance(ctx, id, len(plans), progress{refused: &refusal}); err != nil {
			return s.end(ctx, id, err)
		}
	}
	return s.end(ctx, id, nil)
}

// prepare reads the import and the project again and plans every row, with the problems by line; line 0 holds
// one about the file as a whole.
func (s *Service) prepare(ctx context.Context, id uuid.UUID) (*job, []plan, map[int]string, error) {
	var (
		j        *job
		result   analysis
		problems = map[int]string{}
	)
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if j, err = findJob(ctx, tx, id); err != nil {
			return err
		}
		p, err := running(ctx, tx, j.createdBy, j.ProjectKey)
		if err != nil {
			return err
		}
		w, err := loadWorld(ctx, tx, j.createdBy, p)
		if err != nil {
			return err
		}
		result = w.analyze(j.headers, j.rows, j.Mapping, j.choices)
		return nil
	})
	for _, problem := range result.preview.Problems {
		if _, seen := problems[problem.Row]; !seen {
			problems[problem.Row] = problem.Message
		}
	}
	return j, result.plans, problems, err
}

// write makes or changes the requirement one row stands for, as the import's maker.
func (s *Service) write(ctx context.Context, j *job, p plan, comment string) (*requirement.Requirement, bool, error) {
	if p.Existing == nil {
		in := requirement.CreateInput{Kind: p.Kind, Title: p.Title, Statement: p.Statement, Rationale: p.Rationale, TypeID: p.TypeID,
			Attributes: p.Attributes, OwnerID: p.Owner, Priority: p.Priority, Labels: p.Labels, Comment: comment, ImportedStatus: p.Status}
		r, _, err := s.requirements.Create(ctx, j.createdBy, j.ProjectKey, in)
		return r, false, err
	}
	in := requirement.UpdateInput{Statement: p.Statement, Rationale: p.Rationale, TypeID: p.TypeID, Comment: comment, BaseRevision: &p.Existing.Revision}
	if p.Title != "" {
		in.Title = &p.Title
	}
	if len(p.Attributes) > 0 {
		in.Attributes = p.Attributes
	}
	if p.OwnerSet {
		in.OwnerID, _ = json.Marshal(p.Owner)
	}
	if p.Priority != nil {
		in.Priority, _ = json.Marshal(p.Priority)
	}
	r, _, err := s.requirements.Update(ctx, j.createdBy, p.Existing.Key, in)
	if err != nil {
		return nil, true, err
	}
	for _, name := range p.Labels {
		if _, _, err := s.requirements.AddLabel(ctx, j.createdBy, r.Key, name); err != nil {
			return nil, true, err
		}
	}
	return r, true, nil
}

// connect is the second pass: each row under its parent in the file's order, then the links.
func (s *Service) connect(ctx context.Context, j *job, plans []plan, refs map[string]made) []Refusal {
	var refused []Refusal
	resolve := func(key string, p plan) (made, bool) {
		if m, ok := refs[key]; ok {
			return m, true
		}
		if p.Existing != nil && key == p.Existing.Key {
			return made{ID: p.Existing.ID, Key: key}, true
		}
		return made{}, false
	}
	keyOf := func(p plan) (made, bool) {
		if p.Ref != "" {
			if m, ok := refs[p.Ref]; ok {
				return m, true
			}
		}
		if p.Existing != nil {
			return made{ID: p.Existing.ID, Key: p.Existing.Key}, true
		}
		return made{}, false
	}
	last := map[uuid.UUID]*uuid.UUID{}
	for _, p := range plans {
		self, ok := keyOf(p)
		if !ok || !p.ParentSet || p.Parent == "" {
			continue
		}
		parent, ok := resolve(p.Parent, p)
		if !ok {
			parent, ok = s.existingKey(ctx, j, p.Parent)
		}
		if !ok {
			refused = append(refused, Refusal{Row: p.Line, Reason: fmt.Sprintf("Line %d stays at the top: its parent %s was not imported.", p.Line, p.Parent)})
			continue
		}
		place := requirement.Place{ParentID: &parent.ID, AfterID: last[parent.ID]}
		if _, _, err := s.requirements.Move(ctx, j.createdBy, self.Key, requirement.MoveInput{Place: place}); err != nil {
			refused = append(refused, Refusal{Row: p.Line, Reason: reason(err, p.Line)})
			continue
		}
		moved := self.ID
		last[parent.ID] = &moved
	}
	for _, p := range plans {
		self, ok := keyOf(p)
		if !ok {
			continue
		}
		for _, l := range p.Links {
			other := l.Other
			if m, ok := refs[other]; ok {
				other = m.Key
			}
			in := trace.LinkInput{TypeKey: l.TypeKey, Direction: trace.Outgoing, OtherKey: other}
			if _, _, err := s.trace.Link(ctx, j.createdBy, self.Key, in); err != nil {
				refused = append(refused, Refusal{Row: p.Line, Reason: fmt.Sprintf("The link %s:%s of line %d: %s", l.TypeKey, l.Other, p.Line, reason(err, p.Line))})
			}
		}
	}
	return refused
}

// existingKey finds a requirement of the project by its key, for a parent the file names but did not hold.
func (s *Service) existingKey(ctx context.Context, j *job, key string) (made, bool) {
	var out made
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := requirement.FindEndpoint(ctx, tx, j.createdBy, key)
		if err == nil {
			out = made{ID: e.ID, Key: e.Key}
		}
		return err
	})
	return out, err == nil
}

// progress is what one step of a run adds to the import.
type progress struct {
	key     string
	updated bool
	refused *Refusal
}

// advance records a step, and says the import was cancelled when it no longer runs.
func (s *Service) advance(ctx context.Context, id uuid.UUID, done int, note progress) error {
	var imported, updated []string
	if note.key != "" && note.updated {
		updated = append(updated, note.key)
	} else if note.key != "" {
		imported = append(imported, note.key)
	}
	refused := []Refusal{}
	if note.refused != nil {
		refused = append(refused, *note.refused)
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var state State
		err := tx.QueryRow(ctx, `
			UPDATE import_job SET done = $2, imported = imported || $3::text[], updated = updated || $4::text[], refused = refused || $5::jsonb
			WHERE id = $1 AND state = 'running' RETURNING state`,
			id, done, nonNil(imported), nonNil(updated), refused).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return errStopped
		}
		return err
	})
	return err
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// end closes a run: done, cancelled already, or failed for a reason its maker reads.
func (s *Service) end(ctx context.Context, id uuid.UUID, err error) error {
	if errors.Is(err, errStopped) {
		return nil
	}
	if err != nil {
		return s.fail(ctx, id, err)
	}
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE import_job SET state = 'done', finished_at = now() WHERE id = $1 AND state = 'running'`, id)
		return err
	})
	return err
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, cause error) error {
	message := "The import stopped: " + reason(cause, 0)
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE import_job SET state = 'failed', error = $2, finished_at = now() WHERE id = $1 AND state = 'running'`,
			id, message)
		return err
	})
	return err
}

// reason is why a row was not written, in the sentence the refusal that stopped it carries.
func reason(err error, line int) string {
	var (
		req  *requirement.FieldError
		attr *attribute.FieldError
		link *trace.FieldError
		file *FileError
	)
	if sentence, ok := requirement.LockedSentence(err); ok {
		return sentence
	}
	switch {
	case errors.As(err, &req):
		return req.Message
	case errors.As(err, &attr):
		return attr.Message
	case errors.As(err, &link):
		return link.Message
	case errors.As(err, &file):
		return file.Message
	case errors.Is(err, requirement.ErrForbidden), errors.Is(err, ErrForbidden):
		return "The person who started the import no longer edits the project. Ask an administrator to give them the editor role, and import again."
	}
	if line > 0 {
		return fmt.Sprintf("Line %d could not be written. Import the file again; if it fails again, ask an administrator.", line)
	}
	return err.Error()
}

// requested is what import.requested carries.
type requested struct {
	ActorID  uuid.UUID `json:"actorId"`
	ImportID uuid.UUID `json:"importId"`
}

// Handler runs the import an import.requested event names, as the person who started it.
func Handler(s *Service) events.HandlerFunc {
	return func(ctx context.Context, e events.Event) error {
		var r requested
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return fmt.Errorf("read %s: %w", e.Topic, err)
		}
		ctx = db.WithUser(ctx, r.ActorID)
		return s.Run(ctx, r.ImportID)
	}
}
