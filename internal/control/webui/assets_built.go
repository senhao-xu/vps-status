//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

// The webui build tag is used only after `pnpm --dir web build` creates dist.
//
//go:embed dist/*
var builtAssets embed.FS

var assets = sub(builtAssets, "dist")

func sub(source fs.FS, directory string) fs.FS {
	result, err := fs.Sub(source, directory)
	if err != nil {
		panic(err)
	}

	return result
}
