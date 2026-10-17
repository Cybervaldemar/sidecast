// Package web embeds the production React build into the Go executable.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

func Assets() fs.FS {
	assets, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return assets
}
