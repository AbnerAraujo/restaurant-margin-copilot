package mcptools_test

// fakeQuerier is a hand-written, in-memory stand-in for storage.Querier
// (backend/internal/storage/querier.go) — the same technique
// internal/httpapi already uses to fake its own dependencies for tests
// (e.g. ask_paraphrase_test.go's fakeParaphraseMatcher), applied to this
// package's own seam. Finding 4: every test in reconciliation_tools_test.go,
// platform_comparison_tools_test.go, and promo_tools_test.go gates on
// DATABASE_URL and is skipped by default, leaving this package's
// refuse-rather-than-guess logic (periodMargin's missing-day check, most
// importantly) completely dark in a normal `go test ./...` run. These new
// tests exercise the exact same core functions those live-Postgres tests
// do, against this fake instead, so the logic runs in every default test
// run with zero Postgres dependency. The live-gated tests are kept as-is —
// this fake is additive coverage of the same logic, not a replacement.
//
// It embeds storage.Querier itself (left nil) so it satisfies the full
// ~28-method interface by construction; only the handful of methods
// internal/mcptools' tool functions actually reach — via internal/storage's
// own hand-written adapters (reconciliation.go, reconciliation_period.go,
// promotion.go) — are overridden below. Calling any other method would be a
// real bug (a tool reaching a query it has no business touching), so it is
// left to panic on the embedded nil interface rather than silently
// returning a zero value that could mask that bug.
//
// Seeding reuses the SAME production write path the live-Postgres tests
// use — storage.SaveDailyReconciliation / storage.SavePromotionRoiRecord,
// both of which take the storage.Querier interface, not the concrete
// *storage.Queries — so a test's setup can never drift from how a real row
// is actually shaped; this fake only stands in for the SQL underneath.

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

const fakeDateLayout = "2006-01-02"

type fakeQuerier struct {
	storage.Querier // left nil: any unimplemented method panics if ever called

	mu         sync.Mutex
	daily      map[string]storage.DailyReconciliation // key: date, fakeDateLayout
	promotions []storage.PromotionRoiRecord
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{daily: make(map[string]storage.DailyReconciliation)}
}

// --- daily_reconciliation: backs GetDailySummary/GetMarginDelta/
// ListDiscrepancies (reconciliation_tools.go) and ComparePlatformEconomics
// (platform_comparison_tools.go), via storage.LoadDailyReconciliation(sInPeriod). ---

func (f *fakeQuerier) GetDailyReconciliationByDate(_ context.Context, date pgtype.Date) (storage.DailyReconciliation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.daily[date.Time.Format(fakeDateLayout)]
	if !ok {
		return storage.DailyReconciliation{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQuerier) ListDailyReconciliationsInPeriod(_ context.Context, arg storage.ListDailyReconciliationsInPeriodParams) ([]storage.DailyReconciliation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start, end := arg.Date.Time, arg.Date_2.Time
	var out []storage.DailyReconciliation
	for _, row := range f.daily {
		d := row.Date.Time
		if !d.Before(start) && !d.After(end) {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Time.Before(out[j].Date.Time) })
	return out, nil
}

func (f *fakeQuerier) UpsertDailyReconciliation(_ context.Context, arg storage.UpsertDailyReconciliationParams) (storage.DailyReconciliation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := storage.DailyReconciliation{
		Date:                arg.Date,
		GrossSalesBySource:  arg.GrossSalesBySource,
		Commissions:         arg.Commissions,
		CommissionsBySource: arg.CommissionsBySource,
		Refunds:             arg.Refunds,
		InputCosts:          arg.InputCosts,
		Margin:              arg.Margin,
		DiscrepancyFlags:    arg.DiscrepancyFlags,
		SourceRowRefs:       arg.SourceRowRefs,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	f.daily[arg.Date.Time.Format(fakeDateLayout)] = row
	return row, nil
}

// --- promotion_roi_record: backs GetPromotionRoi/ListNegativeRoiPromotions
// (promo_tools.go) and ComparePlatformEconomics' promo-spend lookup
// (platform_comparison_tools.go). ---

func (f *fakeQuerier) GetPromotionRoiByCampaign(_ context.Context, campaignID string) ([]storage.PromotionRoiRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.PromotionRoiRecord
	for _, p := range f.promotions {
		if p.CampaignID == campaignID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeQuerier) GetPromotionRoiByPlatformAndPeriod(_ context.Context, arg storage.GetPromotionRoiByPlatformAndPeriodParams) ([]storage.PromotionRoiRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	qStart, qEnd, err := storage.PeriodFromRange(arg.Column2)
	if err != nil {
		return nil, err
	}
	var out []storage.PromotionRoiRecord
	for _, p := range f.promotions {
		if p.Platform != arg.Platform {
			continue
		}
		pStart, pEnd, err := storage.PeriodFromRange(p.Period)
		if err != nil {
			return nil, err
		}
		if periodsOverlap(pStart, pEnd, qStart, qEnd) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeQuerier) ListNegativeRoiPromotions(_ context.Context, rng pgtype.Range[pgtype.Date]) ([]storage.PromotionRoiRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	qStart, qEnd, err := storage.PeriodFromRange(rng)
	if err != nil {
		return nil, err
	}
	var out []storage.PromotionRoiRecord
	for _, p := range f.promotions {
		if !p.FlaggedNegative {
			continue
		}
		pStart, pEnd, err := storage.PeriodFromRange(p.Period)
		if err != nil {
			return nil, err
		}
		if periodsOverlap(pStart, pEnd, qStart, qEnd) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeQuerier) ListDistinctCampaignIDs(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := make(map[string]bool)
	var out []string
	for _, p := range f.promotions {
		if !seen[p.CampaignID] {
			seen[p.CampaignID] = true
			out = append(out, p.CampaignID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeQuerier) UpsertPromotionRoiRecord(_ context.Context, arg storage.UpsertPromotionRoiRecordParams) (storage.PromotionRoiRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := storage.PromotionRoiRecord{
		Platform:                     arg.Platform,
		CampaignID:                   arg.CampaignID,
		Period:                       arg.Period,
		Spend:                        arg.Spend,
		AttributedIncrementalOrders:  arg.AttributedIncrementalOrders,
		AttributedIncrementalRevenue: arg.AttributedIncrementalRevenue,
		Roi:                          arg.Roi,
		FlaggedNegative:              arg.FlaggedNegative,
		SourceRowRefs:                arg.SourceRowRefs,
		CreatedAt:                    now,
		UpdatedAt:                    now,
		Origin:                       "ingested",
	}
	for i, existing := range f.promotions {
		if existing.Platform == row.Platform && existing.CampaignID == row.CampaignID && rangesEqual(existing.Period, row.Period) {
			f.promotions[i] = row
			return row, nil
		}
	}
	f.promotions = append(f.promotions, row)
	return row, nil
}

func periodsOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return !aStart.After(bEnd) && !bStart.After(aEnd)
}

func rangesEqual(a, b pgtype.Range[pgtype.Date]) bool {
	return a.Lower.Time.Equal(b.Lower.Time) && a.Upper.Time.Equal(b.Upper.Time)
}
