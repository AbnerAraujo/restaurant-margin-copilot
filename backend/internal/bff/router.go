// Package bff is the composition root for the owner app's HTTP surface —
// the backend-for-frontend boundary named in specs/013-bff-layer.
//
// # Why this package exists, and why it is not a service
//
// This product has ONE experience (the restaurant owner's React app), ONE
// consumer of this API, and ONE team. `internal/httpapi` has been this
// experience's BFF since spec 001: it shapes results for one client, holds
// no arithmetic and no domain rules, and would leave nothing behind if it
// were deleted except the shaping.
//
// It was simply never NAMED as one, and because nothing named it, nothing
// enforced the properties that make the boundary worth having. That cost a
// real bug: the CORS preflight's Access-Control-Allow-Methods was one
// hand-maintained string literal covering the whole mux, and PUT
// /api/profile shipped broken from the browser — invisibly, since the
// failure is a blocked preflight rather than a visible 405, and a direct
// curl to the same handler succeeded. See cmd/server/main.go's own comment
// on that incident, and specs/013-bff-layer/spec.md D1.
//
// So this package adds no deployable, no network hop, and no service. The
// BFF pattern's own cost accounting (Newman; Azure's "Backends for
// Frontends", whose explicit non-fit is "only one interface interacts with
// the backend") puts a modular BFF — per-experience modules, one
// deployable — ahead of a separate service until independent scaling,
// cadence, or language is actually exercised. None of the three is. What
// this package buys instead is that the API surface becomes DATA:
//
//   - One route table (routes.go) is the only place the surface is declared.
//   - Access-Control-Allow-Methods is DERIVED from each route's own handler
//     map, so the methods a route advertises and the methods it can
//     dispatch are the same data and cannot drift apart.
//   - Method dispatch is one policy applied before the handler, not
//     seventeen copies of a guard plus a methodSplit fallback.
//   - A handler panic becomes the same {error, detail} envelope every other
//     failure produces, instead of a dropped connection.
//
// # What this package must never grow into
//
// Shaping, not deciding. No arithmetic (Constitution: all arithmetic is
// deterministic Go in internal/reconcile), no business rules, no
// persistence. If logic here ever starts deciding something about the
// restaurant's data rather than about the HTTP request, it belongs one
// layer down.
//
// Also deliberately absent: retries, backoff, circuit breakers, bulkheads,
// hedging. The BFF pattern's resilience spine assumes a NETWORK upstream;
// this product's connector "upstream" (internal/platformconnector) is a
// function call in the same process. docs/architecture.html already ruled
// this out — "in-process function calls do not fail transiently, and
// simulating flakiness so resilience code has something to catch would be
// fiction stacked on fiction" — and that decision stands. Context
// propagation, which is the part that IS real, already works: r.Context()
// is threaded from the handler into every per-day upstream call.
package bff

import (
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/httpapi"
)

// Route is one entry on the owner app's API surface.
//
// Handlers is keyed by HTTP method rather than being a []string of methods
// beside a single handler, and that is the load-bearing decision in this
// file. It means the set of methods a route ADVERTISES in its preflight and
// the set it can actually DISPATCH are literally the same map keys — they
// are one fact, so they cannot disagree. A `Methods []string` field beside a
// handler would be two facts that must be kept in sync, which is precisely
// the defect this package was written to remove, reintroduced one level in.
//
// It also makes multi-method routes fall out for free: /api/promotions
// declares {GET: list, POST: create} and cmd/server's old methodSplit —
// which sent every non-POST method to the GET handler and let that handler's
// own guard decide the shape of the answer — has nothing left to do.
type Route struct {
	// Pattern is the http.ServeMux pattern, e.g. "/api/profile". Patterns
	// here are all exact (no trailing slash), so /api/connectors/sync and
	// /api/connectors/sync/preview do not shadow each other.
	Pattern string

	// Handlers maps an HTTP method to the handler serving it. OPTIONS must
	// NOT appear here: preflights are answered by the CORS layer, above
	// dispatch, so that eighteen route declarations do not each carry an
	// entry to satisfy one layer's need.
	Handlers map[string]http.HandlerFunc

	// Summary is one line describing the route, for the startup log. The
	// log line on main was a 400-character hand-maintained string listing
	// every route; deriving it means it cannot fall out of date.
	Summary string
}

// allowedMethods returns the methods this route serves plus OPTIONS, sorted
// so the header is stable across process restarts (map iteration order is
// not). Stability matters for the tests, and for anyone diffing a response.
func (rt Route) allowedMethods() []string {
	methods := make([]string, 0, len(rt.Handlers)+1)
	for method := range rt.Handlers {
		methods = append(methods, method)
	}
	methods = append(methods, http.MethodOptions)
	sort.Strings(methods)
	return methods
}

// allowHeader is the allowedMethods list in the comma-space form both
// Access-Control-Allow-Methods and Allow use.
func (rt Route) allowHeader() string {
	return strings.Join(rt.allowedMethods(), ", ")
}

