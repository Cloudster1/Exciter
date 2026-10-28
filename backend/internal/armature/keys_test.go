package armature

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/migrations"
)

// issueKeyCheck is the CHECK the issue link tables hold their keys to.
var issueKeyCheck = regexp.MustCompile(`issue_key ~ '([^']+)'`)

// A key the service takes is one the database stores, so a link Exciter accepted never fails on its CHECK.
func TestAKeyIsTakenAsTheDatabaseStoresIt(t *testing.T) {
	raw, err := migrations.FS.ReadFile("00310_armature_issue_links.sql")
	if err != nil {
		t.Fatal(err)
	}
	checks := issueKeyCheck.FindAllStringSubmatch(string(raw), -1)
	if len(checks) == 0 {
		t.Fatal("no CHECK on issue_key in the migration that makes the link tables")
	}
	for _, c := range checks {
		if c[1] != KeyPattern {
			t.Errorf("the database holds keys to %s, the service to %s", c[1], KeyPattern)
		}
	}

	for raw, want := range map[string]string{" cp-12 ": "CP-12", "CP-1": "CP-1", "a1-9": "A1-9", "ABCDEFGHIJ-1": "ABCDEFGHIJ-1",
		"CP-" + strings.Repeat("9", 18): "CP-" + strings.Repeat("9", 18)} {
		if key, ok := NormalizeKey(raw); !ok || key != want {
			t.Errorf("NormalizeKey(%q) = %q %v, want %q", raw, key, ok, want)
		}
	}
	for _, raw := range []string{"CP-0", "CP-01", "ABCDEFGHIJK-1", "CP-" + strings.Repeat("9", 19), "C-1", "1P-1", "CP_1", "CP-1 2", ""} {
		if key, ok := NormalizeKey(raw); ok {
			t.Errorf("NormalizeKey(%q) took %q", raw, key)
		}
	}
}
