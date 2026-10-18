package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// Reuse and variants (#49): a library's requirement copied into another project follows its source, whose words it
// takes revision by revision, until it is made a variant with words of its own.

// ReuseState is whether a reused requirement's words are its source's or its own.
type ReuseState string

const (
	ReuseFollowing ReuseState = "following"
	ReuseVariant   ReuseState = "variant"
)

// ReuseStates lists every ReuseState, for the API document.
var ReuseStates = []ReuseState{ReuseFollowing, ReuseVariant}

// Reuse is what a reused requirement knows of its source. Revision is the source's revision it shows, or branched
// from; Source is the source now, for a reader who may read it, and null for anybody else.
type Reuse struct {
	State    ReuseState `json:"state"`
	Revision int        `json:"revision"`
	Source   *Ref       `json:"source"`
	// Behind says the source has a newer revision than Revision, as far as the reader may see it.
	Behind bool `json:"behind"`
}

// ReuseInput reuses a library requirement, and what is below it when asked, in another project.
type ReuseInput struct {
	ProjectKey string `json:"projectKey"`
	Place      Place  `json:"place"`
	Children   bool   `json:"children"`
}

// TakeInput takes the source's words at its latest revision, which the caller names so a newer one is not taken
// unseen.
type TakeInput struct {
	Revision int    `json:"revision"`
	Comment  string `json:"comment,omitempty"`
}

var (
	// ErrNotLibrary is a requirement offered for reuse from a project that is not a library.
	ErrNotLibrary = errors.New("only a library's requirements are reused")
	// ErrNotReused is a take or a variant asked of a requirement that follows no source.
	ErrNotReused = errors.New("the requirement follows no source")
	// ErrFollowsSource is a change of the words of a requirement that takes them from its source.
	ErrFollowsSource = errors.New("a requirement that follows its source takes its words from there")
	// ErrSourceHidden is a take by somebody who may not read the source.
	ErrSourceHidden = errors.New("the source is not readable")
)

// SourceMovedError is a take of a revision the source has moved past.
type SourceMovedError struct {
	Key      string
	Revision int
}

func (e *SourceMovedError) Error() string {
	return fmt.Sprintf("%s has revision %d now", e.Key, e.Revision)
}

// reuseSQL reads what scan needs of a reuse: its own columns, and its source as the reader may see it.
const reuseSQL = `,
       r.reuse_source, COALESCE(r.reuse_revision, 0), COALESCE(r.reuse_state, ''), rs.id, COALESCE(rsp.key, ''),
       COALESCE(rs.number, 0), COALESCE(rs.kind, ''), COALESCE(rs.title, ''), COALESCE(rs.revision, 0), rs.trashed_at IS NOT NULL`

// reuseJoins find the source under the reader's own rules, so one they may not read joins nothing.
const reuseJoins = `
LEFT JOIN requirement rs ON rs.id = r.reuse_source
LEFT JOIN project rsp ON rsp.id = rs.project_id`

// scannedReuse holds the reuse columns until scan knows whether there is a reuse at all.
type scannedReuse struct {
	source   *uuid.UUID
	revision int
	state    string
	seenID   *uuid.UUID
	seen     Ref
	number   int
}

func (sr *scannedReuse) targets() []any {
	return []any{&sr.source, &sr.revision, &sr.state, &sr.seenID, &sr.seen.ProjectKey, &sr.number, &sr.seen.Kind, &sr.seen.Title,
		&sr.seen.Revision, &sr.seen.Trashed}
}

func (sr *scannedReuse) reuse() *Reuse {
	if sr.source == nil {
		return nil
	}
	out := &Reuse{State: ReuseState(sr.state), Revision: sr.revision}
	if sr.seenID != nil && sr.seen.ProjectKey != "" {
		src := sr.seen
		src.ID, src.Key = *sr.seenID, Key(src.ProjectKey, sr.number)
		out.Source, out.Behind = &src, src.Revision > sr.revision
	}
	return out
}

// reusable refuses reuse out of a project that is no library, or into the library itself, where a copy serves.
func reusable(here *project.Project, target string) error {
	if !here.Settings.Library {
		return ErrNotLibrary
	}
	if strings.TrimSpace(target) == "" || NormalizedProjectKey(target) == here.Key {
		return &FieldError{"projectKey", "Reuse it in another project. To repeat it in its library, copy it instead."}
	}
	return nil
}

// Reuse copies a library requirement, and what is below it when asked, into another project the actor edits; each
// copy follows its own original. Links are not copied: a reuse's links are its project's own.
func (s *Service) Reuse(ctx context.Context, actor uuid.UUID, key string, in ReuseInput) (*Requirement, int, db.LSN, error) {
	return s.copyTree(ctx, actor, key, CopyInput{ProjectKey: in.ProjectKey, Place: in.Place, Children: in.Children}, true)
}

// following refuses a change of the words of a requirement that takes them from its source.
func following(cur *loaded, next Normative) error {
	if cur.Reuse == nil || cur.Reuse.State != ReuseFollowing {
		return nil
	}
	if next.Title != cur.Title || !sameDocument(next.Statement, cur.Statement) || !sameDocument(next.Rationale, cur.Rationale) {
		return ErrFollowsSource
	}
	return nil
}

