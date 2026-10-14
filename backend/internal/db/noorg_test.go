package db

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// organizationColumn is what product code named while an installation held organizations: its column, setting,
// tables and membership. armature_org_id is Armature's and stays.
var organizationColumn = regexp.MustCompile(`\b(org_id|current_org_id|app\.org_id|org_member|org_join_request)\b|(?i:\b(from|join|into|update|table)\s+org\b)`)

// The code no longer carries the organization (#107) nor names its tables (#108), which the schema has dropped, so
// no query fails only when it runs.
func TestNoProductCodeAsksForTheOrganization(t *testing.T) {
	seen := 0
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			seen++
			for i, line := range strings.Split(string(body), "\n") {
				if found := organizationColumn.FindString(line); found != "" {
					t.Errorf("%s:%d names %s; the installation is the only one, so leave it out", path, i+1, found)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// A walk that found nothing would pass for the wrong reason.
	if seen < 100 {
		t.Fatalf("only %d Go files read; the walk is not looking at the backend", seen)
	}
}
