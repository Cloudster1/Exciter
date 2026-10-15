package version

import "testing"

func stamp(t *testing.T, version, commit, builtAt string) {
	t.Helper()
	saved := [3]string{Version, Commit, BuiltAt}
	t.Cleanup(func() { Version, Commit, BuiltAt = saved[0], saved[1], saved[2] })
	Version, Commit, BuiltAt = version, commit, builtAt
}

func TestCurrentPrefersTheLinkerAndNeverLeavesVersionEmpty(t *testing.T) {
	stamp(t, "", "", "")
	if got := Current().Version; got == "" {
		t.Fatalf("an unversioned build still needs a version to show, got %q", got)
	}
	Version, Commit, BuiltAt = "1.2.3+4.gabc1234", "abc1234", "2026-09-28T00:00:00Z"
	got := Current()
	if got.Version != "1.2.3+4.gabc1234" || got.Commit != "abc1234" || got.BuiltAt != "2026-09-28T00:00:00Z" {
		t.Fatalf("linker values were not kept: %+v", got)
	}
}

func TestATagReadsAsTheVersionWithoutItsPrefix(t *testing.T) {
	stamp(t, "v0.2.0", "", "")
	if got := Current().Version; got != "0.2.0" {
		t.Fatalf("the v of a tag should not reach the version, got %q", got)
	}
}

func TestAFullHashIsShortened(t *testing.T) {
	stamp(t, "0.2.0", "0123456789abcdef0123456789abcdef01234567", "")
	if got := Current().Commit; got != "0123456" {
		t.Fatalf("commit = %q, want the first %d characters", got, ShortCommitLength)
	}
}
