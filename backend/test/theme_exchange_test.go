//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/theme"
)

// Themes between Exciter, Armature and Stator (#59): a file exported from any of the three imports here and leaves
// again the same, and Exciter's own export is a committed file the other two import in their tests.

// envUpdateFixtures rewrites Exciter's committed export from what this build exports, once its theme changed on
// purpose: EXCITER_UPDATE_FIXTURES=1 make test-integration TESTFLAGS='-run ExcitersExport'.
const envUpdateFixtures = "EXCITER_UPDATE_FIXTURES"

// exciterThemeFixture is Exciter's own export among the theme fixtures, beside the partners' files.
const exciterThemeFixture = "exciter.armature-theme.json"

var anyID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// travelled is what a theme file says once the ids each installation gave it are left out: files are numbered in the
// order the file carries them, and any other id, a theme's own in its CSS, is the theme.
type travelled struct {
	Name  string
	Spec  string
	Files []string
}

func travel(t *testing.T, raw []byte) travelled {
	t.Helper()
	var pkg theme.Package
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("not a theme file: %v", err)
	}
	if pkg.Format != theme.PackageFormat {
		t.Fatalf("a file of format %q", pkg.Format)
	}
	numbered := map[string]string{}
	out := travelled{Name: pkg.Name}
	for i, a := range pkg.Assets {
		numbered[a.ID.String()] = fmt.Sprintf("file-%d", i+1)
		out.Files = append(out.Files, fmt.Sprintf("%s %s %x", a.Name, a.ContentType, a.Data))
	}
	// All three products trim a theme's CSS as they save it, so a hand-written file's last newline is not a change.
	pkg.Spec.CSS = strings.TrimSpace(pkg.Spec.CSS)
	spec, err := json.Marshal(pkg.Spec)
	if err != nil {
		t.Fatal(err)
	}
	out.Spec = anyID.ReplaceAllStringFunc(string(spec), func(id string) string {
		if n, ok := numbered[id]; ok {
			return n
		}
		return "theme"
	})
	return out
}

// sameTravel fails the test where two files part ways, showing that place rather than both files whole.
func sameTravel(t *testing.T, what string, got, want travelled) {
	t.Helper()
	if got.Name != want.Name {
		t.Errorf("%s: the name became %q, want %q", what, got.Name, want.Name)
	}
	if got.Spec != want.Spec {
		at := 0
		for at < len(got.Spec) && at < len(want.Spec) && got.Spec[at] == want.Spec[at] {
			at++
		}
		from := max(at-80, 0)
		t.Errorf("%s: the spec parts ways at %d:\n got  ...%s\n want ...%s", what, at, got.Spec[from:min(at+120, len(got.Spec))],
			want.Spec[from:min(at+120, len(want.Spec))])
	}
	if fmt.Sprint(got.Files) != fmt.Sprint(want.Files) {
		t.Errorf("%s: the files are %d, want %d, or their bytes differ", what, len(got.Files), len(want.Files))
	}
}

func themeFile(name string) string {
	return filepath.Join("..", "internal", "theme", "testdata", name)
}

// roundTrip imports a file as owner and answers what exporting it again gives.
func roundTrip(t *testing.T, owner *client, name string, body []byte) []byte {
	t.Helper()
	imported := obj(t, want(t, owner.upload(t, "/api/v1/themes/import", name, body), http.StatusCreated, "import "+name), "theme")
	resp, out := owner.download(t, "/api/v1/themes/"+imported["id"].(string)+"/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export %s: %d %s", name, resp.StatusCode, out)
	}
	return out
}

