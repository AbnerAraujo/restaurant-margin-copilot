// Spec 008 FR-001 (chart click-to-ask): pure functions that turn a clicked
// chart data point into the same shape of self-contained, real question
// `deriveFollowUpSuggestions` already produces server-side
// (backend/internal/httpapi/suggestions.go) — grounded in the real date or
// campaign the bar/segment represents, never a placeholder.
//
// `/close` and `/promotions` (where these charts render) are separate
// routes from `/ask` (where the chat lives) — this app has no shared chat
// context spanning routes, so a chart click navigates to `/ask` carrying
// the built question as router state (`AskPageNavigationState`) rather than
// calling into an already-mounted `ChatPanel` directly. See AskPage.tsx,
// which reads this state once per navigation and passes it to
// `ChatPanel`'s `autoSubmitQuestion` prop.

/** Router state `/ask` reads on mount to auto-submit a chart-click question. */
export interface AskPageNavigationState {
  autoSubmitQuestion: string
}

/** The one route every chart click-to-ask action navigates to. */
export const ASK_PAGE_PATH = '/ask'

/** One clicked bar/bucket on {@link MarginTrendChart}. */
export interface MarginTrendDataPointClick {
  /** ISO date of the bucket's first day (equal to rangeEndDate when unbucketed). */
  date: string
  /** ISO date of the bucket's last day. */
  rangeEndDate: string
}

/**
 * Builds a real, self-contained question about one clicked margin bar — a
 * single day ("What happened on 2026-08-07?") or, once a wide period has
 * been bucketed for display, the whole bucketed range ("What happened
 * between 2026-08-07 and 2026-08-13?") — never a vague "tell me more".
 */
export function buildMarginTrendFollowUpQuestion(
  point: MarginTrendDataPointClick,
): string {
  if (point.date === point.rangeEndDate) {
    return `What happened on ${point.date}?`
  }
  return `What happened between ${point.date} and ${point.rangeEndDate}?`
}

/** One clicked bar on {@link PromoRoiChart} — a single campaign. */
export interface PromoRoiDataPointClick {
  campaignId: string
  campaignName: string
}

/**
 * Builds a real, self-contained question about one clicked campaign bar,
 * naming the campaign the way an owner would recognize it (its real name),
 * not its internal id alone.
 */
export function buildPromoRoiFollowUpQuestion(
  point: PromoRoiDataPointClick,
): string {
  return `What happened with the "${point.campaignName}" campaign (${point.campaignId})?`
}
