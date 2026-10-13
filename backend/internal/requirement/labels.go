package requirement

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/label"
)

// checkLabels normalizes the labels a new requirement is written with, each once.
func checkLabels(raw []string) ([]string, error) {
	var names []string
	for _, each := range raw {
		name, err := label.Field("labels", each)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) > label.MaxPerRequirement {
		return nil, &FieldError{"labels", fmt.Sprintf("A requirement carries at most %d labels. Leave some out.", label.MaxPerRequirement)}
	}
	return names, nil
}

// putLabels puts names on the requirement id in the actor's name; the policies hold them to editing it.
func putLabels(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, names []string) error {
	if len(names) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO requirement_label (org_id, requirement_id, name, created_by)
		SELECT current_org_id(), $1, n, $3 FROM unnest($2::text[]) AS n
		ON CONFLICT DO NOTHING`, id, names, actor); err != nil {
		return fmt.Errorf("label the requirement: %w", err)
	}
	return nil
}

// Labels answers the labels on a requirement, by name.
func (s *Service) Labels(ctx context.Context, reader uuid.UUID, key string) ([]string, error) {
	var out []string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err == nil {
			out = l.Labels
		}
		return err
	})
	return out, err
}

// relabelling reads a requirement whose labels the actor changes, held so two tabs cannot pass the limit together.
func relabelling(ctx context.Context, tx db.DBTX, actor uuid.UUID, key string) (*loaded, error) {
	l, err := changing(ctx, tx, actor, key)
	if err != nil {
		return nil, err
	}
	if l.TrashedAt != nil {
		return nil, ErrTrashed
	}
	return l, nil
}

// relabelled logs a change of labels, which writes no revision, and marks who changed the requirement last.
func relabelled(ctx context.Context, tx db.DBTX, actor uuid.UUID, l *loaded, after []string) error {
	if _, err := tx.Exec(ctx, `UPDATE requirement SET updated_by = $2 WHERE id = $1`, l.ID, actor); err != nil {
		return fmt.Errorf("relabel %s: %w", l.Key, err)
	}
	return logChange(ctx, tx, actor, l.ID, ChangeLabels, strings.Join(l.Labels, ", "), strings.Join(after, ", "))
}

// AddLabel puts a label on a requirement, coining it if nobody uses it yet, and answers its labels; one it
// carries already is no change.
func (s *Service) AddLabel(ctx context.Context, actor uuid.UUID, key, raw string) ([]string, db.LSN, error) {
	name, err := label.Field("name", raw)
	if err != nil {
		return nil, 0, err
	}
	var out []string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := relabelling(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if slices.Contains(l.Labels, name) {
			out = l.Labels
			return nil
		}
		if len(l.Labels) >= label.MaxPerRequirement {
			return &FieldError{"name", fmt.Sprintf("A requirement carries at most %d labels. Remove one first.", label.MaxPerRequirement)}
		}
		if err := putLabels(ctx, tx, actor, l.ID, []string{name}); err != nil {
			return err
		}
		out = append(slices.Clone(l.Labels), name)
		slices.Sort(out)
		return relabelled(ctx, tx, actor, l, out)
	})
	return out, lsn, err
}

// RemoveLabel takes a label off a requirement; one it does not carry is no change.
func (s *Service) RemoveLabel(ctx context.Context, actor uuid.UUID, key, raw string) (db.LSN, error) {
	name, err := label.Field("labelName", raw)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := relabelling(ctx, tx, actor, key)
		if err != nil || !slices.Contains(l.Labels, name) {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM requirement_label WHERE org_id = current_org_id() AND requirement_id = $1 AND name = $2`,
			l.ID, name); err != nil {
			return fmt.Errorf("remove the label: %w", err)
		}
		return relabelled(ctx, tx, actor, l, slices.DeleteFunc(slices.Clone(l.Labels), func(each string) bool { return each == name }))
	})
}

// copyLabels gives a copy its original's labels.
func copyLabels(ctx context.Context, tx db.DBTX, actor uuid.UUID, from *loaded, to uuid.UUID) error {
	return putLabels(ctx, tx, actor, to, from.Labels)
}
