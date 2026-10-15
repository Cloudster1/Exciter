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
	"strings"
	"time"

	"github.com/google/uuid"
)

// Stator's webhooks as the stub sends them (#41): a tenant registers an endpoint, and a change made through the
// /_stub/ controls is posted there as Stator's webhook service posts it, without a version or an organization.

const (
	// webhookTimeout is how long the stub waits for an endpoint's answer.
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
	c.events = append(c.events, event{to: *c.tenant.webhook, tenant: c.tenant, id: uuid.New(), topic: topic, payload: payload})
}

// sign is Stator's webhook.Sign: sha256= and the hex HMAC of the body.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// reachable is where the stub posts to an address people open under another name.
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
	body, err := json.Marshal(map[string]any{"id": e.id, "topic": e.topic, "occurredAt": time.Now().UTC(), "payload": e.payload})
	if err != nil {
		return 0, err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.reachable(e.to.URL), bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Stator-Webhook")
	req.Header.Set("X-Stator-Event", e.topic)
	req.Header.Set("X-Stator-Delivery", uuid.NewString())
	req.Header.Set("X-Stator-Signature-256", sign(body, e.to.Secret))
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	return resp.StatusCode, ""
}

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

// sendEvent owes the endpoint one event as given, a redelivery of an earlier id included.
func (s *stub) sendEvent(c *call) {
	var req struct {
		Topic   string         `json:"topic"`
		Payload map[string]any `json:"payload"`
		ID      *uuid.UUID     `json:"id,omitempty"`
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
	if req.ID != nil {
		c.events[len(c.events)-1].id = *req.ID
	}
	c.answer = func(done []delivery) { respond(c.w, http.StatusOK, done[len(done)-1]) }
}
