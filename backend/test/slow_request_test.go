//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// slowRequestReport is how long a request runs before the test hears what the database is doing: well inside the
// api's deadline, so the report shows what the request waits on while it still waits (#405).
const slowRequestReport = 10 * time.Second

// watched logs the pools and the sessions holding locks when a request outlasts slowRequestReport, which on CI's
// runners has been the only trace of a write that waited half a minute.
func watched(t *testing.T, h *harness, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		timer := time.AfterFunc(slowRequestReport, func() {
			t.Logf("%s %s still runs after %s\n%s%s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond),
				poolReport(h.cluster.Stats()), h.lockReport())
		})
		defer timer.Stop()
		next.ServeHTTP(w, r)
	})
}

// poolReport is how full each of the cluster's pools is, and how long acquiring has waited for an idle connection.
func poolReport(s db.Stats) string {
	var b strings.Builder
	for _, p := range s.Pools {
		fmt.Fprintf(&b, "pool %s: %d of %d acquired, %d idle, %d being made, %d of %d acquires waited %s\n",
			p.Name, p.Acquired, p.Max, p.Idle, p.Constructing, p.EmptyAcquires, p.Acquires, p.Wait)
	}
	for _, r := range s.Replicas {
		fmt.Fprintf(&b, "replica %+v\n", r)
	}
	return b.String()
}
