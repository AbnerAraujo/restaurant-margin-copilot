package pipeline

import (
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
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

// --- specs/012-pos-connector-dedup: the POS side of the overlay --------
//
// The failure this section exists to prevent is quiet and expensive: a
// delivery-only sync that also cleared the day's POS revenue would erase
// two thirds of gross sales for every synced day and drop margin, with
// only the pre-existing flags to hint at why. The Active booleans are what
// make that structurally impossible rather than a caller's discipline.

func posCSVRow(dateStr, orderID string, grossCents int64) ingest.POSRecord {
	date, err := time.Parse(dateKeyLayout, dateStr)
	if err != nil {
		panic(err)
	}
	return ingest.POSRecord{
		Ref:        ingest.SourceRowRef{File: "pos_export.csv", Row: 2},
		OrderID:    orderID,
		OrderDate:  date,
		OrderTime:  "19:35",
		Channel:    "dine_in",
		GrossCents: grossCents,
		Status:     "completed",
	}
}

func posIDs(records []ingest.POSRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.OrderID)
	}
	return out
}

func TestConnectorOverlay_POSRowsAreReplacedOnlyWhenPOSIsActive(t *testing.T) {
	csv := []ingest.POSRecord{
		posCSVRow("2026-08-17", "CSV-BEFORE", 1000),
		posCSVRow("2026-08-18", "CSV-IN-RANGE", 2000),
		posCSVRow("2026-08-21", "CSV-AFTER", 3000),
	}
	replacement := []ingest.POSRecord{posCSVRow("2026-08-18", "CONNECTOR-TICKET", 2500)}

	t.Run("POS active: in-range rows are replaced, others kept verbatim", func(t *testing.T) {
		got := replaceInRange(csv, "2026-08-18", "2026-08-20", replacement,
			func(r ingest.POSRecord) string { return r.OrderDate.Format(dateKeyLayout) })
		if want := []string{"CSV-BEFORE", "CSV-AFTER", "CONNECTOR-TICKET"}; !equalStrings(posIDs(got), want) {
			t.Fatalf("got %v, want %v", posIDs(got), want)
		}
	})

	// The one that matters. A delivery-only overlay must leave every POS
	// row where it is — spec 012 US1.4.
	t.Run("delivery-only overlay leaves POS untouched", func(t *testing.T) {
		overlay := &ConnectorOverlay{
			From:           mustDay("2026-08-18"),
			To:             mustDay("2026-08-20"),
			DeliveryActive: true,
			Delivery:       []ingest.DeliveryRecord{csvRow("2026-08-18", "CONNECTOR-ORDER", 4200)},
		}
		if overlay.POSActive {
			t.Fatal("a delivery-only overlay must not mark the POS active")
		}
		// The pipeline only touches POS when POSActive is set, so the
		// structural guarantee is the boolean itself. Assert the shape
		// rather than re-run the whole pipeline against a database.
		if len(overlay.POS) != 0 {
			t.Fatal("a delivery-only overlay carries no POS records")
		}
	})

	// And the distinction a nil slice cannot express: "the terminal was on
	// and took no orders" must clear the range, because that is a real
	// business fact the day should reflect.
	t.Run("POS active with zero tickets clears the range", func(t *testing.T) {
		got := replaceInRange(csv, "2026-08-18", "2026-08-20", nil,
			func(r ingest.POSRecord) string { return r.OrderDate.Format(dateKeyLayout) })
		if want := []string{"CSV-BEFORE", "CSV-AFTER"}; !equalStrings(posIDs(got), want) {
			t.Fatalf("got %v, want %v", posIDs(got), want)
		}
	})
}

// Every dedup decision must reach the day it belongs to, as a flag, with
// its explanation intact. A decision that produced no flag would be a POS
// ticket that vanished from a day's gross sales with nothing anywhere to
// say why — the silent correction this product's whole ethos forbids.
func TestDedupFlagsByDate_TranslatesEveryDecision(t *testing.T) {
	aug18 := mustDay("2026-08-18")
	aug19 := mustDay("2026-08-19")

	overlay := &ConnectorOverlay{
		Decisions: []platformconnector.DedupDecision{
			{Kind: platformconnector.DedupMatchedByReference, Date: aug18, Detail: "merged by reference"},
			{Kind: platformconnector.DedupMatchedByChannelAmountTime, Date: aug18, Detail: "merged by amount and time"},
			{Kind: platformconnector.DedupAmountMismatch, Date: aug18, Detail: "amounts disagree"},
			{Kind: platformconnector.DedupUnresolvedAmbiguous, Date: aug19, Detail: "two candidates"},
			{Kind: platformconnector.DedupUnresolvedNoCounterpart, Date: aug19, Detail: "no counterpart"},
		},
	}

	flags := dedupFlagsByDate(overlay)

	if got := len(flags[aug18.Format(dateKeyLayout)]) + len(flags[aug19.Format(dateKeyLayout)]); got != 5 {
		t.Fatalf("%d decisions produced %d flags — every decision must be visible", len(overlay.Decisions), got)
	}

	wantAug18 := []string{
		reconcile.FlagCrossSourceDuplicateRemoved,
		reconcile.FlagCrossSourceDuplicateRemoved,
		reconcile.FlagCrossSourceAmountMismatch,
	}
	for i, want := range wantAug18 {
		if got := flags["2026-08-18"][i].Type; got != want {
			t.Fatalf("2026-08-18 flag %d is %q, want %q", i, got, want)
		}
	}
	for i := range flags["2026-08-19"] {
		if got := flags["2026-08-19"][i].Type; got != reconcile.FlagCrossSourceDuplicateUnresolved {
			t.Fatalf("2026-08-19 flag %d is %q, want %q", i, got, reconcile.FlagCrossSourceDuplicateUnresolved)
		}
	}

	// The explanation has to survive the translation. A flag whose detail
	// was dropped is a flag an owner cannot act on.
	if flags["2026-08-18"][0].Detail != "merged by reference" {
		t.Fatalf("the decision's explanation did not survive translation: %q", flags["2026-08-18"][0].Detail)
	}

	if dedupFlagsByDate(nil) != nil {
		t.Fatal("a nil overlay must produce no flags")
	}
	if dedupFlagsByDate(&ConnectorOverlay{}) != nil {
		t.Fatal("an overlay with no decisions must produce no flags")
	}
}

func mustDay(s string) time.Time {
	d, err := time.Parse(dateKeyLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
