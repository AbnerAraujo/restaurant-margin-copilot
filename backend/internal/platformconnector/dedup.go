package platformconnector

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// --- Cross-source order deduplication ---------------------------------------
//
// # The problem
//
// A POS that integrates with a delivery aggregator records the
// aggregator's orders as its own tickets, so front-of-house sees one
// screen. The same real-world order therefore appears twice in one sync:
// once in the platform's settlement feed, once in the POS's ticket feed.
// Adding both inflates gross sales, understates the cost ratio, and
// reports a margin percentage that looks better than reality — every day,
// systematically, with nothing anywhere to explain it.
//
// # The problem's mirror image, which is just as expensive
//
// A matcher that is too eager merges two genuinely different orders that
// happen to share a price and a rough time. On a 20-to-30-order evening
// with a $32 mean ticket, that is not an exotic coincidence; it is
// expected. The result is REAL revenue deleted from the day, unrecoverable
// from the reconciliation output, and invisible — the day is simply lower.
//
// Dropping a real order and double-counting a duplicate one are the same
// financial-integrity failure wearing different signs, and this file is
// designed against both. CLAUDE.md's "confidently wrong is worse than a
// refusal" cuts in both directions here.
//
// # The rule, in three sentences (spec 012 SC-003)
//
// A POS ticket is a duplicate of a delivery order only if the POS itself
// said the ticket arrived through a delivery channel. Within that set: if
// the ticket carries the platform's order reference and that reference
// resolves, they are the same order. Otherwise they are the same order
// only if they share a platform, a calendar date and an exact amount in
// cents, their times are within matchWindowMinutes, and no other reading
// of the day's tickets is equally consistent.
//
// # What this file will not do
//
// It will not match on amount and time alone. Without an assertion from
// one of the two systems that a delivery channel was involved, the
// evidence is "these two numbers are similar", and acting on that deletes
// revenue. An untagged dine-in ticket is ineligible at any amount, at any
// time, forever. That is a deliberate false-negative: this connector would
// rather leave a duplicate uncaught in a configuration it cannot reason
// about than remove a real order it cannot get back (spec 012 FR-011).
//
// It will not pick a winner under ambiguity. Where the evidence admits
// more than one reading, nothing merges, every record survives, and the
// day carries a flag naming the candidates. A rule that quietly chose the
// "best" match would be estimating money, which this product does not do
// (Constitution Principle II).
//
// # No model, anywhere
//
// Every decision below is integer-cent equality, case-folded string
// equality, and a minute difference. There is no similarity score, no
// threshold on a distance metric, and no LLM. Any decision this file makes
// can be recomputed by hand from the two records involved, which is what
// "auditable" means in a reconciliation product.

// matchWindowMinutes is how far apart a platform's order-placed time and a
// POS ticket time may be and still describe the same order.
//
// A stated modelling choice, not a derived constant. It covers the
// aggregator's injection into the POS, the moment before someone accepts
// the ticket, and ordinary clock skew between two systems nobody keeps in
// sync. Fifteen minutes is wide enough that a real pair is not missed on a
// slow acceptance, and narrow enough that — combined with exact-cent
// equality, a matching platform, and the channel tag the POS itself
// applied — an accidental UNIQUE match is rare rather than routine.
//
// Widening it does not make the matcher more aggressive on its own; it
// makes ambiguity more likely, and ambiguity resolves to "merge nothing".
// That is the intended failure direction.
const matchWindowMinutes = 15

// DedupKind is what the matcher decided about one POS ticket.
type DedupKind string

const (
	// DedupMatchedByReference: the POS recorded the platform's own order
	// id and it resolved. Identity, not inference — the strongest
	// evidence available, and the only kind that does not require the
	// amounts to agree.
	DedupMatchedByReference DedupKind = "matched_by_reference"

	// DedupMatchedByChannelAmountTime: no reference, but the POS said the
	// ticket came through this platform, the cents match exactly, the
	// times are inside the window, and the pairing is the unique solution
	// from both directions.
	DedupMatchedByChannelAmountTime DedupKind = "matched_by_channel_amount_time"

	// DedupUnresolvedNoCounterpart: the POS says this ticket arrived
	// through a delivery platform, and no order in that platform's feed
	// for the day can be it. Nothing is merged. The day may double-count
	// this ticket, and says so.
	DedupUnresolvedNoCounterpart DedupKind = "unresolved_no_counterpart"

	// DedupUnresolvedAmbiguous: more than one reading of the day is
	// equally consistent. Nothing is merged, every record survives, and
	// the flag names the candidates so a human can settle it.
	DedupUnresolvedAmbiguous DedupKind = "unresolved_ambiguous"

	// DedupAmountMismatch: a pair whose identity was established
	// independently of amount (a resolved reference) reports two
	// different amounts. The merge still happens; the disagreement is
	// reported rather than absorbed, because it is usually a real
	// business fact — a platform-funded promotion the POS never saw, or a
	// POS-side correction.
	DedupAmountMismatch DedupKind = "amount_mismatch"
)

