package bff

// Tests for the boundary properties a composition root exists to enforce.
// These are written against the ROUTE TABLE, not against a literal, which is
// the whole point: cmd/server/main_test.go's predecessor asserted that one
// hand-maintained Access-Control-Allow-Methods string contained today's
// methods, and therefore could not fail for route eighteen — the only
// failure anybody needed it to catch (specs/013-bff-layer/spec.md, D1).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// okHandler is a handler that records that it ran. Several tests below assert
// a request was refused BEFORE the handler — a 405 that still executes the
// handler is not a refusal, it is a status code.
func okHandler(ran *bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusOK)
	}
}

func TestPreflightAdvertisesOnlyTheMethodsTheRouteServes(t *testing.T) {
	// The defect this pins: on main, every route advertised
	// "GET, POST, PUT, OPTIONS" from one shared literal, so
	// GET /api/reconciliation advertised PUT and POST /api/usage advertised
	// GET. The preflight answer was not a statement about the route.
	cases := []struct {
		name    string
		methods []string
		want    string
	}{
		{"read-only route", []string{http.MethodGet}, "GET, OPTIONS"},
		{"write-only route", []string{http.MethodPost}, "OPTIONS, POST"},
		{"read-write route", []string{http.MethodGet, http.MethodPut}, "GET, OPTIONS, PUT"},
		{"two-verb resource", []string{http.MethodGet, http.MethodPost}, "GET, OPTIONS, POST"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handlers := map[string]http.HandlerFunc{}
			for _, m := range tc.methods {
				handlers[m] = func(w http.ResponseWriter, r *http.Request) {}
			}
			route := Route{Pattern: "/api/thing", Handlers: handlers, Summary: "test"}

			req := httptest.NewRequest(http.MethodOptions, "/api/thing", nil)
			req.Header.Set("Origin", "http://localhost:5173")
			rec := httptest.NewRecorder()
			route.handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if got := rec.Header().Get("Access-Control-Allow-Methods"); got != tc.want {
				t.Errorf("Access-Control-Allow-Methods = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPreflightNeverRunsTheHandler: OPTIONS is answered by the CORS layer and
// must not reach dispatch — otherwise every route would have to declare an
// OPTIONS entry to satisfy one layer's need.
func TestPreflightNeverRunsTheHandler(t *testing.T) {
	ran := false
	route := Route{
		Pattern:  "/api/thing",
		Handlers: map[string]http.HandlerFunc{http.MethodGet: okHandler(&ran)},
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/thing", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	route.handler().ServeHTTP(httptest.NewRecorder(), req)

	if ran {
		t.Fatal("the preflight reached the route's handler; it must be answered by the CORS layer alone")
	}
}

// TestUnservedMethodIsRefusedBeforeTheHandler covers FR-003. On main this
// policy was seventeen copies of one `if r.Method != …` guard, plus
// methodSplit — which routed every non-POST method to the GET handler and
// let ITS guard decide the shape of the answer.
func TestUnservedMethodIsRefusedBeforeTheHandler(t *testing.T) {
	ran := false
	route := Route{
		Pattern:  "/api/thing",
		Handlers: map[string]http.HandlerFunc{http.MethodGet: okHandler(&ran)},
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/thing", nil)
	rec := httptest.NewRecorder()
	route.handler().ServeHTTP(rec, req)

	if ran {
		t.Error("the handler ran for a method the route does not serve; the refusal must precede it")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	// RFC 9110 makes Allow mandatory on a 405. It is also what tells a
	// caller what to do instead, which is the "how" half of this project's
	// what -> why -> how error rule.
	if got := rec.Header().Get("Allow"); got != "GET, OPTIONS" {
		t.Errorf("Allow = %q, want %q", got, "GET, OPTIONS")
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("405 body is not the standard {error, detail} envelope: %v", err)
	}
	if body["error"] != "method_not_allowed" {
		t.Errorf("error code = %q, want %q", body["error"], "method_not_allowed")
	}
	if body["detail"] == "" {
		t.Error("405 detail is empty; a refusal must say what is allowed instead")
	}
}

// TestPanicBecomesAnErrorNotADroppedConnection covers FR-005 and D5. net/http
// recovers a handler panic per connection so the process survives, but the
// client gets a closed socket rather than the {error, detail} envelope every
// other failure produces.
func TestPanicBecomesAnErrorNotADroppedConnection(t *testing.T) {
	route := Route{
		Pattern: "/api/thing",
		Handlers: map[string]http.HandlerFunc{
			http.MethodGet: func(w http.ResponseWriter, r *http.Request) {
				panic("a secret internal detail nobody outside this process should read")
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	rec := httptest.NewRecorder()
	route.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	raw := rec.Body.String()
	var body map[string]string
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&body); err != nil {
		t.Fatalf("panic body is not the standard envelope: %v (body %q)", err, raw)
	}
	if body["error"] != "internal_error" {
		t.Errorf("error code = %q, want %q", body["error"], "internal_error")
	}
	// The panic value must not reach the owner. A stack trace or a raw Go
	// string in owner-facing copy is the same class of leak as the
	// "platformconnector: " prefix connector_sync.go's ownerFacing strips.
	if strings.Contains(raw, "a secret internal detail") {
		t.Errorf("the panic value leaked into the response body: %q", raw)
	}
}

// TestOriginAllowlistUnchanged carries over cmd/server/main_test.go's
// TestWithDevCORSReflectsLocalhostOrigin verbatim in intent (FR-009): this
// refactor tightens the METHOD advertisement and must not loosen the ORIGIN
// policy while doing it.
func TestOriginAllowlistUnchanged(t *testing.T) {
	route := Route{
		Pattern:  "/api/thing",
		Handlers: map[string]http.HandlerFunc{http.MethodGet: func(w http.ResponseWriter, r *http.Request) {}},
	}

	cases := []struct{ origin, want string }{
		{"http://localhost:5173", "http://localhost:5173"},
		{"http://localhost:4173", "http://localhost:4173"},
		{"http://127.0.0.1:5173", "http://127.0.0.1:5173"},
		// The substring-check trap isLocalhostOrigin exists to avoid.
		{"http://localhost:5173.evil.example", ""},
		{"https://evil.example", ""},
		{"", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodOptions, "/api/thing", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		route.handler().ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
			t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want %q", tc.origin, got, tc.want)
		}
	}
}

// TestServedMethodReachesTheHandler is the boring half that keeps the three
// tests above honest: a router that refused everything would pass all of them.
func TestServedMethodReachesTheHandler(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		ran := false
		route := Route{
			Pattern:  "/api/thing",
			Handlers: map[string]http.HandlerFunc{method: okHandler(&ran)},
		}
		req := httptest.NewRequest(method, "/api/thing", nil)
		rec := httptest.NewRecorder()
		route.handler().ServeHTTP(rec, req)

		if !ran {
			t.Errorf("%s: the handler did not run for a method the route serves", method)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", method, rec.Code, http.StatusOK)
		}
	}
}
