//go:build noui

package main

import "io/fs"

func getEmbeddedUI() fs.FS {
	return nil
}
