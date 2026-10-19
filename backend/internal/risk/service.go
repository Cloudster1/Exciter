package risk

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// TopicSchemeSaved is a project's risk scheme set, which changes the level of every risk in it.
const TopicSchemeSaved = "risk_scheme.saved"

var (
	// ErrForbidden refuses setting a scheme to whoever does not administer its project.
	ErrForbidden = errors.New("only the project's administrators set its risk scheme")
	// ErrNotFound is a risk or a project the caller may not view.
	ErrNotFound = errors.New("not found")
	// ErrNotRisk is a requirement asked for its mitigations that is no risk.
	ErrNotRisk = errors.New("the requirement is no risk")
)

// FieldError is input refused on one field, with the sentence to show beside it.
type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// ScaleHeldError refuses taking a value off a scale, or detection off the scheme, while a rating uses it.
type ScaleHeldError struct {
	Dimension Dimension
	// Value is the value held, 0 for detection as a whole.
	Value int
}

func (e *ScaleHeldError) Error() string {
	return fmt.Sprintf("a rating uses %s %d", e.Dimension, e.Value)
}

// Service keeps the projects' risk schemes.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// defaultLabels are the scales' labels when a scheme leaves one out.
var defaultLabels = map[Dimension]string{Severity: "Severity", Probability: "Probability", Detection: "Detection"}

// Scheme reads a project's scheme as its reader sees it; nil until its administrators set one.
func (s *Service) Scheme(ctx context.Context, reader uuid.UUID, projectKey string) (*Scheme, error) {
	var out *Scheme
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = ReadScheme(ctx, tx, p.ID)
		return err
	})
	return out, err
}

// ReadScheme reads a project's scheme within a transaction; nil until one is set.
func ReadScheme(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*Scheme, error) {
	var (
		sc     Scheme
		labels = map[Dimension]string{}
		sev    string
		prob   string
		det    string
		byID   *uuid.UUID
		byName string
	)
	err := tx.QueryRow(ctx, `
		SELECT s.method, s.detection, s.severity_label, s.probability_label, s.detection_label, s.updated_by, COALESCE(u.name, ''), s.updated_at
		FROM risk_scheme s LEFT JOIN app_user u ON u.id = s.updated_by WHERE s.project_id = $1`, projectID).
		Scan(&sc.Method, &sc.Detection, &sev, &prob, &det, &byID, &byName, &sc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the risk scheme: %w", err)
	}
	if byID != nil {
		sc.UpdatedBy.ID, sc.UpdatedBy.Name = *byID, byName
	}
	labels[Severity], labels[Probability], labels[Detection] = sev, prob, det
	dimensions := []Dimension{Severity, Probability}
	if sc.Detection {
		dimensions = append(dimensions, Detection)
	}
	for _, d := range dimensions {
		sc.Scales = append(sc.Scales, Scale{Dimension: d, Label: labels[d], Values: []ScaleValue{}, Held: []int{}})
	}
	rows, err := tx.Query(ctx, `
		SELECT l.dimension, l.value, l.name, l.description, risk_scale_used(l.project_id, l.dimension, l.value)
		FROM risk_scale_level l WHERE l.project_id = $1 ORDER BY l.dimension, l.value`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the risk scales: %w", err)
	}
	for rows.Next() {
		var (
			d    Dimension
			v    ScaleValue
			held bool
		)
		if err := rows.Scan(&d, &v.Value, &v.Name, &v.Description, &held); err != nil {
			rows.Close()
			return nil, err
		}
		for i := range sc.Scales {
			if sc.Scales[i].Dimension == d {
				sc.Scales[i].Values = append(sc.Scales[i].Values, v)
				if held {
					sc.Scales[i].Held = append(sc.Scales[i].Held, v.Value)
				}
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if sc.Levels, err = readLevels(ctx, tx, projectID); err != nil {
		return nil, err
	}
	sc.Matrix = []MatrixCell{}
	rows, err = tx.Query(ctx, `
		SELECT c.severity, c.probability, l.name FROM risk_matrix_cell c JOIN risk_level l ON l.id = c.level_id
		WHERE c.project_id = $1 ORDER BY c.probability, c.severity`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the risk matrix: %w", err)
	}
	sc.Matrix, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (MatrixCell, error) {
		var c MatrixCell
		return c, row.Scan(&c.Severity, &c.Probability, &c.Level)
	})
	return &sc, err
}

func readLevels(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]Level, error) {
	rows, err := tx.Query(ctx, `SELECT name, colour, acceptable, min_rpn FROM risk_level WHERE project_id = $1 ORDER BY position`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the risk levels: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Level, error) {
		var l Level
		return l, row.Scan(&l.Name, &l.Colour, &l.Acceptable, &l.MinRPN)
	})
}

