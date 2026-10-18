package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

var (
	ErrNotFound = errors.New("dashboard or widget not found")
	// ErrForbidden is sharing without edit, or changing a shared dashboard without administering its project.
	ErrForbidden = errors.New("only a dashboard's owner, or once shared the project's administrators, change it")
	// ErrTooManyDashboards and ErrTooManyWidgets are past MaxDashboards and MaxWidgets.
	ErrTooManyDashboards = errors.New("too many dashboards")
	ErrTooManyWidgets    = errors.New("too many widgets")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// infos describe each kind for the chooser, in Kinds' order.
var infos = []KindInfo{
	{Breakdown, "Breakdown", "Requirements counted by status, category, type, owner, priority, label or an attribute.", 1},
	{Count, "Count", "How many requirements the query keeps.", 1},
	{List, "List", "The first requirements the query keeps, with the grid's columns.", 2},
	{Coverage, "Coverage", "How far the requirements are decomposed, implemented and verified.", 2},
	{Review, "Review", "How many are approved, waiting for review, outdated or never reviewed.", 1},
	{Suspects, "Suspect links", "How many have suspect links, and those with most.", 1},
	{Changes, "Changes", "Requirements created, revised and moved to another status, per week or month.", 2},
	{Quality, "Quality", "How the requirements score against the writing rules.", 1},
}

// KindInfos describes every kind of widget.
func KindInfos() []KindInfo { return slices.Clone(infos) }

func infoOf(k Kind) (KindInfo, bool) {
	i := slices.IndexFunc(infos, func(info KindInfo) bool { return info.Kind == k })
	if i < 0 {
		return KindInfo{}, false
	}
	return infos[i], true
}

// Service keeps dashboards and reads their widgets as the reader.
type Service struct {
	db     *db.Cluster
	search *search.Service
	trace  *trace.Service
}

func NewService(cluster *db.Cluster, s *search.Service, t *trace.Service) *Service {
	return &Service{db: cluster, search: s, trace: t}
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", &FieldError{"name", "Give the dashboard a name."}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", &FieldError{"name", fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)}
	}
	return name, nil
}

// editable reads the project a dashboard change is made in, refusing an archived one.
func editable(ctx context.Context, tx db.DBTX, reader uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, reader, projectKey)
	if err != nil {
		return nil, err
	}
	if p.ArchivedAt != nil {
		return nil, project.ErrArchived
	}
	return p, nil
}

const selectDashboard = `
SELECT d.id, p.key, d.name, d.shared, d.owner_id, COALESCE(u.name, ''), COALESCE(p.settings ->> 'homeDashboardId', '') = d.id::text,
       dashboard_kept(d.id), perm_project_holds(d.project_id, current_actor_id(), 'edit'), d.updated_at, d.created_at, d.project_id
FROM dashboard d
JOIN project p ON p.id = d.project_id
LEFT JOIN app_user u ON u.id = d.owner_id`

type found struct {
	Dashboard
	projectID uuid.UUID
}

