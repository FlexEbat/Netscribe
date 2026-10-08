// Package web embeds the built web interface.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built interface with index.html at its root.
func Dist() (fs.FS, error) { return fs.Sub(dist, "dist") }
