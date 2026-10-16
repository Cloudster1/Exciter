package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Remote links on pages as Stator's #419 keeps them: put by whoever may edit the page, a link with the same url
// replacing the one there, and page.remote_link.removed sent whoever takes one off.

const (
	// maxLinkTitle and maxLinkSource are Stator's bounds on a remote link.
	maxLinkTitle  = 255
	maxLinkSource = 100
	// lastServerError is the highest 5xx an outage may answer.
	lastServerError = 599
	// cannotEdit is Stator's refusal of a change to a page the person may only read.
	cannotEdit = "You may read this page but not change it. Ask an administrator of the space for access."
)

type remoteLink struct {
	ID        uuid.UUID
	PageID    uuid.UUID
	URL       string
	Title     string
	Source    string
	IconURL   *string
	CreatedBy *person
	CreatedAt time.Time
	UpdatedAt time.Time
}

func linkView(l *remoteLink) map[string]any {
	return map[string]any{
		"id": l.ID, "url": l.URL, "title": l.Title, "source": l.Source, "iconUrl": l.IconURL,
		"createdBy": l.CreatedBy.ID, "createdByName": l.CreatedBy.Display, "createdAt": l.CreatedAt, "updatedAt": l.UpdatedAt,
	}
}

func refuseField(w http.ResponseWriter, field, message string) {
	respond(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
		"code": "validation_failed", "message": "Some fields need attention.", "fields": map[string]string{field: message}}})
}

// linkedPage is the page of a remote link call as the person may read it, or nil once refused; an outage set by a
// test answers first.
func (s *stub) linkedPage(c *call) *page {
	if c.tenant.outage > 0 {
		c.tenant.outage--
		refuse(c.w, c.tenant.outageStatus, "unavailable", "Stator is not available right now. Try again in a moment.")
		return nil
	}
	id, err := uuid.Parse(c.r.PathValue("pageID"))
	var p *page
	if err == nil {
		p = c.tenant.readable(c.person, id)
	}
	if p == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That page was not found.")
	}
	return p
}

// editablePage is linkedPage refused for a read token or a person who may only read the page, as Stator refuses.
func (s *stub) editablePage(c *call) *page {
	if c.person.ReadOnly {
		refuse(c.w, http.StatusForbidden, "read_only_token", "This token can only read. Use a token without the read scope, or sign in, to make changes.")
		return nil
	}
	p := s.linkedPage(c)
	if p != nil && !c.person.edits(p.Space) {
		refuse(c.w, http.StatusForbidden, "forbidden", cannotEdit)
		return nil
	}
	return p
}

func (s *stub) listLinks(c *call) {
	p := s.linkedPage(c)
	if p == nil {
		return
	}
	out := []map[string]any{}
	for _, l := range c.tenant.links {
		if l.PageID == p.ID {
			out = append(out, linkView(l))
		}
	}
	respond(c.w, http.StatusOK, map[string]any{"remoteLinks": out})
}

type remoteLinkRequest struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Source  string `json:"source"`
	IconURL string `json:"iconUrl,omitempty"`
}

