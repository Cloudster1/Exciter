package reqifio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// errStopped is an import somebody cancelled while it ran.
var errStopped = errors.New("the import was cancelled")

// made is what an object became: the requirement or section it is now.
type made struct {
	ID  uuid.UUID
	Key string
}

// staged is a file of the archive waiting with its import.
type staged struct {
	data        []byte
	contentType string
}

// Run carries out a queued import as its maker, whom the context names: the schema, then each object in its own
// transaction in the order of the specifications, then the relations.
func (s *Service) Run(ctx context.Context, id uuid.UUID) error {
	var claimed bool
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE reqif_import SET state = 'running', started_at = now() WHERE id = $1 AND state = 'queued'`, id)
		claimed = err == nil && tag.RowsAffected() == 1
		return err
	}); err != nil || !claimed {
		return err
	}
	j, a, err := s.prepare(ctx, id)
	if err != nil {
		return s.fail(ctx, id, err)
	}
	if len(a.preview.Problems) > 0 {
		return s.fail(ctx, id, errors.New(a.preview.Problems[0].Message))
	}
	if err := s.makeSchema(ctx, j, a); err != nil {
		return s.fail(ctx, id, err)
	}
	// Read again, so every type, attribute and option the file names is now tied to the one it became.
	if j, a, err = s.prepare(ctx, id); err != nil {
		return s.fail(ctx, id, err)
	}
	files, err := s.stagedFiles(ctx, id)
	if err != nil {
		return s.fail(ctx, id, err)
	}
	r := &run{ctx: ctx, s: s, j: j, a: a, files: files, made: map[string]made{}, last: map[uuid.UUID]*uuid.UUID{}, comment: "Imported from " + j.FileName}
	done := 0
	for _, p := range a.plans {
		note := r.write(p)
		if p.Specification == "" {
			done++
		}
		if err := s.advance(ctx, id, min(done, j.Total), note); err != nil {
			return s.end(ctx, id, err)
		}
	}
	for _, refusal := range r.connect() {
		if err := s.advance(ctx, id, min(done, j.Total), progress{refused: &refusal}); err != nil {
			return s.end(ctx, id, err)
		}
	}
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM reqif_import_object WHERE import_id = $1`, id)
		return err
	}); err != nil {
		return s.end(ctx, id, err)
	}
	return s.end(ctx, id, nil)
}

// prepare reads the import and the project again and analyses the file with the choices made.
func (s *Service) prepare(ctx context.Context, id uuid.UUID) (*job, *analysis, error) {
	var (
		j *job
		a *analysis
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
		w, err := loadWorld(ctx, tx, p)
		if err != nil {
			return err
		}
		a = analyze(j.file, w, j.Choices)
		return nil
	})
	return j, a, err
}

