package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// RegisterFrontend serves the embedded React build, falling back to index.html for SPA routing.
func RegisterFrontend(mux *http.ServeMux, dist embed.FS) {
	sub, err := fs.Sub(dist, "web/dist")
	if err != nil {
		return
	}

	fileServer := http.FileServer(http.FS(sub))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Serve static files if they exist
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Try to open the file
		f, err := sub.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for all non-API, non-file routes
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
