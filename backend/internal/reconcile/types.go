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
)

// DailyReconciliation is the deterministic, provenanced daily margin
// computation (data-model.md). All money fields are integer cents
// (internal/money) — never float64 — so margin is always exactly
// reproducible from the same inputs.
type DailyReconciliation struct {
	Date               time.Time
	GrossSalesBySource map[string]int64 // cents, keyed by normalized source: "ifood", "just_eat_takeaway", "pos", ...
	CommissionsCents   int64
	RefundsCents       int64
	InputCostsCents    int64
	MarginCents        int64
	DiscrepancyFlags   []DiscrepancyFlag
	SourceRowRefs      []SourceRowRef
}
