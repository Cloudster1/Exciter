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

// bins are named mailboxes for deliveries: each keeps what it was sent and
// answers with the status it was told, 204 until then.
type bins struct {
	mu       sync.Mutex
	received map[string][]binDelivery
	status   map[string]int
}

func newBins() *bins {
	return &bins{received: map[string][]binDelivery{}, status: map[string]int{}}
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
	status, set := b.status[bin]
	b.mu.Unlock()
	if !set {
		status = http.StatusNoContent
	}
	w.WriteHeader(status)
}

func (b *bins) list(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	out := append([]binDelivery{}, b.received[r.PathValue("bin")]...)
	b.mu.Unlock()
	respond(w, http.StatusOK, map[string]any{"deliveries": out})
}

func (b *bins) setStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status int `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil || req.Status < 200 || req.Status > 599 {
		refuse(w, http.StatusBadRequest, "bad_request", "Send the status the bin should answer, 200 to 599.")
		return
	}
	b.mu.Lock()
	b.status[r.PathValue("bin")] = req.Status
	b.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (b *bins) forget(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	delete(b.received, r.PathValue("bin"))
	delete(b.status, r.PathValue("bin"))
	b.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
