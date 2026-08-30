package platformconnector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// These tests are the ones that carry this feature's financial risk, and
// they come in matched pairs on purpose: for every test that a real
// duplicate IS caught, there is one that a real non-duplicate is NOT. An
// over-eager matcher deletes revenue an owner can never get back; an
// under-eager one inflates gross sales every day. Both are the same
// failure, and a suite that only tested one direction would pass while
// the product quietly lost money.
//
// Everything below is hand-built rather than drawn from the mocks. The
// mocks generate a realistic mix; these tests need adversarial cases the
// mocks deliberately do not manufacture (see seed.go's "Ambiguity is not
// manufactured").

func testDay() time.Time {
	return time.Date(2026, 8, 20, 0, 0, 0, 0, merchantZone)
}

// deliveryFixture builds one delivery record with the fields the matcher
// actually reads. Commission is filled consistently so the record would
// also pass the proxy's contract check, keeping the fixture honest.
func deliveryFixture(platform Platform, orderID, hhmm string, subtotalCents int64) ingest.DeliveryRecord {
	commission := divRoundHalfUp(subtotalCents*ifoodCommissionBps, 10000)
	return ingest.DeliveryRecord{
		Ref:               ingest.SourceRowRef{File: "simulated://test/delivery", Row: 1},
		Platform:          platform.DisplayName(),
		OrderID:           orderID,
		OrderDate:         testDay(),
		OrderTime:         hhmm,
		SubtotalCents:     subtotalCents,
		CommissionRateBps: ifoodCommissionBps,
		CommissionCents:   commission,
		NetPayoutCents:    subtotalCents - commission,
		Status:            "completed",
	}
}

// posFixture builds one POS ticket. channel is "" for an in-house order,
// or a delivery Platform for a ticket the POS tagged as arriving through
// one. ref is the partner order reference, or "" when the integration did
// not record it.
func posFixture(ticketID, hhmm string, grossCents int64, channel Platform, ref string) POSOrder {
	placedAt, err := time.ParseInLocation("2006-01-02 15:04", "2026-08-20 "+hhmm, merchantZone)
	if err != nil {
		panic(err)
	}
	order := POSOrder{
		Record: ingest.POSRecord{
			Ref:        ingest.SourceRowRef{File: "simulated://test/pos", Row: 1},
			OrderID:    ticketID,
			OrderDate:  testDay(),
			OrderTime:  hhmm,
			GrossCents: grossCents,
			Channel:    "dine_in",
			Status:     "completed",
		},
		PlacedAt: placedAt,
	}
	if channel != "" {
		order.Record.Channel = string(channel)
		order.DeliveryPlatform = channel
		order.PartnerOrderRef = ref
	}
	return order
}

func allDeliveryFetched() map[Platform]bool {
	return map[Platform]bool{PlatformIFood: true, PlatformJustEatTakeaway: true}
}

func keptIDs(records []ingest.POSRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.OrderID)
	}
	return out
}

func kindsOf(decisions []DedupDecision) []DedupKind {
	out := make([]DedupKind, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, d.Kind)
	}
	return out
}

// --- Tier 1: the reference is identity ---------------------------------

func TestDedup_ReferencedDuplicateIsRemovedAndDeliveryRecordSurvives(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
	}
	pos := []POSOrder{
		posFixture("POS-A", "19:38", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007"),
	}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Empty(t, kept, "the POS copy of a referenced duplicate must be dropped")
	require.Len(t, delivery, 1, "the matcher must never remove a delivery record")
	require.Equal(t, []DedupKind{DedupMatchedByReference}, kindsOf(decisions))

	d := decisions[0]
	require.True(t, d.Kind.Merged())
	require.Equal(t, "POS-A", d.POSOrderID)
	require.Equal(t, "IFOOD-SIM-20260820-0007", d.PlatformOrderID)
	// Both sides' provenance, so the removal is traceable in both
	// directions (Constitution Principle IV).
	require.Contains(t, d.Detail, "simulated://test/pos")
	require.Contains(t, d.Detail, "simulated://test/delivery")
}

