// Package version exposes the binary version and commit SHA, both stamped at
// build time via -ldflags -X and defaulting to "dev" / "unknown".
package version

var Version = "dev"

var Commit = "unknown"
