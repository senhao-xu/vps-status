// Package webui provides the static web application served by Control.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves embedded web assets and falls back to index.html for paths
// that will eventually be handled by the client-side router.
func Handler() http.Handler {
	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestedPath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if requestedPath == "." {
			requestedPath = ""
		}

		if requestedPath != "" {
			if _, err := fs.Stat(assets, requestedPath); err != nil {
				request = request.Clone(request.Context())
				request.URL.Path = "/"
			}
		}

		files.ServeHTTP(response, request)
	})
}
