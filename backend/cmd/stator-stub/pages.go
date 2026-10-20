package main

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Pages and the quick search as Stator answers them (#41), in Stator's shapes, which the contract test holds them to.

const (
	// searchDefaultLimit and searchMaxLimit are Stator's bounds on a quick search.
	searchDefaultLimit = 8
	searchMaxLimit     = 20
	// labelDefaultLimit and labelMaxLimit are Stator's bounds on listing a label's pages.
	labelDefaultLimit = 20
	labelMaxLimit     = 100
)

func labelsOf(p *page) []string {
	if p.Labels == nil {
		return []string{}
	}
	return p.Labels
}

// labelPages lists the pages carrying a label that the reader may read, as Stator's GET /labels/{labelName}/pages.
func (s *stub) labelPages(c *call) {
	q := c.r.URL.Query()
	limit, offset := labelDefaultLimit, 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > labelMaxLimit {
			refuse(c.w, http.StatusUnprocessableEntity, "validation_failed", "Ask for 1 to 100 pages.")
			return
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			refuse(c.w, http.StatusUnprocessableEntity, "validation_failed", "Start at page 0 or later.")
			return
		}
		offset = n
	}
	label, in := c.r.PathValue("labelName"), q.Get("space")
	all := []map[string]any{}
	for _, p := range c.tenant.pages {
		if p.Trashed || !c.person.reads(p.Space) || (in != "" && !strings.EqualFold(in, p.Space.Key)) || !slices.Contains(p.Labels, label) {
			continue
		}
		all = append(all, map[string]any{"id": p.ID, "title": p.Title, "spaceKey": p.Space.Key, "spaceName": p.Space.Name,
			"path": p.path(), "labels": labelsOf(p), "unpublished": false, "updatedByName": p.UpdatedBy, "updatedAt": p.UpdatedAt})
	}
	listed := all[min(offset, len(all)):min(offset+limit, len(all))]
	respond(c.w, http.StatusOK, map[string]any{"pages": listed, "total": len(all), "limit": limit, "offset": offset})
}

// pageView is Stator's Page: every field its document requires, at what a page of the stub's holds.
func pageView(p *page) map[string]any {
	ancestors := []map[string]any{}
	for _, a := range p.ancestors() {
		ancestors = append(ancestors, map[string]any{"id": a.ID, "title": a.Title, "home": a.Home})
	}
	var parent any
	if p.Parent != nil {
		parent = p.Parent.ID
	}
	return map[string]any{
		"id": p.ID, "spaceId": p.Space.ID, "spaceKey": p.Space.Key, "parentId": parent, "title": p.Title, "kind": "page",
		"postedAt": nil, "appearance": map[string]any{"icon": nil, "width": "fixed", "cover": nil}, "mode": "live",
		"body": map[string]any{"type": "doc", "content": []any{}}, "version": p.Version, "home": p.Home, "unpublished": false,
		"draft": nil, "schedule": nil, "restricted": map[string]bool{"view": false, "edit": false},
		"can":       map[string]bool{"edit": true, "delete": true, "restrict": false, "comment": true, "add": true, "grantEdit": false, "archive": false},
		"ancestors": ancestors, "labels": labelsOf(p), "comments": map[string]int{"page": 0, "inline": 0, "detached": 0},
		"watching": map[string]any{"page": false, "subtree": false, "inherited": nil}, "starred": false, "reactions": []any{},
		"owner": nil, "verification": nil, "archived": nil, "review": map[string]any{"rule": nil, "pending": nil},
		"readAsk":       map[string]any{"since": nil, "confirmedAt": nil},
		"createdByName": p.UpdatedBy, "createdAt": p.CreatedAt, "updatedByName": p.UpdatedBy, "updatedAt": p.UpdatedAt,
	}
}

// spaceView is Stator's Space.
func spaceView(sp *space) map[string]any {
	return map[string]any{
		"id": sp.ID, "key": sp.Key, "name": sp.Name, "description": "", "homePageId": sp.Home.ID,
		"createdAt": seeded, "updatedAt": seeded, "archivedAt": nil, "archivedByName": "",
		"can":      map[string]bool{"addComments": true, "administer": false, "delete": false, "deletePages": true, "editPages": true, "purgeTrash": false},
		"watching": false, "starred": false, "owner": nil,
	}
}

func hit(p *page) map[string]any {
	return map[string]any{"id": p.ID, "title": p.Title, "spaceKey": p.Space.Key, "spaceName": p.Space.Name, "path": p.path()}
}

