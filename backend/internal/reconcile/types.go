// Package reconcile is the deterministic core of the product (Constitution
// Principle I): it turns raw ingested records (internal/ingest) into one
// DailyReconciliation per calendar day, handling duplicate rows, refund
// netting, missing sources, and anomaly thresholds — all in plain Go, with
// no model call anywhere in this package. Every number it produces must be
// reproducible by re-running this package against the same source rows
// (data-model.md's DailyReconciliation validation rule).
package reconcile

import (
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// SourceRowRef re-exports ingest.SourceRowRef under this package's
// vocabulary so provenance travels unbroken from parsing through to the
// persisted DailyReconciliation (Constitution Principle IV).
type SourceRowRef = ingest.SourceRowRef

// DiscrepancyFlag records something reconciliation had to notice and
// handle explicitly — a duplicate row it collapsed, a missing source, a
// commission figure that didn't match its own recomputation, or an anomaly
// threshold breach — so nothing is ever silently dropped or smoothed over
// (spec FR-002, FR-003).
type DiscrepancyFlag struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

// Discrepancy flag type constants, used both by reconcile.go/discrepancies.go
// and by tests asserting on them.
const (
	FlagDuplicateOrderRemoved    = "duplicate_order_removed"
	FlagMissingDeliverySource    = "missing_delivery_source"
	FlagCommissionMismatch       = "commission_mismatch"
	FlagPOSNonCompletedExcluded  = "pos_non_completed_row_excluded"
	FlagAnomalyThresholdExceeded = "anomaly_threshold_exceeded"
	// FlagOrphanRefund fires when a "refunded" delivery row has no matching
	// "completed" row for the same order id on the same date — the two-row
	// convention (backend/cmd/gendata/opening/README.md) every refund is
	// supposed to follow. Found live: both simulated connector adapters
	// once emitted refunds as a single mutated row with no completed
	// counterpart, silently double-subtracting the refund from margin with
	// no flag anywhere to explain why. That specific bug is fixed at the
	// source (platformconnector), but this flag stays as the invariant
	// check that would catch the SAME shape of defect in a real,
	// hand-produced or third-party export this code has not seen yet.
	FlagOrphanRefund = "orphan_refund"

	// The cross-source family (specs/012-pos-connector-dedup). These
	// describe an overlap BETWEEN two sources, which is a different
	// condition from FlagDuplicateOrderRemoved — that one is one export
	// repeating a row within itself (a webhook retry), decided by exact
	// byte equality on a shared order id. These three are decided across
	// two systems that share no id at all, which is why they can be
	// uncertain and why two of the three exist to say so.
	//
	// This package does not raise them. They are produced by
	// internal/platformconnector's matcher and carried in by
	// internal/pipeline, which is the layer that knows both packages. The
	// constants live here because this package owns the flag vocabulary,
	// and a flag type spelled slightly differently in two places is a
	// filter that silently returns nothing.

	// FlagCrossSourceDuplicateRemoved: a POS ticket was identified as the
	// same real-world order as a delivery-platform order and was removed,
	// so the order is counted once. The delivery record is always the one
	// kept — it carries the commission, the payout and the refund state
	// the POS ticket knows nothing about.
	FlagCrossSourceDuplicateRemoved = "cross_source_duplicate_removed"

	// FlagCrossSourceDuplicateUnresolved: a POS ticket the POS itself
	// tagged as arriving through a delivery platform could not be paired
	// with confidence — either no counterpart was found, or more than one
	// reading of the day was equally consistent. NOTHING was merged. This
	// day may therefore count that order twice, and the flag exists so
	// that possibility is disclosed rather than either silently accepted
	// or silently "fixed" by a guess.
	FlagCrossSourceDuplicateUnresolved = "cross_source_duplicate_unresolved"

	// FlagCrossSourceAmountMismatch: a pair whose identity was established
	// independently of amount reports two different amounts — typically a
	// platform-funded promotion the POS never saw. The merge stands; the
	// disagreement is reported rather than absorbed.
	FlagCrossSourceAmountMismatch = "cross_source_amount_mismatch"
)

// DailyReconciliation is the deterministic, provenanced daily margin
// computation (data-model.md). All money fields are integer cents
// (internal/money) — never float64 — so margin is always exactly
// reproducible from the same inputs.
type DailyReconciliation struct {
	Date               time.Time
	GrossSalesBySource map[string]int64 // cents, keyed by normalized source: "ifood", "just_eat_takeaway", "pos", ...
	CommissionsCents   int64
	// CommissionsBySource breaks CommissionsCents down per normalized
	// delivery-platform source key, the same keys GrossSalesBySource uses
	// (added for specs/003-platform-comparator's compare_platform_economics
	// tool). It always sums back to CommissionsCents. "pos" never appears
	// here — POS orders carry no commission at all. A refund's commission
	// reversal is keyed by the SAME source as its original order (both are
	// the same platform, by construction), so it nets within that source's
	// entry exactly the way it already nets within the CommissionsCents
	// total.
	CommissionsBySource map[string]int64
	RefundsCents        int64
	// RefundsBySource breaks RefundsCents down per normalized delivery-
	// platform source key, the same keys GrossSalesBySource/
	// CommissionsBySource use (added to close a real, measured evaluation
	// gap: A15, "Delivery revenue on 2026-08-02, net of the refund?" —
	// docs/product-strategy.md). It always sums back to RefundsCents. "pos"
	// never appears here: a refunded delivery-platform row always carries
	// its own Platform field (internal/ingest.DeliveryRecord), but POS rows
	// have no refunded status at all in this reconciliation — a non-
	// "completed" POS row is excluded entirely (FlagPOSNonCompletedExcluded)
	// rather than netted as a refund, so POS can never contribute here.
	RefundsBySource  map[string]int64
	InputCostsCents  int64
	MarginCents      int64
	DiscrepancyFlags []DiscrepancyFlag
	SourceRowRefs    []SourceRowRef
}
