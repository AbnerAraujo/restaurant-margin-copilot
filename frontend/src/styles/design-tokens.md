# Design Tokens — Restaurant Margin Copilot

Read this before building any component. It is the single source of truth for
color, type, and spacing across the three parallel build streams. The tokens
below are **already implemented** in `frontend/src/index.css` (Tailwind v4,
CSS-first config — there is no `tailwind.config.ts` in this project; v4
generates utility classes directly from the `@theme` block in that file). Use
the Tailwind utility classes named here; do not invent new color values,
one-off hex codes, or ad hoc `text-*`/`p-*` sizes in component code.

Design brief this implements: `docs/product-strategy.md` ("Design &
Experience" thinking, badge system) and `docs/prd.md` §4. Grounding:
time-poor, non-technical owner (glanceable, not a dashboard); the
deterministic/probabilistic split must be visible via provenance on every
number (spec FR-005); badges are quiet acknowledgment, not gamified
(Reconciliation category only: "Clean Close", "Discrepancy Catcher"); the
running-cost panel (FR-009) is a small persistent stat, not a hero metric.

---

## 1. Color

### 1.1 Brand accent (primary)

`#EA1D2C` — an iFood-inspired warm, appetite-associated red. This is a
commonly-cited third-party sourced value, **not** confirmed from an official
iFood brand guideline. It is inspiration for the accent, not a trademark
clone — do not use any iFood logo, wordmark, or copy iFood's exact UI chrome.

| Token | Class | Light | Dark | Use |
|---|---|---|---|---|
| `--primary` | `bg-primary` / `text-primary` | `#EA1D2C` | `#F0384A` | Primary CTA fills, active nav/tab indicator, links, focus accents |
| `--primary-foreground` | `text-primary-foreground` | `#FFFFFF` | `#FFFFFF` | Text/icons on a solid `bg-primary` |
| `--primary-hover` | `hover:bg-primary-hover` | mixed 12% black | mixed 12% white | Hover state on primary buttons/links |
| `--primary-active` | `active:bg-primary-active` | mixed 24% black | mixed 22% white | Pressed state |

Subtle brand tints (e.g. a faint highlight behind the active nav item) use
Tailwind's opacity modifier directly rather than a separate token —
`bg-primary/10`, `border-primary/25`. This adapts correctly to both themes
for free (10% red over a white card vs. a dark card both read as a correct
"subtle tint") without a second hardcoded value to maintain.

**Rule: never use `--primary` to signal a badge or status outcome**, even
though it happens to be red. It means "brand/action," full stop. Status
meaning always comes from the tokens in 1.2.

### 1.2 Status / semantic (badges, flags, reconciliation states)

Kept as **independent tokens from `--primary`** on purpose — badges must
never be brand-colored by coincidence; each of these has its own meaning and
can be re-tuned independently of the brand accent later.

| Category | Base token | Foreground (for solid fill) | Text token (for quiet pill, see 1.3) | Meaning |
|---|---|---|---|---|
| Success | `--success` (`bg-success`) | `--success-foreground` | `--success-text` (`text-success-text`) | "Clean Close" badge, positive/resolved states, positive promo ROI |
| Warning | `--warning` (`bg-warning`) | `--warning-foreground` | `--warning-text` (`text-warning-text`) | "Discrepancy Catcher" badge — the system *caught* a duplicate/refund/anomaly. This is a quiet "it worked" acknowledgment, not a failure — amber, never red |
| Danger | `--destructive` (already in the shadcn scaffold) | `--destructive-foreground` | `--destructive-text` | Negative-ROI promo flags (FR-013), refusal banners, hard errors — genuine bad news only |

Light-mode hex reference: success `#16A34A`/`#15803D`, warning
`#D97706`/`#B45309`. Dark-mode values are pre-tuned in `index.css` for
contrast on dark surfaces — use the token/class, never the hex, so theme
switching stays correct automatically.

### 1.3 The "quiet pill" recipe (badges and status chips)

Per the gamification research in `product-strategy.md`: badges read as quiet
acknowledgment — a small pill, never a loud popup, banner takeover, or
confetti. Every badge and status chip uses this exact recipe, swapping only
the color family:

```html
<span class="inline-flex items-center gap-1 rounded-full border
             border-success/25 bg-success/10 px-2 py-0.5
             text-xs font-medium text-success-text">
  <CheckIcon class="size-3" />
  Clean Close
</span>
```

- Background: `bg-<color>/10` (a faint tint, not a solid fill)
- Border: `border-<color>/25`
- Text: `text-<color>-text` (the darker/legible variant, tuned per-theme for
  contrast on the pale tint — not the base `--success`/`--warning` color)
- Size: always `text-xs font-medium`, `px-2 py-0.5`, `rounded-full`. Never
  larger, never bold, never animated in.
- A badge fires as this inline pill next to the thing it describes (e.g. next
  to the day's date in the reconciliation view), or — if it needs a beat of
  its own — a single-line subtle banner using the same tint/border pattern
  at `px-3 py-2 rounded-md` (not `rounded-full`), with a leading icon. It is
  never a modal, toast-with-animation, or full-width celebratory banner.

Solid fill (`bg-success` + `text-success-foreground`) is reserved for small,
non-text-heavy elements only — a status dot, a small icon chip — never for
badge text itself, which always uses the quiet-pill recipe above.

### 1.4 Neutrals

Unchanged from the existing shadcn scaffold — use these as-is, do not
introduce new grays:

`--background` / `--foreground` (page canvas + body text), `--card` /
`--card-foreground` (surface elevated one step off the page canvas —
reconciliation cards, the cost panel, etc.), `--muted` / `--muted-foreground`
(secondary/de-emphasized text, disabled states), `--border`, `--input`,
`--ring`. All already theme-aware (`.dark` overrides exist).

---

## 2. Provenance citation (FR-005) — not a color token, but a required pattern

Every number shown must carry a visible, clickable/hoverable citation (source
file, rows, period) per spec FR-005 and the Constitution's hard limit. Use:

```html
<button type="button"
        class="inline-flex items-center gap-1 text-xs text-muted-foreground
               underline decoration-dotted underline-offset-2
               hover:text-foreground focus-visible:text-foreground">
  POS export · rows 12–47 · Aug 21
</button>
```

Dotted underline signals "this is a citation, not prose" at a glance;
hover/focus darkens to full `text-foreground` so it reads as interactive
without shouting. Clicking/hovering opens the source detail (popover or
inline expansion — implementation is the builder's call, the citation
affordance itself is not). A refused answer never renders this element
(`refusal_fired = true` implies no provenance, per the data model) — render
nothing here rather than an empty/fake citation.

---

## 3. Type scale — role → Tailwind classes

Default Tailwind `text-base font-normal` everywhere is the thing to avoid.
Every piece of UI text maps to exactly one of these five roles:

| Role | Classes | Example |
|---|---|---|
| Page title | `text-2xl font-semibold tracking-tight text-foreground` | "Today's Close" |
| Section label (eyebrow) | `text-xs font-medium uppercase tracking-wide text-muted-foreground` | "RECONCILIATION" |
| Body | `text-sm text-foreground leading-relaxed` | explanatory sentence, answer narration |
| Secondary/helper text | `text-xs text-muted-foreground` | timestamps, fine print |
| Hero data number | `text-3xl font-semibold tabular-nums tracking-tight text-foreground` | today's margin figure |
| Inline/table data number | `text-sm font-medium tabular-nums text-foreground` | a row in a discrepancy list |
| Small persistent stat (cost panel) | `text-sm font-semibold tabular-nums text-foreground` | running cost-per-interaction total |
| Delta/change indicator | `text-xs font-semibold tabular-nums` + `text-success-text` or `text-destructive-text` | "+12%" / "−3.2pp" |
| Badge/status pill text | `text-xs font-medium` (see 1.3) | "Clean Close" |
| Provenance citation | `text-xs` (see §2) | "POS export · rows 12–47" |

**Every numeric value — hero, inline, delta, or panel — gets `tabular-nums`.**
This is a plain built-in Tailwind utility (no custom token needed); it keeps
digits from jittering horizontally when a number updates or when numbers
stack in a column.

Do not use `text-4xl`/`text-5xl` anywhere — this is a glanceable phone-first
tool for a time-poor owner, not a marketing hero section; `text-3xl` is the
largest thing on screen, reserved for the single most important number in
view.

---

## 4. Spacing conventions

Use Tailwind's default spacing scale (4px base) — no custom spacing tokens.
Conventions:

| Context | Classes |
|---|---|
| Page gutter | `px-4 sm:px-6 lg:px-8` |
| Content column width | `max-w-3xl mx-auto` — a narrow reading column, not a wide dashboard grid. This is a deliberate choice: the persona is one owner glancing at one answer, not a multi-panel BI dashboard |
| Card padding | `p-4 sm:p-5` |
| Gap between major page sections | `space-y-4` or `gap-4` |
| Gap within a tight label+value cluster | `gap-1` to `gap-1.5` |
| Badge/pill internal padding | `px-2 py-0.5` (pill), `px-3 py-2` (single-line banner variant) |
| Border radius, cards/panels | default `rounded-lg` (`--radius`, already themed) |
| Border radius, badges/pills | `rounded-full` |

### Running cost panel (FR-009)

Small, glanceable, always visible — explicitly **not** a hero element.
Convention: a fixed/sticky compact pill in a corner of the viewport (top- or
bottom-right), not a banner and not inline in the main content flow:

```html
<div class="fixed bottom-4 right-4 flex items-center gap-1.5 rounded-full
            border border-border bg-card/95 px-3 py-1.5 shadow-sm
            backdrop-blur-sm">
  <span class="text-xs text-muted-foreground">Session cost</span>
  <span class="text-sm font-semibold tabular-nums text-foreground">$0.014</span>
</div>
```

Small footprint, low visual weight (`shadow-sm`, not a heavy card), always
present, never competing with the day's margin figure for attention.

---

## 5. Quick reference — what's new vs. already-existing in `index.css`

**New tokens added** (see `frontend/src/index.css` `:root`/`.dark`/`@theme`):
`--primary` (value changed to brand red), `--primary-hover`,
`--primary-active`, `--destructive-foreground`, `--destructive-text`,
`--success`, `--success-foreground`, `--success-text`, `--warning`,
`--warning-foreground`, `--warning-text` — each exposed as a Tailwind color
via `@theme inline` (`bg-success`, `text-warning-text`, etc.), themed for
both light and dark.

**Unchanged, already usable as-is:** `--background`, `--foreground`,
`--card`, `--popover`, `--secondary`, `--muted`, `--accent`, `--border`,
`--input`, `--ring`, `--radius` and its `sm`/`md`/`lg`/`xl` variants.
