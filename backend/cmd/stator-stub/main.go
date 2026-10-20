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
	"strings"
	"syscall"
	"time"

	"github.com/cloudster1/exciter/backend/internal/exchangestub"
)

// Settings, from the environment like everything in the stack.
const (
	addrEnv = "STATOR_STUB_ADDR"
	// backchannelEnv holds public=reachable origin pairs, comma separated, for posting webhooks inside the stack.
	backchannelEnv = "STATOR_STUB_BACKCHANNEL"
	// issuersEnv names the identity provider issuers whose access tokens the exchange takes, comma separated, and
	// oidcBackchannelEnv how the stub reaches them, as public=reachable origin pairs.
	issuersEnv         = "STATOR_STUB_OIDC_ISSUERS"
	oidcBackchannelEnv = "STATOR_STUB_OIDC_BACKCHANNEL"
	defaultAddr        = ":8080"

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

	var verify exchangestub.Verifier
	if issuers := exchangestub.List(os.Getenv(issuersEnv)); len(issuers) > 0 {
		verify = exchangestub.Keycloak(issuers, pairs(os.Getenv(oidcBackchannelEnv)))
	}
	stub := newStub(verify)
	stub.backchannel = pairs(os.Getenv(backchannelEnv))
	srv := &http.Server{Addr: addr, Handler: stub.handler(), ReadHeaderTimeout: readHeaderTimeout}
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

// pairs reads "public=reachable" origins, comma separated.
func pairs(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		if public, inside, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && public != "" && inside != "" {
			out[public] = inside
		}
	}
	return out
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
