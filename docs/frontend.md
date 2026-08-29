# Frontend reference: design system and architecture

**Status:** Reference · **Scope:** `frontend/` (React + TypeScript + Vite + Tailwind v4)

This is the grep-able, in-editor companion to `docs/architecture.html`'s "01
Design system" tab. That tab is the visual specimen sheet — live-rendered
tokens, buttons, badges, and type roles in both themes, meant to be looked
at. This document is for someone with the code open: it names the actual
files, the actual usage counts, and the constraints those files encode, so a
contributor can find "where does this rule live" without re-deriving it from
the rendered page. Where the two overlap (colour families, spacing scale,
component specimens), this doc cross-references the HTML tab rather than
re-describing the same swatches in prose — read that tab first for the
visual form, this doc for the source-level detail and the frontend's
non-visual architecture (routing, error handling, persistence, the API
client, testing).

The brand mark, colour rationale, and palette validation methodology live in
`docs/brand.md`; this document assumes that context and does not repeat it
except where a token's value is needed to explain a component rule.

## Design system

### Tokens — `frontend/src/index.css`

Tailwind v4's CSS-based config, not a `tailwind.config.ts` — there is no JS
config file in `frontend/`; every token is a CSS custom property under
`:root` / `.dark`, registered into Tailwind's utility namespace via
`@theme inline`. Three points worth stating precisely, beyond what the HTML
tab's swatches show:

- **Three colour families that never lend a hue to each other.** `--primary`
  (`#0E6E52` light / `#1FA876` dark, "prosperity emerald" — teal-leaning by
  deliberate choice) is the sole CTA/link/active-nav colour and is never
  reused for status. `--success` / `--warning` / `--destructive` are a
  separate, yellow-leaning-green/amber/red status vocabulary reserved for
  reconciliation semantics (Clean Close, Discrepancy Catcher, negative ROI).
  `--chart-1..4` are a third, purely categorical vocabulary (which platform,
  which campaign) that never carries state. The rule is stated directly in
  the CSS comments: "a semantic colour must never double as the brand
  accent, and neither may be reused as a chart series colour."
- **The `-text` variants were re-stepped after a real contrast failure, not
  eyeballed from the Tailwind ramp.** `index.css`'s own comments record the
  measurement: `#15803d` on the success tint measured 4.49:1 (a real axe
  failure against the 4.5:1 floor) and `#b45309` on the warning tint sat
  exactly on 4.50:1 with no margin. Both were stepped one shade darker
  (`--success-text: #166534`, `--warning-text: #92400e`), landing at 6.40:1
  and 6.36:1. `--muted-foreground` moved the same way, `oklch(0.556)` →
  `oklch(0.535)`, because the old value failed against the brand-tinted
  panel header at the 12–14px sizes it's actually used at. The base fill
  colours (used for solid dots, icons, chart bars) were deliberately left
  untouched, so badges and charts keep the exact hues the palette validation
  in `docs/brand.md` was run against — only the *on-tint text* colour moved.
- **`--text-micro` (0.6875rem) exists because the alternative was a raw
  `text-[11px]` repeated by hand.** The comment in `index.css` states the
  actual count: this literal was showing up in 29 places across 10
  components before being named as a token — the largest single finding of
  a hardcode lint pass. Every micro-overline (`PageHeader`'s eyebrow,
  `Chip`'s label, `Stat`'s label) uses this token now instead of a repeated
  arbitrary value.

Layout tokens registered the same way: `--content-max: 1200px` and
`--prose-max: 68ch`, both exposed through Tailwind v4's `--container-*`
namespace as `max-w-content` / `max-w-prose-measure`. `--content-max`
replaced an ad-hoc `max-w-3xl` (768px) that had been copy-pasted into five
page components — at a 1512px viewport that left roughly 55% of the content
area empty, which `page.tsx`'s own doc comment calls "most of what 'too
little application' meant." `--radius: 0.5rem` drives a four-rung ladder
(`sm` 4px, `md` 6px, `lg` 8px, `xl` 12px) via `calc()`, rather than one
radius applied everywhere. Full spacing/radius table: `docs/architecture.html`
§"Spacing, radius, layout".

