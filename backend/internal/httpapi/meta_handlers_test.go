package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/version"
)

// Armature, Stator and Dynamo tell which build answers by this, so it must
// say exactly what the linker stamped, with no session at all.
func TestMetaReportsTheBuildStamp(t *testing.T) {
	saved := [3]string{version.Version, version.Commit, version.BuiltAt}
	t.Cleanup(func() { version.Version, version.Commit, version.BuiltAt = saved[0], saved[1], saved[2] })
	version.Version, version.Commit, version.BuiltAt = "0.1.0+4.gabc1234", "abc1234", "2026-10-14T09:00:00Z"

	rec := httptest.NewRecorder()
	newServer(t).Routes(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, APIPrefix+"/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("meta = %d %s", rec.Code, rec.Body)
	}
	raw, _ := io.ReadAll(rec.Body)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"product":      "exciter",
		"build":        map[string]any{"version": "0.1.0+4.gabc1234", "commit": "abc1234", "builtAt": "2026-10-14T09:00:00Z"},
		"apiVersion":   Spec().Info.Version,
		"capabilities": []any{"mcp@1", "requirement-embed@1", "requirement-list@1", "specifications@1", "theme-export@1", "trace-links@1", "webhooks@1", "webhooks@2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meta =\n%s\nwant %v", raw, want)
	}
}
