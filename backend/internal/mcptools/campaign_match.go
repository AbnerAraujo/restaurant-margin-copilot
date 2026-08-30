// This file backs the fix for the "campaign name/entity lookup defect"
// recorded in docs/plan.md's mistakes log and docs/product-strategy.md's
// "Real evaluation results" section: get_promotion_roi's campaign_id
// lookup previously required an exact string match against the real
// campaign_id (e.g. "JET-CAMP-LUNCHFIX"), so a real, computable campaign
// referenced by its full human-readable display name ("Banner Ad - Lunch
// Fix Menu (JET-CAMP-LUNCHFIX)") or a shortened form ("LUNCHFIX") produced
// a no_data tool error, which the explain step then narrated as a
// hallucinated refusal ("this campaign isn't in the data") even though it
// is.
//
// matchCampaignID is deliberately a bounded, typed match against the real,
// already-persisted campaign_id set (storage.LoadDistinctCampaignIDs) —
// never open-ended fuzzy computation or a guess at a value that might not
// exist (Constitution Principle III: "Typed Tools Only, No Open
// Computation"). It can only ever return a string that is already a member
// of `known`, or "" ("no confident match — do not guess"). In particular,
// it deliberately refuses to pick a winner when normalization makes more
// than one known id plausible (e.g. a bare "camp", which is a substring of
// every id in this dataset) rather than silently guessing one —
// Principle II's "refuse rather than guess" applies just as much to this
// package's own internal resolution step as it does to the model layer.
package mcptools

import "strings"

// normalizeCampaignRef strips everything but letters and digits and
// upper-cases the rest, so "JET-CAMP-LUNCHFIX", "jet camp lunchfix", and
// the fragment "(JET-CAMP-LUNCHFIX)" embedded in a longer display name all
// normalize identically — matching is then a plain substring check on this
// normalized form, not per-punctuation-variant special-casing.
func normalizeCampaignRef(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// minNormalizedMatchLen is the shortest normalized input this function will
// ever resolve via substring containment. Every known campaign_id's own
// hyphen-delimited suffix in this dataset ("BOOST01", "LUNCHFIX",
// "WEEKEND", "NEWMENU") normalizes to 7-8 characters; a much shorter
// fragment (e.g. "CAMP", common to every id's platform-prefix segment)
// would be a substring of several real ids at once, which is exactly the
// ambiguous case this package refuses to guess through rather than
// arbitrarily picking one.
const minNormalizedMatchLen = 5

// matchCampaignID resolves a free-form campaign reference (an exact id, a
// shortened form, or a full human-readable display name that embeds the id)
// against known — the real, currently-persisted campaign_id set — and
// returns the matched id, or "" if nothing resolves confidently. It never
// returns a value that isn't already in known.
func matchCampaignID(input string, known []string) string {
	normInput := normalizeCampaignRef(input)
	if normInput == "" {
		return ""
	}

	// Exact match first (case/punctuation-insensitive) — the common case,
	// and never ambiguous.
	for _, id := range known {
		if normalizeCampaignRef(id) == normInput {
			return id
		}
	}

	if len(normInput) < minNormalizedMatchLen {
		return ""
	}

	// Substring containment in either direction: the normalized known id is
	// a substring of the normalized input (a display name embedding the
	// real id, e.g. "...JETCAMPLUNCHFIX)"), or the normalized input is a
	// substring of the normalized known id (a shortened form, e.g.
	// "LUNCHFIX" inside "JETCAMPLUNCHFIX"). Collect every id that matches
	// either way; resolve only if exactly one does.
	var matches []string
	for _, id := range known {
		normID := normalizeCampaignRef(id)
		if len(normID) < minNormalizedMatchLen {
			continue
		}
		if strings.Contains(normInput, normID) || strings.Contains(normID, normInput) {
			matches = append(matches, id)
		}
	}

	if len(matches) == 1 {
		return matches[0]
	}
	// Zero matches (genuinely unknown campaign — a real no_data case) or
	// more than one (ambiguous — refuse rather than guess which one).
	return ""
}