func (s *stub) getPage(c *call) {
	id, err := uuid.Parse(c.r.PathValue("pageID"))
	var p *page
	if err == nil {
		p = c.tenant.readable(c.person, id)
	}
	if p == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That page was not found.")
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"page": pageView(p), "space": spaceView(p.Space)})
}

// quickSearch finds the pages whose titles have a word starting with each word typed, as the reader may read them.
func (s *stub) quickSearch(c *call) {
	q := c.r.URL.Query()
	limit := searchDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > searchMaxLimit {
			refuse(c.w, http.StatusUnprocessableEntity, "validation_failed", "Ask for 1 to 20 pages.")
			return
		}
		limit = n
	}
	words := strings.Fields(strings.ToLower(q.Get("q")))
	in := q.Get("space")
	hits := []map[string]any{}
	for _, p := range c.tenant.pages {
		if len(hits) == limit || len(words) == 0 {
			break
		}
		if p.Trashed || !c.person.reads(p.Space) || (in != "" && !strings.EqualFold(in, p.Space.Key)) || !starts(p.Title, words) {
			continue
		}
		hits = append(hits, hit(p))
	}
	respond(c.w, http.StatusOK, map[string]any{"pages": hits})
}

func starts(title string, words []string) bool {
	have := strings.Fields(strings.ToLower(title))
	for _, w := range words {
		found := false
		for _, h := range have {
			found = found || strings.HasPrefix(h, w)
		}
		if !found {
			return false
		}
	}
	return true
}

// The controls of the changes a person makes in Stator, each announced as Stator's webhooks announce it.

// pageRef is Stator's webhook PageRef.
func (c *call) pageRef(p *page) map[string]any {
	return map[string]any{"id": p.ID, "title": p.Title, "version": p.Version, "kind": "page",
		"url":   "http://" + c.r.Host + "/s/" + p.Space.Key + "/p/" + p.ID.String(),
		"space": map[string]string{"key": p.Space.Key, "name": p.Space.Name}}
}

func (c *call) actor() map[string]any {
	a := c.tenant.person(adminName)
	return map[string]any{"id": a.ID, "name": a.Display}
}

func (s *stub) controlled(c *call) *page {
	id, err := uuid.Parse(c.r.PathValue("pageID"))
	var p *page
	if err == nil {
		p = c.tenant.page(id)
	}
	if p == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That page was not found.")
	}
	return p
}

func (s *stub) listPages(c *call) {
	out := []map[string]any{}
	for _, p := range c.tenant.pages {
		out = append(out, map[string]any{"id": p.ID, "title": p.Title, "spaceKey": p.Space.Key, "version": p.Version, "trashed": p.Trashed})
	}
	respond(c.w, http.StatusOK, map[string]any{"pages": out})
}

// publishPage publishes a page's next version, under a new title when one is given.
func (s *stub) publishPage(c *call) {
	var req struct {
		Title string `json:"title"`
	}
	if !decode(c, &req) {
		return
	}
	p := s.controlled(c)
	if p == nil {
		return
	}
	if req.Title != "" {
		p.Title = req.Title
	}
	p.Version++
	p.UpdatedAt = time.Now().UTC()
	c.emit("page.published", map[string]any{"page": c.pageRef(p), "actor": c.actor()})
	respond(c.w, http.StatusOK, map[string]any{"version": p.Version})
}

// movePage moves a page below the home of another space.
func (s *stub) movePage(c *call) {
	var req struct {
		SpaceKey string `json:"spaceKey"`
	}
	if !decode(c, &req) {
		return
	}
	p := s.controlled(c)
	if p == nil {
		return
	}
	to := c.tenant.space(req.SpaceKey)
	if to == nil || p.Home {
		refuse(c.w, http.StatusUnprocessableEntity, "validation_failed", "Name another space, and a page that is not a home.")
		return
	}
	from := map[string]any{"space": map[string]string{"key": p.Space.Key, "name": p.Space.Name}, "parentId": p.Parent.ID}
	p.Space, p.Parent = to, to.Home
	c.emit("page.moved", map[string]any{"page": c.pageRef(p), "from": from,
		"to": map[string]any{"space": map[string]string{"key": to.Key, "name": to.Name}, "parentId": to.Home.ID}, "actor": c.actor()})
	c.w.WriteHeader(http.StatusNoContent)
}

// trashPage puts a page in the trash, where nobody reads it.
func (s *stub) trashPage(c *call) {
	p := s.controlled(c)
	if p == nil {
		return
	}
	c.emit("page.deleted", map[string]any{"page": c.pageRef(p), "actor": c.actor()})
	p.Trashed = true
	c.w.WriteHeader(http.StatusNoContent)
}
