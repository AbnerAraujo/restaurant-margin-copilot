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
	return RunIngestionPipelineWithDeliveryOverlay(dataDir, store, nil)
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
	deliveryPath, posPath, costPath, err := findSourceFiles(dataDir)
	if err != nil {
		return err
	}

	delivery, err := parseIfPresent(deliveryPath, ingest.ParseDeliveryExport)
	if err != nil {
		return err
	}
	delivery = applyDeliveryOverlay(delivery, overlay)
	pos, err := parseIfPresent(posPath, ingest.ParsePOSExport)
	if err != nil {
		return err
	}
	costs, err := parseIfPresent(costPath, ingest.ParseCostSheet)
	if err != nil {
		return err
	}
	if deliveryPath == "" && overlay == nil {
		fmt.Printf("pipeline: no delivery-platform export found in %s — every day will carry a %s flag\n", dataDir, reconcile.FlagMissingDeliverySource)
	}
	if overlay != nil {
		fmt.Printf("pipeline: delivery overlay active for %s..%s — %d record(s) replace the CSV export's rows for those dates\n",
			overlay.From.Format(dateKeyLayout), overlay.To.Format(dateKeyLayout), len(overlay.Records))
	}

	days := reconcile.ComputeDailyReconciliations(delivery, pos, costs)
	if len(days) == 0 {
		return fmt.Errorf("pipeline: no daily reconciliations produced from %s (no dated rows found in any source)", dataDir)
	}

	ctx := context.Background()
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

// applyDeliveryOverlay drops CSV-sourced delivery rows inside the
// overlay's date range and appends the overlay's records in their place.
// A nil overlay returns the input untouched, which is what keeps
// RunIngestionPipeline's behavior bit-for-bit unchanged for the -ingest
// CLI flag and the cost-sheet upload.
func applyDeliveryOverlay(csvRecords []ingest.DeliveryRecord, overlay *DeliveryOverlay) []ingest.DeliveryRecord {
	if overlay == nil {
		return csvRecords
	}

	from := overlay.From.Format(dateKeyLayout)
	to := overlay.To.Format(dateKeyLayout)

	merged := make([]ingest.DeliveryRecord, 0, len(csvRecords)+len(overlay.Records))
	for _, rec := range csvRecords {
		key := rec.OrderDate.Format(dateKeyLayout)
		// Dates in YYYY-MM-DD sort lexicographically the same as
		// chronologically, which is what makes a plain string comparison
		// correct here (the same property reconcile.ComputeDailyReconciliations
		// already relies on to sort its date keys).
		if key >= from && key <= to {
			continue
		}
		merged = append(merged, rec)
	}
	return append(merged, overlay.Records...)
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
