// Package web embeds the built frontend (web/dist) into the Go binary
// (ADR-0002). Run `make fe-build` to populate dist before `go build`; the
// committed dist/.gitkeep keeps `go build ./...` working on a fresh clone.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the built SPA with index.html at the filesystem root.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// The embed directive guarantees dist exists at compile time.
		panic(err)
	}
	return sub
}
