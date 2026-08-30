package bff

// Tests against the REAL route table, not a synthetic one.
//
// These are the direct successors to cmd/server/main_test.go's
// TestWithDevCORSAllowsProfilePUT. That test was the right response to the
// incident it followed (PUT /api/profile shipping invisible to the browser)
// and the wrong response to the defect: it asserted that one hand-maintained
// string literal contained today's four methods, so it could not fail for
// route eighteen — the only failure anybody needed it to catch. It also
// could not live anywhere that saw the route table, because the table lived
// in package main, which is not importable.
//
// The tests below read the table. They fail when a route's declared methods
// and its advertised methods disagree, for any route, including ones that do
// not exist yet.

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// testRoutes builds the real table with zero-value dependencies. Every
// handler in it is a closure that captures its dependencies and touches them
// only when a request arrives, so constructing the table needs no Postgres,
// no API key, and no network. These tests never dispatch to a handler — they
// assert the boundary layers that sit in front of every handler — so the nil
// dependencies are never dereferenced.
func testRoutes(t *testing.T) []Route {
	t.Helper()
	return Routes(Deps{})
}

// TestEveryRouteAdvertisesExactlyWhatItServes is the test the old CORS
// regression test could not be. It is derivation-checking, not literal-
// checking: it walks the real table, sends each route a real preflight, and
// requires the advertised methods to equal the declared ones. A route added
// tomorrow is covered the moment it is added.
func TestEveryRouteAdvertisesExactlyWhatItServes(t *testing.T) {
	for _, rt := range testRoutes(t) {
		t.Run(rt.Pattern, func(t *testing.T) {
			declared := make([]string, 0, len(rt.Handlers)+1)
			for method := range rt.Handlers {
				declared = append(declared, method)
			}
			declared = append(declared, http.MethodOptions)
			sort.Strings(declared)

			req := httptest.NewRequest(http.MethodOptions, rt.Pattern, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			rec := httptest.NewRecorder()
			rt.handler().ServeHTTP(rec, req)

			want := strings.Join(declared, ", ")
			if got := rec.Header().Get("Access-Control-Allow-Methods"); got != want {
				t.Errorf("advertises %q, declares %q — a route must advertise exactly what it serves", got, want)
			}
		})
	}
}

// TestProfileRouteStillAdvertisesPUT is the named regression. The bug was
// specific and it cost real debugging time: a browser preflight for PUT
// /api/profile was rejected because the shared allow-methods literal listed
// only "GET, POST, OPTIONS", while a direct curl -X PUT against the same
// handler always worked. Keeping it by name means the incident stays
// findable from the test that guards it.
func TestProfileRouteStillAdvertisesPUT(t *testing.T) {
	route := findRoute(t, "/api/profile")

	req := httptest.NewRequest(http.MethodOptions, "/api/profile", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	rec := httptest.NewRecorder()
	route.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if allowed := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(allowed, http.MethodPut) {
		t.Errorf("Access-Control-Allow-Methods = %q, want it to include PUT", allowed)
	}
}

// TestReadOnlyRoutesNoLongerAdvertiseWrites pins the tightening this
// refactor introduced. On main every route advertised the union
// "GET, POST, PUT, OPTIONS", because one literal had to cover the whole mux
// — so GET /api/reconciliation told the browser it accepted PUT. Nothing
// was exploitable (the handlers still refused), but the preflight was not a
// statement about the route.
func TestReadOnlyRoutesNoLongerAdvertiseWrites(t *testing.T) {
	for _, pattern := range []string{"/api/reconciliation", "/api/badges", "/api/platforms"} {
		route := findRoute(t, pattern)

		req := httptest.NewRequest(http.MethodOptions, pattern, nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		route.handler().ServeHTTP(rec, req)

		allowed := rec.Header().Get("Access-Control-Allow-Methods")
		for _, write := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			if strings.Contains(allowed, write) {
				t.Errorf("%s advertises %s (%q); it is read-only", pattern, write, allowed)
			}
		}
	}
}

// TestPromotionsServesBothVerbsWithoutMethodSplit covers FR-004. On main
// this route needed a methodSplit helper that sent POST to the create
// handler and every OTHER method — DELETE, PATCH, HEAD — to the LISTING
// handler, on the reasoning that the listing handler would reject what it
// did not like. It does, but that made the shape of a DELETE's refusal an
// accident of which handler happened to be the fallback.
func TestPromotionsServesBothVerbsWithoutMethodSplit(t *testing.T) {
	route := findRoute(t, "/api/promotions")

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if _, ok := route.Handlers[method]; !ok {
			t.Errorf("/api/promotions does not declare %s", method)
		}
	}

	rec := httptest.NewRecorder()
	route.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/promotions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/promotions = %d, want %d from the router, not from whichever handler was the fallback",
			rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET, OPTIONS, POST" {
		t.Errorf("Allow = %q, want %q", got, "GET, OPTIONS, POST")
	}
}