// putLink puts a link on a page, or replaces the one with the same url and keeps who first put it, as Stator does.
func (s *stub) putLink(c *call) {
	var req remoteLinkRequest
	if !decode(c, &req) {
		return
	}
	p := s.editablePage(c)
	if p == nil {
		return
	}
	req.URL, req.Title, req.Source = strings.TrimSpace(req.URL), strings.TrimSpace(req.Title), strings.TrimSpace(req.Source)
	switch {
	case !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://"):
		refuseField(c.w, "url", "Make the object's address a web address starting with http:// or https://.")
		return
	case req.Title == "":
		refuseField(c.w, "title", "Give the link a title.")
		return
	case utf8.RuneCountInString(req.Title) > maxLinkTitle:
		refuseField(c.w, "title", fmt.Sprintf("Shorten the title to %d characters or fewer.", maxLinkTitle))
		return
	case req.Source == "" || utf8.RuneCountInString(req.Source) > maxLinkSource:
		refuseField(c.w, "source", "Name the tool the object lives in as the link's source.")
		return
	}
	var icon *string
	if req.IconURL != "" {
		icon = &req.IconURL
	}
	now := time.Now().UTC()
	for _, l := range c.tenant.links {
		if l.PageID == p.ID && l.URL == req.URL {
			l.Title, l.Source, l.IconURL, l.UpdatedAt = req.Title, req.Source, icon, now
			respond(c.w, http.StatusOK, map[string]any{"remoteLink": linkView(l)})
			return
		}
	}
	l := &remoteLink{ID: uuid.New(), PageID: p.ID, URL: req.URL, Title: req.Title, Source: req.Source, IconURL: icon,
		CreatedBy: c.person, CreatedAt: now, UpdatedAt: now}
	c.tenant.links = append(c.tenant.links, l)
	respond(c.w, http.StatusCreated, map[string]any{"remoteLink": linkView(l)})
}

func (s *stub) deleteLink(c *call) {
	p := s.editablePage(c)
	if p == nil {
		return
	}
	id, err := uuid.Parse(c.r.PathValue("linkID"))
	i := slices.IndexFunc(c.tenant.links, func(l *remoteLink) bool { return err == nil && l.ID == id && l.PageID == p.ID })
	if i < 0 {
		refuse(c.w, http.StatusNotFound, "not_found", "That remote link was not found.")
		return
	}
	c.emit("page.remote_link.removed", c.linkRemoved(p, c.tenant.links[i], c.person))
	c.tenant.links = slices.Delete(c.tenant.links, i, i+1)
	c.w.WriteHeader(http.StatusNoContent)
}

// linkRemoved is the payload of page.remote_link.removed, the link as it was, sent whoever took it off.
func (c *call) linkRemoved(p *page, l *remoteLink, actor *person) map[string]any {
	return map[string]any{"page": c.pageRef(p),
		"remoteLink": map[string]any{"id": l.ID, "url": l.URL, "title": l.Title, "source": l.Source},
		"actor":      map[string]any{"id": actor.ID, "name": actor.Display}}
}

// The controls a test steers remote links with.

// allLinks lists every remote link of the tenant with the page it is on, for a test to read.
func (s *stub) allLinks(c *call) {
	out := []map[string]any{}
	for _, l := range c.tenant.links {
		v := linkView(l)
		v["pageId"] = l.PageID
		out = append(out, v)
	}
	respond(c.w, http.StatusOK, map[string]any{"remoteLinks": out})
}

func (s *stub) setOutage(c *call) {
	var req struct {
		// Status answers the next Count remote link calls, a 5xx or a 429.
		Status int `json:"status"`
		Count  int `json:"count"`
	}
	if !decode(c, &req) {
		return
	}
	if req.Count > 0 && req.Status != http.StatusTooManyRequests && (req.Status < http.StatusInternalServerError || req.Status > lastServerError) {
		refuseField(c.w, "status", "Name a 5xx status or 429.")
		return
	}
	c.tenant.outage, c.tenant.outageStatus = max(req.Count, 0), req.Status
	c.w.WriteHeader(http.StatusNoContent)
}

// takeOffLink removes the remote link to url from a page as a person in Stator would, rather than its source.
func (s *stub) takeOffLink(c *call) {
	p := s.controlled(c)
	if p == nil {
		return
	}
	url := c.r.URL.Query().Get("url")
	actor := c.tenant.person(adminName)
	for i, l := range c.tenant.links {
		if l.PageID == p.ID && l.URL == url {
			c.tenant.links = slices.Delete(c.tenant.links, i, i+1)
			c.emit("page.remote_link.removed", c.linkRemoved(p, l, actor))
			c.w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	refuse(c.w, http.StatusNotFound, "not_found", "That page has no link to that address.")
}