// SaveScheme sets a project's scheme whole, for its administrators while it is open. A value a rating uses stays.
func (s *Service) SaveScheme(ctx context.Context, actor uuid.UUID, projectKey string, in SchemeInput) (*Scheme, db.LSN, error) {
	in, err := CheckScheme(in)
	if err != nil {
		return nil, 0, err
	}
	var out *Scheme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		switch {
		case err != nil:
			return err
		case p.ArchivedAt != nil:
			return project.ErrArchived
		case !p.Can.Administer:
			return ErrForbidden
		}
		before, err := ReadScheme(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if err := heldValues(ctx, tx, p.ID, before, in); err != nil {
			return err
		}
		labels := map[Dimension]string{Severity: defaultLabels[Severity], Probability: defaultLabels[Probability], Detection: defaultLabels[Detection]}
		if before != nil {
			for _, sc := range before.Scales {
				labels[sc.Dimension] = sc.Label
			}
		}
		for _, sc := range in.Scales {
			labels[sc.Dimension] = sc.Label
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO risk_scheme (project_id, method, detection, severity_label, probability_label, detection_label, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (project_id) DO UPDATE
			SET method = EXCLUDED.method, detection = EXCLUDED.detection, severity_label = EXCLUDED.severity_label,
			    probability_label = EXCLUDED.probability_label, detection_label = EXCLUDED.detection_label,
			    updated_by = EXCLUDED.updated_by, updated_at = now()`,
			p.ID, in.Method, in.Detection, labels[Severity], labels[Probability], labels[Detection], actor); err != nil {
			return schemeRefusal(err)
		}
		if err := writeScales(ctx, tx, p.ID, in); err != nil {
			return err
		}
		if err := writeLevels(ctx, tx, p.ID, in); err != nil {
			return err
		}
		if out, err = ReadScheme(ctx, tx, p.ID); err != nil {
			return err
		}
		return noteSaved(ctx, tx, actor, p, out)
	})
	return out, lsn, err
}

// heldValues refuses, by name, a value or detection a rating uses that the new scheme would take away.
func heldValues(ctx context.Context, tx db.DBTX, projectID uuid.UUID, before *Scheme, in SchemeInput) error {
	if before == nil {
		return nil
	}
	if before.Detection && !in.Detection {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT risk_scale_used($1, 'detection', NULL)`, projectID).Scan(&used); err != nil {
			return err
		}
		if used {
			return &ScaleHeldError{Dimension: Detection}
		}
	}
	kept := map[Dimension][]int{}
	for _, sc := range in.Scales {
		for _, v := range sc.Values {
			kept[sc.Dimension] = append(kept[sc.Dimension], v.Value)
		}
	}
	for _, sc := range before.Scales {
		for _, v := range sc.Held {
			if !slices.Contains(kept[sc.Dimension], v) && (sc.Dimension != Detection || in.Detection) {
				return &ScaleHeldError{Dimension: sc.Dimension, Value: v}
			}
		}
	}
	return nil
}

// writeScales keeps each value of the new scales, renamed as given, and takes away the others.
func writeScales(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in SchemeInput) error {
	var dims []string
	var values []int
	for _, sc := range in.Scales {
		for _, v := range sc.Values {
			if _, err := tx.Exec(ctx, `
				INSERT INTO risk_scale_level (project_id, dimension, value, name, description) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (project_id, dimension, value) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description`,
				projectID, sc.Dimension, v.Value, v.Name, v.Description); err != nil {
				return schemeRefusal(err)
			}
			dims, values = append(dims, string(sc.Dimension)), append(values, v.Value)
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM risk_scale_level l WHERE l.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM unnest($2::text[], $3::integer[]) k(dimension, value) WHERE k.dimension = l.dimension AND k.value = l.value)`,
		projectID, dims, values); err != nil {
		return schemeRefusal(err)
	}
	return nil
}

// writeLevels lays the levels and the matrix anew: a level is never stored with a rating, so nothing points at them.
func writeLevels(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in SchemeInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM risk_level WHERE project_id = $1`, projectID); err != nil {
		return schemeRefusal(err)
	}
	ids := map[string]uuid.UUID{}
	for i, l := range in.Levels {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO risk_level (project_id, position, name, colour, acceptable, min_rpn) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			projectID, i, l.Name, l.Colour, l.Acceptable, l.MinRPN).Scan(&id); err != nil {
			return schemeRefusal(err)
		}
		ids[strings.ToLower(l.Name)] = id
	}
	for _, c := range in.Matrix {
		if _, err := tx.Exec(ctx, `INSERT INTO risk_matrix_cell (project_id, severity, probability, level_id) VALUES ($1, $2, $3, $4)`,
			projectID, c.Severity, c.Probability, ids[strings.ToLower(c.Level)]); err != nil {
			return schemeRefusal(err)
		}
	}
	return nil
}