// The delivery side wins because it is the side that knows the commission.
// Keeping the POS ticket instead would zero that order's commission and
// move margin UP — a wrong number in the flattering direction.
func TestDedup_ResolutionKeepsTheSideThatKnowsTheCommission(t *testing.T) {
	rec := deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200)
	require.Greater(t, rec.CommissionCents, int64(0), "fixture sanity: the delivery side carries a commission")

	kept, _ := dedupeAcrossSources(
		[]ingest.DeliveryRecord{rec},
		[]POSOrder{posFixture("POS-A", "19:38", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007")},
		allDeliveryFetched(),
	)
	require.Empty(t, kept)
}

// A reference resolves regardless of amount, because identity was
// established without reference to it. The disagreement is reported, not
// absorbed — it is usually a platform-funded promotion the POS never saw.
func TestDedup_AmountMismatchOnAConfirmedMatchIsReported(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 3780),
	}
	// The POS rang the full menu price; the platform settled 10% lower.
	pos := []POSOrder{posFixture("POS-A", "19:38", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007")}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Empty(t, kept, "an amount disagreement must not stop a confirmed identity from resolving")
	require.Equal(t, []DedupKind{DedupMatchedByReference, DedupAmountMismatch}, kindsOf(decisions))
	require.Contains(t, decisions[1].Detail, "42.00")
	require.Contains(t, decisions[1].Detail, "37.80")
	require.Contains(t, decisions[1].Detail, "4.20", "the difference itself should be named, not left to be computed")
}

// A reference that names an order nobody has is proof the picture is
// incomplete. Falling through to amount matching there would risk binding
// this ticket to a DIFFERENT order that happens to cost the same —
// deleting that order's revenue to hide this one's.
func TestDedup_UnresolvableReferenceIsFlaggedAndNotAmountMatched(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		// Same amount, same minute — a perfect amount-and-time candidate,
		// and the matcher must still not take it.
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0099", "19:35", 4200),
	}
	pos := []POSOrder{posFixture("POS-A", "19:35", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007")}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Equal(t, []string{"POS-A"}, keptIDs(kept), "nothing may be merged on a reference that does not resolve")
	require.Equal(t, []DedupKind{DedupUnresolvedNoCounterpart}, kindsOf(decisions))
	require.Contains(t, decisions[0].Detail, "IFOOD-SIM-20260820-0007")
	require.Contains(t, decisions[0].Detail, "may count that order twice",
		"an unresolved overlap must state the consequence, not just the fact")
}

// --- Tier 2: channel + exact amount + bounded time ---------------------

func TestDedup_ChannelTaggedDuplicateMatchesOnExactAmountAndWindow(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
		// A decoy at the same time on a different platform, and a decoy
		// on the same platform at a different amount. Neither may be
		// taken.
		deliveryFixture(PlatformJustEatTakeaway, "JET-SIM-20260820-0003", "19:36", 4200),
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0008", "19:37", 4201),
	}
	pos := []POSOrder{posFixture("POS-A", "19:42", 4200, PlatformIFood, "")}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Empty(t, kept)
	require.Equal(t, []DedupKind{DedupMatchedByChannelAmountTime}, kindsOf(decisions))
	require.Equal(t, "IFOOD-SIM-20260820-0007", decisions[0].PlatformOrderID)
}

// THE FALSE-POSITIVE BAR (spec 012 FR-011).
//
// Two in-house tickets sitting on exact-cent, one-minute-away matches for
// real delivery orders. On a busy evening at a $32 mean ticket this is not
// exotic, it is expected — and merging either one would silently delete a
// real dine-in order's revenue from the day. The POS never said these came
// through a delivery channel, so the matcher must not consider them at
// all, no matter how well the numbers line up.
func TestDedup_InHouseTicketSharingAmountAndTimeIsNotRemoved(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
		deliveryFixture(PlatformJustEatTakeaway, "JET-SIM-20260820-0003", "12:10", 2850),
	}
	pos := []POSOrder{
		posFixture("POS-DINEIN", "19:36", 4200, "", ""),  // same cents, one minute away
		posFixture("POS-COUNTER", "12:10", 2850, "", ""), // same cents, same minute
	}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Equal(t, []string{"POS-DINEIN", "POS-COUNTER"}, keptIDs(kept),
		"an untagged POS ticket must survive regardless of how well its amount and time align")
	require.Empty(t, decisions,
		"an in-house ticket is not a candidate, so it must not even produce an unresolved flag — that would be noise on every dine-in order that happens to cost the same as a delivery one")
}

