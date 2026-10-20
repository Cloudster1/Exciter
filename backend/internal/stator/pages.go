package stator

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Pages read as the viewer (#41), each fetched alone a few at once, as Armature's issues are.

// MaxSpaceKeyLength bounds the space a search stays inside, as the database bounds a link's.
const MaxSpaceKeyLength = 100

// PageURL is where a page opens in the Stator at baseURL.
func PageURL(baseURL, spaceKey string, pageID uuid.UUID) string {
	return strings.TrimSuffix(baseURL, "/") + "/s/" + url.PathEscape(spaceKey) + "/p/" + pageID.String()
}

// wirePage is the part of Stator's PageResponse that Exciter shows; the body stays behind.
type wirePage struct {
	Page struct {
		ID            uuid.UUID `json:"id"`
		Title         string    `json:"title"`
		SpaceKey      string    `json:"spaceKey"`
		Version       int       `json:"version"`
		UpdatedAt     time.Time `json:"updatedAt"`
		UpdatedByName string    `json:"updatedByName"`
		Ancestors     []struct {
			Title string `json:"title"`
		} `json:"ancestors"`
	} `json:"page"`
	Space struct {
		Name string `json:"name"`
	} `json:"space"`
}

func (w wirePage) page(baseURL string) *Page {
	p := w.Page
	out := &Page{ID: p.ID, Title: p.Title, SpaceKey: p.SpaceKey, SpaceName: w.Space.Name, Path: []string{}, Version: p.Version,
		URL: PageURL(baseURL, p.SpaceKey, p.ID), UpdatedAt: p.UpdatedAt, UpdatedByName: p.UpdatedByName}
	for _, a := range p.Ancestors {
		out.Path = append(out.Path, a.Title)
	}
	return out
}

// Lookup answers one result per id, in order, as the viewer may read them. Ids are distinct; a status other than ok
// has no results.
func (s *Service) Lookup(ctx context.Context, ids []uuid.UUID) (Status, []LookupResult, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	found := make(map[uuid.UUID]*Page, len(ids))
	var missing []uuid.UUID
	for _, id := range ids {
		var cached *Page
		if s.cache.Page(ctx, v.TokenID, id, &cached) {
			found[id] = cached
			continue
		}
		missing = append(missing, id)
	}
	if len(missing) > 0 {
		fetched, err := s.fetchPages(ctx, v, missing)
		if err != nil {
			return s.failed(ctx, v, err), nil, nil
		}
		for _, id := range missing {
			found[id] = fetched[id]
			s.cache.PutPage(ctx, v.TokenID, id, fetched[id])
		}
		s.noteVersions(ctx, fetched)
	}
	out := make([]LookupResult, len(ids))
	for i, id := range ids {
		out[i] = LookupResult{ID: id, Page: found[id]}
	}
	return StatusOK, out, nil
}

// Search is the picker's: Stator's quick search, pages whose titles start with the words typed, within a space when
// one is named.
func (s *Service) Search(ctx context.Context, query, space string, limit int) (Status, []PageHit, error) {
	query, space = strings.TrimSpace(query), strings.TrimSpace(space)
	switch {
	case query == "":
		return "", nil, &FieldError{Field: "q", Message: "Type the first words of a page's title."}
	case utf8.RuneCountInString(query) > MaxSearchLength:
		return "", nil, &FieldError{Field: "q", Message: "Search with at most " + strconv.Itoa(MaxSearchLength) + " characters."}
	case utf8.RuneCountInString(space) > MaxSpaceKeyLength:
		return "", nil, &FieldError{Field: "space", Message: "Name a space by its key, at most " + strconv.Itoa(MaxSpaceKeyLength) + " characters."}
	case limit < 1 || limit > SearchMaxLimit:
		return "", nil, &FieldError{Field: "limit", Message: "Ask for 1 to " + strconv.Itoa(SearchMaxLimit) + " pages."}
	}
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	out := []PageHit{}
	if s.cache.Search(ctx, v.TokenID, query, space, limit, &out) {
		return StatusOK, out, nil
	}
	params := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	if space != "" {
		params.Set("space", space)
	}
	var answer struct {
		Pages []PageHit `json:"pages"`
	}
	if err := v.Caller.Get(ctx, "/search/quick", params, &answer); err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	for _, hit := range answer.Pages {
		if hit.Path == nil {
			hit.Path = []string{}
		}
		hit.URL = PageURL(v.Caller.BaseURL(), hit.SpaceKey, hit.ID)
		out = append(out, hit)
	}
	s.cache.PutSearch(ctx, v.TokenID, query, space, limit, out)
	return StatusOK, out, nil
}

// fetchPages asks for each page alone, a few at once.
func (s *Service) fetchPages(ctx context.Context, v *Viewer, ids []uuid.UUID) (map[uuid.UUID]*Page, error) {
	out := make(map[uuid.UUID]*Page, len(ids))
	var (
		mu    sync.Mutex
		first error
		wg    sync.WaitGroup
		slots = make(chan struct{}, LookupParallel)
	)
	for _, id := range ids {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			page, err := s.fetchPage(ctx, v, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			out[id] = page
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, nil
}

// labeledLimit is how many pages a label lookup reads, enough to pass over a draft that carries the label too.
const labeledLimit = 10

// LabeledPage is the first published page Stator labels with label that the person ctx acts for may read, or
// uuid.Nil when there is none; the suite demo finds its pages so (#270).
func (s *Service) LabeledPage(ctx context.Context, label string) (uuid.UUID, error) {
	v, err := s.writer(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	var answer struct {
		Pages []struct {
			ID          uuid.UUID `json:"id"`
			Unpublished bool      `json:"unpublished"`
		} `json:"pages"`
	}
	err = v.Caller.Get(ctx, "/labels/"+url.PathEscape(label)+"/pages", url.Values{"limit": {strconv.Itoa(labeledLimit)}}, &answer)
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	for _, p := range answer.Pages {
		if !p.Unpublished {
			return p.ID, nil
		}
	}
	return uuid.Nil, nil
}

// fetchPage is Stator's GET /pages/{pageID}; nil when there is none the viewer may read.
func (s *Service) fetchPage(ctx context.Context, v *Viewer, id uuid.UUID) (*Page, error) {
	var answer wirePage
	err := v.Caller.Get(ctx, "/pages/"+id.String(), nil, &answer)
	var refused *RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return answer.page(v.Caller.BaseURL()), nil
}

// noteVersions stores each page Stator answered on every link to it, as the admin role, since a version is Stator's
// word and not the reader's; it never goes back, and a link that has seen none takes it as seen.
func (s *Service) noteVersions(ctx context.Context, fetched map[uuid.UUID]*Page) {
	var (
		ids        []uuid.UUID
		versions   []int
		keys, urls []string
	)
	for id, page := range fetched {
		if page != nil {
			ids, versions = append(ids, id), append(versions, page.Version)
			keys, urls = append(keys, page.SpaceKey), append(urls, page.URL)
		}
	}
	if len(ids) == 0 {
		return
	}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE stator_page_link l SET page_version = GREATEST(l.page_version, f.version),
			    page_version_seen = COALESCE(l.page_version_seen, f.version), space_key = f.space_key, url = f.url
			FROM unnest($1::uuid[], $2::int[], $3::text[], $4::text[]) AS f (page_id, version, space_key, url)
			WHERE l.page_id = f.page_id
			  AND (l.page_version IS NULL OR l.page_version < f.version OR l.page_version_seen IS NULL
			       OR l.space_key <> f.space_key OR l.url <> f.url)`, ids, versions, keys, urls)
		return err
	})
	if err != nil {
		s.opts.Log.Warn("the versions of Stator pages could not be stored", "error", err)
	}
}
