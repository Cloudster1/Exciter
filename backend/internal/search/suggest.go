package search

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/rql"
)

// How many words and requirements a completion offers.
const (
	DefaultSuggestLimit = 8
	MaxSuggestLimit     = 20
)

// Word is one completion: what to write, what kind of word it is, and what it names, such as a requirement's title.
type Word struct {
	Text   string `json:"text"`
	Detail string `json:"detail"`
	Label  string `json:"label"`
}

// Suggestions are what may come next at the caret of a query being typed, as Armature's search bar finishes the
// sentence: the words the language takes there, the names the project has, and the requirements the text finds.
type Suggestions struct {
	Completion   rql.Completion `json:"completion"`
	Words        []Word         `json:"words"`
	Requirements []QuickHit     `json:"requirements"`
}

// looksLikeQuery tells a query from words: an operator or a keyword of the language.
var looksLikeQuery = regexp.MustCompile(`(?i)[=<>~!]|\b(AND|OR|IN|IS|ORDER)\b`)

// Suggest completes a query of project projectKey at the caret at, counted in characters.
func (s *Service) Suggest(ctx context.Context, reader uuid.UUID, projectKey, text string, at, limit int) (*Suggestions, error) {
	out := &Suggestions{Words: []Word{}, Requirements: []QuickHit{}}
	takes := ""
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		defs, err := attribute.Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		cat, err := catalogOf(ctx, tx, p, defs)
		if err != nil {
			return err
		}
		c := cat.Complete(text, at)
		out.Completion = c
		for _, w := range c.Words {
			out.Words = append(out.Words, Word{Text: w, Detail: detailOf(c.Slot, w)})
		}
		if c.Slot != rql.SlotValue {
			return nil
		}
		takes = cat.Takes(rql.Field{Name: c.Field, Custom: c.Custom})
		names, err := namesOf(ctx, tx, p.ID, takes, c.Prefix, limit)
		if err != nil {
			return err
		}
		out.Words = append(out.Words, names...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A value that names a requirement is offered by key, found as the command palette finds one.
	if takes == "requirement" {
		hits, err := s.Quick(ctx, reader, out.Completion.Prefix, projectKey, limit)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			out.Words = append(out.Words, Word{Text: h.Requirement.Key, Detail: "requirement", Label: h.Requirement.Title})
		}
	}
	if len(out.Words) > limit {
		out.Words = out.Words[:limit]
	}
	out.Requirements, err = s.found(ctx, reader, projectKey, text, limit)
	return out, err
}

// found are the requirements the text finds: what it keeps as a query that runs, the requirements it names by key or
// title otherwise. A query that does not run yet finds nothing, rather than an error while it is typed.
func (s *Service) found(ctx context.Context, reader uuid.UUID, projectKey, text string, limit int) ([]QuickHit, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []QuickHit{}, nil
	}
	if !looksLikeQuery.MatchString(text) {
		return s.Quick(ctx, reader, text, projectKey, limit)
	}
	rows, _, err := s.Grid(ctx, reader, projectKey, Filter{Query: text}, Sort{}, limit, 0)
	if err != nil {
		return []QuickHit{}, nil
	}
	out := make([]QuickHit, len(rows))
	for i, r := range rows {
		out[i] = QuickHit{Outline: r.Outline, Requirement: r.Ref}
	}
	return out, nil
}

// detailOf says what kind of word the language offers.
func detailOf(slot rql.Slot, word string) string {
	switch {
	case slot == rql.SlotField && strings.HasPrefix(word, `"`):
		return "attribute"
	case slot == rql.SlotField:
		return "field"
	case slot == rql.SlotOperator:
		return "operator"
	case strings.HasSuffix(word, "()"):
		return "function"
	case slot == rql.SlotValue:
		return "value"
	}
	return "keyword"
}

// likeEscaper backslashes what LIKE would read as a pattern in a prefix typed.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// namesOf are the project's names of a kind that start with what was typed, written as a query takes them.
func namesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID, takes, prefix string, limit int) ([]Word, error) {
	like := strings.ToLower(likeEscaper.Replace(prefix)) + "%"
	var sql string
	switch takes {
	case "status":
		sql = `SELECT x.name, '' FROM req_status x WHERE x.project_id = $1 AND lower(x.name) LIKE $2 ORDER BY x.position, x.name LIMIT $3`
	case "type":
		sql = `SELECT x.name, '' FROM requirement_type x WHERE x.project_id = $1 AND NOT x.retired AND lower(x.name) LIKE $2 ORDER BY x.position, x.name LIMIT $3`
	case "label":
		sql = `SELECT DISTINCT l.name, '' FROM requirement_label l JOIN requirement r ON r.id = l.requirement_id
			WHERE r.project_id = $1 AND l.name LIKE $2 ORDER BY l.name LIMIT $3`
	case "person":
		// People are offered by name, the way a reader knows them; the address tells two of a name apart.
		sql = `SELECT u.name, u.email::text FROM app_user u WHERE u.is_active AND $1::uuid IS NOT NULL
			AND (lower(u.name) LIKE $2 OR lower(u.email::text) LIKE $2) ORDER BY lower(u.name) LIMIT $3`
	default:
		return nil, nil
	}
	rows, err := tx.Query(ctx, sql, projectID, like, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Word, error) {
		var name, label string
		if err := row.Scan(&name, &label); err != nil {
			return Word{}, err
		}
		return Word{Text: rql.Quote(name), Detail: takes, Label: label}, nil
	})
}
