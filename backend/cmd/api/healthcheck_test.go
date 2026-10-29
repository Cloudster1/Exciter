package main

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// readiness serves /readyz with status, as the api's own does when its database is up or down.
func readiness(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// healthcheckRun runs the binary's own entry point as the container runtime does: api -healthcheck.
func healthcheckRun(t *testing.T, addr string) error {
	t.Helper()
	t.Setenv("EXCITER_HTTP_ADDR", addr)
	args := os.Args
	os.Args = []string{"api", "-healthcheck"}
	t.Cleanup(func() { os.Args = args })
	return run()
}

func TestTheHealthcheckPassesOnlyWhenReady(t *testing.T) {
	ready := readiness(t, http.StatusOK)
	if err := healthcheckRun(t, ready.Listener.Addr().String()); err != nil {
		t.Errorf("api -healthcheck against a ready api = %v, want it to pass", err)
	}
	// The api's own router with no database answers /readyz as it does when its database is down.
	down := httptest.NewServer((&httpapi.Server{Log: slog.New(slog.DiscardHandler)}).Routes(nil))
	t.Cleanup(down.Close)
	err := healthcheckRun(t, down.Listener.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "readiness returned 503") {
		t.Errorf("api -healthcheck against an api whose database is down = %v, want it to fail naming the 503", err)
	}
}

// The address the api listens on is usually just a port, which the check reaches on the loopback.
func TestTheHealthcheckReachesAPortOnTheLoopback(t *testing.T) {
	ready := readiness(t, http.StatusOK)
	_, port, err := net.SplitHostPort(ready.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := healthcheckRun(t, ":"+port); err != nil {
		t.Errorf("api -healthcheck with EXCITER_HTTP_ADDR=:%s = %v, want it to reach 127.0.0.1", port, err)
	}
}

func TestTheHealthcheckFailsWhenNothingListens(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	if err := healthcheckRun(t, addr); err == nil {
		t.Error("api -healthcheck with nothing listening passed")
	}
}
