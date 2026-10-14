package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// unmarked readies a text column for ts_headline; Split undoes it.
func unmarked(column string) string {
	return `translate(translate(` + column + `, '` + startSel + stopSel + openAngle + `', ''), '<', '` + openAngle + `')`
}

// textOfHit is the plain text of a hit's statement and rationale, cut to what a snippet reads.
func textOfHit(chars int) string {
	return `left(requirement_plain_text(h.statement) || E'\n' || COALESCE(requirement_plain_text(h.rationale), ''), ` + strconv.Itoa(chars) + `)`
}

// Without words a search reads requirements as they are, among those the reader may view.
const unmatched = `(SELECT r.id, r.project_id, r.number, r.kind, r.title, r.statement, r.rationale, r.revision, r.status_id,
	r.owner_id, r.updated_at, r.updated_by, 0::real AS rank, false AS key_match
	FROM requirement r WHERE r.org_id = current_org_id() AND r.trashed_at IS NULL AND ` + `%s)`

// How much of a statement is read for its snippet. A headline reads its whole text, so a long one is cut first;
// its opening is what a hit mostly shows.
const (
	headlineChars    = 100000
	snippetScanChars = 4000
)

// Search answers a full search with one window of hits and how many there are, among the requirements out of
// the trash that the reader may view.
func (s *Service) Search(ctx context.Context, reader uuid.UUID, q Query) ([]Hit, int, error) {
	var args []any
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	var tsq, source string
	if q.Text == "" {
		bind(reader)
		source = fmt.Sprintf(unmatched, perm.ViewableRequirement("r", len(args)))
	} else {
		tsq = `websearch_to_tsquery(` + config + `, ` + bind(q.Text) + `)`
		keyProject, keyNumber := "", 0
		if project, number, ok := requirement.ParseKey(q.Text); ok {
			keyProject, keyNumber = project, number
		}
		// The words are matched through the index before anybody's rights are checked, which row level security
		// alone would do the other way round; requirement_search_rows has checked them (00200_search.sql).
		source = `requirement_search_rows(` + tsq + `, ` + bind(keyProject) + `, ` + bind(keyNumber) + `)`
	}
	where := []string{`TRUE`}
	if len(q.Projects) > 0 {
		where = append(where, `p.key = ANY (`+bind(q.Projects)+`::text[])`)
	}
	if len(q.Categories) > 0 {
		where = append(where, `st.category = ANY (`+bind(q.Categories)+`::text[])`)
	}
	if len(q.Labels) > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = h.id AND l.name = ANY (`+
			bind(q.Labels)+`::text[]))`)
	}
	if len(q.Owners) > 0 {
		where = append(where, `h.owner_id = ANY (`+bind(q.Owners)+`::uuid[])`)
	}
	if q.After != nil {
		where = append(where, `h.updated_at >= `+bind(*q.After))
	}
	if q.Before != nil {
		where = append(where, `h.updated_at < `+bind(*q.Before))
	}
	titleMatch := `false`
	if tsq != "" {
		titleMatch = `to_tsvector(` + config + `, h.title) @@ ` + tsq
	}
	hits := `WITH hit AS (
		SELECT h.*, p.key AS project_key, p.name AS project_name, st.name AS status_name, st.category AS status_category,
		       ` + titleMatch + ` AS title_match
		FROM ` + source + ` h
		JOIN project p ON p.id = h.project_id
		LEFT JOIN req_status st ON st.id = h.status_id
		WHERE ` + strings.Join(where, ` AND `) + `
	)`
	order := `h.updated_at DESC, h.id`
	if !q.ByUpdate && tsq != "" {
		order = `h.key_match DESC, h.title_match DESC, h.rank DESC, ` + order
	}
	counted := len(args)
	title := `h.title`
	snippet := textOfHit(snippetScanChars)
	if tsq != "" {
		titleOpts, snippetOpts := bind(titleOptions), bind(headlineOptions)
		title = `ts_headline(` + config + `, ` + unmarked(`h.title`) + `, ` + tsq + `, ` + titleOpts + `)`
		snippet = `ts_headline(` + config + `, ` + unmarked(textOfHit(headlineChars)) + `, ` + tsq + `, ` + snippetOpts + `)`
	}
	limit, offset := bind(q.Limit), bind(q.Offset)
	sql := hits + `, chosen AS (
			SELECT h.*, row_number() OVER (ORDER BY ` + order + `) AS ord FROM hit h
			ORDER BY ` + order + `
			LIMIT ` + limit + ` OFFSET ` + offset + `
		)
		SELECT h.id, h.project_key, h.number, h.kind, h.title, h.revision, h.status_id, COALESCE(h.status_name, ''),
		       COALESCE(h.status_category, ''), h.project_name, ` + title + `, ` + snippet + `, h.updated_at, COALESCE(u.name, ''),
		       ARRAY(SELECT l.name FROM requirement_label l WHERE l.requirement_id = h.id ORDER BY l.name)
		FROM chosen h LEFT JOIN app_user u ON u.id = h.updated_by
		ORDER BY h.ord`

	out := []Hit{}
	var total int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, hits+` SELECT count(*) FROM hit`, args[:counted]...).Scan(&total); err != nil {
			return fmt.Errorf("count the hits: %w", err)
		}
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				h               Hit
				number          int
				statusID        *uuid.UUID
				status          requirement.StatusRef
				marked, snipped string
			)
			ref := &h.Requirement
			if err := rows.Scan(&ref.ID, &ref.ProjectKey, &number, &ref.Kind, &ref.Title, &ref.Revision, &statusID, &status.Name,
				&status.Category, &h.ProjectName, &marked, &snipped, &h.UpdatedAt, &h.UpdatedByName, &h.Labels); err != nil {
				return err
			}
			ref.Key = requirement.Key(ref.ProjectKey, number)
			if statusID != nil {
				status.ID = *statusID
				ref.Status = &status
			}
			if h.Labels == nil {
				h.Labels = []string{}
			}
			if tsq != "" {
				h.Title, h.Snippet = Split(marked), Split(snipped)
			} else {
				h.Title, h.Snippet = Plain(marked), Plain(firstWords(snipped, snippetMinWords))
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, total, err
}

// Quick finds requirements for the command palette: by key when typed starts like one, else those whose title
// words start with each word typed, the best matches first; projectKey stays inside one project.
func (s *Service) Quick(ctx context.Context, reader uuid.UUID, typed, projectKey string, limit int) ([]QuickHit, error) {
	out := []QuickHit{}
	args := []any{reader, limit}
	within := ``
	if projectKey = strings.ToUpper(strings.TrimSpace(projectKey)); projectKey != "" {
		args = append(args, projectKey)
		within = ` AND p.key = $3`
	}
	var sql string
	if keyProject, digits, ok := KeyPrefix(typed); ok {
		args = append(args, keyProject, digits+"%")
		n := strconv.Itoa(len(args))
		sql = `
			SELECT r.id, r.project_id, p.key, p.name, r.number, r.kind, r.title, r.revision, r.status_id,
			       COALESCE(st.name, ''), COALESCE(st.category, '')
			FROM requirement r
			JOIN project p ON p.org_id = r.org_id AND p.id = r.project_id
			LEFT JOIN req_status st ON st.id = r.status_id
			WHERE r.org_id = current_org_id() AND r.trashed_at IS NULL AND ` + perm.ViewableRequirement("r", 1) + within + `
			  AND p.key = $` + strconv.Itoa(len(args)-1) + ` AND r.number::text LIKE $` + n + `
			ORDER BY char_length(r.number::text), r.number
			LIMIT $2`
	} else {
		tsq := PrefixQuery(typed)
		if tsq == "" {
			return out, nil
		}
		args = append(args, tsq, strings.TrimSpace(typed))
		n := len(args)
		// requirement_search_rows checks the reader's rights, which the transaction names.
		sql = `
			SELECT h.id, h.project_id, p.key, p.name, h.number, h.kind, h.title, h.revision, h.status_id,
			       COALESCE(st.name, ''), COALESCE(st.category, '')
			FROM requirement_search_rows(to_tsquery(` + config + `, $` + strconv.Itoa(n-1) + `), '', 0) h
			JOIN project p ON p.id = h.project_id
			LEFT JOIN req_status st ON st.id = h.status_id
			WHERE $1::uuid IS NOT NULL` + within + `
			ORDER BY starts_with(lower(h.title), lower($` + strconv.Itoa(n) + `)) DESC, h.rank DESC,
			         char_length(h.title), h.updated_at DESC, h.id
			LIMIT $2`
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("quick search: %w", err)
		}
		type found struct {
			hit     QuickHit
			project uuid.UUID
		}
		var all []found
		for rows.Next() {
			var (
				f        found
				number   int
				statusID *uuid.UUID
				status   requirement.StatusRef
			)
			ref := &f.hit.Requirement
			if err := rows.Scan(&ref.ID, &f.project, &ref.ProjectKey, &f.hit.ProjectName, &number, &ref.Kind, &ref.Title, &ref.Revision,
				&statusID, &status.Name, &status.Category); err != nil {
				rows.Close()
				return err
			}
			ref.Key = requirement.Key(ref.ProjectKey, number)
			if statusID != nil {
				status.ID = *statusID
				ref.Status = &status
			}
			all = append(all, f)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, f := range all {
			if f.hit.Outline, err = requirement.Outline(ctx, tx, f.project, f.hit.Requirement.ID); err != nil {
				return fmt.Errorf("number %s: %w", f.hit.Requirement.Key, err)
			}
			out = append(out, f.hit)
		}
		return nil
	})
	return out, err
}