// TestNoRouteDeclaresOPTIONS: preflights are answered by the CORS layer,
// above dispatch. A route that declared OPTIONS would be declaring a handler
// that can never run, and would silently suggest the layering is different
// from what it is.
func TestNoRouteDeclaresOPTIONS(t *testing.T) {
	for _, rt := range testRoutes(t) {
		if _, ok := rt.Handlers[http.MethodOptions]; ok {
			t.Errorf("%s declares an OPTIONS handler; the CORS layer answers preflights", rt.Pattern)
		}
	}
}

// TestEveryRouteIsWellFormed guards the table itself: an entry with no
// methods would register a pattern that refuses everything, and an entry
// with no summary would silently degrade the startup log the surface is now
// derived from.
func TestEveryRouteIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range testRoutes(t) {
		if len(rt.Handlers) == 0 {
			t.Errorf("%s declares no methods", rt.Pattern)
		}
		if rt.Summary == "" {
			t.Errorf("%s has no summary", rt.Pattern)
		}
		if !strings.HasPrefix(rt.Pattern, "/api/") {
			t.Errorf("%s is not under /api/", rt.Pattern)
		}
		if seen[rt.Pattern] {
			t.Errorf("%s is registered twice; the second would panic at mux registration", rt.Pattern)
		}
		seen[rt.Pattern] = true
		for method, handler := range rt.Handlers {
			if handler == nil {
				t.Errorf("%s declares %s with a nil handler", rt.Pattern, method)
			}
		}
	}
}

// TestSurfaceIsUnchangedFromMain is the FR-008 net: every path that existed
// on main before this refactor must still be registered, at the same path.
// Listed as a literal on purpose — this is the one place a hand-written list
// is the right tool, because its job is to be a snapshot of a previous
// commit rather than a description of the present.
func TestSurfaceIsUnchangedFromMain(t *testing.T) {
	before := []string{
		"/api/badges",
		"/api/reconciliation",
		"/api/promotions",
		"/api/platforms",
		"/api/platforms/trend",
		"/api/usage",
		"/api/client-errors",
		"/api/profile",
		"/api/ingest/cost-sheet/preview",
		"/api/ingest/cost-sheet/commit",
		"/api/ingest/cost-sheet/template",
		"/api/connectors/platforms",
		"/api/connectors/sync/preview",
		"/api/connectors/sync",
		"/api/ask",
		"/api/business-insight",
	}

	now := map[string]bool{}
	for _, rt := range testRoutes(t) {
		now[rt.Pattern] = true
	}
	for _, pattern := range before {
		if !now[pattern] {
			t.Errorf("%s was served on main and is no longer registered", pattern)
		}
	}
}

// TestNewServerRegistersEveryRoute proves the table actually reaches a mux —
// the tests above all exercise Route.handler() directly, which would still
// pass if NewServer dropped half the table on the floor.
func TestNewServerRegistersEveryRoute(t *testing.T) {
	routes := testRoutes(t)
	server := NewServer(routes)

	for _, rt := range routes {
		req := httptest.NewRequest(http.MethodOptions, rt.Pattern, nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		// A registered route answers its own preflight with 204. An
		// unregistered one falls through to the mux's 404.
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s: preflight through the mux = %d, want %d (is it registered?)",
				rt.Pattern, rec.Code, http.StatusNoContent)
		}
	}
}

func findRoute(t *testing.T, pattern string) Route {
	t.Helper()
	for _, rt := range testRoutes(t) {
		if rt.Pattern == pattern {
			return rt
		}
	}
	t.Fatalf("route %s is not in the table", pattern)
	return Route{}
}
