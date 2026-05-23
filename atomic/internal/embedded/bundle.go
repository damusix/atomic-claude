// Package embedded holds the artifact bundle embedded at build time.
//
// The bundle/ directory is populated by the mirror tool (cmd/bundle-mirror).
// Run "make bundle" from the atomic/ directory to refresh.
// The mirror tool is the source of truth; never edit bundle/ contents by hand.
//
//go:generate go run ../../cmd/bundle-mirror -repo ../../../ -outdir .
package embedded

import "embed"

//go:embed all:bundle
var FS embed.FS
