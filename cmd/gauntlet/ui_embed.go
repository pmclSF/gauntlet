//go:build !noui

package main

import (
	"embed"
	"io/fs"
)

//go:embed ui_dist
var embeddedUI embed.FS

func getEmbeddedUI() fs.FS {
	sub, err := fs.Sub(embeddedUI, "ui_dist")
	if err != nil {
		return nil
	}
	return sub
}
