package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Armature's webhooks as the stub sends them (#29): a tenant registers an endpoint, and a change to its world,
// made through the API or the /_stub/ controls, is posted there as Armature's webhook service posts it.

const (
	// webhookVersion is the envelope version the stub sends, Armature's current one.
	webhookVersion = 1
	// webhookTimeout is how long Armature waits for an endpoint's answer.
	webhookTimeout = 10 * time.Second
	// keptDeliveries bounds what a tenant remembers of its deliveries.
	keptDeliveries = 100
)

// webhook is a tenant's registered endpoint.
type webhook struct {
	URL    string
	Secret string
}

// event is one delivery the stub owes, built while the world is locked and posted once it is not.
type event struct {
	to      webhook
	tenant  *tenant
	id      uuid.UUID
	topic   string
	version int
	payload map[string]any
}

// delivery is what became of an event, for a test to read.
type delivery struct {
	ID     uuid.UUID `json:"id"`
	Topic  string    `json:"topic"`
	Status int       `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// emit owes the tenant's endpoint an event, when it has one.
func (c *call) emit(topic string, payload map[string]any) {
	if c.tenant.webhook == nil {
		return
	}
	c.events = append(c.events, event{to: *c.tenant.webhook, tenant: c.tenant, id: uuid.New(), topic: topic, version: webhookVersion, payload: payload})
}

// sign is Armature's webhook.Sign: sha256= and the hex HMAC of the body.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// reachable is where the stub posts to an address people open under another name, as EXCITER_ARMATURE_BACKCHANNEL
// does for Exciter.
func (s *stub) reachable(address string) string {
	for public, inside := range s.backchannel {
		if rest, ok := strings.CutPrefix(address, public); ok {
			return inside + rest
		}
	}
	return address
}

// deliver posts the events, unlocked, so a receiver that calls back finds the stub free, and notes each outcome.
func (s *stub) deliver(ctx context.Context, events []event) []delivery {
	out := make([]delivery, 0, len(events))
	for _, e := range events {
		d := delivery{ID: e.id, Topic: e.topic}
		d.Status, d.Error = s.post(ctx, e)
		out = append(out, d)
		s.world.mu.Lock()
		e.tenant.deliveries = append(e.tenant.deliveries, d)
		if n := len(e.tenant.deliveries); n > keptDeliveries {
			e.tenant.deliveries = e.tenant.deliveries[n-keptDeliveries:]
		}
		s.world.mu.Unlock()
	}
	return out
}

func (s *stub) post(ctx context.Context, e event) (int, string) {
	body, err := json.Marshal(map[string]any{
		"version": e.version, "id": e.id, "topic": e.topic, "orgId": e.tenant.OrgID, "occurredAt": time.Now().UTC(), "payload": e.payload,
	})
	if err != nil {
		return 0, err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.reachable(e.to.URL), bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	sent := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Armature-Webhook")
	req.Header.Set("X-Armature-Event", e.topic)
	req.Header.Set("X-Armature-Delivery", uuid.NewString())
	req.Header.Set("X-Armature-Webhook-Version", strconv.Itoa(e.version))
	req.Header.Set("X-Armature-Signature-256", sign(body, e.to.Secret))
	req.Header.Set("X-Armature-Timestamp", sent)
	req.Header.Set("X-Armature-Signature-Timestamped-256", sign(append([]byte(sent+"."), body...), e.to.Secret))
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	return resp.StatusCode, ""
}

// The controls of the tenant's endpoint and of the changes a person makes in Armature.

func (s *stub) setWebhook(c *call) {
	var req struct {
		// URL is where to post, empty to stop; Secret signs every delivery.
		URL    string `json:"url"`
		Secret string `json:"secret"`
	}
	if !decode(c, &req) {
		return
	}
	c.tenant.webhook = nil
	if req.URL != "" {
		c.tenant.webhook = &webhook{URL: req.URL, Secret: req.Secret}
	}
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) listDeliveries(c *call) {
	respond(c.w, http.StatusOK, map[string]any{"deliveries": append([]delivery{}, c.tenant.deliveries...)})
}

// sendEvent owes the endpoint one event as given, a redelivery of an earlier id or another version included.
func (s *stub) sendEvent(c *call) {
	var req struct {
		Topic   string         `json:"topic"`
		Payload map[string]any `json:"payload"`
		ID      *uuid.UUID     `json:"id,omitempty"`
		Version int            `json:"version,omitempty"`
	}
	if !decode(c, &req) {
		return
	}
	if c.tenant.webhook == nil {
		refuse(c.w, http.StatusConflict, "no_webhook", "Register an endpoint with PUT /_stub/{tenant}/webhook first.")
		return
	}
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	c.emit(req.Topic, req.Payload)
	last := &c.events[len(c.events)-1]
	if req.ID != nil {
		last.id = *req.ID
	}
	if req.Version != 0 {
		last.version = req.Version
	}
	c.answer = func(done []delivery) { respond(c.w, http.StatusOK, done[len(done)-1]) }
}

type issueChange struct {
	Summary        *string `json:"summary"`
	StatusCategory *string `json:"statusCategory"`
}

// changeIssue edits or transitions an issue as the stub's admin, announcing each as Armature does.
func (s *stub) changeIssue(c *call) {
	var req issueChange
	if !decode(c, &req) {
		return
	}
	actor := c.tenant.person(adminName)
	is := c.tenant.lookup(actor, c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	now := time.Now().UTC()
	if req.Summary != nil && *req.Summary != is.Summary {
		c.emit("issue.updated", map[string]any{"issueId": is.ID, "key": is.Key, "actorId": actor.ID,
			"changes": []map[string]any{{"field": "summary", "from": is.Summary, "to": *req.Summary}}})
		is.Summary, is.UpdatedAt = *req.Summary, now
	}
	if req.StatusCategory != nil {
		to := c.tenant.statusOf(*req.StatusCategory)
		if to == nil {
			refuseField(c.w, "statusCategory", "Name todo, in_progress or done.")
			return
		}
		if to != is.Status {
			c.emit("issue.transitioned", map[string]any{"issueId": is.ID, "key": is.Key, "transition": "Move to " + to.Name,
				"fromStatus": is.Status.Name, "toStatus": to.Name, "fromStatusCategory": is.Status.Category, "toStatusCategory": to.Category,
				"actorId": actor.ID})
			is.Status, is.UpdatedAt, is.ResolvedAt = to, now, nil
			if to.Category == "done" {
				is.ResolvedAt = &now
			}
		}
	}
	respond(c.w, http.StatusOK, map[string]any{"issue": issueView(is)})
}

func (s *stub) moveIssue(c *call) {
	var req struct {
		ProjectKey string `json:"projectKey"`
	}
	if !decode(c, &req) {
		return
	}
	actor := c.tenant.person(adminName)
	is := c.tenant.lookup(actor, c.r.PathValue("issueKey"))
	to := c.tenant.project(req.ProjectKey)
	if is == nil || to == nil || to == is.Project {
		refuse(c.w, http.StatusNotFound, "not_found", "Name an issue and another project to move it to.")
		return
	}
	from := is.Key
	c.tenant.move(is, to, time.Now().UTC())
	c.emit("issue.updated", map[string]any{"issueId": is.ID, "key": is.Key, "actorId": actor.ID, "movedFrom": from,
		"changes": []map[string]any{{"field": "project", "from": from, "to": is.Key}}})
	respond(c.w, http.StatusOK, map[string]any{"issue": issueView(is)})
}

func (s *stub) deleteIssue(c *call) {
	actor := c.tenant.person(adminName)
	is := c.tenant.lookup(actor, c.r.PathValue("issueKey"))
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	delete(c.tenant.issues, is.Key)
	kept := c.tenant.links[:0]
	for _, l := range c.tenant.links {
		if l.IssueID != is.ID {
			kept = append(kept, l)
		}
	}
	c.tenant.links = kept
	c.emit("issue.deleted", map[string]any{"issueId": is.ID, "key": is.Key, "actorId": actor.ID})
	c.w.WriteHeader(http.StatusNoContent)
}

// takeOffLink removes the remote link to url from an issue as a person in Armature would, rather than its source.
func (s *stub) takeOffLink(c *call) {
	actor := c.tenant.person(adminName)
	is := c.tenant.lookup(actor, c.r.PathValue("issueKey"))
	url := c.r.URL.Query().Get("url")
	if is == nil {
		refuse(c.w, http.StatusNotFound, "not_found", "That issue was not found.")
		return
	}
	for i, l := range c.tenant.links {
		if l.IssueID == is.ID && l.URL == url {
			c.tenant.links = append(c.tenant.links[:i], c.tenant.links[i+1:]...)
			c.emit("issue.remote_link.removed", linkRemoved(is, l, actor))
			c.w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	refuse(c.w, http.StatusNotFound, "not_found", "That issue has no link to that address.")
}

// linkRemoved is the payload of issue.remote_link.removed, the link as it was.
func linkRemoved(is *issue, l *remoteLink, actor *person) map[string]any {
	return map[string]any{"issueId": is.ID, "key": is.Key, "linkId": l.ID, "actorId": actor.ID, "url": l.URL, "title": l.Title,
		"source": l.Source, "relation": l.Relation}
}
