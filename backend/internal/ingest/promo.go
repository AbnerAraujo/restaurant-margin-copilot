package ingest

import (
	"fmt"
	"io"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// PromotionSpendRecord is one promotion/ad-spend line from a delivery
// platform's campaign export (iFood, Just Eat Takeaway, or any similarly
// -shaped platform). Attribution — which orders it drove, and how much
// incremental revenue that produced — is deliberately NOT parsed from this
// file: per spec.md's Assumptions and fixtures/README.md, incremental
// revenue is computed by internal/reconcile as a tag-join against
// DeliveryRecord.CampaignID, not read as a pre-baked column here. This
// package stays as "dumb" about attribution as ingest.go's doc comment says
// it is about everything else.
type PromotionSpendRecord struct {
	Ref           SourceRowRef
	Platform      string
	CampaignID    string
	CampaignName  string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	SpendCents    int64
	PlacementType string
	Notes         string
}

// ParsePromotionExport parses a promotion/ad-spend export into
// PromotionSpendRecords. Column matching tolerates realistic real-world
// header variance, the same real-file-compatibility posture as
// ParseDeliveryExport/ParsePOSExport/ParseCostSheet (research.md).
func ParsePromotionExport(r io.Reader, sourceFile string) ([]PromotionSpendRecord, error) {
	rows, err := readAllRows(r)
	if err != nil {
		return nil, fmt.Errorf("ingest: reading promotion export %s: %w", sourceFile, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("ingest: %s is empty", sourceFile)
	}

	h := newHeaderIndex(rows[0])
	colPlatform, err := h.require("platform", "platform", "delivery_platform", "source_platform")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colCampaignID, err := h.require("campaign_id", "campaign_id", "campaign", "campaign_code")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colCampaignName := h.find("campaign_name", "name", "campaign_title")
	colPeriodStart, err := h.require("period_start", "period_start", "start_date", "campaign_start")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colPeriodEnd, err := h.require("period_end", "period_end", "end_date", "campaign_end")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colSpend, err := h.require("spend_amount", "spend_amount", "spend", "amount", "cost")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colPlacement := h.find("placement_type", "placement", "ad_type", "campaign_type")
	colNotes := h.find("notes", "note", "comment", "comments")

	var out []PromotionSpendRecord
	for i, row := range rows[1:] {
		rowNum := i + 2 // header occupies row 1
		if isBlankRow(row) {
			continue
		}

		periodStart, err := parseDate(get(row, colPeriodStart))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: period_start: %w", sourceFile, rowNum, err)
		}
		periodEnd, err := parseDate(get(row, colPeriodEnd))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: period_end: %w", sourceFile, rowNum, err)
		}
		spendCents, err := money.ParseCents(get(row, colSpend))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: spend_amount: %w", sourceFile, rowNum, err)
		}

		rec := PromotionSpendRecord{
			Ref:         SourceRowRef{File: sourceFile, Row: rowNum},
			Platform:    get(row, colPlatform),
			CampaignID:  get(row, colCampaignID),
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			SpendCents:  spendCents,
		}
		if colCampaignName >= 0 {
			rec.CampaignName = get(row, colCampaignName)
		}
		if colPlacement >= 0 {
			rec.PlacementType = get(row, colPlacement)
		}
		if colNotes >= 0 {
			rec.Notes = get(row, colNotes)
		}

		out = append(out, rec)
	}
	return out, nil
}