// Merged reports whether this decision removed a POS ticket.
func (k DedupKind) Merged() bool {
	return k == DedupMatchedByReference || k == DedupMatchedByChannelAmountTime
}

// DedupDecision is one fully-explained outcome of the matcher.
//
// It carries both sides' identifiers, amounts and provenance, not just a
// sentence, because "nothing is silently corrected" means an owner has to
// be able to find the ticket that was removed AND the order it was folded
// into. A detail string alone would be a claim; these fields are the
// receipt.
//
// It deliberately does not import internal/reconcile. The mapping from a
// connector decision to a reconciliation discrepancy flag belongs in
// internal/pipeline, which already knows both packages — putting it here
// would make the connector depend on the engine it feeds.
type DedupDecision struct {
	Kind DedupKind `json:"kind"`

	// Date is the calendar day the decision belongs to, which is the day
	// whose reconciliation carries its flag.
	Date time.Time `json:"-"`

	// Platform is the delivery platform the POS ticket claimed.
	Platform Platform `json:"platform"`

	POSOrderID    string              `json:"pos_order_id"`
	POSRef        ingest.SourceRowRef `json:"pos_ref"`
	POSGrossCents int64               `json:"-"`

	// PlatformOrderID and PlatformRef are empty on an unresolved
	// decision — there is no counterpart to name.
	PlatformOrderID       string              `json:"platform_order_id,omitempty"`
	PlatformRef           ingest.SourceRowRef `json:"platform_ref,omitzero"`
	PlatformSubtotalCents int64               `json:"-"`

	// Candidates lists the platform order ids that were equally
	// consistent, on an ambiguous decision. Empty otherwise.
	Candidates []string `json:"candidates,omitempty"`

	// Detail is the owner-facing sentence, already naming both sides and
	// their rows. Built here rather than at the display layer so the API
	// response, the discrepancy flag and any log line all say the same
	// thing.
	Detail string `json:"detail"`
}

