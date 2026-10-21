//go:build integration

package test

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// testLog is a logger whose warnings and errors land in t's own log, so a
// 500 the test sees names its cause (#293). Once t is over it drops what is
// left, since t.Log then panics.
func testLog(t *testing.T) *slog.Logger {
	w := &testWriter{t: t}
	t.Cleanup(w.close)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

type testWriter struct {
	mu   sync.Mutex
	t    *testing.T
	done bool
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.done {
		w.t.Log(strings.TrimSuffix(string(p), "\n"))
	}
	return len(p), nil
}

func (w *testWriter) close() {
	w.mu.Lock()
	w.done = true
	w.mu.Unlock()
}
