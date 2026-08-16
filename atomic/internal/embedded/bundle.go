// Package embedded holds the artifact bundle embedded at build time.
//
// bundle/ and manifest.go are build artifacts, not sources: both are gitignored
// and written by the mirror tool (cmd/bundle-mirror) from the repo's context/
// tree. The mirror exists because go:embed cannot reference a parent directory
// and will not follow a symlink, so context/ has to be copied into this package
// before it can be embedded.
//
// Run "make bundle" from atomic/ to refresh; build, test, and vet already
// depend on it. Never edit bundle/ contents by hand — context/ is the source.
//
//go:generate go run ../../cmd/bundle-mirror -repo ../../../ -outdir .
package embedded

import "embed"

//go:embed bundle
var FS embed.FS
