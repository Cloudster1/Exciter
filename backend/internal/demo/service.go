package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// TopicRequested asks the worker for one step of the demo, as the administrator who asked for it.
const TopicRequested = "suite_demo.requested"

// Keys are the projects the demo makes, in the order of the snapshot's tools; Armature and Stator use the same.
var Keys = []string{"ARM", "EXC", "STA"}

var (
	// ErrExists is a second request to add the demo.
	ErrExists = errors.New("the suite demo was added already")
	// ErrNone is a request to link a demo nobody added.
	ErrNone = errors.New("the suite demo has not been added")
	// ErrBusy is a request while the demo is being built or linked.
	ErrBusy = errors.New("the suite demo is being built or linked")
)

// KeyTakenError is a project that already holds one of the demo's keys.
type KeyTakenError struct{ Key string }

func (e *KeyTakenError) Error() string {
	return fmt.Sprintf("The key %s is taken by another project, so the suite demo cannot make its project. Change that project's key, then add the demo.", e.Key)
}

// State is how far the demo got.
type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// Kind is what a run of the demo does.
type Kind string

const (
	KindBuild Kind = "build"
	KindLinks Kind = "links"
)

// Demo is the suite demo as an administrator reads it.
type Demo struct {
	State        State      `json:"state"`
	Kind         Kind       `json:"kind"`
	Taken        string     `json:"taken"`
	ProjectKeys  []string   `json:"projectKeys"`
	Step         int        `json:"step"`
	Steps        int        `json:"steps"`
	Requirements int        `json:"requirements"`
	Links        int        `json:"links"`
	Skipped      int        `json:"skipped"`
	Message      string     `json:"message"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
}

// Service adds the suite demo and links it to Armature and Stator.
type Service struct {
	db           *db.Cluster
	projects     *project.Service
	requirements *requirement.Service
	trace        *trace.Service
	baselines    *baseline.Service
	armature     *armature.Service
	stator       *stator.Service
	snapshot     *Snapshot
	topic        string
	keys         []string
}

// NewService builds the service; armatures and stators may be nil, and then nothing is linked to them.
func NewService(cluster *db.Cluster, projects *project.Service, requirements *requirement.Service, links *trace.Service,
	baselines *baseline.Service, armatures *armature.Service, stators *stator.Service) *Service {
	return &Service{db: cluster, projects: projects, requirements: requirements, trace: links, baselines: baselines,
		armature: armatures, stator: stators, topic: TopicRequested, keys: Keys}
}

// WithSnapshot runs a snapshot other than the embedded one under keys of its own, its steps on a topic of its own,
// so a test's demo meets no other and is never taken up by a worker that holds the embedded snapshot.
func (s *Service) WithSnapshot(snap Snapshot, topic string, keys []string) *Service {
	s.snapshot, s.topic, s.keys = &snap, topic, keys
	return s
}

func (s *Service) snapshotOf() (Snapshot, error) {
	if s.snapshot != nil {
		return *s.snapshot, nil
	}
	return Embedded()
}

const selectDemo = `SELECT state, kind, taken, project_keys, step, steps, requirements, links, skipped, message, created_at,
	started_at, finished_at FROM suite_demo`

func readDemo(ctx context.Context, tx db.DBTX) (*Demo, error) {
	var d Demo
	err := tx.QueryRow(ctx, selectDemo).Scan(&d.State, &d.Kind, &d.Taken, &d.ProjectKeys, &d.Step, &d.Steps, &d.Requirements,
		&d.Links, &d.Skipped, &d.Message, &d.CreatedAt, &d.StartedAt, &d.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

// Status is the demo, or nil when nobody added it.
func (s *Service) Status(ctx context.Context) (*Demo, error) {
	var d *Demo
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		d, err = readDemo(ctx, tx)
		return err
	})
	return d, err
}

// Start adds the demo: its first step is queued for the worker, which builds it as actor.
func (s *Service) Start(ctx context.Context, actor uuid.UUID) (*Demo, db.LSN, error) {
	snap, err := s.snapshotOf()
	if err != nil {
		return nil, 0, err
	}
	steps := len(stepsOf(Make(snap), KindBuild))
	var out *Demo
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if d, err := readDemo(ctx, tx); err != nil || d != nil {
			return cmpErr(err, ErrExists)
		}
		for _, key := range s.keys {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT project_key_taken($1)`, key).Scan(&taken); err != nil {
				return err
			}
			if taken {
				return &KeyTakenError{Key: key}
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO suite_demo (taken, project_keys, requested_by, steps) VALUES ($1, $2, $3, $4)`,
			snap.Taken(), s.keys, actor, steps); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, s.topic, requested{ActorID: actor}); err != nil {
			return err
		}
		id, err := installation.ID(ctx, tx)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSuiteDemoAdded, TargetType: "installation",
			TargetID: &id, Actor: actor, Data: map[string]any{"taken": snap.Taken(), "projects": s.keys}}); err != nil {
			return err
		}
		out, err = readDemo(ctx, tx)
		return err
	})
	return out, lsn, err
}

// Relink queues the demo to add the links it is missing, for a partner that was connected since.
func (s *Service) Relink(ctx context.Context, actor uuid.UUID) (*Demo, db.LSN, error) {
	snap, err := s.snapshotOf()
	if err != nil {
		return nil, 0, err
	}
	steps := len(stepsOf(Make(snap), KindLinks))
	var out *Demo
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		d, err := readDemo(ctx, tx)
		switch {
		case err != nil:
			return err
		case d == nil:
			return ErrNone
		case d.State == StateQueued || d.State == StateRunning:
			return ErrBusy
		}
		if _, err := tx.Exec(ctx, `UPDATE suite_demo SET state = 'queued', kind = 'links', step = 0, steps = $2, skipped = 0,
			requested_by = $1, message = '', started_at = NULL, finished_at = NULL`, actor, steps); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, s.topic, requested{ActorID: actor}); err != nil {
			return err
		}
		out, err = readDemo(ctx, tx)
		return err
	})
	return out, lsn, err
}

func cmpErr(err, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}

// requested is what suite_demo.requested carries: who asked, and the step it is for.
type requested struct {
	ActorID uuid.UUID `json:"actorId"`
	Step    int       `json:"step"`
}

// Handler runs the step a suite_demo.requested event names, as the administrator who asked.
func Handler(s *Service) events.HandlerFunc {
	return func(ctx context.Context, e events.Event) error {
		var r requested
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return fmt.Errorf("read %s: %w", e.Topic, err)
		}
		return s.Step(db.WithUser(ctx, r.ActorID), r.ActorID, r.Step)
	}
}

// Step runs one step of the demo and queues the next; an event for a step that is not next does nothing, so a
// repeated one changes nothing.
func (s *Service) Step(ctx context.Context, actor uuid.UUID, at int) error {
	var d *Demo
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE suite_demo SET state = 'running', started_at = coalesce(started_at, now())
			WHERE step = $1 AND step < steps AND state IN ('queued', 'running')`, at)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		d, err = readDemo(ctx, tx)
		return err
	}); err != nil || d == nil {
		return err
	}
	snap, err := s.snapshotOf()
	if err != nil {
		return s.fail(ctx, err)
	}
	plan := Make(snap)
	all := stepsOf(plan, d.Kind)
	if at >= len(all) {
		return s.fail(ctx, fmt.Errorf("this build plans %d steps, and step %d was asked for", len(all), at+1))
	}
	got, err := s.run(ctx, actor, plan, keysOf(snap, d.ProjectKeys), all[at])
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return s.fail(ctx, err)
	}
	return s.advance(ctx, actor, at, got)
}

