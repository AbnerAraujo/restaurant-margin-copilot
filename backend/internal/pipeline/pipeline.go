// Package pipeline wires internal/ingest -> internal/reconcile ->
// internal/storage into the single ingest -> reconcile -> persist flow
// tasks.md's T017 describes. It lives in its own importable package —
// not backend/cmd/server, which is package main and cannot be imported by
// anything else — specifically so later phases (the MCP tool layer, the
// evaluation harness, a future re-ingest HTTP endpoint) can trigger the
// same pipeline directly, without shelling out to `go run ./cmd/server`.
package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// RunIngestionPipeline reads whichever delivery-platform, POS, and
// supplier-cost-sheet exports it finds in dataDir — matched by filename
// keyword, not a hardcoded exact filename, the same real-file-compatibility
// posture as internal/ingest's column matching — computes one
// DailyReconciliation per calendar day (internal/reconcile), persists each
// to Postgres (internal/storage), and prints a per-day summary to stdout.
//
// It deliberately does not require every source type to be present: a
// directory missing a delivery-platform export still reconciles from
// whatever sources exist, with internal/reconcile's missing_delivery_source
// flag surfacing the gap on every affected day — the same "state plainly
// what's missing" behavior spec Acceptance Scenario US1.3 requires for a
// single missing day, just applied directory-wide when the whole file is
// absent.
//
// Promotion/ad-spend files are recognized and skipped here on purpose:
// PromotionRoiRecord ingestion and computation is User Story 4's scope
// (tasks.md T029-T030), not User Story 1's — this function does not touch
// it.
func RunIngestionPipeline(dataDir string, store *storage.Queries) error {
	return RunIngestionPipelineWithConnectorOverlay(dataDir, store, nil)
}

// DeliveryOverlay replaces the CSV-sourced delivery rows for one inclusive
// calendar-date range with records obtained some other way — today, from
// internal/platformconnector's simulated iFood and Just Eat Takeaway
// partner APIs (specs/010-platform-connector-proxy).
//
// It is a range and a record slice, not a callback or a source enum, on
// purpose: the pipeline must not know or care where these records came
// from. They are ingest.DeliveryRecord values, the exact type
// ParseDeliveryExport produces, so internal/reconcile cannot tell them
// apart from CSV rows and is not asked to (spec FR-004 — this is a new
// data SOURCE, not a new computation path).
type DeliveryOverlay struct {
	// From and To are inclusive calendar dates. Comparison is done on the
	// formatted YYYY-MM-DD key rather than on time.Time ordering, because
	// CSV-parsed dates are UTC midnight while connector-sourced dates are
	// midnight in the merchant's own zone — the same calendar day, two
	// different instants, and a Before/After comparison between them would
	// silently drop a boundary day.
	From time.Time
	To   time.Time

	// Records are authoritative for [From, To]. An empty slice for a day
	// inside the range is meaningful and is honored: it means the platform
	// reported no orders that day, and internal/reconcile's existing
	// missing_delivery_source flag surfaces it, exactly as it does for a
	// gap in a CSV export.
	Records []ingest.DeliveryRecord
}

// RunIngestionPipelineWithDeliveryOverlay is RunIngestionPipeline with an
// optional range-scoped delivery overlay. A nil overlay makes it identical
// to RunIngestionPipeline, which is why that function is now one line.
//
// Semantics, stated plainly because "what happens to the days I did not
// sync" is the question this shape exists to answer: CSV-parsed delivery
// rows whose order date falls inside the overlay's range are DROPPED and
// replaced by the overlay's records; rows outside the range are kept
// verbatim. POS and supplier-cost parsing are untouched.
//
// Every affected day is then recomputed from all three sources, not just
// from delivery. That is not incidental — margin is gross sales minus
// commissions minus refunds minus input costs, so a day whose delivery
// revenue changed cannot have its margin updated without its POS revenue
// and its supplier invoices in hand. Recomputing the whole day from the
// full source set is the only way the resulting number stays true rather
// than becoming a partial figure that looks complete.
//
// Days outside the range are provably untouched: they are reconciled from
// exactly the inputs they were reconciled from before, and re-persisted to
// the same values.
func RunIngestionPipelineWithDeliveryOverlay(dataDir string, store *storage.Queries, overlay *DeliveryOverlay) error {
	if overlay == nil {
		return RunIngestionPipelineWithConnectorOverlay(dataDir, store, nil)
	}
	return RunIngestionPipelineWithConnectorOverlay(dataDir, store, &ConnectorOverlay{
		From:           overlay.From,
		To:             overlay.To,
		DeliveryActive: true,
		Delivery:       overlay.Records,
		// POSActive stays false: a delivery-only overlay must leave the
		// day's CSV-sourced POS rows exactly where they are. See
		// ConnectorOverlay.POSActive for why this is a boolean and not
		// just an empty slice.
	})
}

