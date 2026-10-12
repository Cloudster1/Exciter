package version

import "testing"

func TestCurrentPrefersTheLinkerAndNeverLeavesVersionEmpty(t *testing.T) {
	saved := [3]string{Version, Commit, BuiltAt}
	t.Cleanup(func() { Version, Commit, BuiltAt = saved[0], saved[1], saved[2] })

	Version, Commit, BuiltAt = "", "", ""
	if got := Current().Version; got == "" {
		t.Fatalf("an unversioned build still needs a version to show, got %q", got)
	}
	Version, Commit, BuiltAt = "1.2.3", "abc", "2026-09-28T00:00:00Z"
	got := Current()
	if got.Version != "1.2.3" || got.Commit != "abc" || got.BuiltAt != "2026-09-28T00:00:00Z" {
		t.Fatalf("linker values were not kept: %+v", got)
	}
}
