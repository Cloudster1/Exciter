package label

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// Service finds labels and the requirements that carry them, across the projects the reader may view. Putting
// one on and taking it off belong to the requirement, which decides who may.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// likePrefix matches what starts with typed, taken literally.
func likePrefix(typed string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(typed) + "%"
}

// typedPrefix is what somebody typed so far, read the way Normalize reads a name.
func typedPrefix(typed string) string {
	prefix := strings.Join(strings.Fields(strings.ToLower(typed)), "-")
	if strings.HasSuffix(typed, " ") && prefix != "" {
		prefix += "-"
	}
	return prefix
}

// within narrows to one project when projectKey names one the reader may view, as the next parameter of args.
func within(ctx context.Context, tx db.DBTX, reader uuid.UUID, projectKey string, args []any) (string, []any, error) {
	if projectKey = strings.TrimSpace(projectKey); projectKey == "" {
		return "", args, nil
	}
	p, err := project.Find(ctx, tx, reader, projectKey)
	if err != nil {
		return "", nil, err
	}
	args = append(args, p.ID)
	return ` AND r.project_id = $` + strconv.Itoa(len(args)), args, nil
}

// Suggest offers the labels on requirements the reader may view, out of the trash, that start with what was
// typed, the most used first.
func (s *Service) Suggest(ctx context.Context, reader uuid.UUID, typed, projectKey string, limit int) ([]Suggestion, error) {
	out := []Suggestion{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		narrow, args, err := within(ctx, tx, reader, projectKey, []any{reader, likePrefix(typedPrefix(typed)), limit})
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT l.name, count(*)::int
			FROM requirement_label l
			JOIN requirement r ON r.org_id = l.org_id AND r.id = l.requirement_id
			WHERE l.org_id = current_org_id() AND l.name LIKE $2 AND r.trashed_at IS NULL AND `+perm.ViewableRequirement("r", 1)+narrow+`
			GROUP BY l.name
			ORDER BY count(*) DESC, l.name
			LIMIT $3`, args...)
		if err != nil {
			return fmt.Errorf("suggest labels: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Suggestion])
		return err
	})
	return out, err
}

// Requirements lists the requirements out of the trash that carry a label and that the reader may view, by
// project and number, with how many there are; projectKey narrows them.
func (s *Service) Requirements(ctx context.Context, reader uuid.UUID, raw, projectKey string, limit, offset int) ([]Labelled, int, error) {
	name, err := Field("labelName", raw)
	if err != nil {
		return nil, 0, err
	}
	out := []Labelled{}
	total := 0
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		narrow, args, err := within(ctx, tx, reader, projectKey, []any{reader, name})
		if err != nil {
			return err
		}
		where := `
			FROM requirement_label l
			JOIN requirement r ON r.org_id = l.org_id AND r.id = l.requirement_id
			JOIN project p ON p.org_id = r.org_id AND p.id = r.project_id
			LEFT JOIN app_user u ON u.id = r.updated_by
			WHERE l.org_id = current_org_id() AND l.name = $2 AND r.trashed_at IS NULL AND ` + perm.ViewableRequirement("r", 1) + narrow
		if err := tx.QueryRow(ctx, `SELECT count(*)::int `+where, args...).Scan(&total); err != nil {
			return fmt.Errorf("count the requirements: %w", err)
		}
		n := len(args)
		rows, err := tx.Query(ctx, `
			SELECT r.id, p.key || '-R' || r.number, p.key, p.name, r.kind, r.title,
			       ARRAY(SELECT o.name FROM requirement_label o WHERE o.org_id = r.org_id AND o.requirement_id = r.id ORDER BY o.name),
			       COALESCE(u.name, ''), r.updated_at
			`+where+`
			ORDER BY p.key, r.number
			LIMIT $`+strconv.Itoa(n+1)+` OFFSET $`+strconv.Itoa(n+2), append(args, limit, offset)...)
		if err != nil {
			return fmt.Errorf("list the requirements: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Labelled])
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
