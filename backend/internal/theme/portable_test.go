package theme

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The fixtures are theme files exported from Armature, from Stator and from Exciter. Each has to read as a package
// this version knows and a spec the editor would save, unchanged.
func TestArmatureThemeFilesReadUnchanged(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.armature-theme.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no theme files under testdata: %v", err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		var pkg Package
		if err := dec.Decode(&pkg); err != nil {
			t.Errorf("%s does not parse as a package, or carries a field this version drops: %v", filepath.Base(file), err)
			continue
		}
		if pkg.Format != PackageFormat || strings.TrimSpace(pkg.Name) == "" {
			t.Errorf("%s is not a %s package with a name", filepath.Base(file), PackageFormat)
		}
		assets := map[uuid.UUID]bool{}
		for _, a := range pkg.Assets {
			assets[a.ID] = true
		}
		spec := pkg.Spec
		if err := Validate(&spec, uuid.Nil, assets); err != nil {
			t.Errorf("%s does not validate: %v", filepath.Base(file), err)
		}
	}
}

func TestTheMinecraftFileKeepsEveryPart(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "minecraft.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg Package
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "Minecraft" || pkg.Spec.Shape.RadiusControl == nil || *pkg.Spec.Shape.RadiusControl != 0 {
		t.Errorf("the name or the square corners were lost: %+v", pkg.Spec.Shape)
	}
	if len(pkg.Spec.Colors.Light) == 0 || len(pkg.Spec.Colors.Dark) == 0 || len(pkg.Spec.Shadows) != 3 || pkg.Spec.CSS == "" {
		t.Errorf("a part of the theme was lost: %+v", pkg.Spec)
	}
}

// Lucky Rainbow carries files: two cursors, drawn as SVG, and the moving
// picture the shell draws, every part of which has to survive the reading.
func TestTheLuckyRainbowFileKeepsItsFilesAndItsEffect(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "lucky-rainbow.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg Package
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "Lucky Rainbow" || pkg.Spec.Effect != "confetti" || len(pkg.Spec.Cursors) != 2 || len(pkg.Spec.Icons) == 0 {
		t.Errorf("the name, the effect, the cursors or the icons were lost: %q %q %d %d", pkg.Name, pkg.Spec.Effect, len(pkg.Spec.Cursors), len(pkg.Spec.Icons))
	}
	if len(pkg.Assets) != 2 {
		t.Fatalf("want the two cursor files, got %d", len(pkg.Assets))
	}
	for _, a := range pkg.Assets {
		if kind, err := sniff(a.Name, a.Data); err != nil || kind != a.ContentType {
			t.Errorf("%s reads as %q (%v), the file says %q", a.Name, kind, err, a.ContentType)
		}
	}
}

// A package leaves and comes back the same, files included: their bytes
// travel as base64, which is how Armature writes them.
func TestAPackageRoundTrips(t *testing.T) {
	file := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	themeID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12"/></svg>`)
	out := Package{
		Format: PackageFormat,
		Name:   "Traveller",
		Spec: Spec{
			Colors:   Palette{Light: map[string]string{"accent": "#123456"}, Dark: map[string]string{"accent": "#abcdef"}},
			Fonts:    Fonts{Mono: &Font{Family: "Mono Face"}},
			Shape:    Shape{RadiusControl: ptr(0), RadiusOverlay: ptr(4)},
			Shadows:  map[string]string{"2": "0 4px 8px rgb(0 0 0 / 0.2)"},
			Cursors:  map[string]Cursor{"pointer": {AssetID: file, HotspotX: 1, HotspotY: 2}},
			Icons:    map[string]Icon{"home": {AssetID: &file}},
			Backdrop: &Backdrop{AssetID: file, Fit: "tile"},
			Effect:   "confetti",
			CSS:      ".x { background: url(/api/v1/themes/" + themeID.String() + "/assets/" + file.String() + ") }",
		},
		Assets: []PackagedAsset{{ID: file, Name: "block.svg", ContentType: "image/svg+xml", Data: svg}},
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"data":"PHN2Zy`)) {
		t.Errorf("the file's bytes are not written as base64: %s", encoded)
	}
	var back Package
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, back) {
		t.Fatalf("the package changed on the way:\n out %+v\nback %+v", out, back)
	}
	spec := back.Spec
	if err := Validate(&spec, themeID, map[uuid.UUID]bool{file: true}); err != nil {
		t.Fatalf("the package that came back does not validate: %v", err)
	}
}

