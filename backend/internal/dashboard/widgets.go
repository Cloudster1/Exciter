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

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// A dashboard's widgets: checked, added, changed, ordered and removed by whoever may change the dashboard.

func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", &FieldError{"title", "Give the widget a title."}
	case utf8.RuneCountInString(title) > MaxTitleLength:
		return "", &FieldError{"title", fmt.Sprintf("Keep the title to %d characters.", MaxTitleLength)}
	}
	return title, nil
}

// checkConfig refuses a widget's settings its kind does not take, and fills in what was left out.
func checkConfig(kind Kind, c Config) (Config, error) {
	if c.Source.ViewID != nil && strings.TrimSpace(c.Source.Query) != "" {
		return c, &FieldError{"config.source", "Read a query or a view, not both."}
	}
	switch kind {
	case Breakdown:
		if c.GroupBy == "" {
			c.GroupBy = ByStatus
		}
		if !slices.Contains(GroupBys, c.GroupBy) {
			return c, &FieldError{"config.groupBy", "Count by status, category, type, owner, priority, label or an attribute."}
		}
		if c.GroupBy == ByAttribute && c.AttributeID == nil {
			return c, &FieldError{"config.attributeId", "Pick the attribute to count by."}
		}
		if c.Shape == "" {
			c.Shape = Bars
		}
		if !slices.Contains(Shapes, c.Shape) {
			return c, &FieldError{"config.shape", "Draw it as bars or a donut."}
		}
	case List:
		if c.Rows == 0 {
			c.Rows = DefaultListRows
		}
		if c.Rows < 1 || c.Rows > MaxListRows {
			return c, &FieldError{"config.rows", fmt.Sprintf("List 1 to %d requirements.", MaxListRows)}
		}
	case Changes:
		if c.Interval == "" {
			c.Interval = Week
		}
		if !slices.Contains(Intervals, c.Interval) {
			return c, &FieldError{"config.interval", "Count per week or per month."}
		}
		if c.Days == 0 {
			c.Days = DefaultDays
		}
		if c.Days < 1 || c.Days > MaxDays {
			return c, &FieldError{"config.days", fmt.Sprintf("Look back 1 to %d days.", MaxDays)}
		}
		if len(c.Series) == 0 {
			c.Series = []Series{Created, Revised}
		}
		for _, s := range c.Series {
			if !slices.Contains(SeriesKinds, s) {
				return c, &FieldError{"config.series", "Count created, revised or statusChanged."}
			}
		}
	}
	return c, nil
}

func checkWidget(in WidgetInput) (WidgetInput, error) {
	info, ok := infoOf(in.Kind)
	if !ok {
		return in, &FieldError{"kind", "Pick a kind of widget the dashboard offers."}
	}
	title, err := checkTitle(in.Title)
	if err != nil {
		return in, err
	}
	in.Title = title
	if in.Width == 0 {
		in.Width = info.Width
	}
	if in.Width < 1 || in.Width > MaxWidth {
		return in, &FieldError{"width", fmt.Sprintf("A widget is 1 to %d columns wide.", MaxWidth)}
	}
	in.Config, err = checkConfig(in.Kind, in.Config)
	return in, err
}

func insertWidget(ctx context.Context, tx db.DBTX, dashboardID uuid.UUID, position int, w WidgetInput) error {
	config, err := json.Marshal(w.Config)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO dashboard_widget (dashboard_id, kind, title, position, width, config) VALUES ($1, $2, $3, $4, $5, $6)`,
		dashboardID, w.Kind, w.Title, position, w.Width, config)
	return refusal(err)
}

// AddWidget puts a widget at the end of a dashboard.
func (s *Service) AddWidget(ctx context.Context, actor, dashboardID uuid.UUID, in WidgetInput) (*Widget, db.LSN, error) {
	in, err := checkWidget(in)
	if err != nil {
		return nil, 0, err
	}
	var out *Widget
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := changing(ctx, tx, dashboardID)
		if err != nil {
			return err
		}
		if len(f.Widgets) >= MaxWidgets {
			return ErrTooManyWidgets
		}
		if err := s.checkSource(ctx, tx, actor, f, in.Config.Source); err != nil {
			return err
		}
		if err := checkAttribute(ctx, tx, f.projectID, in.Kind, in.Config); err != nil {
			return err
		}
		if err := insertWidget(ctx, tx, dashboardID, len(f.Widgets), in); err != nil {
			return err
		}
		if err := touch(ctx, tx, dashboardID); err != nil {
			return err
		}
		g, err := get(ctx, tx, dashboardID)
		if err != nil {
			return err
		}
		w := g.Widgets[len(g.Widgets)-1]
		out = &w
		return nil
	})
	return out, lsn, err
}

// checkSource refuses a widget reading a view the others of a dashboard could not read, or a query that does not hold.
func (s *Service) checkSource(ctx context.Context, tx db.DBTX, reader uuid.UUID, f *found, src Source) error {
	if src.ViewID != nil {
		_, err := search.SharedGridView(ctx, tx, reader, f.projectID, *src.ViewID)
		switch {
		case errors.Is(err, search.ErrViewPrivate):
			return &FieldError{"config.source.viewId", "Share the view first, or write its query here."}
		case errors.Is(err, search.ErrViewNotFound):
			return &FieldError{"config.source.viewId", "That view is not one of this project's grid views. Pick another."}
		}
		return err
	}
	if strings.TrimSpace(src.Query) == "" {
		return nil
	}
	p, err := project.Find(ctx, tx, reader, f.ProjectKey)
	if err != nil {
		return err
	}
	_, err = search.Keep(ctx, tx, reader, p, search.Filter{Query: src.Query}, search.Sort{})
	return err
}

// checkAttribute refuses a breakdown by an attribute the project does not have, or one it cannot count by value, as
// its reader would be refused it.
func checkAttribute(ctx context.Context, tx db.DBTX, projectID uuid.UUID, kind Kind, c Config) error {
	if kind != Breakdown || c.GroupBy != ByAttribute {
		return nil
	}
	defs, err := attribute.Defs(ctx, tx, projectID)
	if err != nil {
		return err
	}
	_, _, err = groupingOf(defs, c)
	return err
}

// widgetOf reads a widget with its dashboard, as the reader sees both.
func widgetOf(ctx context.Context, tx db.DBTX, id uuid.UUID) (*found, *Widget, error) {
	var dashboardID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT dashboard_id FROM dashboard_widget WHERE id = $1`, id).Scan(&dashboardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f, err := get(ctx, tx, dashboardID)
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(f.Widgets, func(w Widget) bool { return w.ID == id })
	if i < 0 {
		return nil, nil, ErrNotFound
	}
	return f, &f.Widgets[i], nil
}

