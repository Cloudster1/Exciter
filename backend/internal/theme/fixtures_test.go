package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// armatureFixtures are the example themes of Armature's docs/design/themes at
// 7380763, by their SHA-256: a fixture edited here would test nothing of Armature's.
var armatureFixtures = map[string]string{
	"lucky-rainbow.armature-theme.json": "e691490e73b8bb77e039612a3799234aeaa23f543263c2986fe9bab8d0a84050",
	"minecraft.armature-theme.json":     "f5e087b4e23e9ade216f61ec877d97f4bf5526e1e07d30f3fdb6b8bdd115ea82",
}

func TestTheFixturesAreArmaturesFilesByteForByte(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(armatureFixtures) {
		t.Fatalf("testdata holds %d theme files, want Armature's %d: %v", len(files), len(armatureFixtures), files)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		name := filepath.Base(file)
		if want, ok := armatureFixtures[name]; !ok || hex.EncodeToString(sum[:]) != want {
			t.Errorf("%s is not Armature's file as it came; copy it again from Armature's docs/design/themes", name)
		}
	}
}
