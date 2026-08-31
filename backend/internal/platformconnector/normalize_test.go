package platformconnector

import (
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// The load-bearing claim of this feature: two upstreams that agree on
// nothing produce ONE record shape. This test states the same logical
// order in each platform's own wire format — $42.00 placed at 19:35 on
// 2026-08-20, at a 23% commission — and asserts the two adapters land on
// identical values for every field that reaches a margin figure.
//
// Fields that are legitimately allowed to differ (platform name, order id,
// provenance, notes) are asserted separately, so "they converged" cannot
// be satisfied by two empty records.
func TestBothAdaptersConvergeOnOneRecordShape(t *testing.T) {
	const (
		subtotalCents   = 4200
		rateBps         = 2300
		commissionCents = 966 // 4200 * 23%
		payoutCents     = subtotalCents - commissionCents
	)
	placedAt := time.Date(2026, 8, 20, 19, 35, 0, 0, merchantZone)

	ifoodRecs, err := ifoodAdapter{}.normalize(ifoodOrderDTO{
		ID:        "IFOOD-SIM-20260820-0007",
		CreatedAt: placedAt.Format(time.RFC3339),
		Total:     ifoodAmount{Currency: "USD", Amount: money.FormatCents(subtotalCents)},
		Commission: ifoodCommission{
			RatePercent: money.FormatCents(rateBps), // "23.00"
			Charged:     ifoodAmount{Currency: "USD", Amount: money.FormatCents(commissionCents)},
		},
		NetPayout: ifoodAmount{Currency: "USD", Amount: money.FormatCents(payoutCents)},
		Status:    "CONCLUDED",
	}, ingest.SourceRowRef{File: ifoodEndpoint() + "?date=2026-08-20&page=1", Row: 7})
	if err != nil {
		t.Fatalf("iFood normalize: %v", err)
	}
	if len(ifoodRecs) != 1 {
		t.Fatalf("a CONCLUDED order must normalize to exactly one record, got %d", len(ifoodRecs))
	}
	ifoodRec := ifoodRecs[0]

	// The same order as Just Eat Takeaway would report it: camelCase,
	// integer minor units, epoch milliseconds, and NO rate at all — the
	// adapter has to recover 2300 bps from 966/4200.
	jetRecs, err := jetAdapter{}.normalize(jetOrderDTO{
		OrderReference:   "JET-SIM-20260820-0007",
		PlacedAtEpochMs:  placedAt.UnixMilli(),
		Currency:         "USD",
		GrossAmountMinor: subtotalCents,
		CommissionMinor:  commissionCents,
		PayoutMinor:      payoutCents,
		FulfilmentState:  "DELIVERED",
	}, ingest.SourceRowRef{File: jetEndpoint() + "?day=2026-08-20&page=1", Row: 7})
	if err != nil {
		t.Fatalf("JET normalize: %v", err)
	}
	if len(jetRecs) != 1 {
		t.Fatalf("a DELIVERED order must normalize to exactly one record, got %d", len(jetRecs))
	}
	jetRec := jetRecs[0]

	type economics struct {
		orderDate         string
		orderTime         string
		subtotalCents     int64
		commissionRateBps int64
		commissionCents   int64
		netPayoutCents    int64
		status            string
		hasRefundDate     bool
	}
	extract := func(r ingest.DeliveryRecord) economics {
		return economics{
			orderDate:         r.OrderDate.Format(dateLayout),
			orderTime:         r.OrderTime,
			subtotalCents:     r.SubtotalCents,
			commissionRateBps: r.CommissionRateBps,
			commissionCents:   r.CommissionCents,
			netPayoutCents:    r.NetPayoutCents,
			status:            r.Status,
			hasRefundDate:     r.RefundDate != nil,
		}
	}

	want := economics{
		orderDate:         "2026-08-20",
		orderTime:         "19:35",
		subtotalCents:     subtotalCents,
		commissionRateBps: rateBps,
		commissionCents:   commissionCents,
		netPayoutCents:    payoutCents,
		status:            "completed",
	}
	if got := extract(ifoodRec); got != want {
		t.Errorf("iFood normalized to %+v, want %+v", got, want)
	}
	if got := extract(jetRec); got != want {
		t.Errorf("JET normalized to %+v, want %+v — the derived commission rate is the likely culprit", got, want)
	}

	// And the fields that must NOT converge, so the assertion above can't
	// be satisfied by two blank records.
	if ifoodRec.Platform != "iFood" || jetRec.Platform != "Just Eat Takeaway" {
		t.Errorf("platform names collapsed: %q / %q", ifoodRec.Platform, jetRec.Platform)
	}
	if ifoodRec.OrderID == jetRec.OrderID {
		t.Error("order ids collapsed — each platform must keep its own identifier")
	}
}

// Both platforms report the same business event — a refunded order — with
// OPPOSITE signs on the wire. Both must land on this repository's
// convention: negative money, a refund date, status "refunded" for the
// reversal row — AND, critically, a SEPARATE "completed" row carrying the
// order's original positive amounts, matching the two-row convention
// internal/reconcile.computeOneDay already implements for the CSV path
// (backend/cmd/gendata/opening/README.md: "reversed by a second row with
// the same order_id, negative amounts"). Before this fix, both adapters
// mutated a single record in place instead of emitting the completed
// counterpart — so `reconcile` never added the order's gross in the first
// place, and subtracting the refund from it double-penalized margin by the
// full order amount on every single cancellation. Found live: $1,138.24
// understated over a 31-day sample, 23 of 31 days affected, zero
// discrepancy flags raised — the exact "confidently wrong margin figure"
// class CLAUDE.md says must never happen.
func TestRefundNormalization_BothPlatformsLandNegative(t *testing.T) {
	const (
		subtotalCents      = 6225
		ifoodRateBps       = 2300
		ifoodCommissionAmt = 1432 // round(6225 * 23%)
		jetRateBps         = 2000
		jetCommissionAmt   = 1245 // 6225 * 20%
	)
	placedAt := time.Date(2026, 8, 20, 19, 35, 0, 0, merchantZone)
	refundedAt := placedAt.AddDate(0, 0, 7)

	t.Run("iFood reports it positive and the adapter negates", func(t *testing.T) {
		recs, err := ifoodAdapter{}.normalize(ifoodOrderDTO{
			ID:        "IFOOD-SIM-20260820-0011",
			CreatedAt: placedAt.Format(time.RFC3339),
			Total:     ifoodAmount{Amount: money.FormatCents(subtotalCents)},
			Commission: ifoodCommission{
				RatePercent: money.FormatCents(ifoodRateBps),
				Charged:     ifoodAmount{Amount: money.FormatCents(ifoodCommissionAmt)},
			},
			NetPayout: ifoodAmount{Amount: money.FormatCents(subtotalCents - ifoodCommissionAmt)},
			Status:    "CANCELLED",
			Cancel: &ifoodCancellation{
				CancelledAt: refundedAt.Format(time.RFC3339),
				Reason:      "CUSTOMER_DISPUTE",
			},
		}, ingest.SourceRowRef{File: ifoodEndpoint(), Row: 1})
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		assertCompletedThenRefundedShape(t, recs, "IFOOD-SIM-20260820-0011", subtotalCents, ifoodCommissionAmt, refundedAt)
	})

	t.Run("JET reports it negative and the adapter passes it through", func(t *testing.T) {
		refundMs := refundedAt.UnixMilli()
		recs, err := jetAdapter{}.normalize(jetOrderDTO{
			OrderReference:    "JET-SIM-20260820-0011",
			PlacedAtEpochMs:   placedAt.UnixMilli(),
			GrossAmountMinor:  -subtotalCents,
			CommissionMinor:   -jetCommissionAmt,
			PayoutMinor:       -(subtotalCents - jetCommissionAmt),
			FulfilmentState:   "REFUNDED",
			RefundedAtEpochMs: &refundMs,
		}, ingest.SourceRowRef{File: jetEndpoint(), Row: 1})
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		assertCompletedThenRefundedShape(t, recs, "JET-SIM-20260820-0011", subtotalCents, jetCommissionAmt, refundedAt)
		// The derived rate must survive the negative operands, on BOTH rows.
		for i, rec := range recs {
			if rec.CommissionRateBps != jetRateBps {
				t.Errorf("record %d: derived rate on a refund is %d bps, want %d — sign handling in the derivation is wrong", i, rec.CommissionRateBps, jetRateBps)
			}
		}
	})
}

// assertCompletedThenRefundedShape checks the two-record shape a
// refunded/cancelled order must normalize to: recs[0] is the original
// "completed" charge at its full positive amounts, recs[1] is the
// "refunded" reversal at the same amounts negated — the exact CSV
// convention internal/reconcile.computeOneDay already implements, so
// summing both rows' SubtotalCents/CommissionCents/NetPayoutCents nets to
// exactly zero, matching a cancelled order's real economic effect.
func assertCompletedThenRefundedShape(t *testing.T, recs []ingest.DeliveryRecord, wantOrderID string, wantSubtotal, wantCommission int64, wantRefundDate time.Time) {
	t.Helper()
	if len(recs) != 2 {
		t.Fatalf("a refunded/cancelled order must normalize to exactly 2 records (completed + refunded), got %d", len(recs))
	}
	completed, refunded := recs[0], recs[1]

	if completed.Status != "completed" {
		t.Errorf("recs[0].Status = %q, want \"completed\"", completed.Status)
	}
	if completed.RefundDate != nil {
		t.Error("recs[0] (the completed row) must not carry a refund date")
	}
	if completed.SubtotalCents != wantSubtotal {
		t.Errorf("recs[0].SubtotalCents = %s, want %s (the ORIGINAL positive charge)", money.FormatCents(completed.SubtotalCents), money.FormatCents(wantSubtotal))
	}
	if completed.CommissionCents != wantCommission {
		t.Errorf("recs[0].CommissionCents = %s, want %s", money.FormatCents(completed.CommissionCents), money.FormatCents(wantCommission))
	}
	if completed.NetPayoutCents != wantSubtotal-wantCommission {
		t.Errorf("recs[0].NetPayoutCents = %s, want %s", money.FormatCents(completed.NetPayoutCents), money.FormatCents(wantSubtotal-wantCommission))
	}
	if completed.OrderID != wantOrderID {
		t.Errorf("recs[0].OrderID = %q, want %q — both rows must share the same order id, matching the CSV convention", completed.OrderID, wantOrderID)
	}

	if refunded.Status != "refunded" {
		t.Errorf("recs[1].Status = %q, want \"refunded\"", refunded.Status)
	}
	if refunded.SubtotalCents != -wantSubtotal {
		t.Errorf("recs[1].SubtotalCents = %s, want %s", money.FormatCents(refunded.SubtotalCents), money.FormatCents(-wantSubtotal))
	}
	if refunded.CommissionCents != -wantCommission {
		t.Errorf("recs[1].CommissionCents = %s, want %s", money.FormatCents(refunded.CommissionCents), money.FormatCents(-wantCommission))
	}
	if refunded.NetPayoutCents != -(wantSubtotal - wantCommission) {
		t.Errorf("recs[1].NetPayoutCents = %s, want %s", money.FormatCents(refunded.NetPayoutCents), money.FormatCents(-(wantSubtotal - wantCommission)))
	}
	if refunded.OrderID != wantOrderID {
		t.Errorf("recs[1].OrderID = %q, want %q", refunded.OrderID, wantOrderID)
	}
	if refunded.RefundDate == nil {
		t.Fatal("recs[1] (the refunded row) has no refund date")
	}
	if got, want := refunded.RefundDate.Format(dateLayout), wantRefundDate.Format(dateLayout); got != want {
		t.Errorf("recs[1].RefundDate = %s, want %s", got, want)
	}

	// The load-bearing property: the two rows net to exactly zero, the same
	// way reconcile.computeOneDay's CSV-path convention does — this is what
	// makes a cancelled order economically invisible to margin, instead of
	// silently subtracting its full amount.
	if sum := completed.SubtotalCents + refunded.SubtotalCents; sum != 0 {
		t.Errorf("SubtotalCents across both rows sums to %d, want 0", sum)
	}
	if sum := completed.CommissionCents + refunded.CommissionCents; sum != 0 {
		t.Errorf("CommissionCents across both rows sums to %d, want 0", sum)
	}
	if sum := completed.NetPayoutCents + refunded.NetPayoutCents; sum != 0 {
		t.Errorf("NetPayoutCents across both rows sums to %d, want 0", sum)
	}
}

// Just Eat Takeaway reports timestamps as epoch milliseconds in UTC. A
// 21:30 local order is 00:30 the NEXT day in UTC — and internal/reconcile
// files a day by OrderDate, so reading the date straight off the epoch
// would move that order's revenue and commission into tomorrow's margin,
// on both days, with nothing to flag it.
func TestJETAdapter_LateEveningOrderKeepsItsLocalDay(t *testing.T) {
	placedAt := time.Date(2026, 8, 20, 21, 30, 0, 0, merchantZone)
	if got := placedAt.UTC().Format(dateLayout); got != "2026-08-21" {
		t.Fatalf("test premise broken: 21:30 local should be the next day in UTC, got %s", got)
	}

	recs, err := jetAdapter{}.normalize(jetOrderDTO{
		OrderReference:   "JET-SIM-20260820-0030",
		PlacedAtEpochMs:  placedAt.UnixMilli(),
		GrossAmountMinor: 4000,
		CommissionMinor:  800,
		PayoutMinor:      3200,
		FulfilmentState:  "DELIVERED",
	}, ingest.SourceRowRef{File: jetEndpoint(), Row: 1})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("a DELIVERED order must normalize to exactly one record, got %d", len(recs))
	}
	rec := recs[0]

	if got := rec.OrderDate.Format(dateLayout); got != "2026-08-20" {
		t.Errorf("order date is %s, want 2026-08-20 — the epoch timestamp was read in UTC instead of the merchant's zone", got)
	}
	if rec.OrderTime != "21:30" {
		t.Errorf("order time is %s, want 21:30", rec.OrderTime)
	}
}

