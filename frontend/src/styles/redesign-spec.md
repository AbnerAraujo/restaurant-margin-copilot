# Redesign Spec — Shell, Navigation & Charts

Source of truth for the parallel builder agents restructuring
`MarginCopilotApp.tsx`'s single page into a routed app shell. Read alongside
`design-tokens.md` (still the authority for type/spacing/badge recipes — this
file only adds the shell, home-grid, and chart layer on top of it) and
`docs/brand.md` / `index.css` for color tokens. Do not invent new hex values
anywhere in this doc's implementation — every color below is an existing
token from `index.css`.

This is a **design spec, not implementation** — written so three parallel
builder agents (shell, home, charts) converge on one shape without a
coordination meeting.

---

## 0. What's changing and why

The current app (`MarginCopilotApp.tsx`) is one static page: header, a single
reconciliation card, `ChatPanel`, a fixed cost pill. That reads as a demo
screen, not a product with more than one thing in it. This spec splits it
into:

1. An **app shell** — fixed left sidebar (Toqan-style) + routed content area.
   `CostPanel` stays mounted at the shell level (outside the router outlet)
   so the running-cost pill persists across every route unchanged, exactly
   as it behaves today.
2. A **home route** whose entire content is a grid of navigation tiles built
   on the `BadgeDisplay` pill/banner visual language — capability-as-badge
   doubling as capability-as-nav-link.
3. Two chart-first routes (`/close`, `/promotions`) that replace what would
   otherwise be raw tabular data with a bar chart per the `dataviz` skill,
   with the existing `ProvenanceTag` expand-to-detail pattern kept as the
   underlying-data affordance, not removed.

No new color tokens. No new dependencies are strictly required for the shell
or home grid (plain `<Link>`/`<NavLink>` — see §1 on the one dependency gap).
Charts are specified as hand-rolled inline SVG (see §4) so no charting
library needs to be added for two simple diverging bar charts — this also
keeps every mark spec (rounded data-ends, 2px surface gaps, baseline) exactly
controllable per the `dataviz` skill rather than fighting a library's
defaults.

