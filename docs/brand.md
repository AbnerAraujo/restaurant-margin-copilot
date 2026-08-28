# Brand: My Business Steward

## Name

**My Business Steward.** A steward is someone entrusted to manage another's
affairs faithfully in their absence — exactly the product's job while the
owner runs the floor. Considered and rejected: "My Business Cockpit" (fits,
but reads as generic SaaS dashboard), "My Business Uplift" (only speaks to
the revenue-growth half of the OKR, says nothing about the trust/refusal
half that's the harder, more differentiated thing this project builds).

## Mark

Two batwing café/kitchen doors — the real anatomy of a swinging
restaurant-door, not a generic door or a stock chart icon:

- Tall frame posts (with a top lintel), doors hanging as short panels hinged
  mid-frame — real batwing doors don't run floor-to-ceiling.
- Each panel is a hexagon tapering to a point at the center gap, the
  distinguishing silhouette that makes it read as "café door" rather than
  an arbitrary shape.
- Green, not the red explored earlier — the color of prosperity, and the
  same green in the "in the red / in the green" financial idiom this
  product's entire job is to help a restaurant cross.

Rejected directions, with reasons: a keyhole/signet seal (read as generic
fintech, not restaurant-specific), gold-dominant treatments (good in
isolation, but didn't carry the "restaurant" specificity the door does),
red/black matching iFood's literal palette (the app's UI palette, not
necessarily the right brand-mark identity — the two don't have to be the
same color for the same reason a company's logo and its app chrome often
differ), a dollar-sign inset (too literal, redundant once the green already
carries the money association).

## Palette

| Token | Light | Dark | Use |
|---|---|---|---|
| `--primary` (brand) | `#0E6E52` | `#1FA876` | CTAs, links, active nav, the logo's door color |
| `--success` (semantic) | `#16a34a` | `#22c55e` | Badges only — deliberately a different, more yellow-leaning green than the brand primary, so semantic state never gets mistaken for brand identity |
| Mark background | `#100D0C` | (fixed, theme-independent) | The logo's own ink field — the mark keeps its identity regardless of the app's current light/dark theme |
| Frame posts | `#5A6B5E` | (fixed) | Muted, subordinate to the door panels |

`--primary` and `--success` are deliberately teal-leaning vs. yellow-leaning
respectively, so a badge never reads as "the brand color" and the brand
button never reads as "a success state" — per the rule that semantic color
must never double as the one brand accent.

## Where it lives

- `frontend/public/favicon.svg`, `frontend/src/components/Logo/Logo.tsx`
- `frontend/src/index.css` (`--primary` tokens, both themes)
- Full iteration history and rejected concepts: published artifact from the
  build session (may not be permanent — this file is the durable record).
