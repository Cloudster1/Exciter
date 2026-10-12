package observability

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The metrics port answers a scrape with every series the dashboards expect,
// and two processes' registries in one binary do not collide.
func TestMetricsEndpointServesEverySeries(t *testing.T) {
	tel, err := Setup(t.Context(), Config{Service: "test"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(t.Context(), Config{Service: "other"}, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("a second registry collided with the first: %v", err)
	}
	tel.Metrics.HTTPRequests.WithLabelValues("GET", "/healthz", "200").Inc()
	tel.Metrics.HTTPDuration.WithLabelValues("GET", "/healthz").Observe(0.01)
	tel.Metrics.DBQuery.WithLabelValues("select", "primary").Observe(0.01)

	srv := httptest.NewServer(tel.Mux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics = %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"exciter_http_requests_total", "exciter_http_request_duration_seconds", "exciter_http_in_flight",
		"exciter_db_query_duration_seconds", "go_goroutines",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the scrape has no %s", want)
		}
	}
}

func TestFirstWordKeepsTheSeriesFew(t *testing.T) {
	for sql, want := range map[string]string{
		"  with x as (select 1) select 1": "with",
		"SELECT 1":                        "select",
		"":                                "other",
		"VACUUM":                          "other",
	} {
		if got := firstWord(sql); got != want {
			t.Errorf("firstWord(%q) = %q, want %q", sql, got, want)
		}
	}
}
