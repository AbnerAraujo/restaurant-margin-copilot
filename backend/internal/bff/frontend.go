package bff

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

// WithFrontend serves the built React app (frontend/dist) from the same
// origin as the API, so a deployed instance is one binary on one public
// URL: the browser loads the page and calls /api/* without any cross-origin
// hop, which is why the localhost-only CORS rule in router.go needs no
// production loosening.
//
// It wraps the route table's handler rather than adding a route to it on
// purpose. The table derives the 405 policy, the CORS preflight, and the
// startup log from its rows; a catch-all "serve a file" row would have to
// pretend to be an API route to fit. Here the split is a single prefix
// check, and everything under /api/ still reaches the table untouched,
// including its 404 for an unknown API path, which must never be masked by
// the app shell.
//
// Everything else is the single-page app: an existing file is served as is,
// a missing path WITHOUT an extension is a client-side route (/close,
// /promotions) and gets index.html, and a missing path WITH one is a real
// 404 so a stale asset URL fails loudly instead of parsing HTML as JS.
func WithFrontend(api http.Handler, distDir string) (http.Handler, error) {
	// Go's built-in MIME table does not know the PWA manifest's extension
	// and would serve it as text/plain, which browsers can reject when
	// offering "install as app".
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		return nil, fmt.Errorf("registering the web manifest MIME type: %w", err)
	}

	fsys := os.DirFS(distDir)
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return nil, fmt.Errorf("frontend directory %q has no index.html (run `npm run build` in frontend/ first): %w", distDir, err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// path.Clean on a rooted path cannot climb above "/", and fs.FS
		// rejects ".." segments anyway: no request can name a file outside
		// distDir.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
				setCacheHeaders(w, name)
				http.ServeFileFS(w, r, fsys, name)
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, fsys, "index.html")
	}), nil
}

// setCacheHeaders lets the browser keep Vite's content-hashed bundles
// forever (a new build changes their names) while every unhashed file, the
// service worker and manifest included, is revalidated on each load, so a
// deploy is picked up on the next visit rather than after a stale cache
// expires.
func setCacheHeaders(w http.ResponseWriter, name string) {
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}