// dedupeAcrossSources is the matcher.
//
// It takes every delivery record and every POS ticket from ONE fetch and
// returns the POS tickets that survive, plus a decision for every outcome
// it reached — including the ones where it did nothing.
//
// fetchedPlatforms is the set of delivery platforms this sync actually
// covered. A POS ticket claiming a platform outside that set is left
// alone and NOT flagged: the connector is authoritative only for what it
// fetched, exactly as spec 010 established for date ranges, and flagging
// every iFood-channel ticket during a Just-Eat-only sync would bury the
// real signals under noise the owner cannot act on.
//
// Delivery records are never removed or altered. Only POS tickets are
// dropped, and only ever in favour of a delivery record (spec 012
// FR-013): the delivery side knows the commission, the rate, the payout
// and the refund state, and dropping it instead would zero that order's
// commission and move margin UP — a wrong number in the flattering
// direction, which is the worst shape an error in this product can take.
func dedupeAcrossSources(delivery []ingest.DeliveryRecord, pos []POSOrder, fetchedPlatforms map[Platform]bool) ([]ingest.POSRecord, []DedupDecision) {
	decisions := make([]DedupDecision, 0)

	// claimedBy[i] is the index of the POS ticket that has taken delivery
	// record i, or -1. Only pass 1 writes it before pass 2 reads it, so a
	// resolved reference always outranks an inference (FR-009).
	claimedBy := make([]int, len(delivery))
	for i := range claimedBy {
		claimedBy[i] = -1
	}
	dropped := make([]bool, len(pos))

	// --- Pass 1: the reference is identity ---------------------------------
	for pi, ticket := range pos {
		if !eligible(ticket, fetchedPlatforms) || ticket.PartnerOrderRef == "" {
			continue
		}

		matches := findByReference(delivery, ticket)
		if len(matches) == 0 {
			// The POS named an order that is not in the platform's feed
			// for this day. Do NOT fall through to pass 2: an
			// unresolvable reference is proof the picture is incomplete,
			// and matching by amount on top of a known gap would be
			// guessing twice — it could bind this ticket to a DIFFERENT
			// order that happens to cost the same, deleting that order's
			// revenue to hide this one's.
			decisions = append(decisions, unresolvedDecision(ticket, DedupUnresolvedNoCounterpart, nil,
				fmt.Sprintf("POS ticket %s (row %d of %s, %s) records that it arrived through %s as order %s, but no such order is in %s's feed for %s. Nothing was merged — this day may count that order twice.",
					ticket.Record.OrderID, ticket.Record.Ref.Row, ticket.Record.Ref.File, money.FormatCents(ticket.Record.GrossCents),
					ticket.DeliveryPlatform.DisplayName(), ticket.PartnerOrderRef,
					ticket.DeliveryPlatform.DisplayName(), ticket.Record.OrderDate.Format(dateLayout))))
			continue
		}

		// A reference resolving to more than one delivery record would
		// mean the platform's feed contains two orders with one id, which
		// its own upstream should never produce. Refusing to choose is the
		// only honest response.
		if len(matches) > 1 {
			decisions = append(decisions, unresolvedDecision(ticket, DedupUnresolvedAmbiguous, orderIDs(delivery, matches),
				fmt.Sprintf("POS ticket %s (row %d of %s) references %s order %s, but %d orders in that feed carry that id. Nothing was merged.",
					ticket.Record.OrderID, ticket.Record.Ref.Row, ticket.Record.Ref.File,
					ticket.DeliveryPlatform.DisplayName(), ticket.PartnerOrderRef, len(matches))))
			continue
		}

		di := matches[0]
		// Two POS tickets carrying the same reference both describe that
		// one platform order, so both are duplicates of it and both are
		// removed. Reference equality is identity; a second claim on the
		// same order is not evidence against the first.
		claimedBy[di] = pi
		dropped[pi] = true
		decisions = append(decisions, mergeDecision(ticket, delivery[di], DedupMatchedByReference,
			fmt.Sprintf("POS ticket %s (row %d of %s) carries %s's own order reference %s, so it is the same order as %s (row %d of %s). The POS copy was removed and the platform's record kept, so the commission on it is still charged.",
				ticket.Record.OrderID, ticket.Record.Ref.Row, ticket.Record.Ref.File,
				ticket.DeliveryPlatform.DisplayName(), ticket.PartnerOrderRef,
				delivery[di].OrderID, delivery[di].Ref.Row, delivery[di].Ref.File)))

		if ticket.Record.GrossCents != abs64(delivery[di].SubtotalCents) {
			// Identity was established without reference to the amounts,
			// so a disagreement is information, not a reason to doubt the
			// match. Surface it rather than absorb it (FR-015).
			decisions = append(decisions, DedupDecision{
				Kind:                  DedupAmountMismatch,
				Date:                  ticket.Record.OrderDate,
				Platform:              ticket.DeliveryPlatform,
				POSOrderID:            ticket.Record.OrderID,
				POSRef:                ticket.Record.Ref,
				POSGrossCents:         ticket.Record.GrossCents,
				PlatformOrderID:       delivery[di].OrderID,
				PlatformRef:           delivery[di].Ref,
				PlatformSubtotalCents: delivery[di].SubtotalCents,
				Detail: fmt.Sprintf("POS ticket %s rang %s but %s order %s settled at %s for the same order — a %s difference, usually a platform-funded promotion the POS never saw. The platform's figure is the one counted.",
					ticket.Record.OrderID, money.FormatCents(ticket.Record.GrossCents),
					ticket.DeliveryPlatform.DisplayName(), delivery[di].OrderID, money.FormatCents(abs64(delivery[di].SubtotalCents)),
					money.FormatCents(abs64(ticket.Record.GrossCents-abs64(delivery[di].SubtotalCents)))),
			})
		}
	}

	// --- Pass 2: channel + exact amount + bounded time ---------------------
	//
	// Candidates are computed for EVERY eligible ticket before anything is
	// merged. That is what makes the outcome independent of iteration
	// order: a rule that merged as it walked would give the first ticket
	// its pick and leave the second looking cleanly unmatched, when the
	// truth is that neither could be told apart. See
	// TestDedup_IsIndependentOfInputOrder.
	candidates := make(map[int][]int, len(pos))
	claimants := make(map[int][]int, len(delivery))

	for pi, ticket := range pos {
		if dropped[pi] || !eligible(ticket, fetchedPlatforms) || ticket.PartnerOrderRef != "" {
			continue
		}
		found := findByAmountAndTime(delivery, claimedBy, ticket)
		candidates[pi] = found
		for _, di := range found {
			claimants[di] = append(claimants[di], pi)
		}
	}

	// Deterministic iteration: map order in Go is randomized, so walk the
	// POS slice instead. Decisions therefore come out in ticket order, and
	// two runs of the same fetch produce byte-identical flags (SC-004).
	for pi := range pos {
		found, considered := candidates[pi]
		if !considered {
			continue
		}
		ticket := pos[pi]

		if len(found) == 0 {
			decisions = append(decisions, unresolvedDecision(ticket, DedupUnresolvedNoCounterpart, nil,
				fmt.Sprintf("POS ticket %s (row %d of %s, %s at %s) records that it arrived through %s, but no %s order for %s matches it on amount and time. Nothing was merged — this day may count that order twice.",
					ticket.Record.OrderID, ticket.Record.Ref.Row, ticket.Record.Ref.File,
					money.FormatCents(ticket.Record.GrossCents), ticket.Record.OrderTime,
					ticket.DeliveryPlatform.DisplayName(), ticket.DeliveryPlatform.DisplayName(),
					ticket.Record.OrderDate.Format(dateLayout))))
			continue
		}

		// The confidence bar, stated as code: exactly one candidate from
		// the ticket's side, AND that candidate wanted by nobody else.
		// Either half failing means the day admits more than one reading,
		// and this matcher does not choose between readings.
		if len(found) == 1 && len(claimants[found[0]]) == 1 {
			di := found[0]
			claimedBy[di] = pi
			dropped[pi] = true
			decisions = append(decisions, mergeDecision(ticket, delivery[di], DedupMatchedByChannelAmountTime,
				fmt.Sprintf("POS ticket %s (row %d of %s) records that it arrived through %s and matches %s order %s (row %d of %s) exactly on amount (%s) and within %d minutes on time — the only such pairing available on %s. The POS copy was removed and the platform's record kept.",
					ticket.Record.OrderID, ticket.Record.Ref.Row, ticket.Record.Ref.File,
					ticket.DeliveryPlatform.DisplayName(), ticket.DeliveryPlatform.DisplayName(),
					delivery[di].OrderID, delivery[di].Ref.Row, delivery[di].Ref.File,
					money.FormatCents(ticket.Record.GrossCents), matchWindowMinutes,
					ticket.Record.OrderDate.Format(dateLayout))))
			continue
		}

		ids := orderIDs(delivery, found)
		decisions = append(decisions, unresolvedDecision(ticket, DedupUnresolvedAmbiguous, ids,
			fmt.Sprintf("POS ticket %s (row %d of %s, %s at %s) records that it arrived through %s, but the evidence does not single out which order it is: %s. Nothing was merged, so this day may count that order twice — check these orders against the ticket before trusting the day's gross sales.",
				ticket.Record.OrderID, ticket.Record.Ref.Row, ticket.Record.Ref.File,
				money.FormatCents(ticket.Record.GrossCents), ticket.Record.OrderTime,
				ticket.DeliveryPlatform.DisplayName(), describeAmbiguity(ids, claimants, found))))
	}

	kept := make([]ingest.POSRecord, 0, len(pos))
	for pi, ticket := range pos {
		if dropped[pi] {
			continue
		}
		kept = append(kept, ticket.Record)
	}
	return kept, decisions
}

