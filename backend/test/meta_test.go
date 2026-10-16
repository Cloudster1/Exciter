//go:build integration

package test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/version"
)

const (
	// envWebStack is the web container on the stack's network, which proxies /api to the api as a browser sees it.
	envWebStack = "EXCITER_TEST_WEB_STACK_URL"
	// What the stack's images were stamped with, from scripts/build-stamp.sh.
	envBuildVersion = "EXCITER_TEST_BUILD_VERSION"
	envBuildCommit  = "EXCITER_TEST_BUILD_COMMIT"
)

// The running stack names the build this checkout stamped, to anybody, through the web server as partners reach it.
func TestTheStackReportsTheBuildItWasStamped(t *testing.T) {
	web := os.Getenv(envWebStack)
	if web == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envWebStack)
	}
	resp, err := http.Get(web + httpapi.APIPrefix + "/meta")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var meta struct {
		Product      string       `json:"product"`
		Build        version.Info `json:"build"`
		APIVersion   string       `json:"apiVersion"`
		Capabilities []string     `json:"capabilities"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &meta) != nil {
		t.Fatalf("GET /api/v1/meta = %s, want the build:\n%s", resp.Status, body)
	}
	if meta.Product != httpapi.Product || meta.APIVersion != httpapi.Spec().Info.Version || meta.Capabilities == nil || meta.Build.BuiltAt == "" {
		t.Errorf("GET /api/v1/meta = %s", body)
	}
	// What a Stator page's requirement blocks check for (#140): the stack serves the build's own list.
	for _, name := range []string{"requirement-embed@1", "requirement-list@1", "webhooks@1"} {
		if !slices.Contains(meta.Capabilities, name) {
			t.Errorf("GET /api/v1/meta lists %v, without %s", meta.Capabilities, name)
		}
	}
	for _, stamp := range []struct{ env, got string }{
		{envBuildVersion, meta.Build.Version},
		{envBuildCommit, meta.Build.Commit},
	} {
		if want := os.Getenv(stamp.env); want != "" && stamp.got != want {
			t.Errorf("the api says %q where the build was stamped %q (%s); rebuild the stack with make stack-up", stamp.got, want, stamp.env)
		}
	}
}