// ConnectorOverlay replaces the CSV-sourced rows for one inclusive
// calendar-date range with records obtained from
// internal/platformconnector's simulated upstreams — for delivery
// revenue, for POS revenue, or for both
// (specs/012-pos-connector-dedup).
//
// # Why the two Active booleans are not redundant with a nil slice
//
// "I synced the POS and it reported nothing" and "I did not sync the POS"
// must produce different days, and a nil slice cannot tell them apart.
// The first is an assertion about the business — the terminal was on and
// took no orders — and clearing the range's POS revenue is the correct
// response. The second is an assertion about the REQUEST, and clearing
// anything would be destroying data the owner never asked to touch: a
// delivery-only sync would silently wipe two thirds of every synced day's
// gross sales and drop margin, with only the pre-existing flags to hint at
// why (spec 012 US1.4).
//
// So the boolean, and so a structural guarantee rather than a caller's
// discipline.
type ConnectorOverlay struct {
	// From and To are inclusive calendar dates. Comparison is done on the
	// formatted YYYY-MM-DD key rather than on time.Time ordering — see
	// DeliveryOverlay.From.
	From time.Time
	To   time.Time

	DeliveryActive bool
	Delivery       []ingest.DeliveryRecord

	POSActive bool
	POS       []ingest.POSRecord

	// Decisions are the connector's cross-source deduplication outcomes.
	// They become discrepancy flags on the days they belong to, so a
	// removed POS ticket — and, just as importantly, an overlap the
	// matcher refused to resolve — is visible on the day it affected
	// rather than only in a sync response the owner closed an hour ago.
	Decisions []platformconnector.DedupDecision
}

