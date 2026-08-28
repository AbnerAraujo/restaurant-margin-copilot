package reconcile

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

const dateKeyLayout = "2006-01-02"

// ComputeDailyReconciliations turns raw ingested records into one
// DailyReconciliation per calendar day covered by any of the three
// sources, applying FR-002's duplicate/refund/missing-day handling and
// FR-003's anomaly flagging. Every number here is produced by this
// function alone — no LLM ever computes or overrides it (Constitution
// Principle I).
//
// Design decisions made here, each deliberate and documented rather than
// implicit:
//
//   - Duplicate rows: an exact byte-for-byte duplicate delivery row (same
//     platform, order_id, timestamps, and amounts) is collapsed to one,
//     with a duplicate_order_removed flag naming the rows involved.
//   - Refund netting: netted against the refunded row's order_date field
//     (which the fixture data — and most real platform exports — carries
//     as the *original* order's date, not the refund settlement date),
//     per fixtures/README.md's documented convention. GrossSalesBySource
//     reports completed-only gross bookings; RefundsCents is the separate
//     refunded amount; commissions are summed across completed AND
//     refunded rows for the same order_date, since a platform reverses its
//     commission when it reverses the order.
//   - Missing sources: a calendar day with POS or cost-sheet data but zero
//     delivery rows still produces a DailyReconciliation (using whatever
//     sources ARE present) with an explicit missing_delivery_source flag —
//     never silently treated as zero delivery revenue or omitted from
//     output entirely.
//   - Supplier cost allocation: costs are allocated to their own
//     invoice_date only (point allocation, no smoothing/amortization
//     across the gap until the next invoice). Real supplier billing is not
//     daily (fixtures/README.md), so a day without an invoice simply
//     contributes zero input cost that day — a known, visible modeling
//     choice, not a hidden average.
func ComputeDailyReconciliations(delivery []ingest.DeliveryRecord, pos []ingest.POSRecord, costs []ingest.CostInvoiceRecord) []DailyReconciliation {
	dedupedDelivery, dupFlagsByDate := dedupeDelivery(delivery)

	deliveryByDate := groupBy(dedupedDelivery, func(r ingest.DeliveryRecord) string { return r.OrderDate.Format(dateKeyLayout) })
	posByDate := groupBy(pos, func(r ingest.POSRecord) string { return r.OrderDate.Format(dateKeyLayout) })
	costsByDate := groupBy(costs, func(r ingest.CostInvoiceRecord) string { return r.InvoiceDate.Format(dateKeyLayout) })

	dateSet := map[string]struct{}{}
	for k := range deliveryByDate {
		dateSet[k] = struct{}{}
	}
	for k := range posByDate {
		dateSet[k] = struct{}{}
	}
	for k := range costsByDate {
		dateSet[k] = struct{}{}
	}

	dateKeys := make([]string, 0, len(dateSet))
	for k := range dateSet {
		dateKeys = append(dateKeys, k)
	}
	// Dates format lexicographically the same as chronologically
	// (YYYY-MM-DD), so a plain lexicographic string sort is correct here.
	slices.Sort(dateKeys)

	days := make([]DailyReconciliation, 0, len(dateKeys))
	for _, dk := range dateKeys {
		days = append(days, computeOneDay(dk, deliveryByDate[dk], posByDate[dk], costsByDate[dk], dupFlagsByDate[dk]))
	}

	applyAnomalyFlags(days)
	return days
}

