// This file backs compare_platform_economics (specs/003-platform-comparator,
// contracts/mcp-tools.md's sixth entry): the one typed tool a
// platform-comparison question resolves to, so the narration model never
// reconstructs a comparison itself from two separate single-platform tool
// calls (spec FR-006, the same "no arithmetic across tool calls" rule
// internal/explain's system prompt already states for get_margin_delta).
//
// Follows promo_tools.go's pattern: a plain "core" function with the
// (*Result, *ToolError, error) three-way return (types.go's doc comment),
// plus a typed MCP handler adapter over it (mcp.NewTypedToolHandler /
// mcp.NewToolResultStructuredOnly).
package mcptools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// knownComparatorPlatforms is the fixed set of delivery platforms
// compare_platform_economics compares. Source is the normalized key
// reconcile.DailyReconciliation.GrossSalesBySource/CommissionsBySource use
// ("ifood", "just_eat_takeaway"); DisplayName is the exact string
// reconcile.PromotionRoiRecord.Platform carries (the raw value from
// promotion_ad_spend_export.csv's platform column — see get_promotion_roi's
// own tool description for the same convention) and what a narration should
// show a human.
//
// Deliberately a fixed list, not a scan of which sources happen to appear in
// the requested period's persisted rows: FR-003 requires a platform with
// ZERO orders in the period to still be shown with a real zero, and a source
// with zero completed orders leaves no gross_sales_by_source/
// commissions_by_source key to discover by scanning. This is this spec's own
// documented Assumption ("'Platform' for this comparison means the delivery
// platforms already present in gross_sales_by_source" — i.e. present in this
// product's data model at all, not necessarily in every period compared) —
// a future multi-platform/single-platform-only onboarding (spec Edge Cases)
// is explicitly out of scope here.
var knownComparatorPlatforms = []struct {
	Source      string
	DisplayName string
}{
	{Source: "ifood", DisplayName: "iFood"},
	{Source: "just_eat_takeaway", DisplayName: "Just Eat Takeaway"},
}

// KnownPlatformDisplayNames returns the exact DisplayName strings above —
// the only values reconcile.PromotionRoiRecord.Platform is meant to ever
// carry. Exported so internal/httpapi's promotion-creation endpoint (POST
// /api/promotions) can validate an owner-typed platform field against this
// SAME list rather than inventing a second one: an unconstrained free-text
// platform field is exactly how "iFood" and "Ifood" ended up as two
// distinct, silently-diverging values in this database (see the
// accompanying QA report) — this is the one canonical source both the
// comparator tool and the write path must agree on.
func KnownPlatformDisplayNames() []string {
	names := make([]string, len(knownComparatorPlatforms))
	for i, p := range knownComparatorPlatforms {
		names[i] = p.DisplayName
	}
	return names
}

// IsKnownPlatformDisplayName reports whether name is exactly (case-
// sensitively) one of KnownPlatformDisplayNames' values — "Ifood" is
// deliberately NOT "iFood".
func IsKnownPlatformDisplayName(name string) bool {
	for _, p := range knownComparatorPlatforms {
		if p.DisplayName == name {
			return true
		}
	}
	return false
}

// PlatformEconomicsView is one platform's entry in
// compare_platform_economics' result (FR-001/FR-004). EffectiveRate and
// CombinedEffectiveRate are nil — never a fabricated "0.00%" and never a
// divide-by-zero — exactly when GrossSales is zero for the period: FR-003's
// "the *sales* are a real zero; a *rate* over zero sales is undefined, and
// the two must not be conflated" (plan.md).
type PlatformEconomicsView struct {
	Source                string                   `json:"source"`
	DisplayName           string                   `json:"display_name"`
	GrossSales            string                   `json:"gross_sales"`
	CommissionPaid        string                   `json:"commission_paid"`
	EffectiveRate         *string                  `json:"effective_rate"`
	PromoSpend            string                   `json:"promo_spend"`
	CombinedCost          string                   `json:"combined_cost"`
	CombinedEffectiveRate *string                  `json:"combined_effective_rate"`
	SourceRowRefs         []reconcile.SourceRowRef `json:"source_row_refs"`
}

// PlatformComparisonResult backs compare_platform_economics' success
// response (FR-002: both platforms side by side for the same period).
type PlatformComparisonResult struct {
	Period       Period                  `json:"period"`
	DaysIncluded int                     `json:"days_included"`
	Platforms    []PlatformEconomicsView `json:"platforms"`
}

// PlatformComparisonArgs is compare_platform_economics' input per
// contracts/mcp-tools.md.
type PlatformComparisonArgs struct {
	Period Period `json:"period"`
}