// THE AMBIGUITY BAR, shape 1 (spec 012 FR-012): one ticket, two equally
// good orders. Picking either would be a coin flip presented as a result.
func TestDedup_TicketWithTwoEqualCandidatesMergesNothing(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0011", "19:40", 4200),
	}
	pos := []POSOrder{posFixture("POS-A", "19:38", 4200, PlatformIFood, "")}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Equal(t, []string{"POS-A"}, keptIDs(kept))
	require.Equal(t, []DedupKind{DedupUnresolvedAmbiguous}, kindsOf(decisions))
	require.Equal(t,
		[]string{"IFOOD-SIM-20260820-0007", "IFOOD-SIM-20260820-0011"},
		decisions[0].Candidates,
		"both candidates must be named so a human can settle what the matcher would not")
	require.Contains(t, decisions[0].Detail, "may count that order twice")
}

// THE AMBIGUITY BAR, shape 2: two tickets, one order. This is the case a
// naive first-come-first-served matcher gets wrong while looking correct —
// it hands the order to whichever ticket it walked first and leaves the
// other looking cleanly unmatched, when the truth is that neither could be
// told apart. Nothing may merge, and BOTH tickets must be flagged.
func TestDedup_TwoTicketsContestingOneOrderMergeNothing(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
	}
	pos := []POSOrder{
		posFixture("POS-A", "19:36", 4200, PlatformIFood, ""),
		posFixture("POS-B", "19:40", 4200, PlatformIFood, ""),
	}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	require.Equal(t, []string{"POS-A", "POS-B"}, keptIDs(kept))
	require.Equal(t, []DedupKind{DedupUnresolvedAmbiguous, DedupUnresolvedAmbiguous}, kindsOf(decisions))
	for _, d := range decisions {
		require.Contains(t, d.Detail, "more than one POS ticket matches order")
	}
}

// The symmetry claim, proven rather than asserted: reversing the input
// order changes no outcome. A matcher that merged as it walked would give
// a different answer here.
func TestDedup_IsIndependentOfInputOrder(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0011", "20:10", 3150),
	}
	pos := []POSOrder{
		posFixture("POS-A", "19:36", 4200, PlatformIFood, ""),
		posFixture("POS-B", "19:40", 4200, PlatformIFood, ""),
		posFixture("POS-C", "20:12", 3150, PlatformIFood, ""),
	}

	forward, forwardDecisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	reversedPOS := []POSOrder{pos[2], pos[1], pos[0]}
	reversedDelivery := []ingest.DeliveryRecord{delivery[1], delivery[0]}
	reverse, reverseDecisions := dedupeAcrossSources(reversedDelivery, reversedPOS, allDeliveryFetched())

	// POS-A and POS-B contest one order and neither may take it; POS-C is
	// unambiguous and merges. That verdict must not depend on iteration
	// order in either direction.
	require.ElementsMatch(t, []string{"POS-A", "POS-B"}, keptIDs(forward))
	require.ElementsMatch(t, []string{"POS-A", "POS-B"}, keptIDs(reverse))
	require.Len(t, forwardDecisions, 3)
	require.Len(t, reverseDecisions, 3)
}

// The window is a boundary, and a boundary that is never tested is a
// boundary that drifts.
func TestDedup_TimeWindowIsEnforcedAtItsEdge(t *testing.T) {
	inside := deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200)

	t.Run("exactly at the window", func(t *testing.T) {
		kept, decisions := dedupeAcrossSources(
			[]ingest.DeliveryRecord{inside},
			[]POSOrder{posFixture("POS-A", "19:50", 4200, PlatformIFood, "")},
			allDeliveryFetched(),
		)
		require.Empty(t, kept)
		require.Equal(t, []DedupKind{DedupMatchedByChannelAmountTime}, kindsOf(decisions))
	})

	t.Run("one minute past the window", func(t *testing.T) {
		kept, decisions := dedupeAcrossSources(
			[]ingest.DeliveryRecord{inside},
			[]POSOrder{posFixture("POS-A", "19:51", 4200, PlatformIFood, "")},
			allDeliveryFetched(),
		)
		require.Equal(t, []string{"POS-A"}, keptIDs(kept))
		require.Equal(t, []DedupKind{DedupUnresolvedNoCounterpart}, kindsOf(decisions))
	})
}

// A single cent of difference is a different order as far as this matcher
// is concerned. No tolerance: deciding how much two amounts may differ and
// still be one order is a judgement about money this package has no basis
// for making.
func TestDedup_AmountMatchIsExactToTheCent(t *testing.T) {
	kept, decisions := dedupeAcrossSources(
		[]ingest.DeliveryRecord{deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200)},
		[]POSOrder{posFixture("POS-A", "19:36", 4201, PlatformIFood, "")},
		allDeliveryFetched(),
	)
	require.Equal(t, []string{"POS-A"}, keptIDs(kept))
	require.Equal(t, []DedupKind{DedupUnresolvedNoCounterpart}, kindsOf(decisions))
}

