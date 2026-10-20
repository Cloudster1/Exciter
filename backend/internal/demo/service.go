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

// TopicRequested asks the worker to build the demo or link it again, as the administrator who asked.
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
}

// NewService builds the service; armatures and stators may be nil, and then nothing is linked to them.
func NewService(cluster *db.Cluster, projects *project.Service, requirements *requirement.Service, links *trace.Service,
	baselines *baseline.Service, armatures *armature.Service, stators *stator.Service) *Service {
	return &Service{db: cluster, projects: projects, requirements: requirements, trace: links, baselines: baselines,
		armature: armatures, stator: stators}
}

const selectDemo = `SELECT state, kind, taken, project_keys, requirements, links, skipped, message, created_at, started_at,
	finished_at FROM suite_demo`

func readDemo(ctx context.Context, tx db.DBTX) (*Demo, error) {
	var d Demo
	err := tx.QueryRow(ctx, selectDemo).Scan(&d.State, &d.Kind, &d.Taken, &d.ProjectKeys, &d.Requirements, &d.Links,
		&d.Skipped, &d.Message, &d.CreatedAt, &d.StartedAt, &d.FinishedAt)
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

// Start adds the demo: it is queued for the worker, which builds it as actor.
func (s *Service) Start(ctx context.Context, actor uuid.UUID) (*Demo, db.LSN, error) {
	snap, err := Embedded()
	if err != nil {
		return nil, 0, err
	}
	var out *Demo
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if d, err := readDemo(ctx, tx); err != nil || d != nil {
			return cmpErr(err, ErrExists)
		}
		for _, key := range Keys {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT project_key_taken($1)`, key).Scan(&taken); err != nil {
				return err
			}
			if taken {
				return &KeyTakenError{Key: key}
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO suite_demo (taken, project_keys, requested_by) VALUES ($1, $2, $3)`,
			snap.Taken(), Keys, actor); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicRequested, requested{ActorID: actor}); err != nil {
			return err
		}
		id, err := installation.ID(ctx, tx)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSuiteDemoAdded, TargetType: "installation",
			TargetID: &id, Actor: actor, Data: map[string]any{"taken": snap.Taken(), "projects": Keys}}); err != nil {
			return err
		}
		out, err = readDemo(ctx, tx)
		return err
	})
	return out, lsn, err
}

// Relink queues the demo to add the links it is missing, for a partner that was connected since.
func (s *Service) Relink(ctx context.Context, actor uuid.UUID) (*Demo, db.LSN, error) {
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
		if _, err := tx.Exec(ctx, `UPDATE suite_demo SET state = 'queued', kind = 'links', requested_by = $1, message = '',
			started_at = NULL, finished_at = NULL`, actor); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicRequested, requested{ActorID: actor}); err != nil {
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

// requested is what suite_demo.requested carries.
type requested struct {
	ActorID uuid.UUID `json:"actorId"`
}

// Handler runs the demo a suite_demo.requested event asks for, as the administrator who asked.
func Handler(s *Service) events.HandlerFunc {
	return func(ctx context.Context, e events.Event) error {
		var r requested
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return fmt.Errorf("read %s: %w", e.Topic, err)
		}
		return s.Run(db.WithUser(ctx, r.ActorID), r.ActorID)
	}
}

// Run builds or links the queued demo; one that is not queued is left alone, so a repeated event does nothing.
func (s *Service) Run(ctx context.Context, actor uuid.UUID) error {
	var d *Demo
	if _, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Claimed in one statement, so the worker and the seed never both run it.
		tag, err := tx.Exec(ctx, `UPDATE suite_demo SET state = 'running', started_at = now() WHERE state = 'queued'`)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		d, err = readDemo(ctx, tx)
		return err
	}); err != nil || d == nil {
		return err
	}
	snap, err := Embedded()
	if err != nil {
		return s.finish(ctx, Result{}, err)
	}
	plan := Make(snap)
	keys := map[string]string{}
	for i, t := range snap.Tools {
		if i < len(d.ProjectKeys) {
			keys[t.Key] = d.ProjectKeys[i]
		}
	}
	var res Result
	if d.Kind == KindBuild {
		if res.Requirements, err = s.Build(ctx, actor, plan, keys); err != nil {
			return s.finish(ctx, res, err)
		}
	}
	linked, err := s.Link(ctx, actor, plan, keys)
	res.Links, res.Skipped, res.Notes = linked.Links, linked.Skipped, linked.Notes
	if err == nil {
		err = s.Baseline(ctx, actor, plan, keys)
	}
	return s.finish(ctx, res, err)
}

// Result is what a run made.
type Result struct {
	Requirements int
	Links        int
	Skipped      int
	// Notes say why links were skipped, one sentence per partner.
	Notes []string
}

// finish records how the run ended; a failure is the run's to report, not the worker's to try again.
func (s *Service) finish(ctx context.Context, res Result, cause error) error {
	state, message := StateDone, strings.Join(res.Notes, " ")
	if cause != nil {
		state, message = StateFailed, "The suite demo stopped: "+cause.Error()
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE suite_demo SET state = $1, requirements = requirements + $2, links = links + $3, skipped = $4,
			message = $5, finished_at = now()`, state, res.Requirements, res.Links, res.Skipped, message)
		return err
	})
	return err
}
