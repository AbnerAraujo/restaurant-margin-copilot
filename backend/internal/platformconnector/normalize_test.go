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

	ifoodRec, err := ifoodAdapter{}.normalize(ifoodOrderDTO{
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

	// The same order as Just Eat Takeaway would report it: camelCase,
	// integer minor units, epoch milliseconds, and NO rate at all — the
	// adapter has to recover 2300 bps from 966/4200.
	jetRec, err := jetAdapter{}.normalize(jetOrderDTO{
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
// convention: negative money, a refund date, status "refunded". An adapter
// that got this wrong would have reconcile count a refund as revenue.
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
		rec, err := ifoodAdapter{}.normalize(ifoodOrderDTO{
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
		assertRefundShape(t, rec, -subtotalCents, -ifoodCommissionAmt, refundedAt)
	})

	t.Run("JET reports it negative and the adapter passes it through", func(t *testing.T) {
		refundMs := refundedAt.UnixMilli()
		rec, err := jetAdapter{}.normalize(jetOrderDTO{
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
		assertRefundShape(t, rec, -subtotalCents, -jetCommissionAmt, refundedAt)
		// The derived rate must survive the negative operands.
		if rec.CommissionRateBps != jetRateBps {
			t.Errorf("derived rate on a refund is %d bps, want %d — sign handling in the derivation is wrong", rec.CommissionRateBps, jetRateBps)
		}
	})
}

func assertRefundShape(t *testing.T, rec ingest.DeliveryRecord, wantSubtotal, wantCommission int64, wantRefundDate time.Time) {
	t.Helper()
	if rec.Status != "refunded" {
		t.Errorf("status is %q, want \"refunded\"", rec.Status)
	}
	if rec.SubtotalCents != wantSubtotal {
		t.Errorf("subtotal is %s, want %s", money.FormatCents(rec.SubtotalCents), money.FormatCents(wantSubtotal))
	}
	if rec.CommissionCents != wantCommission {
		t.Errorf("commission is %s, want %s", money.FormatCents(rec.CommissionCents), money.FormatCents(wantCommission))
	}
	if rec.NetPayoutCents != wantSubtotal-wantCommission {
		t.Errorf("payout is %s, want %s", money.FormatCents(rec.NetPayoutCents), money.FormatCents(wantSubtotal-wantCommission))
	}
	if rec.RefundDate == nil {
		t.Fatal("refund date is nil on a refunded record")
	}
	if got, want := rec.RefundDate.Format(dateLayout), wantRefundDate.Format(dateLayout); got != want {
		t.Errorf("refund date is %s, want %s", got, want)
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

	rec, err := jetAdapter{}.normalize(jetOrderDTO{
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
