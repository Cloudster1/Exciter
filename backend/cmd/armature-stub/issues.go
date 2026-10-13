package main

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Armature's issues and their remote links as the stub keeps them, Stator's handlers.

const (
	// defaultLimit and maxLimit bound a page of issues, as Armature's do.
	defaultLimit = 50
	maxLimit     = 100
	// maxBody bounds what the stub reads of a request.
	maxBody = 1 << 20
)

func refuseField(w http.ResponseWriter, field, message string) {
	respond(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
		"code": "validation_failed", "message": "Some fields need attention.", "fields": map[string]string{field: message}}})
}

func decode(c *call, into any) bool {
	dec := json.NewDecoder(io.LimitReader(c.r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		refuse(c.w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON: "+err.Error()+".")
		return false
	}
	return true
}

func (c *call) readOnly() bool {
	if c.person.ReadOnly {
		refuse(c.w, http.StatusForbidden, "read_only_token", "This token can only read. Use a token without the read scope, or sign in, to make changes.")
		return true
	}
	return false
}

func typeView(k *issueType) map[string]any {
	return map[string]any{"id": k.ID, "name": k.Name, "icon": k.Icon, "level": k.Level, "isSubtask": k.IsSubtask}
}

func personRef(p *person) any {
	if p == nil {
		return nil
	}
	return map[string]any{"id": p.ID, "name": p.Display}
}

func issueView(is *issue) map[string]any {
	v := map[string]any{
		"id": is.ID, "key": is.Key, "type": typeView(is.Type), "projectId": is.Project.ID, "projectKey": is.Project.Key,
		"summary": is.Summary, "priority": is.Priority, "timeSpentMinutes": 0,
		"status":      map[string]any{"id": is.Status.ID, "name": is.Status.Name, "category": is.Status.Category, "position": is.Status.Position},
		"labels":      []any{},
		"fixVersions": []any{}, "affectsVersions": []any{}, "components": []any{},
		"commentCount": 0, "childCount": 0,
		"createdAt": is.CreatedAt, "updatedAt": is.UpdatedAt,
	}
	if is.Assignee != nil {
		v["assignee"] = personRef(is.Assignee)
	}
	if is.Reporter != nil {
		v["reporter"] = personRef(is.Reporter)
	}
	if is.DueDate != nil {
		v["dueDate"] = is.DueDate
	}
	if is.ResolvedAt != nil {
		v["resolvedAt"] = is.ResolvedAt
	}
	return v
}

// search answers Armature's filters Exciter sends, text and project; a query in NQL the stub does not read.
func (s *stub) search(c *call) {
	q := c.r.URL.Query()
	limit, offset := defaultLimit, 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			refuseField(c.w, "limit", "Ask for 1 to 100 issues.")
			return
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			refuseField(c.w, "offset", "Start at 0 or later.")
			return
		}
		offset = n
	}
	if q.Get("q") != "" {
		refuse(c.w, http.StatusBadRequest, "bad_query", "The stub reads text and project only. Add NQL to cmd/armature-stub when Exciter starts sending it.")
		return
	}
	text, project := strings.ToLower(strings.TrimSpace(q.Get("text"))), strings.ToUpper(strings.TrimSpace(q.Get("project")))
	var found []*issue
	for _, is := range c.tenant.visible(c.person) {
		if (text == "" || strings.Contains(strings.ToLower(is.Summary), text)) && (project == "" || is.Project.Key == project) {
			found = append(found, is)
		}
	}
	page := []map[string]any{}
	for i := offset; i < len(found) && i < offset+limit; i++ {
		page = append(page, issueView(found[i]))
	}
	respond(c.w, http.StatusOK, map[string]any{"issues": page, "total": len(found), "limit": limit, "offset": offset})
}

func (s *stub) getIssue(c *call) {
	is := c.tenant.lookup(c.person, c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"issue": issueView(is)})
}

func linkView(l *remoteLink) map[string]any {
	return map[string]any{
		"id": l.ID, "url": l.URL, "title": l.Title, "source": l.Source, "iconUrl": l.IconURL,
		"createdBy": l.CreatedBy, "createdAt": l.CreatedAt, "updatedAt": l.UpdatedAt,
	}
}

func (s *stub) linkedIssue(c *call) *issue {
	if c.tenant.outage > 0 {
		c.tenant.outage--
		refuse(c.w, c.tenant.outageStatus, "unavailable", "Armature is not available right now. Try again in a moment.")
		return nil
	}
	is := c.tenant.lookup(c.person, c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
	}
	return is
}

