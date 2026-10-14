package armature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Issues read as the viewer (#27), Stator's lookup with each key fetched alone.

var keyShape = regexp.MustCompile(KeyPattern)

// NormalizeKey is raw upper case, and whether that is an issue key.
func NormalizeKey(raw string) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(raw))
	return key, keyShape.MatchString(key)
}

// IssueURL is where an issue opens in the Armature at baseURL.
func IssueURL(baseURL, key string) string {
	return strings.TrimSuffix(baseURL, "/") + "/issues/" + url.PathEscape(key)
}

// wireIssue is the part of Armature's Issue that Exciter shows.
type wireIssue struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Type    struct {
		Name string `json:"name"`
	} `json:"type"`
	Status   IssueStatus `json:"status"`
	Assignee *struct {
		Name string `json:"name"`
	} `json:"assignee"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (w wireIssue) issue(baseURL string) *Issue {
	out := &Issue{Key: w.Key, Summary: w.Summary, URL: IssueURL(baseURL, w.Key), Type: w.Type.Name, Status: w.Status, UpdatedAt: w.UpdatedAt}
	if w.Assignee != nil {
		out.AssigneeName = w.Assignee.Name
	}
	return out
}

// Lookup answers one result per key, in order, as the viewer may see them. Keys are distinct and normalized; a
// status other than ok has no results.
func (s *Service) Lookup(ctx context.Context, keys []string) (Status, []LookupResult, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	found := make(map[string]*Issue, len(keys))
	var missing []string
	for _, key := range keys {
		var cached *Issue
		if s.cache.Issue(ctx, v.TokenID, key, &cached) {
			found[key] = cached
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) > 0 {
		fetched, err := s.fetchIssues(ctx, v, missing)
		if err != nil {
			return s.failed(ctx, v, err), nil, nil
		}
		for _, key := range missing {
			found[key] = fetched[key]
			s.cache.PutIssue(ctx, v.TokenID, key, fetched[key])
		}
		s.noteStatus(ctx, fetched)
	}
	out := make([]LookupResult, len(keys))
	for i, key := range keys {
		out[i] = LookupResult{Key: key, Issue: found[key]}
	}
	return StatusOK, out, nil
}

// Issue is one issue as the viewer may see it, nil when they may not; the key is normalized.
func (s *Service) Issue(ctx context.Context, key string) (Status, *Issue, error) {
	status, results, err := s.Lookup(ctx, []string{key})
	if err != nil || status != StatusOK {
		return status, nil, err
	}
	return StatusOK, results[0].Issue, nil
}

// Search is the picker's: a key finds that issue, words the issues whose summary holds them.
func (s *Service) Search(ctx context.Context, query string, limit int) (Status, []Issue, error) {
	query = strings.TrimSpace(query)
	switch {
	case query == "":
		return "", nil, &FieldError{Field: "q", Message: "Type an issue key, such as CP-12, or words of its summary."}
	case utf8.RuneCountInString(query) > MaxSearchLength:
		return "", nil, &FieldError{Field: "q", Message: "Search with at most " + strconv.Itoa(MaxSearchLength) + " characters."}
	case limit < 1 || limit > SearchMaxLimit:
		return "", nil, &FieldError{Field: "limit", Message: "Ask for 1 to " + strconv.Itoa(SearchMaxLimit) + " issues."}
	}
	if key, ok := NormalizeKey(query); ok {
		status, issue, err := s.Issue(ctx, key)
		if err != nil || status != StatusOK || issue == nil {
			return status, []Issue{}, err
		}
		return StatusOK, []Issue{*issue}, nil
	}
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	out := []Issue{}
	if s.cache.Search(ctx, v.TokenID, query, limit, 0, &out) {
		return StatusOK, out, nil
	}
	var answer struct {
		Issues []wireIssue `json:"issues"`
	}
	if err := v.Caller.Get(ctx, "/issues", url.Values{"text": {query}, "limit": {strconv.Itoa(limit)}}, &answer); err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	fetched := make(map[string]*Issue, len(answer.Issues))
	for _, w := range answer.Issues {
		issue := w.issue(v.Caller.BaseURL())
		out = append(out, *issue)
		fetched[issue.Key] = issue
	}
	s.cache.PutSearch(ctx, v.TokenID, query, limit, 0, out)
	s.noteStatus(ctx, fetched)
	return StatusOK, out, nil
}

// failed is the status a read answers for a call's error; a refused token is marked so it is not sent again.
func (s *Service) failed(ctx context.Context, v *Viewer, err error) Status {
	status := StatusOf(err)
	switch status {
	case StatusRejected:
		if err := s.NoteRejected(ctx, v); err != nil {
			s.opts.Log.Warn("a refused Armature token could not be marked", "token_id", v.TokenID, "error", err)
		}
	case StatusUnreachable:
		s.opts.Log.Info("Armature did not answer a read", "token_id", v.TokenID, "error", err)
	}
	return status
}

// fetchIssues asks for each key alone, a few at once; Armature answers an issue's old keys too.
func (s *Service) fetchIssues(ctx context.Context, v *Viewer, keys []string) (map[string]*Issue, error) {
	out := make(map[string]*Issue, len(keys))
	var (
		mu    sync.Mutex
		first error
		wg    sync.WaitGroup
		slots = make(chan struct{}, LookupParallel)
	)
	for _, key := range keys {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			issue, err := s.fetchIssue(ctx, v, key)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			out[key] = issue
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, nil
}

// fetchIssue is Armature's GET /issues/{key}; nil when there is none the viewer may see.
func (s *Service) fetchIssue(ctx context.Context, v *Viewer, key string) (*Issue, error) {
	var answer struct {
		Issue wireIssue `json:"issue"`
	}
	err := v.Caller.Get(ctx, "/issues/"+url.PathEscape(key), nil, &answer)
	var refused *RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return answer.Issue.issue(v.Caller.BaseURL()), nil
}

// noteStatus stores the status category of each issue Armature just answered on every link to it, for everyone,
// as the admin role: a reader may not write it, and the category is Armature's word, not theirs.
func (s *Service) noteStatus(ctx context.Context, fetched map[string]*Issue) {
	var keys, categories []string
	for key, issue := range fetched {
		if issue != nil && slices.Contains(StatusCategories, issue.Status.Category) {
			keys, categories = append(keys, key), append(categories, issue.Status.Category)
		}
	}
	if len(keys) == 0 {
		return
	}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE armature_issue_link l SET status_category = f.category, status_stale = false
			FROM unnest($1::text[], $2::text[]) AS f (issue_key, category)
			WHERE l.issue_key = f.issue_key
			  AND (l.status_category IS DISTINCT FROM f.category OR l.status_stale)`, keys, categories)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the status of Armature issues could not be stored", "error", err)
	}
}
