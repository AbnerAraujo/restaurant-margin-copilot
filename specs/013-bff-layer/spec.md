# Feature Specification: A named BFF boundary for the owner app

**Feature Branch**: `feature/013-bff-layer`

**Created**: 2026-08-30

**Status**: Draft

**Input**: Product owner: *"Unify main backend + platform-connector behind one coherent API surface that the frontend talks to, instead of the frontend/callers reasoning about two separate backend concerns directly."*

## Spec number

`013`. `012-pos-connector-dedup` is the highest claimed number on any branch.

## The finding that reshapes this spec

The request assumes two backends. **There are not two backends.**

`backend/internal/platformconnector` is an ordinary in-process Go package. It is compiled into the same binary as everything else, registered on the same `http.ServeMux` in the same `main()`, served from the same origin and port, and reached over the same `/api/*` prefix. The frontend has exactly one base URL (`frontend/src/lib/api.ts`: `API_BASE`), one error type (`ApiError`), and one set of fetch helpers, and it has never had more.

So the literal reading of the request — introduce a BFF to unify two separately-deployed backend services — describes a problem this system does not have, and the honest answer is to say so rather than build a layer that unifies things that were never apart. Standing up a new deployable here would add a network hop, a pipeline, and an SLO in exchange for a boundary that already holds.

What *is* true is the second half of the sentence: **callers do reason about two separate backend concerns**, and the codebase gives them no help not to. The incoherence is real; it is just not where the request placed it. It is in five specific, evidenced places, listed below.

The BFF pattern's own literature covers this case directly. Sam Newman's and Phil Calçado's framing is that a BFF is an *ownership* pattern, not a deployment topology: the API one client application uses is part of that application. Azure's guidance names "only one interface interacts with the backend" as an explicit non-fit for adopting a BFF *service*, and the pattern catalogue's own cost table lists "modular-monolith BFF — per-experience modules, one deployable" as a first-class shape for exactly this situation: boundaries and ownership wanted, independent scaling and cadence not yet exercised. Newman's guidance for a codebase that already has the pattern latent is to apply the discipline to what exists rather than adding another box.

`internal/httpapi` **is already this product's BFF**. It serves one experience (the restaurant owner's React app), it has one consumer, it shapes rather than decides, and it holds no domain arithmetic. It has simply never been named as one, and because it was never named, nothing enforces the properties that make the boundary worth having.

This spec names it and enforces it. It adds no service, no hop, and no deployable.

## The five real defects

Each is evidenced from the tree as it stands on `main` at `b3157d8`, not inferred.

### D1 — There is no composition root, and it has already cost a shipped bug

`backend/cmd/server/main.go` registers seventeen routes by hand with seventeen `mux.HandleFunc` calls. Nothing anywhere can enumerate the API surface: not a test, not a doc generator, not the CORS layer, not a human without reading 120 lines of interleaved wiring and prose.

This is not a stylistic complaint. `withDevCORS` advertises `Access-Control-Allow-Methods` as a **single hand-maintained string literal** covering the whole mux. It has already been wrong once, in production-equivalent conditions, and the bug is documented in the code that fixed it:

> `backend/cmd/server/main.go`: *"a missing method here fails silently in the browser (a blocked CORS preflight, not a visible 405) while a direct curl/Postman request to the same handler succeeds — that gap is exactly what let PUT /api/profile ship broken from the real frontend despite the handler itself working."*

`backend/cmd/server/main_test.go` then pins the string with a regression test. That test is the correct response to the incident and the wrong response to the defect: it asserts that today's literal contains today's methods. It cannot fail for route eighteen, which is the only failure anyone needs.

A hand-maintained list that must stay in sync with a hand-maintained registration, where the failure mode is invisible in the browser and invisible to `curl`, is a defect that will recur. It is scheduled, not hypothetical.

### D2 — The preflight advertises methods no route serves

Because the allow-list is global, every route advertises `GET, POST, PUT, OPTIONS`. `GET /api/reconciliation` advertises `PUT`. `POST /api/usage` advertises `GET` and `PUT`. The only route that serves `PUT` is `/api/profile`.

Nothing exploitable follows from this in a prototype with no auth — the handlers still refuse — but it means the preflight answer is not a statement about the route, and a client that trusted it would be misled about the whole surface.

### D3 — Method dispatch is duplicated seventeen times, and one route dispatches by a hack

Every handler opens with its own `if r.Method != http.MethodX { writeJSONError(..., "method_not_allowed", ...) }`. That is seventeen copies of one policy.