func TestThemesTravelBetweenTheThreeProducts(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "theme-traveller").user)

	for _, tc := range []struct{ file, from string }{
		{"harbour.armature-theme.json", "exported from a running Armature"},
		{"meadow.armature-theme.json", "exported from a running Stator"},
		{"lucky-rainbow.armature-theme.json", "Armature's example with files and an effect"},
		{exciterThemeFixture, "Exciter's own export"},
	} {
		t.Run(tc.file+", "+tc.from+", imports and leaves again the same", func(t *testing.T) {
			body, err := os.ReadFile(themeFile(tc.file))
			if err != nil {
				t.Fatal(err)
			}
			sameTravel(t, tc.file+" on its way through Exciter", travel(t, roundTrip(t, owner, tc.file, body)), travel(t, body))
		})
	}

	t.Run("a token Exciter does not know travels through it, and one a file leaves out stays out", func(t *testing.T) {
		body, err := os.ReadFile(themeFile("meadow.armature-theme.json"))
		if err != nil {
			t.Fatal(err)
		}
		var pkg map[string]any
		if err := json.Unmarshal(body, &pkg); err != nil {
			t.Fatal(err)
		}
		light := pkg["spec"].(map[string]any)["colors"].(map[string]any)["light"].(map[string]any)
		light["coverage-full"] = "#15803d"
		pkg["name"] = "Meadow, with a later token"
		changed, _ := json.Marshal(pkg)
		var back theme.Package
		if err := json.Unmarshal(roundTrip(t, owner, "later.armature-theme.json", changed), &back); err != nil {
			t.Fatal(err)
		}
		if back.Spec.Colors.Light["coverage-full"] != "#15803d" {
			t.Errorf("the unknown token was lost: %v", back.Spec.Colors.Light)
		}
		if _, named := back.Spec.Colors.Light["chart-1"]; named {
			t.Error("a token the file leaves out came back with a value")
		}
	})
}

// workshopCursor is the file Exciter's committed theme carries.
const workshopCursor = `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32"><path d="M6 4h12l8 8v16H6z" fill="#b45309" stroke="#ffffff" stroke-width="2"/></svg>`

// Exciter's export of a theme made here, as the committed fixture holds it, so a change to what Exciter exports shows
// up as a failing test and the other two products' copies of the file can follow.
func TestExcitersExportIsTheCommittedFixture(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "theme-workshop").user)
	spec := map[string]any{
		"colors": map[string]any{
			"light": map[string]string{"canvas": "#fbf8f3", "primary": "#b45309", "primary-hover": "#92400e", "status-done": "#4d7c0f"},
			"dark":  map[string]string{"canvas": "#17120c", "primary": "#f59e0b", "primary-hover": "#fbbf24", "status-done": "#84cc16"},
		},
		"shape":   map[string]any{"radiusControl": 4, "radiusOverlay": 8},
		"shadows": map[string]string{"3": "0 12px 32px rgba(69, 26, 3, 0.3)"},
		"effect":  "constellation",
	}
	made := obj(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Workshop, from Exciter", "spec": spec}), http.StatusCreated, "make the theme"), "theme")
	id := made["id"].(string)
	asset := obj(t, want(t, owner.upload(t, "/api/v1/themes/"+id+"/assets", "grab.svg", []byte(workshopCursor)), http.StatusCreated, "upload the cursor"), "asset")
	spec["cursors"] = map[string]any{"grab": map[string]any{"assetId": asset["id"], "hotspotX": 12, "hotspotY": 8}}
	want(t, owner.patch(t, "/api/v1/themes/"+id, map[string]any{"spec": spec}), http.StatusOK, "give it the cursor")
	resp, exported := owner.download(t, "/api/v1/themes/"+id+"/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export: %d %s", resp.StatusCode, exported)
	}
	if os.Getenv(envUpdateFixtures) != "" {
		if err := os.WriteFile(themeFile(exciterThemeFixture), exported, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s; commit it, and ask Armature and Stator to vendor it again", exciterThemeFixture)
	}
	committed, err := os.ReadFile(themeFile(exciterThemeFixture))
	if err != nil {
		t.Fatalf("read the committed export; write it with %s=1: %v", envUpdateFixtures, err)
	}
	sameTravel(t, "Exciter's export against the committed one (write it again with "+envUpdateFixtures+"=1 if the change is meant)",
		travel(t, exported), travel(t, committed))
}