// RunIngestionPipelineWithConnectorOverlay is RunIngestionPipeline with an
// optional range-scoped connector overlay covering delivery revenue, POS
// revenue, or both. A nil overlay makes it identical to
// RunIngestionPipeline.
//
// Semantics, stated plainly because "what happens to the days I did not
// sync" is the question this shape exists to answer: for each source the
// overlay marks active, CSV-parsed rows whose date falls inside the range
// are DROPPED and replaced by the overlay's records; rows outside the
// range, and rows of any source the overlay did NOT mark active, are kept
// verbatim. Supplier-cost parsing is never overlaid.
//
// Every affected day is then recomputed from all three sources — see
// RunIngestionPipelineWithDeliveryOverlay's own reasoning, which has not
// changed: margin is gross minus commissions minus refunds minus input
// costs, so a day whose delivery or POS revenue moved cannot have its
// margin updated without the rest of the day in hand.
func RunIngestionPipelineWithConnectorOverlay(dataDir string, store *storage.Queries, overlay *ConnectorOverlay) error {
	deliveryPath, posPath, costPath, err := findSourceFiles(dataDir)
	if err != nil {
		return err
	}

	delivery, err := parseIfPresent(deliveryPath, ingest.ParseDeliveryExport)
	if err != nil {
		return err
	}
	pos, err := parseIfPresent(posPath, ingest.ParsePOSExport)
	if err != nil {
		return err
	}
	costs, err := parseIfPresent(costPath, ingest.ParseCostSheet)
	if err != nil {
		return err
	}

	if overlay != nil {
		from, to := overlay.From.Format(dateKeyLayout), overlay.To.Format(dateKeyLayout)
		if overlay.DeliveryActive {
			delivery = replaceInRange(delivery, from, to, overlay.Delivery, func(r ingest.DeliveryRecord) string {
				return r.OrderDate.Format(dateKeyLayout)
			})
			fmt.Printf("pipeline: delivery overlay active for %s..%s — %d record(s) replace the CSV export's rows for those dates\n", from, to, len(overlay.Delivery))
		}
		if overlay.POSActive {
			pos = replaceInRange(pos, from, to, overlay.POS, func(r ingest.POSRecord) string {
				return r.OrderDate.Format(dateKeyLayout)
			})
			fmt.Printf("pipeline: POS overlay active for %s..%s — %d ticket(s) replace the CSV export's rows for those dates\n", from, to, len(overlay.POS))
		}
	}

	if deliveryPath == "" && (overlay == nil || !overlay.DeliveryActive) {
		fmt.Printf("pipeline: no delivery-platform export found in %s — every day will carry a %s flag\n", dataDir, reconcile.FlagMissingDeliverySource)
	}

	externalFlags := dedupFlagsByDate(overlay)
	if n := len(externalFlags); n > 0 {
		fmt.Printf("pipeline: cross-source deduplication produced %d flagged day(s)\n", n)
	}

	days := reconcile.ComputeDailyReconciliationsWithFlags(delivery, pos, costs, externalFlags)
	if len(days) == 0 {
		return fmt.Errorf("pipeline: no daily reconciliations produced from %s (no dated rows found in any source)", dataDir)
	}

	ctx := context.Background()

	// `days` is the complete, authoritative day-set this run just recomputed
	// from every source file in dataDir (an overlay only substitutes rows
	// within a range — it never shrinks the set of dates parsed). Pruning
	// to exactly this set before persisting closes the gap where a date
	// that dropped out of the source (a corrected typo, a row moved to the
	// right day) stayed behind as an orphaned row forever, silently
	// widening or shifting GetDataDateRange's MIN/MAX. See
	// storage.PruneDailyReconciliationsExcept's doc comment for the defect
	// this closes.
	keepDates := make([]time.Time, len(days))
	for i, day := range days {
		keepDates[i] = day.Date
	}
	if err := storage.PruneDailyReconciliationsExcept(ctx, store, keepDates); err != nil {
		return fmt.Errorf("pipeline: pruning stale reconciliation rows: %w", err)
	}

	var totalMarginCents int64
	for _, day := range days {
		if _, err := storage.SaveDailyReconciliation(ctx, store, day); err != nil {
			return fmt.Errorf("pipeline: persisting %s: %w", day.Date.Format("2006-01-02"), err)
		}
		totalMarginCents += day.MarginCents
		printDaySummary(day)
	}
	fmt.Printf("---\n%d day(s) reconciled and persisted. Period total margin: %s\n", len(days), money.FormatCents(totalMarginCents))

	return nil
}

// dateKeyLayout is the YYYY-MM-DD key internal/reconcile groups days by.
// Overlay range membership is decided on this key, never on time.Time
// ordering — see DeliveryOverlay.From's doc comment for why.
const dateKeyLayout = "2006-01-02"

// applyDeliveryOverlay is spec 010's delivery-only overlay, expressed on
// top of the generic replaceInRange. Kept as a named function because
// overlay_test.go's range-boundary cases are written against it, and
// rewriting a passing test to fit a refactor is how a refactor stops
// proving anything.
func applyDeliveryOverlay(csvRecords []ingest.DeliveryRecord, overlay *DeliveryOverlay) []ingest.DeliveryRecord {
	if overlay == nil {
		return csvRecords
	}
	return replaceInRange(csvRecords, overlay.From.Format(dateKeyLayout), overlay.To.Format(dateKeyLayout), overlay.Records,
		func(r ingest.DeliveryRecord) string { return r.OrderDate.Format(dateKeyLayout) })
}

// replaceInRange drops CSV-sourced rows whose date falls inside
// [from, to] and appends the overlay's records in their place. Generic
// over the record type because delivery rows and POS rows need exactly
// the same treatment and differ only in where their date lives — writing
// it twice would be two places for the boundary condition to drift apart.
//
// Dates in YYYY-MM-DD sort lexicographically the same as chronologically,
// which is what makes a plain string comparison correct here (the same
// property reconcile.ComputeDailyReconciliations already relies on to
// sort its date keys), and what keeps a CSV row parsed at UTC midnight
// comparable with a connector row parsed at merchant-zone midnight.
func replaceInRange[T any](csvRecords []T, from, to string, overlayRecords []T, dateKey func(T) string) []T {
	merged := make([]T, 0, len(csvRecords)+len(overlayRecords))
	for _, rec := range csvRecords {
		if key := dateKey(rec); key >= from && key <= to {
			continue
		}
		merged = append(merged, rec)
	}
	return append(merged, overlayRecords...)
}

