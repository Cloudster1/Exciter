//go:build integration

package test

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// workerBuildTimeout bounds building cmd/worker for the startup tests.
	workerBuildTimeout = 5 * time.Minute
	// workerRunTimeout bounds one worker the tests expect to stop on its own.
	workerRunTimeout = time.Minute
	// workerValkeyWait is cmd/worker's valkeyWait, five seconds, and the start before it: the database, the bucket.
	workerValkeyWait = 5*time.Second + 10*time.Second
)

var (
	workerOnce   sync.Once
	workerBinary string
	workerErr    error
)

// builtWorker is cmd/worker built once for the package, so a startup test runs the binary an operator does.
func builtWorker(t *testing.T) string {
	t.Helper()
	workerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "exciter-worker-")
		if err != nil {
			workerErr = err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), workerBuildTimeout)
		defer cancel()
		workerBinary = filepath.Join(dir, "worker")
		out, err := exec.CommandContext(ctx, "go", "build", "-o", workerBinary, "../cmd/worker").CombinedOutput()
		if err != nil {
			workerErr = &buildError{out: string(out), err: err}
		}
	})
	if workerErr != nil {
		t.Fatalf("build cmd/worker: %v", workerErr)
	}
	return workerBinary
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

// runWorkerBinary starts the worker with the suite's settings and overrides, and waits for it to stop on its own.
func runWorkerBinary(t *testing.T, env ...string) (string, time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), workerRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, builtWorker(t))
	// The metrics listener of a worker that did start must not take a port the suite uses.
	cmd.Env = append(append(os.Environ(), "EXCITER_METRICS_ADDR=127.0.0.1:0"), env...)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	took := time.Since(start)
	if ctx.Err() != nil {
		t.Fatalf("the worker was still running after %s:\n%s", workerRunTimeout, out)
	}
	return string(out), took, err
}

// A Valkey address that does not parse stops the worker at once, saying what one looks like.
func TestTheWorkerRefusesAValkeyURLThatDoesNotParse(t *testing.T) {
	newHarness(t)
	out, _, err := runWorkerBinary(t, "EXCITER_VALKEY_URL=not a url")
	if err == nil {
		t.Fatalf("the worker exited 0 with EXCITER_VALKEY_URL=not a url:\n%s", out)
	}
	if !strings.Contains(out, "is not a valid Valkey URL") {
		t.Errorf("the worker stopped without saying the URL is not valid:\n%s", out)
	}
}

// A Valkey that takes the connection and never answers stops the worker within valkeyWait, naming Valkey.
func TestTheWorkerGivesUpOnAValkeyThatDoesNotAnswer(t *testing.T) {
	newHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var held sync.WaitGroup
	t.Cleanup(func() {
		l.Close()
		held.Wait()
	})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			held.Go(func() {
				_, _ = io.Copy(io.Discard, conn)
				conn.Close()
			})
		}
	}()

	out, took, err := runWorkerBinary(t, "EXCITER_VALKEY_URL=redis://"+l.Addr().String()+"/0")
	if err == nil {
		t.Fatalf("the worker exited 0 with a Valkey that never answers:\n%s", out)
	}
	if !strings.Contains(out, "connect to Valkey") {
		t.Errorf("the worker stopped without naming Valkey:\n%s", out)
	}
	if took > workerValkeyWait {
		t.Errorf("the worker took %s to give up on a silent Valkey, want at most %s", took, workerValkeyWait)
	}
}