// UpdateWidget changes a widget's title, width or settings.
func (s *Service) UpdateWidget(ctx context.Context, actor, id uuid.UUID, in WidgetPatch) (*Widget, db.LSN, error) {
	var out *Widget
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, w, err := widgetOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := changing(ctx, tx, f.ID); err != nil {
			return err
		}
		next := WidgetInput{Kind: w.Kind, Title: w.Title, Width: w.Width, Config: w.Config}
		if in.Title != nil {
			next.Title = *in.Title
		}
		if in.Width != nil {
			next.Width = *in.Width
			if next.Width == 0 {
				return &FieldError{"width", fmt.Sprintf("A widget is 1 to %d columns wide.", MaxWidth)}
			}
		}
		if in.Config != nil {
			next.Config = *in.Config
			if err := s.checkSource(ctx, tx, actor, f, next.Config.Source); err != nil {
				return err
			}
		}
		if next, err = checkWidget(next); err != nil {
			return err
		}
		if in.Config != nil {
			if err := checkAttribute(ctx, tx, f.projectID, next.Kind, next.Config); err != nil {
				return err
			}
		}
		config, err := json.Marshal(next.Config)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dashboard_widget SET title = $2, width = $3, config = $4 WHERE id = $1`, id, next.Title, next.Width, config); err != nil {
			return refusal(err)
		}
		if err := touch(ctx, tx, f.ID); err != nil {
			return err
		}
		_, w, err = widgetOf(ctx, tx, id)
		out = w
		return err
	})
	return out, lsn, err
}

// DeleteWidget takes a widget off its dashboard and closes the gap it leaves.
func (s *Service) DeleteWidget(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, _, err := widgetOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := changing(ctx, tx, f.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM dashboard_widget WHERE id = $1`, id); err != nil {
			return refusal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE dashboard_widget w SET position = o.n - 1
			FROM (SELECT id, row_number() OVER (ORDER BY position, created_at) AS n FROM dashboard_widget WHERE dashboard_id = $1) o
			WHERE w.id = o.id`, f.ID); err != nil {
			return err
		}
		return touch(ctx, tx, f.ID)
	})
}

// Order lays a dashboard's widgets out in the order given, which names each once.
func (s *Service) Order(ctx context.Context, actor, id uuid.UUID, in OrderInput) (*Dashboard, db.LSN, error) {
	var out *Dashboard
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := changing(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(in.Widgets) != len(f.Widgets) {
			return &FieldError{"widgets", "Name every widget of the dashboard once, in the order wanted."}
		}
		for i, wid := range in.Widgets {
			if !slices.ContainsFunc(f.Widgets, func(w Widget) bool { return w.ID == wid }) || slices.Index(in.Widgets, wid) != i {
				return &FieldError{"widgets", "Name every widget of the dashboard once, in the order wanted."}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE dashboard_widget w SET position = o.n - 1
			FROM unnest($2::uuid[]) WITH ORDINALITY AS o (id, n) WHERE w.id = o.id AND w.dashboard_id = $1`, id, in.Widgets); err != nil {
			return err
		}
		if err := touch(ctx, tx, id); err != nil {
			return err
		}
		g, err := get(ctx, tx, id)
		if g != nil {
			out = &g.Dashboard
		}
		return err
	})
	return out, lsn, err
}