// dedupFlagsByDate translates the connector's cross-source decisions into
// this product's discrepancy-flag vocabulary, keyed by the day each one
// belongs to.
//
// The translation lives here, not in internal/platformconnector, because
// this is the layer that already imports both packages. Putting it in the
// connector would make the integration layer depend on the reconciliation
// engine it feeds, which is backwards; putting it in internal/reconcile
// would make the engine know what a connector is.
//
// Every decision produces a flag, including the ones where the matcher
// deliberately did nothing. That is the point: an overlap it refused to
// resolve is a possible double-count, and the whole reason to refuse
// rather than guess is that the owner gets told (spec 012 FR-014).
func dedupFlagsByDate(overlay *ConnectorOverlay) map[string][]reconcile.DiscrepancyFlag {
	if overlay == nil || len(overlay.Decisions) == 0 {
		return nil
	}

	out := make(map[string][]reconcile.DiscrepancyFlag)
	for _, decision := range overlay.Decisions {
		var flagType string
		switch decision.Kind {
		case platformconnector.DedupMatchedByReference, platformconnector.DedupMatchedByChannelAmountTime:
			flagType = reconcile.FlagCrossSourceDuplicateRemoved
		case platformconnector.DedupAmountMismatch:
			flagType = reconcile.FlagCrossSourceAmountMismatch
		case platformconnector.DedupUnresolvedAmbiguous, platformconnector.DedupUnresolvedNoCounterpart:
			flagType = reconcile.FlagCrossSourceDuplicateUnresolved
		default:
			// A decision kind this layer does not recognize is a wiring
			// gap, and dropping it would hide exactly the thing the
			// decision exists to disclose. Carry it under the most
			// cautious flag in the family rather than losing it.
			flagType = reconcile.FlagCrossSourceDuplicateUnresolved
		}

		key := decision.Date.Format(dateKeyLayout)
		out[key] = append(out[key], reconcile.DiscrepancyFlag{
			Type:   flagType,
			Detail: decision.Detail,
		})
	}
	return out
}

// findSourceFiles scans dataDir for files recognizable as a delivery-
// platform export, a POS export, or a supplier cost sheet, by filename
// keyword. It errors only if none of the three are found at all — a
// directory with, say, no cost sheet still reconciles gross revenue and
// commissions from what's present.
func findSourceFiles(dataDir string) (deliveryPath, posPath, costPath string, err error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "", "", "", fmt.Errorf("pipeline: reading data directory %s: %w", dataDir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		path := filepath.Join(dataDir, e.Name())

		switch {
		case strings.Contains(name, "promo") || strings.Contains(name, "campaign") || strings.Contains(name, "ad_spend") || strings.Contains(name, "adspend"):
			continue // User Story 4 scope; this pipeline doesn't touch promotion data
		case strings.Contains(name, "delivery") || strings.Contains(name, "platform"):
			deliveryPath = path
		case strings.Contains(name, "pos"):
			posPath = path
		case strings.Contains(name, "cost") || strings.Contains(name, "supplier") || strings.Contains(name, "invoice"):
			costPath = path
		}
	}

	if deliveryPath == "" && posPath == "" && costPath == "" {
		return "", "", "", fmt.Errorf("pipeline: no recognizable delivery-platform, POS, or supplier-cost-sheet file found in %s", dataDir)
	}
	return deliveryPath, posPath, costPath, nil
}

func parseIfPresent[T any](path string, parse func(r io.Reader, sourceFile string) ([]T, error)) ([]T, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pipeline: opening %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Printf("pipeline: closing %s: %v\n", path, cerr)
		}
	}()

	records, err := parse(f, path)
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}
	return records, nil
}

func printDaySummary(day reconcile.DailyReconciliation) {
	fmt.Printf("%s  margin=%-10s commissions=%-9s refunds=%-8s input_costs=%-9s",
		day.Date.Format("2006-01-02"),
		money.FormatCents(day.MarginCents),
		money.FormatCents(day.CommissionsCents),
		money.FormatCents(day.RefundsCents),
		money.FormatCents(day.InputCostsCents),
	)
	if len(day.DiscrepancyFlags) > 0 {
		types := make([]string, 0, len(day.DiscrepancyFlags))
		for _, f := range day.DiscrepancyFlags {
			types = append(types, f.Type)
		}
		fmt.Printf(" flags=%s", strings.Join(types, ","))
	}
	fmt.Println()
}