Dark mode is not a mechanical negation of light mode: the chart palette
(`--chart-1..4`) is independently re-validated against the dark card surface
in `docs/brand.md`, because the light-mode magenta (`#be1e74`) drops to
2.84:1 against the dark card and would fail outright if simply reused.

### `Panel`, `PageContainer`, `PageHeader`, `Chip` — `frontend/src/components/ui/page.tsx`

Extracted from five page components that had each hand-rolled their own
`mx-auto flex max-w-3xl flex-col gap-4` wrapper and their own
`rounded-lg border border-border bg-card p-4 sm:p-5 shadow-sm` card — the
file's own doc comment names this as the origin. Four primitives, each with
a distinct job:

- **`PageContainer`** — the one content-measure wrapper (`max-w-content`)
  every route composes itself from. Real consumers (verified by import):
  **6** — `ClosePage.tsx`, `HomePage.tsx`, `PlatformsPage.tsx`,
  `PointsPage.tsx`, `PromotionsPage.tsx`, `UploadPage.tsx`. That's every
  routed page except `AskPage` (which asks for `className="max-w-none"` on
  `ChatPanel` instead, since chat is meant to fill the viewport, not sit in
  a measured column).
- **`PageHeader`** — title + optional eyebrow + a `meta` slot typed as
  `ReactNode`, not a description string. The type comment states the
  rationale directly: page context should "arrive as structured metadata,
  not as a paragraph under the heading" — chips and counts, not prose.
  Same **6** consumers as `PageContainer`.
