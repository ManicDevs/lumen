package version

import "runtime"

var (
	Version   = "0.1.0"
	Commit    = "unknown"
	Date      = "unknown"
	GoVersion = runtime.Version()
)

func String() string {
	return "lumen " + Version + " (" + Commit + " " + Date + ") " + GoVersion
}