// ComparePlatformEconomics backs compare_platform_economics: per-platform
// gross sales, commission paid, effective commission rate, promo spend, and
// the combined commission+promo cost/rate, for the same period, from
// already-persisted DailyReconciliation and PromotionRoiRecord rows only
// (FR-001 — never estimated, never a hardcoded rate table).
//
// Like get_margin_delta's periodMargin helper, this refuses
// (insufficient_data) rather than compute over partial coverage if any
// calendar day in the requested period has no persisted DailyReconciliation
// at all. A day that DOES have a persisted row but zero delivery activity
// for a platform (fixtures/README.md irregularity #3, 2026-08-08) is not
// "missing" in that sense — its zero contribution to that platform's totals
// is the honest reconciled fact for that day (reconcile.go's own
// missing_delivery_source flag already names it), not a data gap this tool
// needs to special-case.
func ComparePlatformEconomics(ctx context.Context, q storage.Querier, period Period) (*PlatformComparisonResult, *ToolError, error) {
	start, end, err := period.parse()
	if err != nil {
		return nil, invalidInput(err.Error()), nil
	}

	days, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, start, end)
	if err != nil {
		return nil, nil, fmt.Errorf("mcptools: compare_platform_economics: loading period %s..%s: %w", period.Start, period.End, err)
	}

	present := make(map[string]reconcile.DailyReconciliation, len(days))
	for _, d := range days {
		present[d.Date.Format(dateLayout)] = d
	}
	var missing []string
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		key := cur.Format(dateLayout)
		if _, ok := present[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ToolError{Error: "insufficient_data", Missing: missing}, nil
	}

	views := make([]PlatformEconomicsView, 0, len(knownComparatorPlatforms))
	for _, p := range knownComparatorPlatforms {
		var grossCents, commissionCents int64
		var refs []reconcile.SourceRowRef
		for _, d := range days {
			grossCents += d.GrossSalesBySource[p.Source]
			commissionCents += d.CommissionsBySource[p.Source]
			refs = append(refs, d.SourceRowRefs...)
		}

		promos, err := storage.LoadPromotionRoiRecordsByPlatformAndPeriod(ctx, q, p.DisplayName, start, end)
		if err != nil {
			return nil, nil, fmt.Errorf("mcptools: compare_platform_economics: loading promo spend for %s: %w", p.DisplayName, err)
		}
		var promoSpendCents int64
		for _, rec := range promos {
			promoSpendCents += rec.SpendCents
			refs = append(refs, rec.SourceRowRefs...)
		}

		combinedCents := commissionCents + promoSpendCents

		views = append(views, PlatformEconomicsView{
			Source:                p.Source,
			DisplayName:           p.DisplayName,
			GrossSales:            money.FormatCents(grossCents),
			CommissionPaid:        money.FormatCents(commissionCents),
			EffectiveRate:         effectiveRatePercent(commissionCents, grossCents),
			PromoSpend:            money.FormatCents(promoSpendCents),
			CombinedCost:          money.FormatCents(combinedCents),
			CombinedEffectiveRate: effectiveRatePercent(combinedCents, grossCents),
			// collapseSourceRowRefsByFile (period_tools.go): the same
			// per-row-per-day accumulation that produced a real
			// 1,000,000+-token explain-step prompt on get_period_totals
			// applies here too — a wide period times every platform's
			// own daily refs plus its promo-record refs.
			SourceRowRefs: collapseSourceRowRefsByFile(refs),
		})
	}

	return &PlatformComparisonResult{
		Period:       period,
		DaysIncluded: len(days),
		Platforms:    views,
	}, nil, nil
}

// effectiveRatePercent renders numeratorCents/denominatorCents as a
// "23.00%"-style percentage string, or nil when denominatorCents is zero
// (FR-003's zero-safety rule) — never a divide-by-zero, never a fabricated
// "0.00%" standing in for "undefined". Computed in fixed-point basis points
// (internal/money.DivRoundHalfUp), never float64, for the same exactness
// reasons every other ratio/money figure in this codebase avoids float64.
func effectiveRatePercent(numeratorCents, denominatorCents int64) *string {
	if denominatorCents == 0 {
		return nil
	}
	bps := money.DivRoundHalfUp(numeratorCents*10000, denominatorCents)
	s := formatBpsAsPercent(bps)
	return &s
}

// formatBpsAsPercent renders basis points (2300 -> "23.00%") the same
// sign-then-magnitude way internal/money.FormatCents renders cents.
func formatBpsAsPercent(bps int64) string {
	neg := bps < 0
	if neg {
		bps = -bps
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d%%", sign, bps/100, bps%100)
}

// ComparePlatformEconomicsTool is the mcp-go Tool definition for
// compare_platform_economics per contracts/mcp-tools.md.
func ComparePlatformEconomicsTool() mcp.Tool {
	return mcp.NewTool("compare_platform_economics",
		mcp.WithDescription("Compare iFood's and Just Eat Takeaway's economics side by side for one period: gross sales, commission paid, effective commission rate, promotional spend, and the combined commission+promo cost/rate. Call this tool directly for ANY question comparing the two delivery platforms' costs or rates (e.g. \"which platform costs me more in commission?\") — never answer a comparison by calling get_daily_summary or get_promotion_roi once per platform and combining the results yourself. effective_rate/combined_effective_rate are null, never 0%, for a platform with zero gross sales in the period. Returns a typed insufficient_data error, never a comparison computed against partial data, if any calendar day in the period has no computed reconciliation."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithObject("period",
			mcp.Required(),
			mcp.Description("{start, end} as YYYY-MM-DD, inclusive."),
			mcp.Properties(periodPropertySchema),
		),
	)
}

// ComparePlatformEconomicsHandler adapts ComparePlatformEconomics into an
// MCP ToolHandlerFunc, following the same convention as
// GetPromotionRoiHandler/ListNegativeRoiPromotionsHandler.
func ComparePlatformEconomicsHandler(store storage.Querier) server.ToolHandlerFunc {
	return mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args PlatformComparisonArgs) (*mcp.CallToolResult, error) {
		result, toolErr, err := ComparePlatformEconomics(ctx, store, args.Period)
		if err != nil {
			return nil, err
		}
		if toolErr != nil {
			res := mcp.NewToolResultStructuredOnly(toolErr)
			res.IsError = true
			return res, nil
		}
		return mcp.NewToolResultStructuredOnly(result), nil
	})
}

// registerPlatformComparisonTool registers compare_platform_economics on s.
// Unexported, like registerReconciliationTools and registerPromoTools —
// called only from RegisterMCPServer (server.go), this package's sole
// public entry point for building a server.
func registerPlatformComparisonTool(s *server.MCPServer, store storage.Querier) {
	s.AddTool(ComparePlatformEconomicsTool(), ComparePlatformEconomicsHandler(store))
}
