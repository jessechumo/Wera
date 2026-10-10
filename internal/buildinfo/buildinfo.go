// Package buildinfo carries the release version and commit, stamped in at
// build time:
//
//	go build -ldflags "-X wera/internal/buildinfo.Version=v1.2.3 -X wera/internal/buildinfo.Commit=abc1234"
//
// Without ldflags (go run, go test) Version is "dev" and Commit comes from
// the module's VCS stamp when available.
package buildinfo

import "runtime/debug"

var (
	// Version is the release tag, e.g. "v1.4.0".
	Version = "dev"
	// Commit is the short git commit the binary was built from.
	Commit = ""
)

func init() {
	if Commit != "" {
		return
	}
	Commit = "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		}
	}
}

// String is "v1.4.0 (abc1234)".
func String() string {
	return Version + " (" + Commit + ")"
}
