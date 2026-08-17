// frontend/dist is build output — refresh it with "make frontend", never by
// hand.
//
//go:generate bun run --cwd frontend build.ts
package serve

import "embed"

//go:embed all:frontend/dist
var embeddedFrontend embed.FS