`/api/promotions` needs two methods, so `main.go` grew `methodSplit(get, post)` — which routes `POST` to the POST handler and **everything else** to the GET handler, on the stated reasoning that the GET handler will reject what it does not like. It does, but the consequence is that a `DELETE /api/promotions` is answered by the listing handler's method check, and the shape of that answer is an accident of which handler happened to be the fallback.

### D4 — Two URL families do the same job with different shapes

The codebase says this itself. `internal/httpapi/connector_sync.go`:

> *"Three endpoints, mirroring `ingest_cost_sheet.go`'s preview/commit shape **because it is the same job** (stage something, look at it, then let it change the numbers)."*

The job is: bring new source data into the reconciliation engine, let the owner look at it first, then commit. It is exposed as:

- `GET /api/ingest/cost-sheet/template`, `POST /api/ingest/cost-sheet/preview`, `POST /api/ingest/cost-sheet/commit`
- `GET /api/connectors/platforms`, `POST /api/connectors/sync/preview`, `POST /api/connectors/sync`

Two prefixes, two naming schemes, two response vocabularies for one owner-facing concept. The frontend proves the cost: `UploadPage.tsx` is one page with two tabs, and each tab was written against a different API idiom. **This is the "two separate backend concerns" the product owner actually perceives** — the concerns are not "main backend" and "connector", they are "upload" and "connect", and they are the same concern.

### D5 — A panic reaches the client as a dropped connection

No handler is wrapped in recovery. `net/http` catches a handler panic per-connection so the process survives, but the client sees a closed socket, not the `{error, detail}` envelope every other failure produces. `frontend/src/lib/api.ts`'s `toApiError` is built to degrade gracefully here (it codes an unparseable body `unknown_error`), so the frontend already anticipates a failure mode the backend never converts into a response.

## What is *not* defective, and will not be touched

Recorded because the temptation with a refactor of this shape is to change things that are already right.

- **The error envelope is already uniform.** `writeJSON` / `writeJSONError` are shared across `internal/httpapi`, and the frontend's `ApiError` already parses one shape for every verb. Nothing to unify.
- **Context propagation is already correct.** `HandleConnectorSync` threads `r.Context()` into `Proxy.FetchRange`, which threads it into every per-day upstream call. A client disconnect already cancels the work.
- **Resilience machinery must stay absent.** The BFF pattern's resilience spine — retries, circuit breakers, bulkheads, hedging — assumes a *network* upstream. This project's connector upstream is a function call in the same process. `docs/architecture.html` already ruled this out for a stated reason: *"no retries, backoff, rate limiting, or circuit breaking (in-process function calls do not fail transiently, and simulating flakiness so resilience code has something to catch would be fiction stacked on fiction)."* That decision stands, and this spec explicitly declines to overturn it. Adding a circuit breaker around an in-process call would be pattern cosplay.
- **The MCP tool boundary is untouched.** `internal/mcptools` is the model's surface, not the frontend's. Nothing here widens, narrows, or reroutes it. No new tool, no tool removed.
- **No aggregate page endpoint.** See "Decided against", below.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — A new route cannot ship with a broken preflight (Priority: P1)

A developer adds route eighteen. They declare its methods once, in the place they add it. The CORS preflight, the 405 policy, and the startup log all follow from that declaration.

**Why this priority**: It is D1, it is the only defect with a confirmed prior incident, and every other item in this spec is cheaper once the route table exists.

**Acceptance**: A route added to the table with a method the preflight layer does not advertise is impossible to express — the preflight reads the table. A test asserts the *derivation*, not a literal.

### User Story 2 — The owner sees one list of where their data comes from (Priority: P2)

The owner opens "Data sources". They see four sources — the supplier cost sheet, iFood, Just Eat Takeaway, the in-house POS — described uniformly: what each is, how it arrives, and whether it is simulated. They do not have to know that one arrives as a file and three arrive from an emulated API to understand the list.

**Why this priority**: It is D4, and it is the defect the product owner actually felt.

**Acceptance**: One endpoint returns all four. The three simulated ones still carry `simulated: true` and the emulation notice, individually — a uniform list must not launder the disclosure.

### User Story 3 — A crash is an error, not a hang (Priority: P3)

A handler panics. The owner sees the same honest failure copy any other backend fault produces.

**Acceptance**: A route whose handler panics returns a `500` carrying the standard `{error, detail}` envelope, and the detail is a fixed string — never the panic value or a stack trace, which would leak internals into owner-facing copy.