// schemeRefusal says what the database refused of a scheme.
func schemeRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "risk_scale_held" {
		return &ScaleHeldError{}
	}
	return err
}

// CheckScheme settles a scheme as administrators send it, or refuses it on the first field that is wrong.
func CheckScheme(in SchemeInput) (SchemeInput, error) {
	if !slices.Contains(Methods, in.Method) {
		return in, &FieldError{"method", "Give a level by a matrix of severity and probability, or by a priority number."}
	}
	out := SchemeInput{Method: in.Method, Detection: in.Detection}
	wanted := []Dimension{Severity, Probability}
	if in.Detection {
		wanted = append(wanted, Detection)
	}
	byDim := map[Dimension]ScaleInput{}
	for i, sc := range in.Scales {
		field := fmt.Sprintf("scales[%d]", i)
		if !slices.Contains(wanted, sc.Dimension) {
			return in, &FieldError{field + ".dimension", "A scheme rates severity, probability and, when it is on, detection, each once."}
		}
		if _, twice := byDim[sc.Dimension]; twice {
			return in, &FieldError{field + ".dimension", "Each scale comes once. Merge the two."}
		}
		checked, err := checkScale(field, sc)
		if err != nil {
			return in, err
		}
		byDim[sc.Dimension] = checked
	}
	for _, d := range wanted {
		sc, ok := byDim[d]
		if !ok {
			return in, &FieldError{"scales", fmt.Sprintf("The scheme has no %s scale. Give it its values.", d)}
		}
		out.Scales = append(out.Scales, sc)
	}
	levels, err := checkLevels(in.Method, in.Levels)
	if err != nil {
		return in, err
	}
	out.Levels = levels
	if in.Method == MethodMatrix {
		if out.Matrix, err = checkMatrix(in.Matrix, len(byDim[Severity].Values), len(byDim[Probability].Values), levels); err != nil {
			return in, err
		}
	} else {
		out.Matrix = []MatrixCell{}
	}
	return out, nil
}

func checkScale(field string, sc ScaleInput) (ScaleInput, error) {
	out := ScaleInput{Dimension: sc.Dimension, Label: strings.TrimSpace(sc.Label)}
	if out.Label == "" {
		out.Label = defaultLabels[sc.Dimension]
	}
	if utf8.RuneCountInString(out.Label) > MaxLabelLength {
		return out, &FieldError{field + ".label", fmt.Sprintf("A scale's label is at most %d characters. Shorten it.", MaxLabelLength)}
	}
	if len(sc.Values) == 0 || len(sc.Values) > MaxScaleValue {
		return out, &FieldError{field + ".values", fmt.Sprintf("A scale has from 1 to %d values. Add or remove some.", MaxScaleValue)}
	}
	values := slices.Clone(sc.Values)
	slices.SortFunc(values, func(a, b ScaleValue) int { return a.Value - b.Value })
	for i, v := range values {
		vf := fmt.Sprintf("%s.values[%d]", field, i)
		if v.Value != i+1 {
			return out, &FieldError{vf + ".value", "A scale's values run from 1 up without a gap. Number them again."}
		}
		v.Name, v.Description = strings.TrimSpace(v.Name), strings.TrimSpace(v.Description)
		if v.Name == "" || utf8.RuneCountInString(v.Name) > MaxNameLength {
			return out, &FieldError{vf + ".name", fmt.Sprintf("Name each value, in at most %d characters.", MaxNameLength)}
		}
		if utf8.RuneCountInString(v.Description) > MaxDescriptionLength {
			return out, &FieldError{vf + ".description", fmt.Sprintf("A value's description is at most %d characters. Shorten it.", MaxDescriptionLength)}
		}
		values[i] = v
	}
	out.Values = values
	return out, nil
}