**Dependency gap to flag to the builder agents:** `react-router-dom` is not
currently in `frontend/package.json`. The IA below is written in routes
(`/`, `/close`, `/ask`, `/promotions`) because that's the correct information
architecture and it's what "a proper shell + routed pages" means — but
whoever builds the shell needs to `npm install react-router-dom` (or the
team's preferred router) as part of that task. This spec does not choose a
router version; it only fixes the route table and what each route renders.

---

## 1. Information architecture — route table

| Route | Renders | Existing components reused |
|---|---|---|
| `/` | Home: capability tile grid (§3). No chat, no chart, no raw data — a launcher only. | `Logo` |
| `/close` | "Today's Close": today's reconciliation summary card (margin, gross sales, `BadgeDisplay`, `ProvenanceTag`) — the exact card `MarginCopilotApp.tsx` renders today — **above** a new 14-day margin bar chart (§4.1) with its own `ProvenanceTag` for the underlying `daily_reconciliation.csv` rows. | `BadgeDisplay`, `ProvenanceTag` (used twice: once for today's card, once for the chart's source rows) |
| `/ask` | "Ask about your margin": `ChatPanel` full-page/full-width instead of capped at `max-w-3xl` inside a stacked page — it's now the whole page's job, not one section among several. | `ChatPanel` (internals untouched) |
| `/promotions` | "Promotion ROI": one bar chart across the 4 campaigns (§4.2), refused/unattributable campaign shown as its own explicit non-bar state (not a zero), each campaign's `ProvenanceTag` for its `source_row_refs`. | `ProvenanceTag` |

`CostPanel` is **not** a route — it renders once at the shell root (sibling
to the router outlet, same `fixed bottom-4 right-4` positioning it already
has) so the running session-cost pill is visible from every route, matching
its current always-visible behavior.

Page `<h1>` per route, using the existing "Page title" type role
(`text-2xl font-semibold tracking-tight text-foreground`):
- `/` → "My Business Steward" (or omit — the tile grid + sidebar already
  orient the owner; a redundant "Home" title adds nothing)
- `/close` → "Today's Close"
- `/ask` → "Ask about your margin"
- `/promotions` → "Promotion ROI"

---

## 2. Sidebar spec (Toqan-inspired)

Fixed width, not resizable (per the task brief — a real resize handle is
explicitly out of scope). Desktop only by default; §2.3 covers small
viewports.

### 2.1 Structure & classes

```html
<!-- Shell root -->
<div class="flex min-h-screen bg-background">

  <!-- Sidebar -->
  <aside
    class="hidden lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-60 lg:shrink-0
           lg:flex-col lg:border-r lg:border-border lg:bg-card/50"
    aria-label="Primary navigation"
  >
    <!-- Logo / workspace header -->
    <div class="flex items-center border-b border-border px-5 py-5">
      <!-- <Logo /> at its default size=36, variant="lockup" -->
    </div>

    <!-- Nav list -->
    <nav class="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4">
      <!-- one NavItem per route, see 2.2 -->
    </nav>

    <!-- Optional footer slot (left empty — no settings/account menu in this
         prototype's scope; do not add placeholder chrome) -->
  </aside>

  <!-- Routed content -->
  <div class="flex min-w-0 flex-1 flex-col">
    <!-- mobile top bar, see 2.3 -->
    <main class="flex-1 px-4 py-6 sm:px-6 lg:px-8">
      <!-- <Outlet /> -->
    </main>
  </div>

  <!-- <CostPanel /> stays fixed, sibling of everything above -->
</div>
```

Width: `w-60` = 240px, exactly the brief's suggested fixed width.
`bg-card/50` (not a flat `bg-card`) so the sidebar reads as a distinct
plane from both the page background and full-opacity cards, without adding
a new token.

### 2.2 Nav item

One `NavLink` per route in the table above (`/`, `/close`, `/ask`,
`/promotions`), in that order — home first, then the three capabilities in
the same left-to-right/top-to-bottom order as the home grid (§3) so the
sidebar and the home tiles teach the same mental map.

```html
<!-- inactive -->
<a class="flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium
          text-muted-foreground transition-colors
          hover:bg-accent hover:text-foreground
          focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50">
  <IconComponent class="size-4 shrink-0" aria-hidden="true" />
  <span>Label</span>
</a>

<!-- active (NavLink's isActive / react-router's [aria-current="page"]) -->
<a aria-current="page"
   class="flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium
          bg-primary/10 text-primary
          focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50">
  <IconComponent class="size-4 shrink-0" aria-hidden="true" />
  <span>Label</span>
</a>
```

Active state uses `bg-primary/10` / `text-primary` — the brand accent, per
the task brief ("active-state styling using `--primary`") — **never**
`--success`/`--warning`/`--destructive` for the active nav indicator; those
are reserved for reconciliation state and must not be read as "this page has
a status."

Icon + label per route (all from `lucide-react`, already installed and
verified present in this package version):

| Route | Icon | Label |
|---|---|---|
| `/` | `LayoutGrid` | Home |
| `/close` | `CalendarCheck` | Today's Close |
| `/ask` | `MessagesSquare` | Ask |
| `/promotions` | `Megaphone` | Promotions |

### 2.3 Small viewports

No new dependency (no Sheet/Drawer primitive exists in `components/ui` yet,
and adding one is out of scope for a design-only pass). Below `lg`, replace
the sidebar with a horizontal top bar carrying the same four nav items as
icon-only pills, so the IA is still fully reachable without a hidden drawer:

```html
<div class="flex items-center gap-1 overflow-x-auto border-b border-border
            bg-card/50 px-3 py-2 lg:hidden">
  <!-- <Logo variant="icon" size={28} /> -->
  <!-- then one link per nav item: -->
  <a class="flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1.5
            text-xs font-medium text-muted-foreground
            [&[aria-current=page]]:bg-primary/10 [&[aria-current=page]]:text-primary">
    <IconComponent class="size-3.5" aria-hidden="true" />
    Label
  </a>
</div>
```

---

## 3. Home page — capability tile grid

The entire home route's content. No chat box, no chart, no card of numbers —
a launcher, per the brief ("gamification doubling as navigation, not just
achievement decoration").

### 3.1 Grid

```html
<div class="mx-auto grid max-w-3xl grid-cols-1 gap-4 sm:grid-cols-2">
  <!-- one CapabilityTile per route below, /  excluded -->
</div>
```

`max-w-3xl` matches the design-tokens.md content-column convention (kept
consistent with `/close` and `/ask`, not widened into a BI-dashboard grid).

### 3.2 Tile anatomy & classes

Built on the same DNA as `BadgeDisplay`'s banner variant (rounded surface,
tinted icon chip, `text-xs` uppercase eyebrow available if needed) but sized
as a real touch target, wrapped in a `<Link>`:

```html
<a href="/close"
   class="group flex flex-col gap-3 rounded-lg border border-border bg-card
          p-5 text-left shadow-sm transition-all
          hover:-translate-y-0.5 hover:border-primary/30 hover:shadow-md
          focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50">
  <div class="flex items-center justify-between">
    <span class="flex size-10 items-center justify-center rounded-full
                 bg-primary/10 text-primary">
      <IconComponent class="size-5" aria-hidden="true" />
    </span>
    <ArrowRight
      class="size-4 text-muted-foreground transition-transform
             group-hover:translate-x-0.5 group-hover:text-primary"
      aria-hidden="true"
    />
  </div>
  <div>
    <h2 class="text-base font-semibold tracking-tight text-foreground">
      Today's Close
    </h2>
    <p class="mt-1 text-sm leading-relaxed text-muted-foreground">
      Today's margin, reconciliation badges, and the provenance behind
      the number.
    </p>
  </div>
</a>
```

**Tile tone is always the brand tint (`bg-primary/10` / `text-primary` on the
icon chip), never `--success`/`--warning`/`--destructive`.** These tiles are
navigation, not a fired badge — coloring the "Promotions" tile amber
(`--warning`) or the icon destructive-red would visually claim "something's
wrong with promotions" before the owner has even opened it, which is exactly
the false-signal `brand.md` and `design-tokens.md` §1.1/§1.2 both warn
against (semantic color must never leak onto non-semantic chrome, and vice
versa). Reserve `--success`/`--warning`/`--destructive` for what actually
appears *inside* `/close` and `/promotions` — the reconciliation badges and
the chart bars — never for the tile shell that merely links there.

Optional, once real data exists: a small quiet-pill stat inside the tile
(e.g. "1 flag this week") using the exact `BadgeDisplay` pill recipe
(`inline-flex items-center gap-1 rounded-full border border-warning/25
bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning-text`) placed
under the description. This is the one place a status color legitimately
belongs on a tile — it's reporting a real fired state, not decorating the
nav link itself. Omit it entirely when there's nothing to report (same
"silence over an empty placeholder" rule `BadgeDisplay` already follows) —
do not fabricate a placeholder stat.

