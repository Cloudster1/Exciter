// Command armature-stub stands in for Armature in the test suites, serving
// the part of its API Exciter calls, held to Armature's document by a test.
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
	addrEnv    = "ARMATURE_STUB_ADDR"
	openAPIEnv = "ARMATURE_STUB_OPENAPI"
	// backchannelEnv holds public=reachable origin pairs, comma separated, for posting webhooks inside the stack.
	backchannelEnv = "ARMATURE_STUB_BACKCHANNEL"
	// issuersEnv names the identity provider issuers whose access tokens the exchange takes, comma separated, and
	// oidcBackchannelEnv how the stub reaches them, as public=reachable origin pairs.
	issuersEnv         = "ARMATURE_STUB_OIDC_ISSUERS"
	oidcBackchannelEnv = "ARMATURE_STUB_OIDC_BACKCHANNEL"
	defaultAddr        = ":8080"
	// defaultOpenAPI is where the backend image carries Armature's document.
	defaultOpenAPI = "/app/armature-openapi.json"

	readHeaderTimeout = 10 * time.Second
	shutdownGrace     = 5 * time.Second
	healthcheckWait   = 3 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("armature-stub exited", "error", err)
		os.Exit(1)
	}
}

func setting(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run() error {
	addr := setting(addrEnv, defaultAddr)
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck(addr)
	}
	doc, err := os.ReadFile(setting(openAPIEnv, defaultOpenAPI))
	if err != nil {
		return fmt.Errorf("read Armature's OpenAPI document; set %s to api/armature/openapi.json: %w", openAPIEnv, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var verify exchangestub.Verifier
	if issuers := exchangestub.List(os.Getenv(issuersEnv)); len(issuers) > 0 {
		verify = exchangestub.Keycloak(issuers, pairs(os.Getenv(oidcBackchannelEnv)))
	}
	stub := newStub(doc, verify)
	stub.backchannel = pairs(os.Getenv(backchannelEnv))
	srv := &http.Server{Addr: addr, Handler: stub.handler(), ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() {
		slog.Info("armature-stub listening", "addr", addr)
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
