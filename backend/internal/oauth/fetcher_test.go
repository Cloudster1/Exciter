package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/netguard"
)

// A metadata document is read from its own address alone: a redirect is not followed, a body past
// MaxClientMetadataBytes and any status but 200 are refused, and netguard keeps the fetch off the server's network.
func TestAMetadataDocumentIsFetchedFromItsOwnAddressAlone(t *testing.T) {
	var followed atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/moved.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere.json", http.StatusFound)
	})
	mux.HandleFunc("/elsewhere.json", func(w http.ResponseWriter, _ *http.Request) {
		followed.Store(true)
		_, _ = w.Write([]byte(`{"client_id":"elsewhere"}`))
	})
	mux.HandleFunc("/large.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat(" ", MaxClientMetadataBytes+1)))
	})
	mux.HandleFunc("/exact.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = w.Write([]byte(strings.Repeat(" ", MaxClientMetadataBytes)))
	})
	mux.HandleFunc("/missing.json", http.NotFound)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	fetch := HTTPFetcher(srv.Client())
	ctx := context.Background()

	if _, _, err := fetch(ctx, srv.URL+"/moved.json"); err == nil || !strings.Contains(err.Error(), "302") || followed.Load() {
		t.Errorf("a redirect answers %v, followed %t; want it refused with its status and not followed", err, followed.Load())
	}
	if _, _, err := fetch(ctx, srv.URL+"/large.json"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a document of MaxClientMetadataBytes+1 answers %v", err)
	}
	if body, keep, err := fetch(ctx, srv.URL+"/exact.json"); err != nil || len(body) != MaxClientMetadataBytes || keep != time.Minute {
		t.Errorf("a document of MaxClientMetadataBytes answers %d bytes kept %v: %v", len(body), keep, err)
	}
	if _, _, err := fetch(ctx, srv.URL+"/missing.json"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a missing document answers %v, want its status", err)
	}

	guarded := HTTPFetcher(netguard.Client(FetchTimeout, netguard.ParseAllow("")))
	if _, _, err := guarded(ctx, srv.URL+"/exact.json"); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("a document on %s through netguard answers %v, want ErrBlocked", srv.URL, err)
	}
}
