// Package version provides version information for micrun.
package version

import "runtime"

var (
	// Version holds the complete version number.
	// Filled in at build time via ldflags.
	// Default: "0.0.0+unknown"
	Version = "0.0.0+unknown"

	// Revision holds the VCS (e.g. git) revision.
	// Filled in at build time via ldflags.
	// Default: empty string
	Revision = ""

	// GoVersion is the Go runtime version.
	GoVersion = runtime.Version()
)