func computeOneDay(dateKey string, delivery []ingest.DeliveryRecord, pos []ingest.POSRecord, costs []ingest.CostInvoiceRecord, extraFlags []DiscrepancyFlag) DailyReconciliation {
	date, _ := time.Parse(dateKeyLayout, dateKey) // dateKey is always produced by Format(dateKeyLayout) above

	gross := map[string]int64{}
	commissionsBySource := map[string]int64{}
	var commissionsCents, refundsCents int64
	var refs []SourceRowRef
	flags := append([]DiscrepancyFlag{}, extraFlags...)

	for _, r := range delivery {
		refs = append(refs, r.Ref)
		src := normalizeSourceName(r.Platform)

		switch r.Status {
		case "completed":
			gross[src] += r.SubtotalCents
		case "refunded":
			refundsCents += abs64(r.SubtotalCents)
		}

		// Commission is summed across every row for the order (completed
		// AND refunded) grouped by order_date, so a refund's commission
		// reversal nets against its original charge within the same day —
		// see the design-decision doc comment on ComputeDailyReconciliations.
		// Broken down by source too (commissionsBySource), the same way
		// gross sales already are — a refund's reversal is keyed by the
		// same source as its original order, so it nets within that
		// source's own entry exactly as it nets within the total.
		recomputed := recomputeCommissionCents(r)
		commissionsCents += recomputed
		commissionsBySource[src] += recomputed
		if abs64(recomputed-r.CommissionCents) > 1 {
			flags = append(flags, DiscrepancyFlag{
				Type: FlagCommissionMismatch,
				Detail: fmt.Sprintf("order %s: file commission %s does not match recomputed %s (subtotal %s * rate) — row %d of %s",
					r.OrderID, money.FormatCents(r.CommissionCents), money.FormatCents(recomputed), money.FormatCents(r.SubtotalCents), r.Ref.Row, r.Ref.File),
			})
		}
	}

	for _, p := range pos {
		refs = append(refs, p.Ref)
		if p.Status == "" || p.Status == "completed" {
			gross["pos"] += p.GrossCents
		} else {
			flags = append(flags, DiscrepancyFlag{
				Type:   FlagPOSNonCompletedExcluded,
				Detail: fmt.Sprintf("POS order %s status=%q excluded from gross sales (row %d of %s)", p.OrderID, p.Status, p.Ref.Row, p.Ref.File),
			})
		}
	}

	var inputCostsCents int64
	for _, c := range costs {
		refs = append(refs, c.Ref)
		inputCostsCents += c.AmountCents
	}

	if len(delivery) == 0 {
		flags = append(flags, DiscrepancyFlag{
			Type: FlagMissingDeliverySource,
			Detail: fmt.Sprintf(
				"no delivery-platform export rows for %s — margin for this day is computed from POS and supplier-cost data only; delivery-platform revenue and commission are absent, not zeroed",
				dateKey,
			),
		})
	}

	var grossTotalCents int64
	for _, v := range gross {
		grossTotalCents += v
	}

	margin := grossTotalCents - commissionsCents - refundsCents - inputCostsCents

	return DailyReconciliation{
		Date:                date,
		GrossSalesBySource:  gross,
		CommissionsCents:    commissionsCents,
		CommissionsBySource: commissionsBySource,
		RefundsCents:        refundsCents,
		InputCostsCents:     inputCostsCents,
		MarginCents:         margin,
		DiscrepancyFlags:    flags,
		SourceRowRefs:       refs,
	}
}

// recomputeCommissionCents independently derives commission from subtotal
// and the file's stated rate, rather than trusting the file's own
// commission_amount column — a defensive check the fixture data explicitly
// calls for (fixtures/README.md: "the reconciliation engine should still be
// able to recompute and cross-check them from subtotal and
// commission_rate_pct"). CommissionRateBps is rate% * 100 (23% -> 2300), so
// commission = subtotal * rate% / 100 = subtotal * bps / 10000.
func recomputeCommissionCents(r ingest.DeliveryRecord) int64 {
	return money.DivRoundHalfUp(r.SubtotalCents*r.CommissionRateBps, 10000)
}

// dedupeDelivery collapses exact byte-for-byte duplicate rows (same
// platform, order_id, timestamps, amounts, status, campaign, and notes) to
// one, per fixtures/README.md irregularity #1 — a platform export/webhook-
// retry glitch, not a legitimate second line item. It returns the deduped
// records plus, per order_date, a discrepancy flag documenting every
// collapse so the adjustment stays visible (spec FR-002) rather than
// silent.
func dedupeDelivery(records []ingest.DeliveryRecord) ([]ingest.DeliveryRecord, map[string][]DiscrepancyFlag) {
	seen := make(map[string]ingest.DeliveryRecord, len(records))
	out := make([]ingest.DeliveryRecord, 0, len(records))
	flags := make(map[string][]DiscrepancyFlag)

	for _, r := range records {
		key := deliveryDedupKey(r)
		if first, ok := seen[key]; ok {
			dateKey := r.OrderDate.Format(dateKeyLayout)
			flags[dateKey] = append(flags[dateKey], DiscrepancyFlag{
				Type: FlagDuplicateOrderRemoved,
				Detail: fmt.Sprintf("order %s duplicated (rows %d and %d of %s) — counted once",
					r.OrderID, first.Ref.Row, r.Ref.Row, r.Ref.File),
			})
			continue
		}
		seen[key] = r
		out = append(out, r)
	}
	return out, flags
}

func deliveryDedupKey(r ingest.DeliveryRecord) string {
	refundDate := ""
	if r.RefundDate != nil {
		refundDate = r.RefundDate.Format(dateKeyLayout)
	}
	return strings.Join([]string{
		r.Platform, r.OrderID, r.OrderDate.Format(dateKeyLayout), r.OrderTime,
		money.FormatCents(r.SubtotalCents), money.FormatCents(r.CommissionCents), money.FormatCents(r.NetPayoutCents),
		r.Status, refundDate, r.CampaignID, r.Notes,
	}, "|")
}

func normalizeSourceName(platform string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(platform), " ", "_"))
}

func groupBy[T any](items []T, keyFn func(T) string) map[string][]T {
	out := make(map[string][]T)
	for _, item := range items {
		k := keyFn(item)
		out[k] = append(out[k], item)
	}
	return out
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
