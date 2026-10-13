package search

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// dayLayout is how a day is written in updatedAfter and updatedBefore.
const dayLayout = "2006-01-02"

// Sorts are the orders hits come in: relevance, the default, or the last change.
var Sorts = []string{"relevance", "updated"}

// Query is a full search: words, filters and order.
type Query struct {
	Text       string
	Projects   []string
	Categories []string
	Labels     []string
	Owners     []uuid.UUID
	// After is the first day counted, Before the first day not counted, both at midnight UTC.
	After  *time.Time
	Before *time.Time
	// ByUpdate orders by the latest change rather than by relevance.
	ByUpdate bool
	Limit    int
	Offset   int
}

// ParseQuery reads a full search from the query string of GET /search; the handler reads the window.
func ParseQuery(v url.Values) (Query, error) {
	q := Query{Text: strings.TrimSpace(v.Get("q"))}
	if utf8.RuneCountInString(q.Text) > MaxQueryLength {
		return q, &FieldError{"q", fmt.Sprintf("Keep the search to %d characters.", MaxQueryLength)}
	}
	for _, key := range v["project"] {
		if key = strings.ToUpper(strings.TrimSpace(key)); key != "" {
			q.Projects = append(q.Projects, key)
		}
	}
	var err error
	if q.Categories, err = categories(v["category"]); err != nil {
		return q, err
	}
	for _, raw := range v["label"] {
		// A name that is no label matches nothing, as an unknown project does.
		name, err := label.Normalize(raw)
		if err != nil {
			name = strings.TrimSpace(raw)
		}
		if name != "" {
			q.Labels = append(q.Labels, name)
		}
	}
	if q.Owners, err = ids("owner", v["owner"], "An owner is a person's id. Pick the person from the list instead."); err != nil {
		return q, err
	}
	if q.After, err = day(v.Get("updatedAfter"), "updatedAfter"); err != nil {
		return q, err
	}
	if q.Before, err = day(v.Get("updatedBefore"), "updatedBefore"); err != nil {
		return q, err
	}
	switch sort := v.Get("sort"); sort {
	case "", "relevance":
		q.ByUpdate = q.Text == ""
	case "updated":
		q.ByUpdate = true
	default:
		return q, &FieldError{"sort", "Sort by relevance or by updated."}
	}
	return q, nil
}

func categories(raw []string) ([]string, error) {
	var out []string
	for _, each := range raw {
		if !slices.Contains(workflow.Categories, workflow.Category(each)) {
			return nil, &FieldError{"category", "Narrow by draft, review, approved, done or closed."}
		}
		out = append(out, each)
	}
	return out, nil
}

func ids(field string, raw []string, message string) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, each := range raw {
		id, err := uuid.Parse(strings.TrimSpace(each))
		if err != nil {
			return nil, &FieldError{field, message}
		}
		out = append(out, id)
	}
	return out, nil
}

func day(raw, field string) (*time.Time, error) {
	if raw = strings.TrimSpace(raw); raw == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(dayLayout, raw, time.UTC)
	if err != nil {
		return nil, &FieldError{field, "Give the day as YYYY-MM-DD, such as 2026-09-30."}
	}
	return &t, nil
}

// PrefixQuery turns what somebody has typed so far into a tsquery that asks for every word as the prefix of a
// title word. Words are runs of letters and digits, so nothing typed can reach the tsquery syntax; empty means none.
func PrefixQuery(typed string) string {
	words := strings.FieldsFunc(typed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > maxQuickWords {
		words = words[:maxQuickWords]
	}
	terms := make([]string, len(words))
	for i, w := range words {
		terms[i] = "'" + strings.ToLower(w) + "':*A"
	}
	return strings.Join(terms, " & ")
}

// keyStart is what a key looks like while it is typed: a project key, a hyphen, and maybe R and digits.
var keyStart = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-R?([0-9]*)$`)

// KeyPrefix reads typed as the start of a key, such as CHK-R4: the project's key and the digits of the number
// so far; ok is false for anything that does not start like a key.
func KeyPrefix(typed string) (project, digits string, ok bool) {
	m := keyStart.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(typed)))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
