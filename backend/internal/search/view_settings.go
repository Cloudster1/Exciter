package search

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// What a matrix or chain view's settings may say (#152), in the words of internal/trace's parameters; this package
// sits below internal/trace, which checks them again as the view opens.
var (
	viewColumns    = []string{"", "requirements", "issues", "pages"}
	viewDirections = []string{"", "both", "outgoing", "incoming"}
	viewGaps       = []string{"", "rows", "columns", "both"}
	viewRelations  = []string{"implements", "verifies", "relates", "specifies", "describes"}
)

// MaxViewLevels is trace.MaxLevels, the levels a chain view can name; a test of the API's keeps the two equal.
const MaxViewLevels = 6

// checkSettings holds a view's settings and columns to its kind: a grid keeps columns, a matrix or a chain settings.
func checkSettings(kind ViewKind, columns []string, s ViewSettings) error {
	if !slices.Contains(ViewKinds, kind) {
		return &FieldError{"kind", "A view is of the grid, the matrix or the chains."}
	}
	matrix := s.Columns != "" || s.ColumnProject != "" || len(s.LinkTypes) > 0 || s.Direction != "" || len(s.Relations) > 0 ||
		s.ColumnQuery != "" || len(s.ColumnTypes) > 0 || s.Gaps != ""
	chain := len(s.Levels) > 0 || s.OnlyBroken
	switch {
	case kind == ViewGrid && (matrix || chain):
		return &FieldError{"settings", "A grid view keeps its columns, not a matrix's or a chain's settings."}
	case kind != ViewGrid && len(columns) > 0:
		return &FieldError{"columns", "A matrix or chain view keeps settings, not the grid's columns."}
	case kind == ViewMatrix && chain:
		return &FieldError{"settings", "A matrix view keeps no levels. Save them in a chain view."}
	case kind == ViewChain && matrix:
		return &FieldError{"settings", "A chain view keeps only its levels and whether to show broken chains."}
	case !slices.Contains(viewColumns, s.Columns):
		return &FieldError{"settings", "Show requirements, issues or pages as the columns."}
	case !slices.Contains(viewDirections, s.Direction):
		return &FieldError{"settings", "Count the links that point outgoing, incoming or both."}
	case !slices.Contains(viewGaps, s.Gaps):
		return &FieldError{"settings", "Keep the gaps in rows, columns or both."}
	}
	for _, r := range s.Relations {
		if !slices.Contains(viewRelations, r) {
			return &FieldError{"settings", fmt.Sprintf("There is no relation %q. Pick one of %s.", r, strings.Join(viewRelations, ", "))}
		}
	}
	for _, at := range s.Levels {
		if at < 0 || at >= MaxViewLevels {
			return &FieldError{"settings", "A chain view names levels 0 to " + strconv.Itoa(MaxViewLevels-1) + "."}
		}
	}
	return nil
}

// dropLost leaves out of a view what its settings name that the project no longer has, noting each in Dropped: a
// column project gone from sight, link or column types gone, levels past the trace model's last.
func dropLost(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, v *View) error {
	s := &v.Settings
	switch v.Kind {
	case ViewMatrix:
		columns := p
		if s.ColumnProject != "" && !strings.EqualFold(s.ColumnProject, p.Key) {
			other, err := project.Find(ctx, tx, reader, s.ColumnProject)
			if err != nil {
				v.Dropped = append(v.Dropped, "columnProject:"+s.ColumnProject)
				s.ColumnProject = ""
			} else {
				columns = other
			}
		}
		if len(s.LinkTypes) > 0 {
			var known []string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(key), '{}') FROM link_type
				WHERE key = ANY ($1) AND (project_id IS NULL OR project_id = ANY ($2))`, s.LinkTypes, []uuid.UUID{p.ID, columns.ID}).Scan(&known); err != nil {
				return fmt.Errorf("read a view's link types: %w", err)
			}
			s.LinkTypes = keepKnown(s.LinkTypes, known, "linkType:", &v.Dropped)
		}
		if len(s.ColumnTypes) > 0 {
			var known []uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM requirement_type WHERE id = ANY ($1) AND project_id = $2 AND NOT retired`,
				s.ColumnTypes, columns.ID).Scan(&known); err != nil {
				return fmt.Errorf("read a view's column types: %w", err)
			}
			s.ColumnTypes = keepKnown(s.ColumnTypes, known, "columnType:", &v.Dropped)
		}
	case ViewChain:
		if len(s.Levels) > 0 {
			var levels int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM trace_level WHERE project_id = $1`, p.ID).Scan(&levels); err != nil {
				return fmt.Errorf("read a view's trace levels: %w", err)
			}
			kept := s.Levels[:0]
			for _, at := range s.Levels {
				if at < levels {
					kept = append(kept, at)
				} else {
					v.Dropped = append(v.Dropped, "level:"+strconv.Itoa(at))
				}
			}
			s.Levels = kept
		}
	}
	return nil
}

// keepKnown is what of named is known, each one dropped noted under its prefix.
func keepKnown[T string | uuid.UUID](named, known []T, prefix string, dropped *[]string) []T {
	kept := []T{}
	for _, each := range named {
		if slices.Contains(known, each) {
			kept = append(kept, each)
		} else {
			*dropped = append(*dropped, prefix+fmt.Sprint(each))
		}
	}
	return kept
}
