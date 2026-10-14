//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/theme"
)

// minecraftFile is a theme exported from Armature, kept as it came.
var minecraftFile = filepath.Join("..", "internal", "theme", "testdata", "minecraft.armature-theme.json")

// luckyRainbowFile is another, which carries files.
var luckyRainbowFile = filepath.Join("..", "internal", "theme", "testdata", "lucky-rainbow.armature-theme.json")

// A theme leaves as one file with its pictures inside and comes back as the
// importer's own, its files under new ids that the spec and the CSS now name.
func TestAThemeTravelsAsOneFile(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "porter")
	owner := api.as(t, home.user, home.org)

	made := want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Traveller", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "#123456"}}, "shape": map[string]any{"radiusControl": 0}}}), http.StatusCreated, "make a theme")
	themeID := obj(t, made, "theme")["id"].(string)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12v12H2z"/></svg>`)
	assetID := obj(t, want(t, owner.upload(t, "/api/v1/themes/"+themeID+"/assets", "block.svg", svg), http.StatusCreated, "upload"), "asset")["id"].(string)
	want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{
		"colors": map[string]any{"light": map[string]string{"accent": "#123456"}},
		"shape":  map[string]any{"radiusControl": 0},
		"icons":  map[string]any{"plus": map[string]any{"assetId": assetID}},
		"css":    ".x { background: url(/api/v1/themes/" + themeID + "/assets/" + assetID + ") }",
	}}), http.StatusOK, "name the file")

	resp, body := owner.download(t, "/api/v1/themes/"+themeID+"/export")
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), "Traveller.armature-theme.json") {
		t.Fatalf("export: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Disposition"), body)
	}
	var pkg theme.Package
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatalf("the export is not JSON: %v", err)
	}
	if pkg.Format != theme.PackageFormat || pkg.Name != "Traveller" || len(pkg.Assets) != 1 || string(pkg.Assets[0].Data) != string(svg) {
		t.Fatalf("package = %+v", pkg)
	}

	t.Run("somebody else cannot export a private theme", func(t *testing.T) {
		stranger := api.as(t, h.addPerson(t, home.org, "member"), home.org)
		if resp, _ := stranger.download(t, "/api/v1/themes/"+themeID+"/export"); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("somebody else exported a private theme: %d", resp.StatusCode)
		}
	})

	t.Run("imported, it is a theme of one's own with files of its own in the bucket", func(t *testing.T) {
		imported := want(t, owner.upload(t, "/api/v1/themes/import", "Traveller.armature-theme.json", body), http.StatusCreated, "import")
		back := obj(t, imported, "theme")
		if back["name"] != "Traveller (2)" || back["id"] == themeID {
			t.Fatalf("the import is not a second theme with a numbered name: %s", imported.Raw)
		}
		assets := back["assets"].([]any)
		if len(assets) != 1 || assets[0].(map[string]any)["id"] == assetID {
			t.Fatalf("the file did not get an id of its own: %s", imported.Raw)
		}
		newAsset := assets[0].(map[string]any)["id"].(string)
		newID := back["id"].(string)
		spec := back["spec"].(map[string]any)
		if spec["icons"].(map[string]any)["plus"].(map[string]any)["assetId"] != newAsset {
			t.Fatalf("the icon still names the old file: %s", imported.Raw)
		}
		if css := spec["css"].(string); !strings.Contains(css, "/api/v1/themes/"+newID+"/assets/"+newAsset) || strings.Contains(css, assetID) {
			t.Fatalf("the CSS still names the old file: %s", css)
		}
		if resp, data := owner.download(t, "/api/v1/themes/"+newID+"/assets/"+newAsset); resp.StatusCode != http.StatusOK || string(data) != string(svg) {
			t.Fatalf("the imported file does not read back: %d", resp.StatusCode)
		}
		if _, err := api.store.Get(context.Background(), themeObject(home.org, newID, newAsset)); err != nil {
			t.Fatalf("the imported file is not in the bucket: %v", err)
		}
	})

	t.Run("what is not a theme file is refused, and leaves nothing behind", func(t *testing.T) {
		before := len(list(t, want(t, owner.get(t, "/api/v1/themes"), http.StatusOK, "themes"), "themes"))
		if got := owner.upload(t, "/api/v1/themes/import", "notes.json", []byte(`{"hello": "world"}`)); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a stray file was imported: %d %s", got.Status, got.Raw)
		}
		if got := owner.upload(t, "/api/v1/themes/import", "notes.txt", []byte(`not even json`)); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a file that is not JSON was imported: %d %s", got.Status, got.Raw)
		}
		broken := `{"format":"armature-theme/1","name":"Broken","spec":{"backdrop":{"assetId":"00000000-0000-0000-0000-000000000001","fit":"cover"}},"assets":[]}`
		if got := owner.upload(t, "/api/v1/themes/import", "broken.json", []byte(broken)); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a theme naming a file it does not carry was imported: %d %s", got.Status, got.Raw)
		}
		later := `{"format":"armature-theme/2","name":"Later","spec":{},"assets":[]}`
		if got := owner.upload(t, "/api/v1/themes/import", "later.json", []byte(later)); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a package of a later format was imported: %d %s", got.Status, got.Raw)
		}
		if after := len(list(t, want(t, owner.get(t, "/api/v1/themes"), http.StatusOK, "themes"), "themes")); after != before {
			t.Fatalf("a refused import left a theme behind: %d -> %d", before, after)
		}
	})
}