// Refusals inside the adapters: an upstream state neither adapter can
// interpret is never guessed at.
func TestAdapters_RefuseUninterpretableUpstreamStates(t *testing.T) {
	placedAt := time.Date(2026, 8, 20, 19, 35, 0, 0, merchantZone)

	t.Run("iFood unknown status", func(t *testing.T) {
		_, err := ifoodAdapter{}.normalize(ifoodOrderDTO{
			ID:         "IFOOD-SIM-X",
			CreatedAt:  placedAt.Format(time.RFC3339),
			Total:      ifoodAmount{Amount: "42.00"},
			Commission: ifoodCommission{RatePercent: "23.00", Charged: ifoodAmount{Amount: "9.66"}},
			NetPayout:  ifoodAmount{Amount: "32.34"},
			Status:     "IN_TRANSIT",
		}, ingest.SourceRowRef{File: ifoodEndpoint(), Row: 1})
		requireErrorContaining(t, err, "unrecognized status")
	})

	t.Run("iFood CANCELLED without a cancellation block", func(t *testing.T) {
		_, err := ifoodAdapter{}.normalize(ifoodOrderDTO{
			ID:         "IFOOD-SIM-X",
			CreatedAt:  placedAt.Format(time.RFC3339),
			Total:      ifoodAmount{Amount: "42.00"},
			Commission: ifoodCommission{RatePercent: "23.00", Charged: ifoodAmount{Amount: "9.66"}},
			NetPayout:  ifoodAmount{Amount: "32.34"},
			Status:     "CANCELLED",
		}, ingest.SourceRowRef{File: ifoodEndpoint(), Row: 1})
		requireErrorContaining(t, err, "no cancellation block")
	})

	t.Run("JET REFUNDED without a refund timestamp", func(t *testing.T) {
		_, err := jetAdapter{}.normalize(jetOrderDTO{
			OrderReference:   "JET-SIM-X",
			PlacedAtEpochMs:  placedAt.UnixMilli(),
			GrossAmountMinor: -4200,
			CommissionMinor:  -840,
			PayoutMinor:      -3360,
			FulfilmentState:  "REFUNDED",
		}, ingest.SourceRowRef{File: jetEndpoint(), Row: 1})
		requireErrorContaining(t, err, "refusing rather than attributing the reversal to a date this connector made up")
	})

	t.Run("JET zero-value order yields no derivable rate", func(t *testing.T) {
		_, err := jetAdapter{}.normalize(jetOrderDTO{
			OrderReference:   "JET-SIM-X",
			PlacedAtEpochMs:  placedAt.UnixMilli(),
			GrossAmountMinor: 0,
			FulfilmentState:  "DELIVERED",
		}, ingest.SourceRowRef{File: jetEndpoint(), Row: 1})
		requireErrorContaining(t, err, "will not invent one")
	})
}
