// Package webui embeds the interface built from ui/ into the binary.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the interface files (empty if ui/ hasn't been built).
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist/app")
	if err != nil {
		panic(err)
	}
	return sub
}
