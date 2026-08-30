package pipeline

import (
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// specs/010-platform-connector-proxy US1.3: a connector sync is
// authoritative for the range it covered, and for nothing else. A sync
// that quietly reached outside its range would rewrite days the owner
// never asked about — and because the pipeline re-derives everything from
// source on every run, there would be no diff anywhere to notice it by.

func csvRow(dateStr string, orderID string, subtotalCents int64) ingest.DeliveryRecord {
	// UTC midnight, exactly as internal/ingest's date parsing produces it.
	date, err := time.Parse(dateKeyLayout, dateStr)
	if err != nil {
		panic(err)
	}
	return ingest.DeliveryRecord{
		Ref:               ingest.SourceRowRef{File: "delivery_platform_export.csv", Row: 2},
		Platform:          "iFood",
		OrderID:           orderID,
		OrderDate:         date,
		SubtotalCents:     subtotalCents,
		CommissionRateBps: 2300,
		Status:            "completed",
	}
}

func connectorRow(dateStr string, orderID string, subtotalCents int64) ingest.DeliveryRecord {
	rec := csvRow(dateStr, orderID, subtotalCents)
	rec.Ref = ingest.SourceRowRef{File: "simulated://ifood-partner-api/v2/orders?date=" + dateStr + "&page=1", Row: 1}
	// The merchant's own zone, exactly as internal/platformconnector
	// produces it: the same calendar day as the CSV row above, but a
	// different instant. That difference is the reason range membership is
	// decided on the formatted date key rather than on time.Time ordering.
	y, m, d := rec.OrderDate.Date()
	rec.OrderDate = time.Date(y, m, d, 0, 0, 0, 0, time.FixedZone("BRT", -3*60*60))
	return rec
}

func orderIDs(records []ingest.DeliveryRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.OrderID)
	}
	return out
}

func assertOrderIDs(t *testing.T, got []ingest.DeliveryRecord, want ...string) {
	t.Helper()
	ids := orderIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("got orders %v, want %v", ids, want)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Fatalf("got orders %v, want %v", ids, want)
		}
	}
}

func TestApplyDeliveryOverlay(t *testing.T) {
	csv := []ingest.DeliveryRecord{
		csvRow("2026-08-17", "CSV-BEFORE", 1000),
		csvRow("2026-08-18", "CSV-FROM-BOUNDARY", 1000),
		csvRow("2026-08-19", "CSV-INSIDE", 1000),
		csvRow("2026-08-20", "CSV-TO-BOUNDARY", 1000),
		csvRow("2026-08-21", "CSV-AFTER", 1000),
	}

	t.Run("nil overlay leaves the CSV rows exactly as they were", func(t *testing.T) {
		got := applyDeliveryOverlay(csv, nil)
		assertOrderIDs(t, got, "CSV-BEFORE", "CSV-FROM-BOUNDARY", "CSV-INSIDE", "CSV-TO-BOUNDARY", "CSV-AFTER")
	})

	t.Run("in-range rows are replaced and out-of-range rows survive", func(t *testing.T) {
		from, _ := time.Parse(dateKeyLayout, "2026-08-18")
		to, _ := time.Parse(dateKeyLayout, "2026-08-20")

		got := applyDeliveryOverlay(csv, &DeliveryOverlay{
			From: from,
			To:   to,
			Records: []ingest.DeliveryRecord{
				connectorRow("2026-08-19", "API-INSIDE", 2000),
			},
		})

		// Both boundary days are inside the range and must be dropped,
		// even though only 08-19 has a replacement — an owner who synced
		// 08-18..08-20 is telling the product the platforms are the
		// authority for all three days, including a day they reported no
		// orders for.
		assertOrderIDs(t, got, "CSV-BEFORE", "CSV-AFTER", "API-INSIDE")
	})

	t.Run("a single-day range replaces exactly that day", func(t *testing.T) {
		d, _ := time.Parse(dateKeyLayout, "2026-08-19")
		got := applyDeliveryOverlay(csv, &DeliveryOverlay{
			From:    d,
			To:      d,
			Records: []ingest.DeliveryRecord{connectorRow("2026-08-19", "API-ONLY", 2000)},
		})
		assertOrderIDs(t, got, "CSV-BEFORE", "CSV-FROM-BOUNDARY", "CSV-TO-BOUNDARY", "CSV-AFTER", "API-ONLY")
	})

	t.Run("an empty overlay range still clears its days", func(t *testing.T) {
		// A platform that reported no orders for a synced day must leave
		// that day empty, not silently fall back to the CSV rows the sync
		// was meant to supersede. internal/reconcile's existing
		// missing_delivery_source flag is what then surfaces the gap.
		d, _ := time.Parse(dateKeyLayout, "2026-08-19")
		got := applyDeliveryOverlay(csv, &DeliveryOverlay{From: d, To: d})
		assertOrderIDs(t, got, "CSV-BEFORE", "CSV-FROM-BOUNDARY", "CSV-TO-BOUNDARY", "CSV-AFTER")
	})

	t.Run("a zone difference on the same calendar day is still in range", func(t *testing.T) {
		// The regression this guards: connector records are midnight in
		// the merchant's zone (UTC-3), CSV records are midnight UTC. On
		// the same calendar day those are three hours apart, so a
		// time.Time Before/After range check would place the connector's
		// own boundary day outside its own range.
		from, _ := time.Parse(dateKeyLayout, "2026-08-19")
		to := from
		overlayRec := connectorRow("2026-08-19", "API-BOUNDARY", 2000)
		if overlayRec.OrderDate.Equal(csv[2].OrderDate) {
			t.Fatal("test premise broken: the two records should be the same calendar day at different instants")
		}
		got := applyDeliveryOverlay(csv, &DeliveryOverlay{From: from, To: to, Records: []ingest.DeliveryRecord{overlayRec}})
		assertOrderIDs(t, got, "CSV-BEFORE", "CSV-FROM-BOUNDARY", "CSV-TO-BOUNDARY", "CSV-AFTER", "API-BOUNDARY")
	})
}