func (s *Service) stagedFiles(ctx context.Context, id uuid.UUID) (map[string]staged, error) {
	out := map[string]staged{}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT path, data, content_type FROM reqif_import_object WHERE import_id = $1`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				p string
				f staged
			)
			if err := rows.Scan(&p, &f.data, &f.contentType); err != nil {
				return err
			}
			out[p] = f
		}
		return rows.Err()
	})
	return out, err
}

// run is one import being carried out.
type run struct {
	// ctx names the import's maker, as whom every write is made.
	ctx     context.Context
	s       *Service
	j       *job
	a       *analysis
	files   map[string]staged
	made    map[string]made
	last    map[uuid.UUID]*uuid.UUID
	comment string
}

// write makes or changes what one plan stands for, puts it at its place, and says how it went.
func (r *run) write(p *objectPlan) progress {
	ctx := r.ctx
	parent, refused := r.parentOf(p)
	var (
		note progress
		self made
		err  error
	)
	if p.Existing == nil {
		self, err = r.create(ctx, p, parent)
		note.key = self.Key
	} else {
		self = made{ID: p.Existing.ID, Key: p.Existing.Key}
		var changed bool
		if changed, err = r.update(ctx, p); err == nil {
			note.key, note.updated, note.unchanged = self.Key, changed, !changed
			err = r.move(ctx, p, self, parent)
		}
	}
	if err != nil {
		note.refused = &Refusal{Where: p.Where, Reason: reason(err)}
		if self.Key == "" {
			return note
		}
	} else if refused != "" {
		note.refused = &Refusal{Where: p.Where, Reason: refused}
	}
	r.made[p.Ref] = self
	kind, identifier := "object", p.Ref
	if p.Specification != "" {
		kind, identifier = "specification", p.Specification
	}
	note.ties = []tieRow{{kind: kind, identifier: identifier, target: self.ID}}
	return note
}

// parentOf is where a plan goes: below the requirement its parent became, else at the top, with why.
func (r *run) parentOf(p *objectPlan) (uuid.UUID, string) {
	if p.Parent == "" {
		return uuid.Nil, ""
	}
	if m, ok := r.made[p.Parent]; ok {
		return m.ID, ""
	}
	return uuid.Nil, "It stays at the top: what it sits below in the file was not imported."
}

func ptr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (r *run) create(ctx context.Context, p *objectPlan, parent uuid.UUID) (made, error) {
	statement, rationale, attrs := r.documents(p, nil)
	in := requirement.CreateInput{Kind: p.Kind, Title: p.Title, Statement: statement, Rationale: rationale, TypeID: p.TypeID,
		Attributes: attrs, OwnerID: p.Owner, Priority: p.Priority, Labels: p.Labels, Comment: r.comment, ImportedStatus: p.Status}
	after := r.last[parent]
	if parent != uuid.Nil || after != nil {
		in.Place = &requirement.Place{ParentID: ptr(parent), AfterID: after}
	}
	created, _, err := r.s.requirements.Create(ctx, r.j.createdBy, r.j.ProjectKey, in)
	if err != nil {
		return made{}, err
	}
	self := made{ID: created.ID, Key: created.Key}
	r.placed(self.ID, parent, after, in.Place == nil)
	if !p.Images {
		return self, nil
	}
	placer, err := r.attach(ctx, p, self, nil)
	if err != nil {
		return self, err
	}
	statement, rationale, attrs = r.documents(p, placer)
	revision := created.Revision
	_, _, err = r.s.requirements.Update(ctx, r.j.createdBy, self.Key, requirement.UpdateInput{Statement: statement, Rationale: rationale,
		Attributes: attrs, BaseRevision: &revision, Comment: "Imported the files of " + r.j.FileName})
	return self, err
}

// placed keeps the run's picture of the tree, a requirement made without a place going last at the top.
func (r *run) placed(id, parent uuid.UUID, after *uuid.UUID, last bool) {
	if last {
		kids := r.a.world.children[uuid.Nil]
		if len(kids) > 0 {
			after = &kids[len(kids)-1]
		}
	}
	r.a.world.place(id, parent, after)
	r.last[parent] = &id
}

func (r *run) update(ctx context.Context, p *objectPlan) (bool, error) {
	e := p.Existing
	var placer Placer
	if p.Images {
		var err error
		if placer, err = r.attach(ctx, p, made{ID: e.ID, Key: e.Key}, e.Files); err != nil {
			return false, err
		}
	}
	statement, rationale, attrs := r.documents(p, placer)
	in := requirement.UpdateInput{Statement: statement, Rationale: rationale, Attributes: attrs, BaseRevision: &e.Revision, Comment: r.comment}
	if p.Title != "" {
		in.Title = &p.Title
	}
	if p.Kind == requirement.KindRequirement && p.TypeID != nil && (e.TypeID == nil || *e.TypeID != *p.TypeID) {
		in.TypeID = p.TypeID
	}
	if p.OwnerSet {
		in.OwnerID = encode(p.Owner)
	}
	if p.Priority != nil {
		in.Priority = encode(p.Priority)
	}
	changed, _, err := r.s.requirements.Update(ctx, r.j.createdBy, e.Key, in)
	if err != nil {
		return false, err
	}
	for _, label := range p.Labels {
		if _, _, err := r.s.requirements.AddLabel(ctx, r.j.createdBy, e.Key, label); err != nil {
			return true, err
		}
	}
	return changed.Revision > e.Revision, nil
}

// move puts an existing requirement where the file has it, unless it already is there or keeps its place.
func (r *run) move(ctx context.Context, p *objectPlan, self made, parent uuid.UUID) error {
	if p.KeepPlace {
		if p.Existing.Parent == nil {
			r.last[uuid.Nil] = &self.ID
		}
		return nil
	}
	after := r.last[parent]
	if !r.a.world.placedAt(self.ID, parent, after) {
		place := requirement.Place{ParentID: ptr(parent), AfterID: after}
		if _, _, err := r.s.requirements.Move(ctx, r.j.createdBy, self.Key, requirement.MoveInput{Place: place}); err != nil {
			return err
		}
		r.a.world.place(self.ID, parent, after)
	}
	r.last[parent] = &self.ID
	return nil
}

// documents are a plan's rich text read again with its files placed, the attributes' with them.
func (r *run) documents(p *objectPlan, placer Placer) (statement, rationale json.RawMessage, attrs map[string]json.RawMessage) {
	read := func(key string, fallback json.RawMessage) json.RawMessage {
		if x := p.rich[key]; x != nil {
			doc, _ := ToDocument(x, placer)
			return doc
		}
		return fallback
	}
	statement, rationale = read(fieldStatement, p.Statement), read(fieldRationale, p.Rationale)
	if p.Kind == requirement.KindSection {
		rationale = nil
	}
	attrs = make(map[string]json.RawMessage, len(p.Attributes))
	for key, value := range p.Attributes {
		attrs[key] = read(key, value)
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	return statement, rationale, attrs
}

// exportedFile is the path an export gives a file of the project, which names the attachment it was.
var exportedFile = regexp.MustCompile(`^files/([0-9a-f-]{36})\.[A-Za-z0-9]+$`)

// attach makes the files a plan's rich text shows attachments of its requirement; a file an export wrote of an
// attachment the requirement still has is that attachment again.
func (r *run) attach(ctx context.Context, p *objectPlan, self made, have map[uuid.UUID]bool) (Placer, error) {
	placed := map[string]Placed{}
	for _, x := range p.rich {
		var refs []string
		x.walk(func(n *XNode) bool {
			if n.Name != "object" {
				return true
			}
			if chosen, ok := chooseObject(n); ok {
				refs = append(refs, strings.TrimSpace(chosen.Attrs["data"]))
			}
			return false
		})
		for _, ref := range refs {
			if _, done := placed[ref]; done {
				continue
			}
			img, ok := r.a.images[ref]
			if !ok {
				continue
			}
			if m := exportedFile.FindStringSubmatch(ref); m != nil {
				if id, err := uuid.Parse(m[1]); err == nil && have[id] {
					placed[ref] = Placed{ID: id.String(), Image: imageTypes[img.ContentType], FileName: path.Base(ref)}
					continue
				}
			}
			f, ok := r.files[ref]
			if !ok {
				continue
			}
			a, _, err := r.s.files.Upload(ctx, r.j.createdBy, self.Key, attachment.UploadInput{FileName: path.Base(ref), ContentType: f.contentType,
				Body: bytes.NewReader(f.data)})
			if err != nil {
				return nil, err
			}
			placed[ref] = Placed{ID: a.ID.String(), Image: imageTypes[img.ContentType], FileName: a.FileName}
		}
	}
	return func(ref string) (Placed, bool) {
		p, ok := placed[ref]
		return p, ok
	}, nil
}

// connect links what the relations ask for, leaving links the project already has as they are.
func (r *run) connect() []Refusal {
	ctx := r.ctx
	var refused []Refusal
	for _, rel := range r.a.relations {
		target := r.a.schema.links[rel.Type]
		source, okSource := r.end(rel.Source)
		other, okOther := r.end(rel.Target)
		if target == nil || target.id == nil || !okSource || !okOther {
			refused = append(refused, Refusal{Where: rel.Where, Reason: "An end of the relation was not imported, so it is left out."})
			continue
		}
		if r.a.world.links[linkKey{*target.id, source.ID, other.ID}] {
			continue
		}
		in := trace.LinkInput{TypeID: target.id, Direction: trace.Outgoing, OtherKey: other.Key}
		if _, _, err := r.s.trace.Link(ctx, r.j.createdBy, source.Key, in); err != nil {
			refused = append(refused, Refusal{Where: rel.Where, Reason: reason(err)})
			continue
		}
		r.a.world.links[linkKey{*target.id, source.ID, other.ID}] = true
	}
	return refused
}

func (r *run) end(identifier string) (made, bool) {
	if m, ok := r.made[identifier]; ok {
		return m, true
	}
	if tie, ok := r.a.world.tied("object", identifier, prefixObject); ok {
		if e := r.a.world.existing[tie.Target]; e != nil {
			return made{ID: e.ID, Key: e.Key}, true
		}
	}
	return made{}, false
}

// progress is what one step of a run adds to the import.
type progress struct {
	key       string
	updated   bool
	unchanged bool
	refused   *Refusal
	ties      []tieRow
}

// advance records a step and what its object became, and says the import was cancelled when it no longer runs.
func (s *Service) advance(ctx context.Context, id uuid.UUID, done int, note progress) error {
	imported, updated := []string{}, []string{}
	switch {
	case note.key != "" && note.updated:
		updated = append(updated, note.key)
	case note.key != "" && !note.unchanged:
		imported = append(imported, note.key)
	}
	refused := []Refusal{}
	if note.refused != nil {
		refused = append(refused, *note.refused)
	}
	unchanged := 0
	if note.unchanged {
		unchanged = 1
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var j job
		err := tx.QueryRow(ctx, `
			UPDATE reqif_import SET done = $2, imported = imported || $3::text[], updated = updated || $4::text[],
			       unchanged = unchanged + $5, refused = refused || $6::jsonb
			WHERE id = $1 AND state = 'running' RETURNING project_id`,
			id, done, imported, updated, unchanged, refused).Scan(&j.projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errStopped
		}
		if err != nil {
			return err
		}
		return writeTies(ctx, tx, &j, note.ties)
	})
	return err
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
		_, err := tx.Exec(ctx, `UPDATE reqif_import SET state = 'done', finished_at = now() WHERE id = $1 AND state = 'running'`, id)
		return err
	})
	return err
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, cause error) error {
	message := "The import stopped: " + reason(cause)
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE reqif_import SET state = 'failed', error = $2, finished_at = now() WHERE id = $1 AND state = 'running'`,
			id, message)
		return err
	})
	return err
}

// reason is why something was not written, in the sentence the refusal that stopped it carries.
func reason(err error) string {
	var (
		req  *requirement.FieldError
		attr *attribute.FieldError
		link *trace.FieldError
		file *FileError
	)
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
		return "The person who started the import no longer administers the project. Ask an administrator to import the file again."
	}
	return "It could not be written. Import the file again; if it fails again, ask an administrator."
}

// requested is what reqif.requested carries.
type requested struct {
	ActorID  uuid.UUID `json:"actorId"`
	ImportID uuid.UUID `json:"importId"`
}

// Handler runs the import a reqif.requested event names, as the person who started it.
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