// sameDocument compares two documents by what they hold, whatever their spacing or the order of their keys.
func sameDocument(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// TakeSource gives a reused requirement its source's words at the source's latest revision, as its next revision,
// and has a variant follow again. Its type, values and everything else stay its own.
func (s *Service) TakeSource(ctx context.Context, actor uuid.UUID, key string, in TakeInput) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		switch {
		case err != nil:
			return err
		case cur.TrashedAt != nil:
			return ErrTrashed
		case cur.Reuse == nil:
			return ErrNotReused
		case cur.Reuse.Source == nil:
			return ErrSourceHidden
		}
		src, err := findByID(ctx, tx, actor, cur.Reuse.Source.ID)
		if errors.Is(err, ErrNotFound) {
			return ErrSourceHidden
		}
		if err != nil {
			return err
		}
		if in.Revision != src.Revision {
			return &SourceMovedError{Key: src.Key, Revision: src.Revision}
		}
		if cur.Reuse.State == ReuseFollowing && cur.Reuse.Revision == src.Revision {
			out = &cur.Requirement
			return nil
		}
		if err := s.take(ctx, tx, actor, cur, src, in.Comment); err != nil {
			return err
		}
		l, err := findByID(ctx, tx, actor, cur.ID)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}

func (s *Service) take(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur, src *loaded, said string) error {
	comment, err := checkComment(said)
	if err != nil {
		return err
	}
	if comment == "" {
		comment = fmt.Sprintf("Took revision %d of %s", src.Revision, src.Key)
	}
	test, err := testFor(ctx, tx, cur.typeID(), src.test)
	if err != nil {
		return err
	}
	next := Normative{Title: src.Title, Statement: src.Statement, Rationale: src.Rationale, TypeID: cur.typeID(), Attributes: cur.Attributes, Test: test}
	files, err := s.filesShown(ctx, tx, src)
	if err != nil {
		return err
	}
	if err := pointAtFiles(&next, files); err != nil {
		return err
	}
	if cur.Reuse.State == ReuseVariant {
		if err := logChange(ctx, tx, actor, cur.ID, ChangeReuse, string(ReuseVariant), string(ReuseFollowing)); err != nil {
			return err
		}
	}
	hash := next.Hash()
	if hash == cur.hash {
		// The words are the source's already, as when the source changed nothing else but its revision.
		_, err := tx.Exec(ctx, `UPDATE requirement SET reuse_revision = $2, reuse_state = 'following' WHERE id = $1`, cur.ID, src.Revision)
		return err
	}
	pre, steps, params, err := next.Test.columns()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE requirement SET title = $2, statement = $3, rationale = $4, normative_hash = $5, updated_by = $6,
		    reuse_revision = $7, reuse_state = 'following', preconditions = $8, steps = $9, parameters = $10
		WHERE id = $1`, cur.ID, next.Title, next.Statement, next.Rationale, hash, actor, src.Revision, pre, steps, params); err != nil {
		return fmt.Errorf("take %s into %s: %w", src.Key, cur.Key, err)
	}
	if len(files) > 0 {
		if err := s.files.CopyFiles(ctx, tx, actor, cur.ID, files); err != nil {
			return err
		}
	}
	return s.recordRevision(ctx, tx, actor, cur, next, hash, comment)
}

// filesShown plans copies of the source's files its words show, and of no other, so each take brings only those.
func (s *Service) filesShown(ctx context.Context, tx db.DBTX, src *loaded) (map[uuid.UUID]uuid.UUID, error) {
	if s.files == nil {
		return nil, nil
	}
	planned, err := s.files.PlanCopy(ctx, tx, src.ID)
	if err != nil {
		return nil, err
	}
	steps, _ := json.Marshal(src.test)
	words := string(src.Statement) + string(src.Rationale) + string(steps)
	for id := range planned {
		if !strings.Contains(words, id.String()) {
			delete(planned, id)
		}
	}
	return planned, nil
}

// MakeVariant lets a reused requirement have words of its own. It keeps its source and the revision it branched
// from, so a newer revision of the source still shows, and taking one has it follow again.
func (s *Service) MakeVariant(ctx context.Context, actor uuid.UUID, key string) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		switch {
		case err != nil:
			return err
		case cur.TrashedAt != nil:
			return ErrTrashed
		case cur.Reuse == nil:
			return ErrNotReused
		}
		if cur.Reuse.State == ReuseFollowing {
			if _, err := tx.Exec(ctx, `UPDATE requirement SET reuse_state = 'variant' WHERE id = $1`, cur.ID); err != nil {
				return err
			}
			if err := logChange(ctx, tx, actor, cur.ID, ChangeReuse, string(ReuseFollowing), string(ReuseVariant)); err != nil {
				return err
			}
			if err := events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": cur.ID, "field": ChangeReuse}); err != nil {
				return err
			}
		}
		l, err := findByID(ctx, tx, actor, cur.ID)
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, lsn, err
}
