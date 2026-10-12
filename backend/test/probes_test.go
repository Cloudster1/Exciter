//go:build integration

package test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/observability"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// The probes and the metrics answer from a process wired to the real database.
func TestProbesAndMetricsAgainstTheDatabase(t *testing.T) {
	h := newHarness(t)
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "exciter-test"}, discard())
	if err != nil {
		t.Fatal(err)
	}
	if err := tel.Register(observability.NewClusterCollector(h.cluster)); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(observed(t, (&httpapi.Server{DB: h.cluster, Log: discard(), Telemetry: tel}).Routes(nil)))
	defer api.Close()
	metrics := httptest.NewServer(tel.Mux())
	defer metrics.Close()

	for _, probe := range []struct{ url, want string }{
		{api.URL + "/healthz", `"status":"ok"`},
		{api.URL + "/readyz", `"routing"`},
		{api.URL + httpapi.APIPrefix + "/openapi.json", `"title":"Exciter"`},
		{api.URL + httpapi.APIPrefix + "/meta", `"product":"Exciter"`},
		{metrics.URL + "/metrics", `exciter_db_pool_connections{pool="primary"`},
	} {
		resp, err := http.Get(probe.url)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), probe.want) {
			t.Errorf("GET %s = %s, want 200 with %s:\n%s", probe.url, resp.Status, probe.want, body)
		}
	}
}