// The limits are Armature's, so a theme one product saves the other takes.
func TestTheLimitsAreArmatures(t *testing.T) {
	for name, pair := range map[string][2]int{
		"spec":        {MaxSpecBytes, 256 * 1024},
		"css":         {MaxCSSBytes, 32 * 1024},
		"tokens":      {MaxTokensPerSet, 64},
		"icons":       {MaxIcons, 128},
		"paths":       {MaxPathsPerIcon, 16},
		"path bytes":  {MaxPathBytes, 2048},
		"radius":      {MaxRadius, 32},
		"hotspot":     {MaxHotspot, 128},
		"shadow":      {MaxShadowBytes, 200},
		"family":      {MaxFamilyBytes, 64},
		"asset bytes": {MaxAssetBytes, 12 * 1024 * 1024},
		"assets":      {MaxAssetsPerSpec, 64},
		"package":     {MaxPackageBytes, 64 * 1024 * 1024},
	} {
		if pair[0] != pair[1] {
			t.Errorf("the %s limit is %d, Armature's is %d", name, pair[0], pair[1])
		}
	}

	tooMany := Spec{Colors: Palette{Light: map[string]string{}}}
	for i := 0; i <= MaxTokensPerSet; i++ {
		tooMany.Colors.Light["token-"+strings.Repeat("a", i+1)] = "#000"
	}
	if err := Validate(&tooMany, uuid.Nil, nil); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("a palette over the limit = %v", err)
	}
	icons := Spec{Icons: map[string]Icon{}}
	for i := 0; i <= MaxIcons; i++ {
		icons.Icons["icon-"+strings.Repeat("a", i+1)] = Icon{Paths: []string{"M0 0"}}
	}
	if err := Validate(&icons, uuid.Nil, nil); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("icons over the limit = %v", err)
	}
	paths := Spec{Icons: map[string]Icon{"home": {Paths: make([]string, MaxPathsPerIcon+1)}}}
	for i := range paths.Icons["home"].Paths {
		paths.Icons["home"].Paths[i] = "M0 0"
	}
	if err := Validate(&paths, uuid.Nil, nil); err == nil || !strings.Contains(err.Error(), "paths") {
		t.Errorf("paths over the limit = %v", err)
	}
	huge := Spec{CSS: "/*" + strings.Repeat(" ", MaxCSSBytes) + "*/"}
	if err := Validate(&huge, uuid.Nil, nil); err == nil {
		t.Error("css over the limit was accepted")
	}
	if err := Validate(nil, uuid.Nil, nil); err == nil {
		t.Error("no spec at all was accepted")
	}
}

// A token a later version of any of the three products adds travels through this one (#59): it validates as any
// well-formed name does and stays in the spec, and the tokens a file leaves out are simply not in it.
func TestATokenNoProductKnowsYetTravels(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "meadow.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg Package
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatal(err)
	}
	pkg.Spec.Colors.Light["coverage-full"] = "#15803d"
	assets := map[uuid.UUID]bool{}
	for _, a := range pkg.Assets {
		assets[a.ID] = true
	}
	if err := Validate(&pkg.Spec, uuid.Nil, assets); err != nil {
		t.Fatalf("a token Exciter does not know is refused: %v", err)
	}
	again, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var back Package
	if err := json.Unmarshal(again, &back); err != nil {
		t.Fatal(err)
	}
	if back.Spec.Colors.Light["coverage-full"] != "#15803d" {
		t.Errorf("the unknown token was lost: %v", back.Spec.Colors.Light)
	}
	if _, named := back.Spec.Colors.Light["chart-1"]; named {
		t.Error("a token the file leaves out came back with a value")
	}
}
