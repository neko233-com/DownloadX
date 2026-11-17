// Package version holds build-time version information.
package version

import "strings"

// These values are overridden at build time with -ldflags "-X ...".
var (
	Version = "0.1.0-dev"
	Commit  = ""
	Date    = ""
)

// String returns the version, including the commit hash when available.
func String() string {
	s := Version
	if Commit != "" {
		s += " (" + Commit + ")"
	}
	return s
}

// Full returns version, commit and build date on three lines.
func Full() string {
	lines := []string{"downloadx " + Version}
	if Commit != "" {
		lines = append(lines, "commit: "+Commit)
	}
	if Date != "" {
		lines = append(lines, "built:  "+Date)
	}
	return strings.Join(lines, "\n")
}