// handler builds the middleware chain for this route:
//
//	recoverPanic -> cors -> dispatch -> the route's own handler
//
// The ordering is the only part of this with a wrong answer:
//
//   - recoverPanic is OUTERMOST so it catches panics from the middleware
//     below it, not only from the handler. A panic escaping the dispatch
//     layer would be exactly the dropped-connection failure this exists to
//     remove.
//   - cors sits ABOVE dispatch so an OPTIONS preflight is answered and
//     returns without ever reaching dispatch (see Handlers' doc).
//   - dispatch sits ABOVE the handler so a 405 costs no handler work and
//     produces one shape for every route, rather than the shape of whichever
//     handler happened to receive it.
func (rt Route) handler() http.Handler {
	return recoverPanic(rt.cors(rt.dispatch()))
}

// dispatch selects the handler for the request's method, or refuses.
//
// Note that internal/httpapi's handlers keep their own `if r.Method != …`
// guards. Through this mux those guards are now unreachable, and that
// redundancy is deliberate rather than missed: roughly twenty httpapi test
// files call those handler factories DIRECTLY, with no server and no
// router, and assert the guards. Deleting them would be correct in a green
// field and would destroy the regression net this refactor is measured
// against. They come out in a follow-up, once this layer has proven itself.
func (rt Route) dispatch() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next, ok := rt.Handlers[r.Method]
		if !ok {
			// RFC 9110 makes Allow mandatory on a 405 — and it is also the
			// "how" half of this project's what -> why -> how error rule:
			// the refusal says what to do instead.
			w.Header().Set("Allow", rt.allowHeader())
			httpapi.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"this endpoint serves "+rt.allowHeader())
			return
		}
		next(w, r)
	})
}

// cors allows the frontend (a different origin/port from this API) to call
// it from the browser. A local-prototype convenience, not a production CORS
// policy — a real deployment would derive the origin rule from
// configuration rather than a hard-coded localhost allowance.
//
// The one thing that changed when this moved out of cmd/server:
// Access-Control-Allow-Methods is now computed from THIS route's own
// handler map instead of read from a single literal shared by every route.
// That is strictly tighter than before — on main, GET /api/reconciliation
// advertised PUT and POST /api/usage advertised GET, because one literal
// had to cover the union of everything. It is also the fix for the bug
// class described in this package's doc comment: the header can no longer
// be wrong for a route, because there is no longer anywhere to write it
// down incorrectly.
//
// Origin handling is carried over verbatim and must not loosen: a single
// hard-coded port was exactly wrong once the frontend could run from more
// than one (the installed PWA build on :4173 failed every fetch with a real
// CORS error), so any http(s) localhost origin is reflected — but reflected,
// never "*".
func (rt Route) cors(next http.Handler) http.Handler {
	allow := rt.allowHeader()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); isLocalhostOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", allow)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoverPanic converts a handler panic into the same {error, detail}
// envelope every other failure on this surface produces.
//
// Without it, net/http catches the panic per connection — so the process
// survives — but the client receives a CLOSED SOCKET rather than a
// response. The frontend's lib/api.ts already anticipates this (an
// unparseable body becomes a typed ApiError coded unknown_error), which
// means the browser has been handling a failure mode the backend never
// converted into one.
//
// The panic value is logged and never written to the response. A raw Go
// string or a stack trace in owner-facing copy is the same class of leak
// that connector_sync.go's ownerFacing strips the "platformconnector: "
// prefix to avoid.
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("PANIC serving %s %s: %v", r.Method, r.URL.Path, recovered)
				httpapi.WriteError(w, http.StatusInternalServerError, "internal_error",
					"The server hit an unexpected error handling this request. Nothing was changed.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// isLocalhostOrigin reports whether origin is an http(s) origin on localhost
// or 127.0.0.1, at any port — deliberately not a bare substring check, which
// "http://localhost:5173.evil.example" would slip past. Moved from
// cmd/server unchanged.
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
}

// NewServer registers every route in the table on a fresh mux and returns
// it. This is the entire body of what cmd/server's main() used to do by
// hand, seventeen times, interleaved with the prose explaining each one.
func NewServer(routes []Route) http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.Handle(rt.Pattern, rt.handler())
	}
	return mux
}

// LogSurface prints the served surface, derived from the table rather than
// from a hand-maintained sentence.
func LogSurface(routes []Route, addr string) {
	log.Printf("serving %d routes on %s — Ctrl+C to stop", len(routes), addr)
	for _, rt := range routes {
		log.Printf("  %-8s %-38s %s", strings.Join(methodsOnly(rt), ","), rt.Pattern, rt.Summary)
	}
}

// methodsOnly is allowedMethods without OPTIONS — the log is about what the
// route serves, and every route serves OPTIONS.
func methodsOnly(rt Route) []string {
	methods := make([]string, 0, len(rt.Handlers))
	for method := range rt.Handlers {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}
