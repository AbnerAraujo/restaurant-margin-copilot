# Implementation Plan: A named BFF boundary for the owner app

**Spec**: [spec.md](./spec.md) · **Status**: Ready for implementation

## Why there is no tasks.md or checklists/

Specs 009–012 each carry `tasks.md` and `checklists/` because each was a user-facing feature with independently demonstrable user stories that could land in sequence. This is a refactor with one behavioural addition. Its steps are strictly ordered, none of them is separately demo-able, and the acceptance criterion for every one of them is the same sentence: *the existing suites stay green*. Splitting that into a task file and a checklist would be ceremony that adds no decision. The numbered steps below are the task list.

## Technical Context

One Go binary, one mux, one origin, one frontend consumer. `internal/httpapi` is the BFF and has been since spec 001; this change names it, gives it a composition root, and enforces the two boundary properties that a composition root makes possible (per-route CORS, per-route method dispatch).

The design constraint that shapes everything below: **`internal/httpapi`'s handler functions must not change signature.** They are `http.HandlerFunc` factories, they are directly unit-tested by ~20 test files that call them without any server, and those tests are the regression net this refactor is being judged against. A route table that required rewriting handler signatures would invalidate its own safety net.

So the new package wraps; it does not rewrite.

## Constitution Check

- **Principle I (deterministic core)**: Zero arithmetic added. The diff is routing, headers, and a static description list. ✅
- **Principle II (refuse rather than guess)**: Net *more* refusals — a 405 that is now uniform and pre-handler, and a panic that becomes an explicit 500 instead of a dropped connection. No default, fallback, or estimate is introduced. ✅
- **Principle III (typed tools)**: `internal/mcptools` zero-line diff. ✅
- **Principle IV (provenance)**: No number produced. The one honesty risk — flattening three per-source emulation notices into one list-level flag — is closed by FR-007 and pinned by a test. ✅

## Package placement

New package: `backend/internal/bff`.