// eligible is FR-011 in one function: a POS ticket is a matching candidate
// only if the POS ITSELF asserted a delivery origin, and only for a
// platform this sync actually fetched.
func eligible(ticket POSOrder, fetchedPlatforms map[Platform]bool) bool {
	if ticket.DeliveryPlatform == "" {
		return false
	}
	return fetchedPlatforms[ticket.DeliveryPlatform]
}

// findByReference returns the indexes of delivery records whose order id
// equals the ticket's recorded partner reference, on the same platform and
// the same calendar day.
//
// Case-folded and trimmed, because the POS transcribed the id from the
// aggregator and a real integration is not reliably exact about case or
// whitespace. It is not otherwise fuzzy: no prefix matching, no
// normalization of separators. A reference that is nearly right is not a
// reference.
func findByReference(delivery []ingest.DeliveryRecord, ticket POSOrder) []int {
	want := strings.ToLower(strings.TrimSpace(ticket.PartnerOrderRef))
	day := ticket.Record.OrderDate.Format(dateLayout)
	platform := ticket.DeliveryPlatform.DisplayName()

	var out []int
	for i, rec := range delivery {
		if rec.Platform != platform || rec.OrderDate.Format(dateLayout) != day {
			continue
		}
		if strings.ToLower(strings.TrimSpace(rec.OrderID)) == want {
			out = append(out, i)
		}
	}
	return out
}