// Armature's Minecraft theme imports unchanged, over the API and through the
// service alike, and leaves again as the same spec it came in as.
func TestTheMinecraftThemeFromArmatureImports(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "minecraft")
	owner := api.as(t, home.user, home.org)

	file, err := os.ReadFile(minecraftFile)
	if err != nil {
		t.Fatal(err)
	}
	var original theme.Package
	if err := json.Unmarshal(file, &original); err != nil {
		t.Fatal(err)
	}

	imported := want(t, owner.upload(t, "/api/v1/themes/import", "minecraft.armature-theme.json", file), http.StatusCreated, "import Minecraft")
	made := obj(t, imported, "theme")
	if made["name"] != "Minecraft" || made["ownerId"] != home.user.String() || made["shared"] != false {
		t.Fatalf("the import is not a private theme of one's own called Minecraft: %s", imported.Raw)
	}
	themeID := made["id"].(string)

	resp, exported := owner.download(t, "/api/v1/themes/"+themeID+"/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export: %d %s", resp.StatusCode, exported)
	}
	var back theme.Package
	if err := json.Unmarshal(exported, &back); err != nil {
		t.Fatal(err)
	}
	if back.Format != original.Format || back.Name != original.Name || !reflect.DeepEqual(back.Spec, normalised(t, original.Spec)) {
		t.Fatalf("Minecraft changed on the way through:\n  in %+v\n out %+v", original.Spec, back.Spec)
	}

	chosen := want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "use Minecraft")
	if obj(t, chosen, "theme", "spec", "shape")["radiusControl"].(float64) != 0 {
		t.Fatalf("the square corners did not survive: %s", chosen.Raw)
	}

	t.Run("the service imports it too, a second time under a numbered name", func(t *testing.T) {
		// The service hands back where each write landed; reading with it
		// pinned is what keeps the replica from answering with the past.
		again, lsn, err := api.themes.Import(home.ctx, home.user, &original)
		if err != nil {
			t.Fatal(err)
		}
		if again.Name != "Minecraft (2)" || !reflect.DeepEqual(again.Spec, normalised(t, original.Spec)) {
			t.Fatalf("the service's import = %q %+v", again.Name, again.Spec)
		}
		ctx := db.PinLSN(home.ctx, lsn)
		if _, err := api.themes.Export(ctx, again.ID, home.user); err != nil {
			t.Fatalf("export through the service: %v", err)
		}
		gone, err := api.themes.Delete(ctx, again.ID, home.user, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := api.themes.Get(db.PinLSN(ctx, gone), again.ID, home.user); !errors.Is(err, theme.ErrNotFound) {
			t.Fatalf("a deleted theme is still found: %v", err)
		}
	})
}

