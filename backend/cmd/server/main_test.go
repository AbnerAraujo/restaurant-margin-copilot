package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWithDevCORSAllowsProfilePUT is a regression test for the bug found in
// QA: the browser's CORS preflight for PUT /api/profile was rejected because
// withDevCORS only ever advertised "GET, POST, OPTIONS" in
// Access-Control-Allow-Methods, even though the actual /api/profile handler
// (httpapi.HandleProfile) supports GET and PUT. A direct curl -X PUT against
// the handler always worked — only the browser's own preflight, gated on
// this header, ever failed — which is why this needs to assert the header
// value directly rather than only exercising the handler.
func TestWithDevCORSAllowsProfilePUT(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the wrapped handler must not run for an OPTIONS preflight")
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/profile", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	rec := httptest.NewRecorder()

	withDevCORS(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	allowed := rec.Header().Get("Access-Control-Allow-Methods")
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodOptions} {
		if !strings.Contains(allowed, method) {
			t.Errorf("Access-Control-Allow-Methods = %q, want it to include %q (every method a route on this mux actually serves — see /api/profile's GET/PUT handler)", allowed, method)
		}
	}
}

// TestWithDevCORSReflectsLocalhostOrigin is a narrower sanity check that the
// preflight response still only reflects real localhost origins, so the PUT
// fix above didn't also loosen the origin allowlist.
func TestWithDevCORSReflectsLocalhostOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	cases := []struct {
		origin string
		want   string
	}{
		{"http://localhost:5173", "http://localhost:5173"},
		{"http://localhost:4173", "http://localhost:4173"},
		{"https://evil.example", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodOptions, "/api/profile", nil)
		req.Header.Set("Origin", tc.origin)
		rec := httptest.NewRecorder()

		withDevCORS(next).ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
			t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want %q", tc.origin, got, tc.want)
		}
	}
}
