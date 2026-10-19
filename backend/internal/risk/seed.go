package risk

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// SchemeOf is the scheme a template's risk spec sets (#261).
func SchemeOf(spec project.RiskSpec) SchemeInput {
	in := SchemeInput{Method: Method(spec.Method), Detection: spec.Detection, Matrix: []MatrixCell{}}
	for _, sc := range spec.Scales {
		values := make([]ScaleValue, len(sc.Names))
		for i, name := range sc.Names {
			values[i] = ScaleValue{Value: i + 1, Name: name}
		}
		in.Scales = append(in.Scales, ScaleInput{Dimension: Dimension(sc.Dimension), Label: sc.Label, Values: values})
	}
	for _, l := range spec.Levels {
		in.Levels = append(in.Levels, Level{Name: l.Name, Colour: l.Colour, Acceptable: l.Acceptable, MinRPN: l.MinRPN})
	}
	if spec.Level != nil && len(spec.Scales) >= 2 {
		for s := 1; s <= len(spec.Scales[0].Names); s++ {
			for p := 1; p <= len(spec.Scales[1].Names); p++ {
				in.Matrix = append(in.Matrix, MatrixCell{Severity: s, Probability: p, Level: spec.Level(s, p)})
			}
		}
	}
	return in
}

// SeedProject makes what a template holds in a project just made, in the transaction that made it: what
// requirement.Seed makes, and the template's risk scheme.
func SeedProject(ctx context.Context, tx db.DBTX, actor, projectID uuid.UUID, b project.Blueprint) error {
	if err := requirement.Seed(ctx, tx, actor, projectID, b); err != nil {
		return err
	}
	if b.Risk == nil {
		return nil
	}
	in, err := CheckScheme(SchemeOf(*b.Risk))
	if err != nil {
		return fmt.Errorf("the template %s sets a scheme that is refused: %w", b.Key, err)
	}
	labels := map[Dimension]string{Severity: defaultLabels[Severity], Probability: defaultLabels[Probability], Detection: defaultLabels[Detection]}
	for _, sc := range in.Scales {
		labels[sc.Dimension] = sc.Label
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO risk_scheme (project_id, method, detection, severity_label, probability_label, detection_label, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, projectID, in.Method, in.Detection, labels[Severity], labels[Probability], labels[Detection], actor); err != nil {
		return fmt.Errorf("set the template's risk scheme: %w", err)
	}
	if err := writeScales(ctx, tx, projectID, in); err != nil {
		return err
	}
	return writeLevels(ctx, tx, projectID, in)
}
