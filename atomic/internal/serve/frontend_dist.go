// The frontend/dist directory is populated by the Bun build (see
// frontend/build.ts). Run "make frontend" from the atomic/ directory to
// refresh. The build script is the source of truth; never edit dist/
// contents by hand.
//
//go:generate bun run --cwd frontend build.ts
package serve

import "embed"

//go:embed all:frontend/dist
var embeddedFrontend embed.FS