## Requirements *(mandatory)*

- **FR-001** — The API surface MUST be declared as data in one place: for each route, its pattern, the methods it serves, its handler, and a one-line summary.
- **FR-002** — `Access-Control-Allow-Methods` MUST be derived per route from FR-001, never from a maintained literal. A preflight for a route MUST advertise exactly the methods that route serves, plus `OPTIONS`.
- **FR-003** — A request whose method the route does not serve MUST be refused with `405` and the standard error envelope, before the handler runs. `Allow` MUST be set, per RFC 9110.
- **FR-004** — `methodSplit` MUST be deleted. Multi-method routes MUST be expressed by declaring a method-to-handler mapping in the table.
- **FR-005** — A handler panic MUST become a `500` carrying the standard envelope and a fixed detail string. The panic value MUST NOT reach the response body.
- **FR-006** — One endpoint MUST describe every source of data this product ingests, uniformly, regardless of how it arrives.
- **FR-007** — Every simulated source in FR-006's response MUST carry its own `simulated: true` and its own emulation notice. A uniform shape MUST NOT reduce the disclosure to one flag at the top of the list.
- **FR-008** — Every route that exists on `main` MUST remain reachable at its current path with its current response shape. This refactor MUST NOT be observable to any existing frontend code path it does not deliberately rewire.
- **FR-009** — The origin policy MUST remain exactly as strict as it is today: `http(s)` on `localhost`/`127.0.0.1`, any port, parsed rather than substring-matched.

## Success Criteria *(mandatory)*

- **SC-001** — The eighteen-route surface is enumerable by a test, and adding a route to the table is the only edit needed to make it fully served.
- **SC-002** — `backend/cmd/server/main_test.go`'s CORS regression test is replaced by a test that fails when a route's *declared* methods and its *advertised* methods disagree — a test that can catch route eighteen, which the current one cannot.
- **SC-003** — All 18 Go packages and all 608 frontend tests stay green. Any number that moves is explained.
- **SC-004** — `main()` contains no `mux.HandleFunc` call.

## Decided against, with reasons

- **A separate BFF deployable.** No second experience, no second team, no independent cadence, no token confinement need (there is no login). All four of the pattern's adoption triggers are absent, and Azure's non-fit condition is met. A deployable would be cost with no revenue line.
- **An aggregate `GET /api/home`.** The textbook BFF win is collapsing a screen's fan-out. This app's worst fan-out is **two** calls (`HomePage`: reconciliation + badges; `PlatformsPage`: comparison + trend), both on localhost, and — decisively — `HomePage` already holds *independent* error state per call and renders correctly when one fails. Server-side composition would move working per-section degradation from a place it is correct to a place it would have to be rebuilt, to save one round trip. That is a regression dressed as a pattern.
- **Renaming the write paths** (`/api/connectors/sync`, `/api/ingest/cost-sheet/commit`) into one unified family. It is the logical end of D4, and it is a breaking change to two working frontend components with an interview deadline days away. FR-006 unifies the *read* side, which is where the owner's confusion actually lives; the write paths keep their URLs and a follow-up spec can finish the job. Recorded as knowingly unfinished rather than quietly skipped.
- **Retries, breakers, bulkheads, hedging.** See "What is not defective" — `docs/architecture.html` already rejected these with a better argument than this spec could make.
- **Path versioning.** One consumer, same repo, both sides changed together. The pattern literature is explicit that contract ceremony collapses at one controlled consumer.

## Constitution Check

- **Deterministic core** — This spec adds no arithmetic. The BFF layer routes, dispatches by method, sets headers, and recovers panics. FR-006's endpoint reads static descriptions. No number is computed anywhere in this change.
- **Refuse rather than guess** — FR-003 and FR-005 both *add* refusals: a wrong method and a panic each become an explicit, typed refusal instead of, respectively, a handler-dependent accident and a dropped socket. Nothing here introduces a fallback, default, or estimate. Note for the record: the BFF pattern's partial-failure ladder permits degrading a failed section to a "static/safe default" — **this constitution forbids that rung outright** for any numeric section, and this spec adopts no aggregation that could reach for it.
- **Typed tools only** — `internal/mcptools` has a zero-line diff. No new tool, no open SQL, no free-form computation.
- **Provenance** — No number is produced, so no provenance is created or lost. FR-007 explicitly protects the emulation disclosure from being flattened by the uniform shape, which is the one honesty property this refactor could plausibly damage.

No violations requiring justification.
