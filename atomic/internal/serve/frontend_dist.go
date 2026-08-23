// frontend/dist is gitignored build output — produced by "make frontend" (a
// prerequisite of build/test/vet) or "go generate ./...", never by hand. A
// bare "go build" on a fresh clone fails on this embed until one has run.
//
//go:generate bun run --cwd frontend build.ts
package serve

import "embed"

//go:embed all:frontend/dist
var embeddedFrontend embed.FS