func scanDashboard(row pgx.Row) (*found, error) {
	var (
		f    found
		edit bool
	)
	err := row.Scan(&f.ID, &f.ProjectKey, &f.Name, &f.Shared, &f.Owner.ID, &f.Owner.Name, &f.Home, &f.Can.Edit, &edit, &f.UpdatedAt, &f.CreatedAt, &f.projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	f.Can.Share = f.Can.Edit && edit
	f.Widgets = []Widget{}
	return &f, nil
}

func get(ctx context.Context, tx db.DBTX, id uuid.UUID) (*found, error) {
	f, err := scanDashboard(tx.QueryRow(ctx, selectDashboard+` WHERE d.id = $1`, id))
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, kind, title, width, position, config FROM dashboard_widget WHERE dashboard_id = $1 ORDER BY position, created_at`, id)
	if err != nil {
		return nil, err
	}
	f.Widgets, err = pgx.CollectRows(rows, scanWidget)
	return f, err
}

func scanWidget(row pgx.CollectableRow) (Widget, error) {
	var (
		w      Widget
		config []byte
	)
	if err := row.Scan(&w.ID, &w.Kind, &w.Title, &w.Width, &w.Position, &config); err != nil {
		return w, err
	}
	return w, json.Unmarshal(config, &w.Config)
}

// Dashboards answers the project's dashboards the reader may read: their own and the shared ones, by name.
func (s *Service) Dashboards(ctx context.Context, reader uuid.UUID, projectKey string) ([]Summary, error) {
	out := []Summary{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectDashboard+` WHERE d.project_id = $1 ORDER BY d.owner_id <> current_actor_id(), lower(d.name)`, p.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanDashboard(rows)
			if err != nil {
				return err
			}
			out = append(out, f.Summary)
		}
		return rows.Err()
	})
	return out, err
}

// Dashboard answers one dashboard and its widgets.
func (s *Service) Dashboard(ctx context.Context, reader, id uuid.UUID) (*Dashboard, error) {
	var out *Dashboard
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := get(ctx, tx, id)
		if f != nil {
			out = &f.Dashboard
		}
		return err
	})
	return out, err
}

func refusal(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return &FieldError{"name", "You have a dashboard of this name in the project already. Pick another name."}
	case errors.As(err, &pgErr) && pgErr.Code == "42501":
		return ErrForbidden
	}
	return err
}

// Create makes a dashboard for the reader, empty or from a template.
func (s *Service) Create(ctx context.Context, actor uuid.UUID, projectKey string, in Input) (*Dashboard, db.LSN, error) {
	name, err := checkName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	var widgets []WidgetInput
	if in.Template != "" {
		t, ok := templateOf(in.Template)
		if !ok {
			return nil, 0, &FieldError{"template", "There is no such template. Pick one the dashboard offers."}
		}
		widgets = t.Widgets
	}
	var out *Dashboard
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := editable(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if in.Shared && !p.Can.Edit {
			return ErrForbidden
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM dashboard WHERE project_id = $1 AND owner_id = $2`, p.ID, actor).Scan(&count); err != nil {
			return err
		}
		if count >= MaxDashboards {
			return ErrTooManyDashboards
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO dashboard (project_id, owner_id, name, shared) VALUES ($1, $2, $3, $4) RETURNING id`,
			p.ID, actor, name, in.Shared).Scan(&id); err != nil {
			return refusal(err)
		}
		for i, w := range widgets {
			if w, err = checkWidget(w); err != nil {
				return err
			}
			if err := insertWidget(ctx, tx, id, i, w); err != nil {
				return err
			}
		}
		if in.Shared {
			if err := note(ctx, tx, actor, audit.ActionDashboardCreated, id, p.Key, name); err != nil {
				return err
			}
		}
		f, err := get(ctx, tx, id)
		if f != nil {
			out = &f.Dashboard
		}
		return err
	})
	return out, lsn, err
}

func note(ctx context.Context, tx db.DBTX, actor uuid.UUID, action string, id uuid.UUID, projectKey, name string) error {
	return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "dashboard", TargetID: &id, Actor: actor,
		Data: map[string]any{"project": projectKey, "name": name}})
}

// changing reads a dashboard to change, refusing whoever may not.
func changing(ctx context.Context, tx db.DBTX, id uuid.UUID) (*found, error) {
	f, err := get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var archived bool
	if err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM project WHERE id = $1`, f.projectID).Scan(&archived); err != nil {
		return nil, err
	}
	if archived {
		return nil, project.ErrArchived
	}
	if !f.Can.Edit {
		return nil, ErrForbidden
	}
	return f, nil
}

// touch marks a dashboard changed when its widgets are.
func touch(ctx context.Context, tx db.DBTX, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE dashboard SET name = name WHERE id = $1`, id)
	return err
}

// Update renames a dashboard or shares it.
func (s *Service) Update(ctx context.Context, actor, id uuid.UUID, in Patch) (*Dashboard, db.LSN, error) {
	var out *Dashboard
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := changing(ctx, tx, id)
		if err != nil {
			return err
		}
		name, shared := f.Name, f.Shared
		if in.Name != nil {
			if name, err = checkName(*in.Name); err != nil {
				return err
			}
		}
		if in.Shared != nil {
			if *in.Shared && !f.Shared && !f.Can.Share {
				return ErrForbidden
			}
			shared = *in.Shared
		}
		if _, err := tx.Exec(ctx, `UPDATE dashboard SET name = $2, shared = $3 WHERE id = $1`, id, name, shared); err != nil {
			return refusal(err)
		}
		if shared && !f.Shared {
			if err := note(ctx, tx, actor, audit.ActionDashboardShared, id, f.ProjectKey, name); err != nil {
				return err
			}
		}
		if !shared && f.Home {
			if err := unhome(ctx, tx, f.projectID); err != nil {
				return err
			}
		}
		g, err := get(ctx, tx, id)
		if g != nil {
			out = &g.Dashboard
		}
		return err
	})
	return out, lsn, err
}

// unhome clears a project's home dashboard, as its administrator or whoever took the dashboard away from it.
func unhome(ctx context.Context, tx db.DBTX, projectID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT project_clear_home_dashboard($1)`, projectID)
	return err
}

// Delete removes a dashboard and its widgets.
func (s *Service) Delete(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := changing(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM dashboard WHERE id = $1`, id); err != nil {
			return refusal(err)
		}
		if f.Home {
			if err := unhome(ctx, tx, f.projectID); err != nil {
				return err
			}
		}
		if f.Shared {
			return note(ctx, tx, actor, audit.ActionDashboardDeleted, id, f.ProjectKey, f.Name)
		}
		return nil
	})
}
