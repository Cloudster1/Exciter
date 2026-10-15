// Command stator-stub stands in for Stator in the test suites, serving the
// part of its API Exciter calls, held to Stator's document by a test.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Settings, from the environment like everything in the stack.
const (
	addrEnv     = "STATOR_STUB_ADDR"
	defaultAddr = ":8080"

	readHeaderTimeout = 10 * time.Second
	shutdownGrace     = 5 * time.Second
	healthcheckWait   = 3 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("stator-stub exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv(addrEnv)
	if addr == "" {
		addr = defaultAddr
	}
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck(addr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{Addr: addr, Handler: newStub().handler(), ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() {
		slog.Info("stator-stub listening", "addr", addr)
		served <- srv.ListenAndServe()
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// healthcheck lets the container check itself without curl in the image.
func healthcheck(addr string) error {
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	resp, err := (&http.Client{Timeout: healthcheckWait}).Get("http://" + addr + stubPrefix + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("the stub answered %s", resp.Status)
	}
	return nil
}
