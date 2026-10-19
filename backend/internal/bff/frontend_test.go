package bff

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFrontendFixture(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>app shell"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "index-abc123.js"), []byte("console.log(1)"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sw.js"), []byte("self.x=1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.webmanifest"), []byte(`{"name":"x"}`), 0o644))

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/badges" {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		http.NotFound(w, r)
	})
	h, err := WithFrontend(api, dir)
	require.NoError(t, err)
	return h
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestWithFrontend_APIPathsReachTheRouteTable(t *testing.T) {
	h := newFrontendFixture(t)

	assert.Equal(t, http.StatusTeapot, serve(h, http.MethodGet, "/api/badges").Code)
	assert.Equal(t, http.StatusTeapot, serve(h, http.MethodPost, "/api/badges").Code,
		"non-GET methods on /api/ must reach the table, which owns the 405 policy")

	unknown := serve(h, http.MethodGet, "/api/does-not-exist")
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.NotContains(t, unknown.Body.String(), "app shell",
		"an unknown API path must 404, never fall back to the app shell")
}

func TestWithFrontend_ServesFilesAndClientRoutes(t *testing.T) {
	h := newFrontendFixture(t)

	root := serve(h, http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, root.Code)
	assert.Contains(t, root.Body.String(), "app shell")
	assert.Equal(t, "no-cache", root.Header().Get("Cache-Control"))

	route := serve(h, http.MethodGet, "/promotions")
	assert.Equal(t, http.StatusOK, route.Code)
	assert.Contains(t, route.Body.String(), "app shell", "a client-side route gets index.html")

	asset := serve(h, http.MethodGet, "/assets/index-abc123.js")
	assert.Equal(t, http.StatusOK, asset.Code)
	assert.Equal(t, "console.log(1)", asset.Body.String())
	assert.Contains(t, asset.Header().Get("Cache-Control"), "immutable")

	sw := serve(h, http.MethodGet, "/sw.js")
	assert.Equal(t, http.StatusOK, sw.Code)
	assert.Equal(t, "no-cache", sw.Header().Get("Cache-Control"),
		"the service worker must be revalidated so a deploy is picked up")

	manifest := serve(h, http.MethodGet, "/manifest.webmanifest")
	assert.Equal(t, http.StatusOK, manifest.Code)
	assert.Equal(t, "application/manifest+json", manifest.Header().Get("Content-Type"),
		"a text/plain manifest can stop the browser offering to install the PWA")
}

func TestWithFrontend_MissingAssetIsA404NotTheAppShell(t *testing.T) {
	h := newFrontendFixture(t)

	rec := serve(h, http.MethodGet, "/assets/index-stale.js")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "app shell")
}

func TestWithFrontend_CannotEscapeTheDistDirectory(t *testing.T) {
	h := newFrontendFixture(t)

	for _, target := range []string{"/../../etc/passwd", "/assets/../../etc/passwd", "/%2e%2e/%2e%2e/etc/passwd"} {
		rec := serve(h, http.MethodGet, target)
		assert.NotContains(t, rec.Body.String(), "root:", target)
	}
}

func TestWithFrontend_RejectsWritesOutsideTheAPI(t *testing.T) {
	h := newFrontendFixture(t)

	rec := serve(h, http.MethodPost, "/promotions")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
}

func TestWithFrontend_RefusesADirectoryWithoutABuild(t *testing.T) {
	_, err := WithFrontend(http.NotFoundHandler(), t.TempDir())
	assert.ErrorContains(t, err, "index.html")
}
