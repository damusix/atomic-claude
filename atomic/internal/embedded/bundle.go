// Package embedded holds the artifact bundle embedded at build time.
//
// bundle/ and manifest.go are generated, never edited by hand: context/ is the
// source, and the mirror tool copies it here because go:embed cannot reference
// a parent directory or follow a symlink. Refresh with "make bundle".
//
//go:generate go run ../tools/bundle-mirror -repo ../../../ -outdir .
package embedded

import "embed"

//go:embed bundle
var FS embed.FS