// keysOf names each tool's project by the keys the demo was made with.
func keysOf(snap Snapshot, projectKeys []string) map[string]string {
	keys := map[string]string{}
	for i, t := range snap.Tools {
		if i < len(projectKeys) {
			keys[t.Key] = projectKeys[i]
		}
	}
	return keys
}

// Result is what a step made.
type Result struct {
	Requirements int
	Linked
}

// advance records a step and queues the next, or ends the run after the last, in one write.
func (s *Service) advance(ctx context.Context, actor uuid.UUID, at int, got Result) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		d, err := readDemo(ctx, tx)
		if err != nil || d == nil || d.Step != at {
			return err
		}
		message := d.Message
		for _, note := range got.Notes {
			if !strings.Contains(message, note) {
				message = strings.TrimSpace(message + " " + note)
			}
		}
		state, finished := StateRunning, at+1 >= d.Steps
		if finished {
			state = StateDone
		}
		if _, err := tx.Exec(ctx, `UPDATE suite_demo SET step = step + 1, state = $1, requirements = requirements + $2,
			links = links + $3, skipped = skipped + $4, message = $5, finished_at = CASE WHEN $6 THEN now() END`,
			state, got.Requirements, got.Links, got.Skipped, message, finished); err != nil {
			return err
		}
		if finished {
			return nil
		}
		return events.Emit(ctx, tx, s.topic, requested{ActorID: actor, Step: at + 1})
	})
	return err
}

// fail ends the run with why; a failure is the demo's to report, not the worker's to try again.
func (s *Service) fail(ctx context.Context, cause error) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE suite_demo SET state = 'failed', message = $1, finished_at = now() WHERE state = 'running'`,
			"The suite demo stopped: "+cause.Error())
		return err
	})
	return err
}