// Armature's Lucky Rainbow carries its cursors as files: imported, they land in the bucket under new ids the
// spec names, and the export hands the same pictures back.
func TestTheLuckyRainbowThemeImportsWithItsFiles(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "rainbow")
	owner := api.as(t, home.user, home.org)
	file, err := os.ReadFile(luckyRainbowFile)
	if err != nil {
		t.Fatal(err)
	}
	var original theme.Package
	if err := json.Unmarshal(file, &original); err != nil {
		t.Fatal(err)
	}
	made := obj(t, want(t, owner.upload(t, "/api/v1/themes/import", "lucky-rainbow.armature-theme.json", file), http.StatusCreated, "import"), "theme")
	themeID := made["id"].(string)
	assets := made["assets"].([]any)
	if made["name"] != "Lucky Rainbow" || len(assets) != len(original.Assets) {
		t.Fatalf("the import is not Lucky Rainbow with its %d files: %v", len(original.Assets), made)
	}
	cursors := made["spec"].(map[string]any)["cursors"].(map[string]any)
	for _, a := range assets {
		id := a.(map[string]any)["id"].(string)
		if _, err := api.store.Get(context.Background(), themeObject(home.org, themeID, id)); err != nil {
			t.Errorf("%s is not in the bucket: %v", a.(map[string]any)["name"], err)
		}
	}
	for kind, c := range cursors {
		if id := c.(map[string]any)["assetId"].(string); id == original.Spec.Cursors[kind].AssetID.String() {
			t.Errorf("the %s cursor still names the file's id in Armature", kind)
		}
	}
	resp, exported := owner.download(t, "/api/v1/themes/"+themeID+"/export")
	var back theme.Package
	if resp.StatusCode != http.StatusOK || json.Unmarshal(exported, &back) != nil {
		t.Fatalf("export: %d", resp.StatusCode)
	}
	byName := map[string][]byte{}
	for _, a := range back.Assets {
		byName[a.Name] = a.Data
	}
	for _, a := range original.Assets {
		if string(byName[a.Name]) != string(a.Data) {
			t.Errorf("%s left with other bytes than it came with", a.Name)
		}
	}
	if back.Spec.Effect != "confetti" || back.Spec.CSS != strings.TrimSpace(original.Spec.CSS) || len(back.Spec.Icons) != len(original.Spec.Icons) {
		t.Errorf("the effect, the CSS or the icons changed on the way through")
	}
}

// normalised is a spec the way the service stores it: every map present.
func normalised(t *testing.T, spec theme.Spec) theme.Spec {
	t.Helper()
	encoded, _ := json.Marshal(spec)
	var out theme.Spec
	_ = json.Unmarshal(encoded, &out)
	if err := theme.Validate(&out, [16]byte{}, nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// Without a bucket an upload is refused with the setting to fix, and nothing
// is half made.
func TestUploadsWithoutABucketAreRefusedPlainly(t *testing.T) {
	h := newHarness(t)
	home := h.owner(t, "no-bucket")
	svc := theme.NewService(h.cluster, objectstore.Unavailable{})
	made, lsn, err := svc.Create(home.ctx, home.user, theme.Input{Name: ptr("Bucketless")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UploadAsset(home.ctx, made.ID, home.user, false, "x.svg", strings.NewReader(`<svg></svg>`)); !errors.Is(err, objectstore.ErrUnavailable) {
		t.Fatalf("an upload with no bucket = %v, want objectstore.ErrUnavailable", err)
	}
	if !strings.Contains(objectstore.ErrUnavailable.Error(), "EXCITER_S3_ENDPOINT") {
		t.Error("the refusal does not name the setting to fix")
	}
	got, err := svc.Get(db.PinLSN(home.ctx, lsn), made.ID, home.user)
	if err != nil || len(got.Assets) != 0 {
		t.Fatalf("a refused upload left a file behind: %+v %v", got, err)
	}
}
