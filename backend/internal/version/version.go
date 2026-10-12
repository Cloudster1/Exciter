// Package version says which build of Exciter is running, for the log line that
// opens each process and for the API.
package version

import "runtime/debug"

// Set by the linker from the repository's VERSION file (see mk/go.mk). A build
// made without them falls back to what the Go toolchain recorded.
var (
	Version string
	Commit  string
	BuiltAt string
)

// Info is one build, as the API reports it.
type Info struct {
	// Version is the release, or "dev" when the linker was not told one.
	Version string `json:"version"`
	// Commit is the full git hash the binary was built from, when known.
	Commit string `json:"commit,omitempty"`
	// BuiltAt is when the binary was built, RFC 3339, when known.
	BuiltAt string `json:"builtAt,omitempty"`
}

// Current is the running build.
func Current() Info {
	info := Info{Version: Version, Commit: Commit, BuiltAt: BuiltAt}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.BuiltAt == "" {
					info.BuiltAt = s.Value
				}
			}
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}