Considered and rejected: putting the route table in `internal/httpapi` itself. `httpapi` already imports fifteen packages (`docs/architecture.html`'s dependency table) and is the widest package in the tree. The route table must import `httpapi`, `badges`, `platformconnector`, `storage`, `answercache` and the ask-pipeline wiring — that is a *composition* concern, and folding it into the package it composes would make `httpapi` import itself conceptually and would give the widest package a second job.

Considered and rejected: `cmd/server`. That is where it lives today, and the reason it must move is stated in `cmd/server/main.go`'s own doc comment — *"a package named `main` cannot be imported elsewhere"* — which is exactly why no test can enumerate the route surface today. `internal/bff` is importable, so SC-001 and SC-002 become possible.

`internal/bff` depends on `internal/httpapi`; nothing depends on `internal/bff` except `cmd/server`. It is a leaf at the top, which is what a composition root is.

## Step 1 — The route table type, and its tests, before any wiring

Test-first, per the project's build order.

```go
// Route is one entry on the owner app's API surface.
type Route struct {
	Pattern  string                          // "/api/profile"
	Handlers map[string]http.HandlerFunc     // method -> handler
	Summary  string                          // one line, for the startup log
}
```

`Handlers` keyed by method rather than a `Methods []string` plus one handler is the decision that makes FR-004 fall out for free: `/api/promotions` declares `{GET: list, POST: create}` and `methodSplit` has nothing left to do. It also makes FR-002 a map-key enumeration rather than a second field that could disagree with the first — the methods a route *advertises* and the methods it can *dispatch* are the same data, so they cannot drift. That property is the whole point of the exercise (D1), and encoding it in the type is stronger than testing for it.

Tests written first, in `internal/bff/router_test.go`:

1. A preflight for a `{GET}` route advertises `GET, OPTIONS` and not `POST` or `PUT`.
2. A preflight for a `{GET, PUT}` route advertises all three.
3. A `DELETE` to a `{GET}` route returns 405, sets `Allow`, and carries the `{error, detail}` envelope — and the handler never runs.
4. The origin allowlist still reflects `http://localhost:4173` and still rejects `https://evil.example` (FR-009 — carried over from `main_test.go` unchanged).
5. A handler that panics yields a 500 with the standard envelope and no panic value in the body.

## Step 2 — The middleware chain

One `http.Handler` per route, built as: `recover → CORS(route) → methodDispatch(route) → handler`.

Ordering rationale, since it is the only part with a wrong answer:

- **Recover outermost.** It must catch panics from the middleware below it, not only from the handler. A panic in method dispatch that escaped recovery would be the exact failure D5 describes.
- **CORS above dispatch.** An `OPTIONS` preflight must be answered by the CORS layer and must never reach dispatch — otherwise every route needs `OPTIONS` in its `Handlers` map, which would be noise in eighteen declarations to serve one layer's need.
- **Dispatch above the handler**, so a 405 costs no handler work and produces one shape for every route.

The handlers' own `if r.Method != …` guards stay where they are. They are now unreachable through the mux, but they are reachable — and asserted — from the ~20 `httpapi` test files that call the factories directly. Deleting them would be a green-field-correct change that breaks the regression net this refactor is measured by. They come out in a follow-up, once this has proven itself. Recorded here so the redundancy reads as deliberate rather than missed.

## Step 3 — Move the CORS layer, unchanged in behaviour

`withDevCORS`, `isLocalhostOrigin` move from `cmd/server` to `internal/bff`. `isLocalhostOrigin` moves verbatim — it is correct, it is tested, and FR-009 says it must not loosen.

`withDevCORS` changes in exactly one way: `Access-Control-Allow-Methods` is computed from the route's `Handlers` keys plus `OPTIONS`, sorted for a stable header, instead of read from a literal. Everything else — origin reflection, `Vary: Origin`, `Access-Control-Allow-Headers: Content-Type`, the 204 preflight — is untouched.

Note that this is strictly *tighter* than today: `GET /api/reconciliation` stops advertising `PUT`. It is a behaviour change, it is intended, and it is FR-002.

## Step 4 — Declare the eighteen routes

`internal/bff/routes.go`: one `Routes(deps Deps) []Route` function returning the table. `Deps` carries what the handlers need — `*storage.Queries`, `*answercache.Cache`, the connector proxy, `httpapi.Deps` for ask, and the advisor deps.

Every route currently in `main.go` is carried over at its exact path with its exact handler and its actual methods. The methods are read off each handler's own guard, not guessed:

| Pattern | Methods |
|---|---|
| `/api/badges` | GET |
| `/api/reconciliation` | GET |
| `/api/promotions` | GET, POST |
| `/api/platforms` | GET |
| `/api/platforms/trend` | GET |
| `/api/usage` | POST |
| `/api/client-errors` | POST |
| `/api/profile` | GET, PUT |
| `/api/ingest/cost-sheet/preview` | POST |
| `/api/ingest/cost-sheet/commit` | POST |
| `/api/ingest/cost-sheet/template` | GET |
| `/api/connectors/platforms` | GET |
| `/api/connectors/sync/preview` | POST |
| `/api/connectors/sync` | POST |
| `/api/ask` | POST |
| `/api/business-insight` | POST |
| `/api/sources` | GET *(new, step 6)* |

`main()` then reduces to: parse flags, connect, run ingestion if asked, build `Deps`, `bff.NewServer(deps)`, listen. FR-008 is checked by every existing test staying green; SC-004 by inspection.

**`/api/connectors/sync` and `/api/connectors/sync/preview` are a `ServeMux` subtlety worth stating**: Go 1.22+ patterns without a trailing slash match exactly, so these two do not shadow each other. This already works today; the table does not change it. Called out because the table makes the adjacency visible for the first time and it looks like a bug.

## Step 5 — Verify before adding anything

`go build ./... && go vet ./... && go test ./...` and the full frontend suite, on a refactor with zero intended behaviour change beyond the tightened preflight. **This gate is why the new endpoint is step 6 and not step 4**: if a number moves, it must be attributable to the refactor alone, with no new feature in the diff to confuse the attribution.

## Step 6 — `GET /api/sources` (FR-006, FR-007)

The one addition. A read-only endpoint listing every source this product ingests, uniformly:

```json
{
  "sources": [
    { "id": "supplier_cost_sheet", "name": "Supplier cost sheet", "kind": "file_upload",
      "simulated": false, "arrival": "…", "notice": "" },
    { "id": "ifood", "name": "iFood", "kind": "connector",
      "simulated": true, "arrival": "…", "notice": "Emulated connection. …" }
  ]
}
```

Design notes:

- `kind` (`file_upload` | `connector`) is what makes the list uniform *without lying*. The two families really are different in how data arrives; the endpoint's job is to present them in one vocabulary, not to pretend the difference does not exist. This is presentation shaping — the BFF's actual job — as opposed to deciding anything.
- The three connector entries are built from `platformconnector`'s existing `Describe()`, so the descriptions cannot drift from what the proxy actually registers. The cost-sheet entry is a static literal in the BFF, because there is no registry for it and inventing one to serve a description would be over-building.
- **`simulated` and `notice` are per source, never per list** (FR-007). Getting this wrong is the single way this endpoint could damage the project's honesty guarantees: the emulation disclosure exists in five independent places by design, and a "tidier" list-level flag would quietly become a sixth place it could be cropped from. A test asserts each simulated source carries its own notice.
- `GET /api/connectors/platforms` is left in place and keeps serving. `/api/sources` is a superset view, not a replacement, and retiring the old route is a frontend rewire this spec is not spending its risk budget on.

## Step 7 — Documentation

Per the project's standing instruction that docs move with the app:

- `CLAUDE.md` — the **Stack** section's *Backend* bullet currently reads as one flat service. It gains the BFF boundary: `internal/bff` composes, `internal/httpapi` shapes for the one consumer, `internal/mcptools` remains the model's separate typed surface. The deterministic/probabilistic split is unaffected and must not read as though it were.
- `docs/architecture.html` — the package dependency table gains `internal/bff`; the surrounding prose gains a short section on why this is a modular BFF and not a service, since "why didn't you split it" is the obvious interview question and the answer is the interesting part.
- `docs/prd.md` — a section 13 entry for this spec, in the established style.
- `CHANGELOG.md` — dated entry with real verification numbers.
- `docs/openapi.yaml` / `docs/api.html` — `/api/sources` added if those files enumerate routes; checked during the step.

## Step 8 — Full re-verification, then PR

Isolated Postgres, `gendata` + `-ingest` + `-ingest-promo`, `go build`/`go vet`/`go test ./...`, frontend `tsc -b --noEmit` + full `vitest run`. Branch pushed, PR into `develop`, left open.

## Risk register

| Risk | Mitigation |
|---|---|
| A route's real method set is narrower than a caller uses, so the tightened preflight breaks a working page | Methods read off each handler's own guard, not guessed; full frontend suite is the check |
| `ServeMux` pattern precedence differs between the hand-registered and table-driven registration | Same `mux.HandleFunc` calls in the same order, made from a loop instead of by hand — registration semantics are identical |
| The refactor and the new endpoint fail together and cannot be told apart | Step 5's gate exists solely to prevent this |
| `internal/bff` importing `httpapi` creates a cycle | `httpapi` does not and must not import `bff`; the dependency is one-way by construction and `go build` proves it |
