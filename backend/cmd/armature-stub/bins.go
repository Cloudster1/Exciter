package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// hooksPrefix is where the stub receives Exciter's own webhooks (#48) for the test suites, as Stator's stub receives
// Stator's. It is no part of Armature, and lives beside the /_stub/ controls so no path of the two can mean both.
const hooksPrefix = "/_hooks"

// maxHookBody bounds what one bin keeps of a delivery.
const maxHookBody = 1 << 20

// binDelivery is one delivery a bin took, as the receiver saw it.
type binDelivery struct {
	Headers map[string]string `json:"headers"`
	// Body is the raw body, so a test can check the signature over it.
	Body string    `json:"body"`
	At   time.Time `json:"at"`
}

// maxHold bounds how long a bin keeps a sender waiting, past any sender's timeout.
const maxHold = time.Minute

// binAnswer is what a bin answers: a status, a Location to send the sender on to, and how long it waits first, so a
// suite can make a receiver that redirects or never answers in time.
type binAnswer struct {
	Status   int    `json:"status"`
	Location string `json:"location"`
	HoldMs   int    `json:"holdMs"`
}

// bins are named mailboxes for deliveries: each keeps what it was sent and
// answers as it was told, 204 at once until then.
type bins struct {
	mu       sync.Mutex
	received map[string][]binDelivery
	answer   map[string]binAnswer
}

func newBins() *bins {
	return &bins{received: map[string][]binDelivery{}, answer: map[string]binAnswer{}}
}

func (b *bins) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+hooksPrefix+"/{bin}", b.receive)
	mux.HandleFunc("GET "+hooksPrefix+"/{bin}", b.list)
	mux.HandleFunc("PUT "+hooksPrefix+"/{bin}/status", b.setStatus)
	mux.HandleFunc("DELETE "+hooksPrefix+"/{bin}", b.forget)
}

func (b *bins) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody))
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad_request", "The delivery could not be read.")
		return
	}
	headers := map[string]string{}
	for name := range r.Header {
		if strings.HasPrefix(name, "X-") || name == "Content-Type" || name == "User-Agent" {
			headers[name] = r.Header.Get(name)
		}
	}
	bin := r.PathValue("bin")
	b.mu.Lock()
	b.received[bin] = append(b.received[bin], binDelivery{Headers: headers, Body: string(body), At: time.Now().UTC()})
	answer, set := b.answer[bin]
	b.mu.Unlock()
	if !set {
		answer.Status = http.StatusNoContent
	}
	if answer.HoldMs > 0 {
		hold := time.NewTimer(time.Duration(answer.HoldMs) * time.Millisecond)
		defer hold.Stop()
		select {
		case <-hold.C:
		case <-r.Context().Done():
			return
		}
	}
	if answer.Location != "" {
		w.Header().Set("Location", answer.Location)
	}
	w.WriteHeader(answer.Status)
}

func (b *bins) list(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	out := append([]binDelivery{}, b.received[r.PathValue("bin")]...)
	b.mu.Unlock()
	respond(w, http.StatusOK, map[string]any{"deliveries": out})
}

func (b *bins) setStatus(w http.ResponseWriter, r *http.Request) {
	var req binAnswer
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil || req.Status < 200 || req.Status > 599 {
		refuse(w, http.StatusBadRequest, "bad_request", "Send the status the bin should answer, 200 to 599.")
		return
	}
	if req.HoldMs < 0 || time.Duration(req.HoldMs)*time.Millisecond > maxHold {
		refuse(w, http.StatusBadRequest, "bad_request", "Send a holdMs between 0 and a minute.")
		return
	}
	b.mu.Lock()
	b.answer[r.PathValue("bin")] = req
	b.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (b *bins) forget(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	delete(b.received, r.PathValue("bin"))
	delete(b.answer, r.PathValue("bin"))
	b.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