- **`Panel`** — a content surface (`rounded-xl border border-border`) with
  a `tone` prop: `"card"` (default, `bg-card`) or `"muted"` (`bg-muted/40`,
  the recessed step). `tone="muted"` is used specifically so a block that
  must read as "not live" — the points roadmap half of `PointsCard` — is
  carried by the surface itself, not only by its copy. Real consumers:
  **8** — the 6 above, plus `PointsCard.tsx` and
  `Promotions/LogReplacementForm.tsx` (both sub-components, not routed
  pages, which is why they don't also pull `PageContainer`/`PageHeader`).
  `PanelHeader` (the panel-level analogue of `PageHeader`, same file) has
  **2** consumers: `LogReplacementForm.tsx` and `UploadPage.tsx`.
- **`Chip`** — a metadata pill with 5 tones (`neutral`, `brand`, `success`,
  `warning`, `destructive`), each pairing colour with a word (and an icon
  where supplied) so a chip is never read by hue alone. Real consumers
  (verified by import): **9** — `ClosePage.tsx`, `HomePage.tsx`,
  `PlatformsPage.tsx`, `PointsPage.tsx`, `PointsCard.tsx`,
  `PromotionsPage.tsx`, `UploadPage.tsx`, `Points/CompositionBar.tsx`, and
  `Chat/ChatPanel.tsx` — the widest reach of any primitive in `page.tsx`,
  since it's the one piece small enough to drop into a chat bubble as well
  as a page header.

### `Stat`, `StatGroup`, `StatSkeleton` — `frontend/src/components/ui/stat.tsx`

The KPI primitive, and the one component in this codebase whose doc comment
states a hard rule that a code-review pass this session actually caught a
live violation of. Quoting the file directly:

> It renders values, it never produces them. Every `value` handed in is a
> string the Go engine formatted or a `formatUsd` of a decimal the API
> sent; nothing here adds, subtracts, or rounds. […] a presentation
> component that did arithmetic would be a second implementation of the
> reconciliation math living on the client, which is the same defect class
> as an LLM computing a figure.

Structural details that matter beyond the rule itself:

- `value: string | null` — `null` renders as an explicit "Not available"
  pill (dashed border, `CircleSlash` icon), never a `$0`. The type comment
  is direct about why: "A zero and an absence are different facts and this
  product's entire claim rests on not conflating them." This mirrors how
  the chart components render a refused campaign, so an absent figure looks
  identical everywhere it appears.
- `StatGroup` lays cells out with `auto-fit` (not `auto-fill`) so a
  three-stat row stretches to fill the rail instead of clustering left
  against a phantom fourth column, with hairline dividers drawn only
  between cells that actually have a neighbour — a wrapped last row never
  trails a rule into empty space.
- `StatSkeleton` matches the real component's geometry exactly, so a
  loading page doesn't shift layout (CLS) or read as "finished and empty."
- Real consumers of `Stat`/`StatGroup` (verified by import): **4** —
  `ClosePage.tsx`, `HomePage.tsx`, `PointsCard.tsx`, `PromotionsPage.tsx`.
  `StatSkeleton` is pulled into 3 of those (`ClosePage`, `HomePage`,
  `PromotionsPage` — the ones that fetch before rendering).

The file's comment is explicit that the rule is scoped, not absolute: it
governs *reconciliation* figures specifically (sales, margin, commissions,
refunds, week-over-week deltas) — numbers with a server-side source of
truth where a client re-derivation would be a second, possibly-divergent
implementation of the same math. It documents its own one exception by
name, `CostPanel`'s running session-cost total, and points at that file's
`sumCostUsd` for why that specific sum is allowed to stay client-side (see
"Lessons learned" below — this is also where the real violation was found).

### `BadgeDisplay` — `frontend/src/components/Badges/BadgeDisplay.tsx`

Renders fired Reconciliation-category badges (`clean_close`,
`discrepancy_catcher` — Growth/Engagement/Campaign-Creation are named in the
type's own comment as roadmap-only and deliberately not represented here).
One consumer today: `ClosePage.tsx`. Two rendering rules worth naming
because they're enforced by real tests (`BadgeDisplay.test.tsx`), not just
described:

- **Silence is the empty state.** `badges.length === 0` returns `null` —
  no placeholder card, no "no badges yet" copy. The doc comment states the
  reasoning: "this is a financial tool for a time-poor owner, not a game."
- **A badge with `detail` gets a beat of its own** (a single-line banner,
  `px-3 py-2 rounded-md`); one without renders as a compact inline pill.
  Never a modal, never a toast, never animated in — a dedicated test
  (`'never renders loud/animated affordances'`) asserts no `role="alert"`,
  no `role="dialog"`, and no `animate-` class on any rendered child.

**A real, dated example of the design system evolving**: the `count?: number`
field on `ReconciliationBadge`. It was added specifically so a period view
(e.g. a 14-day range) can collapse many identical per-day badges into one
pill labeled `Clean Close ×12` instead of stacking a dozen unlabeled pills —
the commit `999db00` ("Collapse period-view Clean Close badges into one
count pill") added both the field and the rendering branch
(`isCount = Boolean(badge.count && badge.count > 1)`), with `date` ignored
for display once `count` is set (callers still pass a real date for the
React key). `BadgeDisplay.test.tsx` has explicit cases for `count: 12`
(renders one pill, the individual date must not leak into the label) and
`count: 1` (renders exactly like an ordinary single-day badge) — a small,
concrete illustration of a shared primitive's contract being extended
without breaking its existing single-day callers.

### `Button` — `frontend/src/components/ui/button.tsx`

A `cva` recipe from the shadcn scaffold, left largely as generated: 6
variants (`default`, `destructive`, `outline`, `secondary`, `ghost`, `link`)
× 8 sizes (`default`, `xs`, `sm`, `lg`, `icon`, `icon-xs`, `icon-sm`,
`icon-lg`), confirmed by reading the `cva` config directly. Focus is a 3px
`ring-ring/50` plus a border shift, never suppressed. Real consumers
(verified by import): **4** — `ChatPanel.tsx`, `ClosePage.tsx`,
`LogReplacementForm.tsx`, `UploadPage.tsx`. Visual specimen for every
variant/size pair: `docs/architecture.html` §"Buttons".

### The rest of `components/ui/` — thin shadcn wrappers, mostly single-consumer

A systematic pass over `frontend/src/components/ui/` turns up four more
files, none padded into this doc as more "shared" than they actually are:

| File | Real consumers |
|---|---|
| `input.tsx` | `ClosePage.tsx`, `LogReplacementForm.tsx` (2) |
| `textarea.tsx` | `ChatPanel.tsx` (1 — the composer's `field-sizing: content` textarea) |
| `avatar.tsx` | `ChatPanel.tsx` (1 — the assistant/user message avatars) |
| `scroll-area.tsx` | `ChatPanel.tsx` (1 — the Radix `ScrollArea` wrapping the message list; see "The floating composer" below for a real bug this component was at the center of) |

These are close to unmodified shadcn/Radix primitives rather than this
app's own design decisions, which is why they get a table instead of a
subsection each — the load-bearing, in-house primitives are `page.tsx`,
`stat.tsx`, and `BadgeDisplay.tsx` above.

## Frontend architecture

### Routing and error boundaries — `frontend/src/router.tsx`, `frontend/src/components/ErrorBoundary.tsx`

The route table (`export const routes: RouteObject[]`) is exported
separately from the `createBrowserRouter` instance it feeds, specifically so
tests build a `createMemoryRouter` from the same route objects rather than
depending on real browser history — `router.test.tsx` does exactly this.

Every routed page is wrapped in its **own** `ErrorBoundary` via a small
`withBoundary(name, element)` helper, rather than one boundary around the
whole tree. The comment in `router.tsx` states why directly: a crash in one
route "should not force a reload of the sidebar and every other working
page," and naming the specific broken page in the crash report is more
useful than "somewhere in the app." `/ask` is wired by hand instead of
through `withBoundary`, because `withBoundary` has no way to pass
`onReset` — and `/ask` is the one route whose crash was actually caused by
poisoned persisted state (see chatStorage below), so its `Reset` action must
also clear `localStorage`, not just component state:

```tsx
element: (
  <ErrorBoundary component="Ask" onReset={clearThreadStorage}>
    <AskPage />
  </ErrorBoundary>
),
```

**Root-level coverage, added after a real gap was found**: the shell itself
(`AppShell` — sidebar, mobile nav, the pinned `CostPanel`) sits *outside*
every per-page boundary, so a crash there used to take down the entire app
with React's default blank screen and no crash report at all. The root
route now wraps `<AppShell />` in its own `ErrorBoundary` **and** supplies
`errorElement={<RouteErrorBoundary component="App shell" />}` — two
different mechanisms for two different failure classes. A class
`ErrorBoundary`'s `componentDidCatch` only sees an error thrown while
rendering a subtree it wraps; a failed *loader* or a malformed route object
never reaches that subtree's render at all, so only React Router's own
`errorElement` mechanism sees it. `RouteErrorBoundary` (a function component
using `useRouteError`, since a router-level failure has no children left to
mount a class boundary around) renders the identical `ErrorFallback` UI, so
a crash reads the same to the owner regardless of which of the two actually
caught it. `router.test.tsx` proves the shell-level boundary specifically by
mocking `Sidebar` to throw and asserting the app doesn't blank.

**The "retro feed"**: `componentDidCatch` (and `RouteErrorBoundary`'s effect)
fire a best-effort `postJson('/api/client-errors', ...)` — fire-and-forget,
its own failure swallowed, since "a failed report must never throw again
inside code that already exists to handle failure." The file's own comment
names the incident this was built from: a stale, pre-schema-change chat
message in `localStorage` broke rendering once, and there was no record of
it having happened at all — only a report hours later. `ErrorBoundary.test.tsx`
asserts the POST fires with `expect(fetch).toHaveBeenCalledWith('/api/client-errors', ...)`.

`onReset` exists because `reset()` re-rendering identical children is not
"starting fresh" when the crash was caused by bad persisted data — without
it, clicking Reset re-reads the same poisoned key and crashes again
immediately, contradicting the fallback UI's own copy ("resetting this
section starts it fresh"). `ErrorBoundary.test.tsx` has a matched pair of
tests proving both directions: `onReset` supplied recovers; `onReset` absent
reproduces the exact same crash on Reset.

### Chat persistence — `frontend/src/lib/chatStorage.ts`

`localStorage`, deliberately, not Postgres — the file's own comment explains
why `question_interaction` (the backend's per-model-call audit log) is the
wrong place to hang a resumable conversation on: it has no
session/ownership concept, and inventing one is out of scope for a
single-tenant prototype. Every read/write is wrapped in try/catch: Safari
private mode throws on `setItem`, storage can be full, and a hand-edited key
is always possible — none of that is worth failing a chat over, so a
storage failure degrades to "no history," never a broken page.

**Versioning is per-key, not global, and the reason is a real, disclosed
bug.** `THREADS_KEY` is `mbs.chat.threads.v${THREADS_VERSION}` with
`THREADS_VERSION = 2`; `PROMPTS_KEY` is versioned independently at `1`. The
file's comment states the incident plainly: `ChatMessage`'s shape changed
more than once (`ErrorChatMessage` added, `AnswerCacheInfo` added) without a
version bump at the time it happened, so a browser that stayed open across
those changes could hold an old-shape message that the current renderer
assumed had fields it didn't — "plausibly why a message stopped appearing
in a long-running session," per commit `5c46745`'s message. Saved prompts
are plain strings that have never changed shape, so bumping their version in
lockstep would invalidate data that was never actually at risk.

Two independent layers guard against this now, not just the version bump:
`loadThreadStore()` drops any thread missing a well-formed shell (`id`,
`messages` array), and — a second, narrower check —
`isWellFormedMessage()` validates **each message inside a well-formed
thread** against its `kind`-specific required fields. The comment is
explicit that this second layer is "the general-purpose defense for the
next time a shape changes and a version bump is missed — belt and
suspenders, not a substitute for versioning."

### The shared API client — `frontend/src/lib/api.ts`

Three helpers — `getJson`, `postJson`, `postMultipart` — and one rule the
module's own comment states in its first line: "One place that knows where
the Go backend lives, and one way to call it." `API_BASE` reads
`VITE_API_BASE_URL` with a `localhost:8080` fallback matching
`backend/cmd/server -serve :8080`. Every helper turns a non-2xx response
into a thrown error carrying the server's real message — `postJson` and
`postMultipart` additionally parse a `{error, detail}` body into a typed
`ApiError` with a `.code` field, so a caller can branch on a specific
refusal code (e.g. spec 002's `replaces_not_flagged_negative`) without
parsing prose. The rule stated directly in `getJson`'s comment: a page that
can't reach the reconciliation engine "has to say so, not render an empty
state that looks like 'you have no data'" — an empty-but-reachable state and
an unreachable-backend state must never look the same.

**Why every page should go through this rather than a local `fetch`, with a
real counter-example from this exact codebase**: `AskPage.tsx` briefly did
exactly that. When it was first wired to the live `/api/ask` endpoint
(commit `d310eb4`), it declared its own `const API_BASE =
'http://localhost:8080'` and called `fetch` directly — bypassing
`VITE_API_BASE_URL` entirely, so a build pointed at any non-localhost
backend would have silently kept hitting `localhost:8080` from this one
page while every other page correctly followed the environment variable.
Fixed later (commit `4e4fedc`) by switching to `postJson('/api/ask', …)`
and deleting the local constant. The comment now in `AskPage.tsx` states the
fix as the rule: "The request goes through `postJson` from `lib/api` — the
same helper every other page uses — rather than a page-local `fetch`, so
this page honors `VITE_API_BASE_URL` like the rest of the app instead of
always hitting localhost."

### `ChatPanel` — `frontend/src/components/Chat/ChatPanel.tsx`

At 1,286 lines this is the largest component in the app; the architecture
worth documenting is the message-type union and two layout patterns fixed
this session by measurement rather than guesswork.

**The message union.** `ChatMessage = UserChatMessage | AssistantChatMessage`,
where `AssistantChatMessage` is a closed union of four `kind`s:

- `AnswerChatMessage` — carries `provenance: SourceRowRef[]` and an optional
  `visualization` (the chart/table/grid form the *backend* chose from which
  MCP tool ran — never a second model call, never a client-side decision).
  Also carries an optional `followUps?: string[]` — 0-3 next-question
  strings the backend generates deterministically in Go
  (`httpapi.deriveFollowUpSuggestions`, keyed on the real tool invocation
  that grounded this answer) and `AnswerBubble` renders as `SuggestionChips`
  after the provenance/cache line, wired to the same `submitQuestion` every
  other suggestion source already uses. Shipped to close a real gap: every
  successful answer used to end in a blank composer, since `SuggestionChips`
  previously rendered only in the empty state and inside a refusal.
- `ClarificationChatMessage` — carries `options?: string[]` for one-tap
  quick replies.
- `RefusalChatMessage` — carries `missing: string[]`.
- `ErrorChatMessage` — deliberately **not** folded into `RefusalChatMessage`
  even though both render similarly. The type's own comment draws the
  distinction precisely: a refusal is "a product decision the system stands
  behind," while an error is "a defect ('I never got an answer at all')."
  Collapsing the two would let a genuine outage read as principled caution
  — exactly the kind of flattering misreport this product's honesty
  discipline is built to rule out.

All four carry an optional `cache?: AnswerCacheInfo`, because a cached
refusal is exactly as much a cache hit as a cached answer.

**The floating composer's real height, not a guessed one.** The message
list sits under an absolutely-positioned composer overlay and needs bottom
padding wide enough to clear it. The old value was a static `pb-28` (112px)
— fine for a single-line draft with the suggestions panel closed, wrong the
moment the textarea's `field-sizing: content` grew across a Shift+Enter
multi-line question, or the suggestions panel opened above the input row.
Either state taller than 112px silently hid the bottom of the newest
message (fixed in commit `768492f`). The fix replaces the guess with a
measurement:

```tsx
const observer = new ResizeObserver((entries) => {
  const height = entries[0]?.borderBoxSize?.[0]?.blockSize ?? composer.offsetHeight
  setComposerHeight(Math.ceil(height) + 24)
})
observer.observe(composer)
```

`composerHeight` (initialized to 112 only so there's no visible jump before
the observer's first callback) then drives `style={{ paddingBottom:
composerHeight }}` on the message list directly. The general pattern here —
measure the real rendered node instead of estimating its size — is the same
discipline the codebase already applies to data (never trust a claimed
result without checking it independently); this is that same rule applied
to layout.

**Auto-scroll pin, and the real scroll bug that motivated rewriting it.**
Before this pass, `<ScrollArea className="flex-1">` had no `min-h-0`. A flex
column item's automatic minimum size is its *content* height, so the Radix
scroll viewport grew to fit the entire message list — measured directly at
761px/761px (`scrollHeight === clientHeight`, nothing to scroll) inside a
574px panel. With nothing to scroll, `scrollIntoView` did exactly what it's
defined to do when the nearest container isn't scrollable: it walked up to
the next real scrollable ancestor and scrolled that instead (measured:
`section.scrollTop = 245`), dragging the panel header off-screen. The fix is
`min-h-0` on the scroll area plus scrolling the viewport element directly:

```tsx
const scrollToBottom = React.useCallback(() => {
  const viewport = viewportRef.current
  if (!viewport) return
  viewport.scrollTop = viewport.scrollHeight
}, [])
```

`scrollIntoView` is not used anywhere in the file any more — the comment
notes it's defined to scroll *every* scrollable ancestor, so even once the
viewport itself works correctly, it could still move the page underneath
the panel. The scroll is snapped instantly (never `behavior: 'smooth'`)
specifically so a `ResizeObserver` re-pin (firing when a chart or a
late-loading font grows the content after the initial paint) can't race a
multi-frame animation and read an intermediate, "not actually at the
bottom" scroll position as un-pinning the view.

Pin state itself follows Nielsen's user-control heuristic: auto-scroll only
applies while the reader is already within `BOTTOM_STICK_THRESHOLD_PX` (48px)
of the bottom; scrolling up hands control back, and a "Jump to latest"
affordance hands it forward again on request. The scroll listener is bound
as a native `addEventListener('scroll', …)` on the viewport element itself,
not passed as an `onScroll` prop to `<ScrollArea>` — that prop spreads onto
Radix's `Root`, and `scroll` events do not bubble, so a handler placed there
never fires at all (verified: the jump-to-latest control stayed permanently
hidden until this was corrected).

### The shell and cross-route state — `frontend/src/components/Shell/AppShell.tsx`

`AppShell` mounts once at the router root and owns two pieces of state that
must survive navigation between routes: the running `interactions:
CostInteraction[]` list (so `CostPanel`, pinned at the shell level, keeps
its total across a visit to `/close` and back) and the usage ping fired
exactly once per shell mount (not per page view — the comment is explicit
that this guards against ordinary in-app navigation inflating a
"distinct days used" badge metric). Routed pages read and write this state
through `useShellOutletContext()`, a thin wrapper over React Router's
`useOutletContext` typed as `ShellOutletContext { interactions,
logInteractions }` — `AskPage` is the only page that currently calls
`logInteractions`, once per `/api/ask` response that actually ran a model.

### Testing conventions

A survey of `BadgeDisplay.test.tsx`, `ClosePage.test.tsx`,
`ErrorBoundary.test.tsx`, and `router.test.tsx` shows one consistent
pattern across the suite: **Vitest + `@testing-library/react`, real
component logic under test, network faked at the lowest possible layer.**

- `fetch` itself is stubbed globally (`vi.stubGlobal('fetch', vi.fn()...)`),
  never `lib/api`'s helpers — so `getJson`/`postJson`'s real error-handling
  and `ApiError`-parsing code actually executes in every test that touches
  the network, rather than being assumed correct. `ClosePage.test.tsx`
  stubs `fetch` to resolve with a **realistic, fixture-shaped**
  `/api/reconciliation` payload (real field names like
  `total_delivery_gross_sales`, `discrepancy_flags`, `source_row_refs`,
  a deliberately-absent day) rather than an invented, simplified shape —
  so a contract drift between the real backend and the test's assumed shape
  is more likely to be caught.
- `router.test.tsx` imports `routes` (not `router`) from `router.tsx` and
  builds its own `createMemoryRouter(routes, …)`, exactly the reason that
  export exists per `router.tsx`'s own comment — no dependency on real
  browser history. It also demonstrates dependency faking at the
  component level: `vi.mock('@/components/Shell/Sidebar', ...)` replaces
  the real sidebar with one that throws, to prove the shell-level
  `ErrorBoundary` actually catches a chrome crash rather than trusting the
  wrapping by inspection.
- `console.error` is spied and silenced (`vi.spyOn(console, 'error')
  .mockImplementation(() => undefined)`) in every test that deliberately
  throws inside a boundary, since React logs the caught error on every
  render pass — expected noise for these tests specifically, not a signal.
- Assertions favor accessibility queries (`getByRole('alert')`,
  `getByLabelText(...)`) over structural ones, and `BadgeDisplay.test.tsx`
  in particular asserts *absences* as real requirements — no `role="alert"`,
  no `role="dialog"`, no `animate-` class anywhere in the rendered tree —
  turning "never do X" doc-comment rules into things a test run actually
  fails on.
- Live-data assumptions are avoided structurally, not just by convention:
  `frontend/src/test/setup.ts` stubs `Element.prototype.scrollTo` (jsdom has
  no layout engine and no real scrolling API, so `ChatPanel`'s own
  scroll-pinning would throw on mount under test) and installs an in-memory
  `Storage` polyfill (the Node test runtime's own experimental
  `localStorage` is inert without a CLI flag and would otherwise make every
  `chatStorage.ts` test see storage as permanently unavailable) — both
  environment gaps are patched in one shared place rather than defended
  against with test-only branches inside product code.

## Lessons learned

Three real defects, found and fixed in this exact codebase during a review
pass, each illustrating a rule the design system already states in writing
elsewhere in this document — not hypothetical anti-patterns, but things that
actually shipped and were then corrected:

1. **Client-side arithmetic on a reconciliation-adjacent figure**
   (`CostPanel.tsx`, fixed in commit `4e4fedc`). `stat.tsx`'s rule says a
   presentation component never computes; `CostPanel`'s running session-cost
   total is the one *documented* exception, because it sums an ephemeral,
   browser-only session state the backend has no matching concept of. But
   the exception's implementation itself had a latent defect: it originally
   summed the raw `estimated_cost_usd` floats directly
   (`sum(interactions.map(i => i.estimated_cost_usd))`), which can
   accumulate IEEE-754 rounding drift across many small values — the exact
   class of bug `0.1 + 0.2 !== 0.3` demonstrates, here on figures as small
   as $0.0001. Fixed by converting each value to an integer number of
   micro-dollars, summing those as integers, and converting back
   (`sumCostUsd`) — the same fixed-point discipline `internal/money` uses in
   cents on the Go side, applied at the smaller unit this domain needs.

2. **A page reaching around the shared API client** (`AskPage.tsx`, hardcode
   introduced in commit `d310eb4`, fixed in `4e4fedc`). `lib/api.ts` exists
   specifically so every page honors `VITE_API_BASE_URL`; `AskPage.tsx`
   briefly declared its own `const API_BASE = 'http://localhost:8080'` and
   called `fetch` directly when it was first wired to the real backend,
   silently reintroducing the exact class of "always hits localhost
   regardless of environment" bug the shared client exists to prevent.
   Fixed by switching to `postJson` from `lib/api`.

3. **Estimated layout instead of measured layout** (`ChatPanel.tsx`, fixed
   in commit `768492f`). A static `pb-28` (112px) reservation for the
   floating composer was correct for exactly one composer state — single
   line, suggestions closed — and silently wrong for every taller one, since
   the textarea's `field-sizing: content` and the suggestions panel both
   change the composer's real height at runtime. Fixed with a
   `ResizeObserver` reading `entries[0].borderBoxSize[0].blockSize` off the
   actual composer node. The same file's earlier scroll-pin fix (missing
   `min-h-0` on the `ScrollArea`, confirmed by directly measuring
   `scrollHeight`/`clientHeight` as 761/761 before the fix and 1610/501
   after) is the same lesson from the opposite direction: a claimed
   "should be enough" or "should already be correct" layout value is worth
   measuring directly before trusting it, the same way this codebase
   already insists on measuring a reconciliation figure before trusting an
   agent's report of it.

All three fixes are visible in the current source at the file paths cited
above, and the versioned-persistence pattern in `chatStorage.ts`
(`THREADS_VERSION`/`PROMPTS_VERSION`, fixed in commit `5c46745`) is a fourth
example of the same underlying discipline — a shape can drift silently
across commits, so the defense (a version bump, or a per-message shape
check) has to be explicit in code, not left to "nothing changed on purpose
so it's probably fine."
