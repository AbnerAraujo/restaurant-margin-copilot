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
	if deliveryPath == "" {
		fmt.Printf("pipeline: no delivery-platform export found in %s — every day will carry a %s flag\n", dataDir, reconcile.FlagMissingDeliverySource)
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
