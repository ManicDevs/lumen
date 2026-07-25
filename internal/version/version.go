// Package version provides build-time version metadata injected via -ldflags.
package version

import "runtime"

var (
	// Version is the semantic version string (e.g. "0.1.0").
	Version = "0.1.0"
	// Commit is the git commit hash at build time.
	Commit = "unknown"
	// Date is the RFC 3339 UTC timestamp of the build.
	Date = "unknown"
	// GoVersion is the Go toolchain version used to build the binary.
	GoVersion = runtime.Version()
)

// String returns a human-readable version summary for display and logging.
func String() string {
	return "lumen " + Version + " (" + Commit + " " + Date + ") " + GoVersion
}