// --- Scope --------------------------------------------------------------

// A ticket naming a platform this sync did not fetch is left alone AND
// left unflagged. Flagging it would put a warning on every iFood-channel
// ticket during a Just-Eat-only sync — noise the owner cannot act on,
// burying the signals they can.
func TestDedup_TicketForAPlatformOutsideTheFetchIsLeftAlone(t *testing.T) {
	kept, decisions := dedupeAcrossSources(
		nil,
		[]POSOrder{posFixture("POS-A", "19:36", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007")},
		map[Platform]bool{PlatformJustEatTakeaway: true},
	)
	require.Equal(t, []string{"POS-A"}, keptIDs(kept))
	require.Empty(t, decisions)
}

// Two tickets carrying the same reference both describe that one platform
// order, so both are duplicates of it. Reference equality is identity; a
// second claim is not evidence against the first.
func TestDedup_TwoTicketsWithTheSameReferenceAreBothRemoved(t *testing.T) {
	kept, decisions := dedupeAcrossSources(
		[]ingest.DeliveryRecord{deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200)},
		[]POSOrder{
			posFixture("POS-A", "19:36", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007"),
			posFixture("POS-B", "19:37", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007"),
		},
		allDeliveryFetched(),
	)
	require.Empty(t, kept)
	require.Equal(t, []DedupKind{DedupMatchedByReference, DedupMatchedByReference}, kindsOf(decisions))
}

// The reference is compared case-folded and trimmed, because the POS
// transcribed it from the aggregator. It is not otherwise fuzzy — a
// reference that is nearly right is not a reference.
func TestDedup_ReferenceMatchingIsCaseFoldedButNotFuzzy(t *testing.T) {
	delivery := []ingest.DeliveryRecord{deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200)}

	t.Run("case and whitespace are forgiven", func(t *testing.T) {
		kept, _ := dedupeAcrossSources(delivery,
			[]POSOrder{posFixture("POS-A", "19:36", 4200, PlatformIFood, "  ifood-sim-20260820-0007 ")},
			allDeliveryFetched())
		require.Empty(t, kept)
	})

	t.Run("a nearly-right reference is not a reference", func(t *testing.T) {
		kept, decisions := dedupeAcrossSources(delivery,
			[]POSOrder{posFixture("POS-A", "19:36", 4200, PlatformIFood, "IFOOD-SIM-20260820-007")},
			allDeliveryFetched())
		require.Equal(t, []string{"POS-A"}, keptIDs(kept))
		require.Equal(t, []DedupKind{DedupUnresolvedNoCounterpart}, kindsOf(decisions))
	})
}

// Nothing is silently corrected: every removal produces a decision, and
// the count of removals must equal the count of dropped tickets exactly.
// A merge with no decision would be a ticket that vanished from the day
// with nothing anywhere to explain it.
func TestDedup_EveryRemovalProducesAVisibleDecision(t *testing.T) {
	delivery := []ingest.DeliveryRecord{
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0007", "19:35", 4200),
		deliveryFixture(PlatformIFood, "IFOOD-SIM-20260820-0011", "20:10", 3150),
	}
	pos := []POSOrder{
		posFixture("POS-A", "19:38", 4200, PlatformIFood, "IFOOD-SIM-20260820-0007"),
		posFixture("POS-B", "20:12", 3150, PlatformIFood, ""),
		posFixture("POS-C", "13:05", 2600, "", ""),
	}

	kept, decisions := dedupeAcrossSources(delivery, pos, allDeliveryFetched())

	removed := len(pos) - len(kept)
	merged := 0
	for _, d := range decisions {
		if d.Kind.Merged() {
			merged++
			require.NotEmpty(t, d.Detail)
			require.NotEmpty(t, d.PlatformOrderID)
			require.NotZero(t, d.POSRef.Row)
			require.NotZero(t, d.PlatformRef.Row)
		}
	}
	require.Equal(t, removed, merged, "every dropped ticket must be accounted for by exactly one merge decision")
	require.Equal(t, 2, removed)
	require.Equal(t, []string{"POS-C"}, keptIDs(kept))
}
