package server

import "embed"

// webDist holds the compiled Vue SPA. The build output lives under
// internal/server/webdist and is committed to the repository so the Go binary
// can be built without Node installed. The "all:" prefix also embeds files and
// directories whose names begin with "_" or ".".
//
// Run `npm run build` in web/ (or `make web-build`) to refresh it.
//
//go:embed all:webdist
var webDist embed.FS