### 3.3 Tile content (3 tiles — `/` itself is not a tile)

| Tile | Icon | Title | Description |
|---|---|---|---|
| `/close` | `CalendarCheck` | Today's Close | Today's margin, reconciliation badges, and the provenance behind the number. |
| `/ask` | `MessagesSquare` | Ask about your margin | Natural-language questions about your numbers — a grounded answer, or an honest refusal. |
| `/promotions` | `Megaphone` | Promotion ROI | Which campaigns paid for themselves, which didn't, and which we won't guess at. |

---

## 4. Charts

Per the `dataviz` skill procedure: form first, then color, then validate,
then marks. Both datasets here are the same job — **magnitude with a
polarity question already built in** (margin/ROI can be negative) — so both
are **diverging bar charts with a zero baseline**, never a line chart (a line
implies a continuous trend interpolation between discrete daily/campaign
totals that isn't there) and never a value colored by magnitude on a
sequential ramp (the story is "which side of zero," not "how far along one
ramp").

No charting library is added for these — see §0. Each is a hand-rolled
inline `<svg>` so the mark spec below (rounded data-ends, baseline, 2px
gaps) is exactly controllable.

### 4.1 `/close` — 14-day margin chart

**Data** (`daily_reconciliation.csv`, Aug 1–14 2026 — exact fixture values,
do not substitute different numbers):

| Date | Margin (USD) |
|---|---|
| 08-01 | 43.26 |
| 08-02 | −227.09 |
| 08-03 | −120.26 |
| 08-04 | 34.27 |
| 08-05 | 182.91 |
| 08-06 | −183.90 |
| 08-07 | 375.82 |
| 08-08 | **null — missing delivery source, flagged** |
| 08-09 | −70.58 |
| 08-10 | 328.82 |
| 08-11 | 25.77 |
| 08-12 | −214.55 |
| 08-13 | 184.94 |
| 08-14 | −29.86 |

**Form:** diverging bar, zero baseline, one bar per day, 14 categories —
comfortably fits one row at `max-w-3xl` without horizontal scroll (~50px per
category slot).

**Color (see §5 for the validator's real output — read that before treating
this as final):**
- Positive bar → `fill="var(--success)"` (bar) — no new hex, reuses the
  token `index.css` already defines.
- Negative bar → `fill="var(--destructive)"`.
- Aug 8 (missing) → **no bar drawn at zero** (drawing a zero-height/zero-value
  bar would fabricate a reading that isn't there — the constitution's
  "refuse rather than estimate" rule applies to the chart exactly as much as
  to a chat answer). Instead render a **fixed-height placeholder capsule
  centered on the baseline**: `28px` tall, `fill="var(--muted)"` at low
  opacity, `45°` hatch texture in `var(--muted-foreground)` (the skill's
  "Lines" texture — see `marks-and-anatomy.md` §Texture; legitimate default
  use here because this is *encoding a missing-data state*, not standing in
  for a color under a CVD/print/forced-colors toggle, so it does not need to
  be gated behind an accessibility setting the way the CVD-mitigation
  texture in §5 does), plus a small flag glyph (`lucide-react`'s
  `TriangleAlert`, `size-3`, `text-muted-foreground`) and a direct label
  "No data" in `text-xs text-muted-foreground` beneath the axis tick for
  that day.

**Baseline:** a solid `1px` hairline at `y=0` in `var(--border)`, spanning
the full chart width, drawn *under* the bars — this is what makes
above/below legible independent of color at all (see §5's finding on why
this matters here specifically).

**Bar geometry:** ≤24px thick per the mark spec, `rx=4` rounded only at the
data-end (top corners for a positive bar, bottom corners for a negative
bar), square at the baseline edge, `2px` surface-color gap between adjacent
bars (not a border).

**Labels:**
- Y-axis ticks at clean round numbers: `−200`, `0`, `200`, `400`
  (`text-xs text-muted-foreground`, `tabular-nums`).
- X-axis: day-of-month only (`1`–`14`), with "Aug" as a single axis label,
  not repeated per tick.
- Direct value labels **only on the two extremes** (Aug 7, +375.82; Aug 2,
  −227.09) at the bar tip, explicit sign included (`+$375.82` /
  `−$227.09`), in `text-success-text` / `text-destructive-text` respectively
  — per the mark spec's "label the endpoint/extreme, never every point."
  Every other bar's exact value is available on hover (tooltip: date, signed
  dollar amount) and in the table view behind the `ProvenanceTag` below the
  chart, not omitted — just not printed on every bar.
- Legend (mandatory at 2+ series, per `marks-and-anatomy.md`): two entries,
  swatch + text label, not color chips alone — **"Profit day"**
  (`bg-success` swatch) and **"Loss day"** (`bg-destructive` swatch), plus a
  third **"No data"** entry with the hatch swatch. This is the icon+label
  pairing the status-color rule requires, and it's also the mandatory
  secondary encoding the CVD finding in §5 obligates.

**Provenance:** a `ProvenanceTag` below the chart with one `SourceRowRef`
covering `daily_reconciliation.csv`, rows spanning the 14-day period
(`period_start: '2026-08-01'`, `period_end: '2026-08-14'`) — clicking it
opens the same expand-to-detail panel already built, unchanged. This is how
"the underlying data stays available... via the existing ProvenanceTag
pattern" is satisfied without a raw table on the page.

### 4.2 `/promotions` — promotion ROI chart

**Data** (exact fixture values):

| Campaign | Spend | Incremental revenue | Net | Verdict |
|---|---|---|---|---|
| IFOOD-CAMP-BOOST01 | 180.00 | 214.00 | **+34.00** | positive |
| JET-CAMP-LUNCHFIX | 220.00 | 55.00 | **−165.00** | negative — flagged |
| IFOOD-CAMP-WEEKEND | 95.00 | unattributable | **null** | refuse (FR-013) |
| JET-CAMP-NEWMENU | 60.00 | 79.50 | **+19.50** | positive |

**Form:** diverging bar, zero baseline, one bar per campaign — 4 categories,
same reasoning as §4.1. `net` (not `spend` or `revenue` alone) is the
plotted value — it's the number the polarity question is actually about.

**Color:** same two tokens as §4.1 — `var(--success)` for a positive `net`,
`var(--destructive)` for a negative `net`. **IFOOD-CAMP-WEEKEND gets no bar
at all**, for the same reason Aug 8 gets none: `roi`/`net` is `NULL` per
FR-013, and a bar of any height (including zero) would assert a value that
was explicitly refused. Render it as its own explicit state, matching the
visual language `ChatPanel`'s `RefusalBubble` already uses for a chat
refusal (destructive-toned icon + label, not the neutral gray "missing data"
hatch from §4.1 — this is a different situation: not an absent input, but an
active policy refusal to estimate, which is the single most
product-defining "hard limit" behavior in `CLAUDE.md`, and deserves to look
like the thing it is):

```html
<div class="flex flex-col items-center gap-1" style="width: <bar-slot-width>">
  <div class="flex h-7 w-16 items-center justify-center rounded-md
              border border-dashed border-destructive/40 bg-destructive/5">
    <ShieldAlert class="size-3.5 text-destructive-text" aria-hidden="true" />
  </div>
  <span class="text-xs font-medium text-destructive-text">Unattributable</span>
</div>
```

positioned centered on the zero baseline, same slot width as a real bar
category, so the x-axis stays evenly spaced.

**Bar geometry & baseline:** identical spec to §4.1 (≤24px thick, 4px rounded
data-end, square at baseline, solid `1px` baseline hairline).

**Labels:** at only 4 categories, the series-count ladder in
`choosing-a-form.md` makes direct labels **mandatory on every bar** (not
just the extremes as in §4.1's 14-bar case) — label each bar at its tip with
the signed net dollar amount (`+$34.00`, `−$165.00`, `+$19.50`), in
`text-success-text` / `text-destructive-text`. The refused campaign's label
is the "Unattributable" text above, not a dollar figure.

**Legend:** three entries — **"Positive ROI"** (success swatch),
**"Negative ROI"** (destructive swatch), **"Unattributable — refused"**
(dashed destructive-outline swatch + `ShieldAlert`).

**Provenance:** one `ProvenanceTag` per campaign (using each
`PromotionRoiRecord.source_row_refs`), placed under that campaign's x-axis
label or surfaced in the hover tooltip — builder's call on exact placement,
but every campaign, including the refused one, must carry its citation per
the cross-cutting MCP contract rule ("every tool response that includes a
number includes `source_row_refs`") — the refused campaign's citation points
at the ad-spend export that *exists* (proving the refusal is because
attribution is missing, not because the campaign itself is unsourced).

---

## 5. Palette validation — real output, not eyeballed

Resolved the CSS custom properties to hex before validating (OKLCH → sRGB;
`--success`/`--destructive` are the only two colors either chart uses):

| Token | Light hex | Dark hex |
|---|---|---|
| `--success` | `#16a34a` | `#22c55e` |
| `--destructive` | `#e7000b` | `#ff6467` |

Chart surface resolved for the validator: light `--card` = `#ffffff`, dark
`--card` = `#171717` (both charts sit inside a `bg-card` panel, matching the
existing `/close` summary card's surface).

```
$ node scripts/validate_palette.js "#16a34a,#e7000b" --mode light --surface "#ffffff"

Palette (light, surface #ffffff, categorical): 2 slots
  [PASS] Lightness band         all 2 inside L 0.43–0.77
  [PASS] Chroma floor           all 2 >= 0.1
  [FAIL] CVD separation         worst adjacent #e7000b↔#16a34a ΔE 5.4 (deutan) · tritan 36.2
  [PASS] Normal-vision floor    worst adjacent #e7000b↔#16a34a ΔE 35.9 (normal)
  [PASS] Contrast vs surface    all 2 >= 3:1
  → FAILED — fix the marked checks

$ node scripts/validate_palette.js "#22c55e,#ff6467" --mode dark --surface "#171717"

Palette (dark, surface #171717, categorical): 2 slots
  [FAIL] Lightness band         outside band: [["#22c55e",0.723],["#ff6467",0.702]]
  [PASS] Chroma floor           all 2 >= 0.1
  [FAIL] CVD separation         worst adjacent #ff6467↔#22c55e ΔE 0.8 (deutan) · tritan 35.3
  [PASS] Normal-vision floor    worst adjacent #ff6467↔#22c55e ΔE 34.2 (normal)
  [PASS] Contrast vs surface    all 2 >= 3:1
  → FAILED — fix the marked checks
```

**Both runs FAIL.** This needs to be said plainly rather than shipped
quietly: `--success` green and `--destructive` red are, unsurprisingly, the
classic red/green confusion pair. Under simulated deuteranopia they sit
5.4 ΔE apart in light mode (floor is 6.0) and a nearly-indistinguishable
**0.8 ΔE** in dark mode — a deuteranope on the dark theme cannot tell a
profit bar from a loss bar by hue alone. Light mode also fails the
categorical lightness band, expected: these are semantic tokens pre-tuned
for their own contrast job, not stepped for this validator's band.

**This was flagged as a requirement to reuse these exact tokens, not invent
new hex values — that instruction stands, but "reuse the token" cannot mean
"ship it as the only channel."** The skill's own rule is explicit that a
sub-floor CVD result is not rescued by secondary encoding when the ask is to
treat color as the identity channel outright — the actual fix here is that
**color is not the primary encoding in either chart to begin with**. Both
charts are diverging bars around a drawn zero baseline: which side of the
baseline a bar sits on is a *position* fact, not a color fact, and position
degrades not at all under any CVD simulation. Every mitigation specified in
§4.1/§4.2 is mandatory, not decorative, specifically because of this result:

1. **The solid baseline hairline** — above/below is legible with color
   entirely removed.
2. **A legend with text labels**, never bare swatches (`"Profit day"` /
   `"Loss day"` / `"No data"`; `"Positive ROI"` / `"Negative ROI"` /
   `"Unattributable — refused"`).
3. **Direct value labels carrying an explicit `+`/`−` sign** — a colorblind
   reader gets the sign from the character, not the hue, everywhere a value
   is printed.
4. **The refused-campaign state uses shape (dashed outline) + icon + text**,
   never color alone, matching how `ChatPanel`'s existing `RefusalBubble`
   already handles the same semantic state.

If a future pass wants these two specific tokens to also pass the
categorical CVD check outright (e.g. for a chart with *more* series where
position can't carry the whole load), that requires re-stepping one of the
two hexes per the skill's "snap-to-passing" procedure (nudge lightness,
hold hue) — which is explicitly a token change, out of scope for "reuse the
existing tokens," and a decision for whoever owns `index.css`'s palette, not
this spec.

**Ordinal/categorical scope note:** the six-check validator is built for
identity color (which series). What these charts actually need color to do
is closer to the fixed two-state "status" job (`color-formula.md`'s "Status"
row: positive/negative is a fixed, reserved-meaning scale, always paired
with icon + label) — which is exactly why the mandatory-pairing mitigations
above are the correct fix rather than a red flag to pick different colors.
Running the categorical validator against the pair was still the right
check to run first: it's what surfaced that this pairing cannot be
color-only, which is the load-bearing conclusion for §4.

---

## 6. Summary for builder agents

- Add `react-router-dom` (or equivalent) — not currently a dependency.
- Shell: `w-60` fixed sidebar, `Logo` at top, 4 nav items, active state =
  `bg-primary/10 text-primary`, never a semantic-status color. `CostPanel`
  stays mounted outside the router outlet.
- Home (`/`): 3 tiles only (`/close`, `/ask`, `/promotions`), brand-tinted
  (`bg-primary/10` icon chip), never badge-toned.
- `/close`: today's summary card (unchanged) + new 14-day margin bar chart.
- `/ask`: `ChatPanel`, full width, internals untouched.
- `/promotions`: new 4-campaign ROI bar chart, one campaign rendered as an
  explicit refusal state, not a bar.
- Both charts: diverging bars, zero baseline, `--success`/`--destructive`
  fills, missing/refused states never rendered as a zero-value bar.
- Palette validator result is a real **FAIL** on CVD separation for this
  pair in both themes — mandatory mitigation (baseline, legend text, signed
  direct labels, icon+label for the refusal state) is specified in §4 and
  is not optional polish.