// findByAmountAndTime returns every unclaimed delivery record that could be
// this ticket: same platform, same day, EXACTLY equal cents, and placed
// within matchWindowMinutes.
//
// Exact cents, never a tolerance. A tolerance would mean deciding how much
// two amounts may differ and still be one order, which is a judgement
// about money this package has no basis for making. If two figures for the
// same order genuinely differ, the reference tier catches it and the
// amount-mismatch flag reports it; here, where amount IS the evidence,
// anything less than equality is not evidence.
//
// Refunded delivery records are compared on the absolute value of their
// subtotal, because this repository's convention stores a reversal
// negative (see client.go's contract rule 4) while the POS ticket for the
// same order was rung positive.
func findByAmountAndTime(delivery []ingest.DeliveryRecord, claimedBy []int, ticket POSOrder) []int {
	day := ticket.Record.OrderDate.Format(dateLayout)
	platform := ticket.DeliveryPlatform.DisplayName()

	out := make([]int, 0, 2)
	for i, rec := range delivery {
		if claimedBy[i] != -1 {
			continue
		}
		if rec.Platform != platform || rec.OrderDate.Format(dateLayout) != day {
			continue
		}
		if abs64(rec.SubtotalCents) != ticket.Record.GrossCents {
			continue
		}
		if minutesApart(rec.OrderTime, ticket.Record.OrderTime) > matchWindowMinutes {
			continue
		}
		out = append(out, i)
	}
	return out
}

// minutesApart is the absolute difference between two "HH:MM" wall-clock
// strings, in minutes. A string that does not parse returns a difference
// larger than any window, so an unparseable time can never produce a
// match — the failure direction here is "do not merge", which is the safe
// one.
func minutesApart(a, b string) int {
	am, aok := minutesOfDay(a)
	bm, bok := minutesOfDay(b)
	if !aok || !bok {
		return matchWindowMinutes + 1
	}
	if am > bm {
		return am - bm
	}
	return bm - am
}

func minutesOfDay(hhmm string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func mergeDecision(ticket POSOrder, rec ingest.DeliveryRecord, kind DedupKind, detail string) DedupDecision {
	return DedupDecision{
		Kind:                  kind,
		Date:                  ticket.Record.OrderDate,
		Platform:              ticket.DeliveryPlatform,
		POSOrderID:            ticket.Record.OrderID,
		POSRef:                ticket.Record.Ref,
		POSGrossCents:         ticket.Record.GrossCents,
		PlatformOrderID:       rec.OrderID,
		PlatformRef:           rec.Ref,
		PlatformSubtotalCents: rec.SubtotalCents,
		Detail:                detail,
	}
}

func unresolvedDecision(ticket POSOrder, kind DedupKind, candidates []string, detail string) DedupDecision {
	return DedupDecision{
		Kind:          kind,
		Date:          ticket.Record.OrderDate,
		Platform:      ticket.DeliveryPlatform,
		POSOrderID:    ticket.Record.OrderID,
		POSRef:        ticket.Record.Ref,
		POSGrossCents: ticket.Record.GrossCents,
		Candidates:    candidates,
		Detail:        detail,
	}
}

func orderIDs(delivery []ingest.DeliveryRecord, indexes []int) []string {
	out := make([]string, 0, len(indexes))
	for _, i := range indexes {
		out = append(out, delivery[i].OrderID)
	}
	sort.Strings(out)
	return out
}

// describeAmbiguity says which of the two ambiguity shapes was hit, in
// words an owner can act on: either this ticket has several possible
// orders, or several tickets want the same one.
func describeAmbiguity(ids []string, claimants map[int][]int, found []int) string {
	if len(found) > 1 {
		return fmt.Sprintf("%d orders match it equally well (%s)", len(ids), strings.Join(ids, ", "))
	}
	return fmt.Sprintf("more than one POS ticket matches order %s equally well", strings.Join(ids, ", "))
}
