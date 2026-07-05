// Package version reports build information. Version and Date are injected at
// build time via -ldflags; the VCS commit falls back to the one Go embeds from
// the git checkout.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var (
	// Version is the release version (e.g. "v1.2.3"), set via -ldflags.
	Version = "dev"
	// Date is the build date, set via -ldflags.
	Date = ""
)

// String returns a human-readable one-line version summary.
func String() string {
	commit := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				commit = s.Value
			}
		}
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}

	s := "remotedesk " + Version
	if commit != "" {
		s += " (" + commit + ")"
	}
	if Date != "" {
		s += " " + Date
	}
	return fmt.Sprintf("%s %s/%s %s", s, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
