package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// partnerFixture is a theme file another product wrote, pinned by its SHA-256: a fixture edited here would test
// nothing of theirs.
type partnerFixture struct {
	sum, source string
}

// partnerFixtures are Armature's example themes of docs/design/themes at 7380763, and two themes exported from
// running installations of Armature and Stator, each made there with a file of its own (#59).
var partnerFixtures = map[string]partnerFixture{
	"lucky-rainbow.armature-theme.json": {"e691490e73b8bb77e039612a3799234aeaa23f543263c2986fe9bab8d0a84050", "Armature 7380763, docs/design/themes"},
	"minecraft.armature-theme.json":     {"f5e087b4e23e9ade216f61ec877d97f4bf5526e1e07d30f3fdb6b8bdd115ea82", "Armature 7380763, docs/design/themes"},
	"harbour.armature-theme.json":       {"e17a20ebc9c57efbe3a29acd904ca080b3df0f0670b47baad37048bb076df802", "Armature 0.1.0+dev at e3dccd1, exported over its API"},
	"meadow.armature-theme.json":        {"eee3574a277663035ac006c154ab4ad477017001e769ccfafd3cb583b725c86b", "Stator 0.1.0+dev at 9db315b, exported over its API"},
}

// exciterFixture is Exciter's own export, which the integration suite writes and keeps current.
const exciterFixture = "exciter.armature-theme.json"

func TestThePartnersFixturesAreTheirFilesByteForByte(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, file := range files {
		name := filepath.Base(file)
		if name == exciterFixture {
			continue
		}
		seen++
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		if want, ok := partnerFixtures[name]; !ok || hex.EncodeToString(sum[:]) != want.sum {
			t.Errorf("%s is not the partner's file as it came (%s); vendor it again rather than edit it", name, want.source)
		}
	}
	if seen != len(partnerFixtures) {
		t.Errorf("testdata holds %d partner theme files, want %d", seen, len(partnerFixtures))
	}
}