// linkWritable refuses a change to an issue's links in a project the person may only read, as Armature asks for
// issue.write there.
func (s *stub) linkWritable(c *call, is *issue) bool {
	if !c.person.writes(is.Project) {
		refuse(c.w, http.StatusForbidden, "forbidden", "You may not change the links of issues in this project.")
		return false
	}
	return true
}

func (s *stub) listLinks(c *call) {
	is := s.linkedIssue(c)
	if is == nil {
		return
	}
	out := []map[string]any{}
	for _, l := range c.tenant.links {
		if l.IssueID == is.ID {
			out = append(out, linkView(l))
		}
	}
	respond(c.w, http.StatusOK, map[string]any{"remoteLinks": out})
}

type remoteLinkRequest struct {
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	Source  string  `json:"source"`
	IconURL *string `json:"iconUrl,omitempty"`
}

// putLink puts a link on an issue, or retitles the one with the same url, as Armature does, so sending the same
// link again is harmless.
func (s *stub) putLink(c *call) {
	var req remoteLinkRequest
	if !decode(c, &req) || c.readOnly() {
		return
	}
	is := s.linkedIssue(c)
	if is == nil || !s.linkWritable(c, is) {
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		refuseField(c.w, "url", "Link to an http or https address.")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		refuseField(c.w, "title", "Give the link a title.")
		return
	}
	now := time.Now().UTC()
	for _, l := range c.tenant.links {
		if l.IssueID == is.ID && l.URL == req.URL {
			l.Title, l.Source, l.IconURL, l.UpdatedAt = req.Title, req.Source, req.IconURL, now
			respond(c.w, http.StatusOK, map[string]any{"remoteLink": linkView(l)})
			return
		}
	}
	l := &remoteLink{ID: uuid.New(), IssueID: is.ID, URL: req.URL, Title: req.Title, Source: req.Source, IconURL: req.IconURL,
		CreatedBy: c.person.ID, CreatedAt: now, UpdatedAt: now}
	c.tenant.links = append(c.tenant.links, l)
	respond(c.w, http.StatusCreated, map[string]any{"remoteLink": linkView(l)})
}

func (s *stub) deleteLink(c *call) {
	if c.readOnly() {
		return
	}
	is := s.linkedIssue(c)
	if is == nil || !s.linkWritable(c, is) {
		return
	}
	id, err := uuid.Parse(c.r.PathValue("remoteLinkID"))
	i := slices.IndexFunc(c.tenant.links, func(l *remoteLink) bool { return err == nil && l.ID == id && l.IssueID == is.ID })
	if i < 0 {
		refuse(c.w, http.StatusNotFound, "not_found", "That link was not found.")
		return
	}
	c.tenant.links = slices.Delete(c.tenant.links, i, i+1)
	c.w.WriteHeader(http.StatusNoContent)
}

// The controls under /_stub/ a test steers the stub with.

func (s *stub) setReads(c *call) {
	var req struct {
		// Projects are the keys of the projects the person may only read.
		Projects []string `json:"projects"`
	}
	if !decode(c, &req) {
		return
	}
	p := c.tenant.person(c.r.PathValue("person"))
	p.Reads = map[string]bool{}
	for _, key := range req.Projects {
		p.Reads[key] = true
	}
	c.w.WriteHeader(http.StatusNoContent)
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
	if req.Count > 0 && req.Status != http.StatusTooManyRequests && (req.Status < 500 || req.Status > 599) {
		refuseField(c.w, "status", "Name a 5xx status or 429.")
		return
	}
	c.tenant.outage, c.tenant.outageStatus = max(req.Count, 0), req.Status
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) allLinks(c *call) {
	out := []map[string]any{}
	byID := map[uuid.UUID]*issue{}
	for _, is := range c.tenant.issues {
		byID[is.ID] = is
	}
	names := map[uuid.UUID]string{}
	for _, p := range c.tenant.people {
		names[p.ID] = p.Display
	}
	for _, l := range c.tenant.links {
		v := linkView(l)
		if is := byID[l.IssueID]; is != nil {
			v["issueKey"] = is.Key
		}
		v["createdByName"] = names[l.CreatedBy]
		out = append(out, v)
	}
	respond(c.w, http.StatusOK, map[string]any{"remoteLinks": out})
}
