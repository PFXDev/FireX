// Package version carries the build identity stamped in by the release
// workflow. A plain `go build` leaves the placeholders, which the updater
// reads as "locally built" and always treats as out of date.
package version

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func Info() map[string]string {
	return map[string]string{
		"version":    Version,
		"commit":     Commit,
		"build_time": BuildTime,
	}
}
