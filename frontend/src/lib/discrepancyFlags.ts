/**
 * Shared, plain-language summarization of a day's `discrepancy_flags` — the
 * one place this exists, after the same raw-dump bug was found and fixed
 * independently in two different pages (`ClosePage.tsx`'s badge detail, then
 * `HomePage.tsx`'s "Recent closes" tooltip) on the same day. Duplicating the
 * fix a second time would only set up a third recurrence the next time a new
 * surface renders `discrepancy_flags` — this module exists so there is
 * exactly one place to get it right.
 *
 * Never each flag's raw `detail` sentence concatenated. Reported live: a
 * POS-heavy sync day can legitimately carry 40+ `cross_source_duplicate_removed`
 * flags alone, and joining every one's technical detail (internal type names,
 * `simulated://...` provenance URIs, row numbers) produces a wall of jargon
 * wherever it's shown — a badge, a tooltip, or any future surface.
 *
 * Grouped by type and counted, ordered by what the OWNER needs to act on — an
 * overlap the system could not resolve outranks an amount difference, which
 * outranks a duplicate that was already caught and handled — then capped to
 * the two most important groups, with the rest folded into a plain "N more
 * things flagged" rather than dropped or spelled out in full.
 */

export interface DiscrepancyFlag {
  type: string
  detail: string
}

export function summarizeFlags(flags: DiscrepancyFlag[]): string {
  const counts = new Map<string, number>()
  for (const flag of flags) {
    counts.set(flag.type, (counts.get(flag.type) ?? 0) + 1)
  }

  const phraseFor = (type: string, count: number): string => {
    switch (type) {
      case 'cross_source_duplicate_unresolved':
        return `${count} possible duplicate${count === 1 ? '' : 's'} left unresolved`
      case 'cross_source_amount_mismatch':
        return `${count} order${count === 1 ? '' : 's'} with a promotion-driven amount difference`
      case 'anomaly_threshold_exceeded':
        return 'an unusual change in revenue'
      case 'missing_delivery_source':
        return 'a delivery source with no data for this day'
      case 'commission_mismatch':
        return `${count} commission rate mismatch${count === 1 ? '' : 'es'}`
      case 'pos_non_completed_row_excluded':
        return `${count} voided sale${count === 1 ? '' : 's'} excluded`
      case 'cross_source_duplicate_removed':
        return `${count} duplicate${count === 1 ? '' : 's'} counted once`
      case 'duplicate_order_removed':
        return `${count} duplicate order${count === 1 ? '' : 's'} removed`
      default:
        // A future flag type this list doesn't know about yet — still
        // counted, never silently dropped from the summary.
        return `${count} other item${count === 1 ? '' : 's'} flagged`
    }
  }

  // Priority order: needs-a-decision, then informational, then
  // already-resolved — matching DedupSummary's own discipline in
  // ConnectedPlatformsTab.tsx that an unresolved case outranks a handled one.
  const priority = [
    'cross_source_duplicate_unresolved',
    'cross_source_amount_mismatch',
    'anomaly_threshold_exceeded',
    'missing_delivery_source',
    'commission_mismatch',
    'pos_non_completed_row_excluded',
    'cross_source_duplicate_removed',
    'duplicate_order_removed',
  ]
  const knownTypes = new Set(priority)
  const orderedTypes = [
    ...priority.filter((type) => counts.has(type)),
    // Any type not in the known list, in first-seen order, so nothing
    // vanishes just because this list hasn't been taught its name.
    ...[...counts.keys()].filter((type) => !knownTypes.has(type)),
  ]
  const phrases = orderedTypes.map((type) => phraseFor(type, counts.get(type)!))

  const MAX_PHRASES = 2
  if (phrases.length <= MAX_PHRASES) {
    return capitalizeFirst(joinWithAnd(phrases))
  }
  const shown = phrases.slice(0, MAX_PHRASES)
  const remaining = phrases.length - MAX_PHRASES
  return capitalizeFirst(
    `${shown.join(', ')}, and ${remaining} more thing${remaining === 1 ? '' : 's'} flagged`,
  )
}

function joinWithAnd(phrases: string[]): string {
  if (phrases.length <= 1) return phrases.join('')
  return `${phrases.slice(0, -1).join(', ')} and ${phrases[phrases.length - 1]}`
}

function capitalizeFirst(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toUpperCase() + s.slice(1)
}
