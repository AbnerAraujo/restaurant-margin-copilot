// This file, not pipeline.go, carries User Story 4's promotion-ROI flow —
// pipeline.go's own doc comment on RunIngestionPipeline explicitly defers
// promotion/ad-spend handling to here (tasks.md T029-T030) rather than
// folding it into the US1 daily-margin pipeline.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// RunPromotionIngestionPipeline is User Story 4's ingest -> reconcile ->
// persist flow for promotion/ad-spend data. It is deliberately independent
// of RunIngestionPipeline (US1's daily-margin pipeline): attribution needs
// the delivery-platform export too (a promotion spend record carries no
// attribution of its own — internal/reconcile computes it as a tag-join
// over delivery orders, see fixtures/README.md), so this function re-parses
// the delivery export on its own rather than sharing state with
// RunIngestionPipeline, keeping the two pipelines independently runnable
// (spec: US4 has no dependency on US1's pipeline having already run in the
// same process).
func RunPromotionIngestionPipeline(fixtureDir string, store *storage.Queries) error {
	deliveryPath, _, _, err := findSourceFiles(fixtureDir)
	if err != nil {
		return err
	}
	if deliveryPath == "" {
		return fmt.Errorf("pipeline: no delivery-platform export found in %s — promotion ROI cannot be attributed without it", fixtureDir)
	}
	promoPath, err := findPromotionFile(fixtureDir)
	if err != nil {
		return err
	}

	delivery, err := parseIfPresent(deliveryPath, ingest.ParseDeliveryExport)
	if err != nil {
		return err
	}
	promos, err := parseIfPresent(promoPath, ingest.ParsePromotionExport)
	if err != nil {
		return err
	}
	if len(promos) == 0 {
		return fmt.Errorf("pipeline: no promotion/ad-spend rows found in %s", fixtureDir)
	}

	records := reconcile.ComputePromotionRoiRecords(promos, delivery)

	ctx := context.Background()
	for _, rec := range records {
		if _, err := storage.SavePromotionRoiRecord(ctx, store, rec); err != nil {
			return fmt.Errorf("pipeline: persisting promotion %s: %w", rec.CampaignID, err)
		}
		printPromoSummary(rec)
	}
	fmt.Printf("---\n%d promotion(s) reconciled and persisted.\n", len(records))

	return nil
}

// findPromotionFile scans fixtureDir for a file recognizable as a
// promotion/ad-spend export, by the same filename keywords findSourceFiles
// uses to deliberately SKIP such files from the US1 pipeline.
func findPromotionFile(fixtureDir string) (string, error) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		return "", fmt.Errorf("pipeline: reading fixture directory %s: %w", fixtureDir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.Contains(name, "promo") || strings.Contains(name, "campaign") || strings.Contains(name, "ad_spend") || strings.Contains(name, "adspend") {
			return filepath.Join(fixtureDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("pipeline: no recognizable promotion/ad-spend file found in %s", fixtureDir)
}

func printPromoSummary(rec reconcile.PromotionRoiRecord) {
	fmt.Printf("%-22s platform=%-20s spend=%-8s", rec.CampaignID, rec.Platform, money.FormatCents(rec.SpendCents))
	if rec.ROICents == nil {
		fmt.Printf(" roi=unattributable (0 tagged completed orders, FR-013)")
	} else {
		flag := ""
		if rec.FlaggedNegative {
			flag = " FLAGGED_NEGATIVE"
		}
		fmt.Printf(" incremental_revenue=%-8s net_roi=%-8s%s",
			money.FormatCents(*rec.AttributedIncrementalRevenueCents), money.FormatCents(*rec.ROICents), flag)
	}
	fmt.Println()
}
