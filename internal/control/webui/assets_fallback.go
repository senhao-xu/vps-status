//go:build !webui

package webui

import (
	"embed"
	"io/fs"
)

//go:embed fallback/*
var fallbackAssets embed.FS

var assets = sub(fallbackAssets, "fallback")

func sub(source fs.FS, directory string) fs.FS {
	result, err := fs.Sub(source, directory)
	if err != nil {
		panic(err)
	}

	return result
}