// colourShape is a colour as the database takes it, as a type's colour (#14).
var colourShape = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func checkLevels(method Method, in []Level) ([]Level, error) {
	if len(in) == 0 || len(in) > MaxLevels {
		return nil, &FieldError{"levels", fmt.Sprintf("A scheme has from 1 to %d levels. Add or remove some.", MaxLevels)}
	}
	out := make([]Level, len(in))
	seen := map[string]bool{}
	last := 0
	for i, l := range in {
		field := fmt.Sprintf("levels[%d]", i)
		l.Name, l.Colour = strings.TrimSpace(l.Name), strings.ToLower(strings.TrimSpace(l.Colour))
		if l.Name == "" || utf8.RuneCountInString(l.Name) > MaxNameLength {
			return nil, &FieldError{field + ".name", fmt.Sprintf("Name each level, in at most %d characters.", MaxNameLength)}
		}
		if seen[strings.ToLower(l.Name)] {
			return nil, &FieldError{field + ".name", "Two levels have this name. Name each its own."}
		}
		seen[strings.ToLower(l.Name)] = true
		if !colourShape.MatchString(l.Colour) {
			return nil, &FieldError{field + ".colour", "Give the level a colour such as #d97706."}
		}
		switch {
		case method == MethodMatrix || i == 0:
			l.MinRPN = nil
		case l.MinRPN == nil || *l.MinRPN <= last || *l.MinRPN > MaxScaleValue*MaxScaleValue*MaxScaleValue:
			return nil, &FieldError{field + ".minRpn", "Give each level after the first the priority number it starts at, higher than the level before."}
		default:
			last = *l.MinRPN
		}
		out[i] = l
	}
	return out, nil
}

func checkMatrix(in []MatrixCell, severities, probabilities int, levels []Level) ([]MatrixCell, error) {
	names := map[string]string{}
	for _, l := range levels {
		names[strings.ToLower(l.Name)] = l.Name
	}
	seen := map[[2]int]bool{}
	out := make([]MatrixCell, 0, len(in))
	for i, c := range in {
		field := fmt.Sprintf("matrix[%d]", i)
		if c.Severity < 1 || c.Severity > severities || c.Probability < 1 || c.Probability > probabilities {
			return nil, &FieldError{field, "A cell is of a severity and a probability the scales have."}
		}
		if seen[[2]int{c.Severity, c.Probability}] {
			return nil, &FieldError{field, "A cell comes once. Keep one of the two."}
		}
		seen[[2]int{c.Severity, c.Probability}] = true
		name, ok := names[strings.ToLower(strings.TrimSpace(c.Level))]
		if !ok {
			return nil, &FieldError{field + ".level", "Name one of the scheme's levels for the cell."}
		}
		out = append(out, MatrixCell{Severity: c.Severity, Probability: c.Probability, Level: name})
	}
	if len(out) != severities*probabilities {
		return nil, &FieldError{"matrix", "Give every cell of severity and probability its level."}
	}
	return out, nil
}

// noteSaved audits a scheme set and tells whoever listens that every level in the project may have moved.
func noteSaved(ctx context.Context, tx db.DBTX, actor uuid.UUID, p *project.Project, sc *Scheme) error {
	levels := make([]string, len(sc.Levels))
	for i, l := range sc.Levels {
		levels[i] = l.Name
	}
	id := p.ID
	if err := audit.Write(ctx, tx, audit.Entry{
		Action: audit.ActionRiskSchemeSaved, TargetType: "project", TargetID: &id, Actor: actor,
		Data: map[string]any{"projectKey": p.Key, "method": sc.Method, "detection": sc.Detection, "levels": levels},
	}); err != nil {
		return err
	}
	return events.Emit(ctx, tx, TopicSchemeSaved, map[string]any{"actorId": actor, "projectId": p.ID})
}
