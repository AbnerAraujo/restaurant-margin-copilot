// This file backs get_period_totals (contracts/mcp-tools.md's seventh
// entry): a single tool that totals and ranks an entire period's
// DailyReconciliation rows in one call. It closes a real, measured gap —
// two failing evaluation questions ("Supplier cost total for the two-week
// period?", "Which campaigns should be flagged as underperforming?"-shaped
// period-total phrasing) and a real observed failure where "which day had
// the most profit and why" burned through the per-interaction tool-call
// budget (limits.go's DefaultMaxToolCallsPerInteraction) calling
// get_daily_summary once per day, because no single existing tool could sum
// or rank a period as a whole. get_margin_delta sums margin across a
// period, but only as one side of a two-period delta and only margin
// itself — it has no per-source breakdown, no best/worst-day ranking, and
// no top-level total for a single period asked about alone.
//
// Follows platform_comparison_tools.go's/promo_tools.go's current
// convention (this package's most recently added tools): a plain "core"
// function with the (*Result, *ToolError, error) three-way return
// (types.go's doc comment), plus mcp.NewTypedToolHandler /
// mcp.NewToolResultStructuredOnly as the MCP adapter — rather than
// reconciliation_tools.go's older hand-rolled BindArguments/jsonResult
// pair, which predates that typed-handler helper's introduction into this
// package. Kept as its own file rather than folded into
// reconciliation_tools.go (already ~400 lines covering three tools) or
// promo_tools.go (a different domain entirely) — one file per tool
// "family," matching how platform_comparison_tools.go already got its own
// file for a single tool.
package mcptools

import (
	"context"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// DayMarginRef names one day and its margin — get_period_totals'
// best_day/worst_day shape. A plain {date, margin} pair, not a full
// DailySummaryResult: the model already has get_daily_summary for a full
// day breakdown if it needs one, and this tool's job is the period-level
// total, not a second copy of the daily-detail contract.
type DayMarginRef struct {
	Date   string `json:"date"`
	Margin string `json:"margin"`
}

// PeriodTotalsResult backs get_period_totals' success response: every
// money figure summed across the period, plus which single day was best
// and worst by margin, plus one combined source_row_refs list spanning
// every day included (the same "concatenate every included day's own
// refs" convention periodMargin already uses for get_margin_delta).
type PeriodTotalsResult struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	DaysIncluded int    `json:"days_included"`
	// GrossSalesBySource is the per-source sum across every day in the
	// period, keyed the same way DailyReconciliation.GrossSalesBySource is
	// ("ifood", "just_eat_takeaway", "pos", ...) — see
	// reconciliation_tools.go's DailySummaryResult for the single-day
	// version of this same field.
	GrossSalesBySource map[string]string `json:"gross_sales_by_source"`
	// TotalDeliveryGrossSales sums every GrossSalesBySource entry EXCEPT
	// "pos" across the period, mirroring DailySummaryResult's
	// TotalDeliveryGrossSales field and existing for the identical reason:
	// computed once here in Go, never left for the narration model to add
	// up itself across a map (Constitution Principle I).
	TotalDeliveryGrossSales string `json:"total_delivery_gross_sales"`
	Commissions             string `json:"commissions"`
	Refunds                 string `json:"refunds"`
	InputCosts              string `json:"input_costs"`
	MarginTotal             string `json:"margin_total"`
	// AvgDailyMargin is MarginTotal / DaysIncluded, computed with
	// internal/money.DivRoundHalfUp — never a float64 divide — the same
	// rounding convention every other ratio in this codebase
	// (effectiveRatePercent, platform_comparison_tools.go) already uses.
	AvgDailyMargin string `json:"avg_daily_margin"`
	// BestDay/WorstDay are the single highest/lowest per-day MarginCents in
	// the period. On an exact tie, the chronologically earliest date wins
	// both slots (this tool's own documented tie-break — contracts/
	// mcp-tools.md does not prescribe one, matching get_margin_delta's
	// DeltaMarginTotal sign convention also being this package's own
	// documented choice).
	BestDay       DayMarginRef             `json:"best_day"`
	WorstDay      DayMarginRef             `json:"worst_day"`
	SourceRowRefs []reconcile.SourceRowRef `json:"source_row_refs"`
}

// GetPeriodTotalsArgs is get_period_totals' input per
// contracts/mcp-tools.md: one required {start, end} period.
type GetPeriodTotalsArgs struct {
	Period Period `json:"period"`
}

// GetPeriodTotals backs get_period_totals' core logic: sum every money
// field and rank every day's margin across [period.Start, period.End].
// Refuses (insufficient_data) rather than partially total a period, the
// exact same policy periodMargin already enforces for get_margin_delta and
// ComparePlatformEconomics enforces for compare_platform_economics — if
// any calendar day in the range has no persisted DailyReconciliation row,
// nothing is summed or ranked, and every missing date is named.
func GetPeriodTotals(ctx context.Context, q storage.Querier, period Period) (*PeriodTotalsResult, *ToolError, error) {
	start, end, err := period.parse()
	if err != nil {
		return nil, invalidInput(err.Error()), nil
	}

	days, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, start, end)
	if err != nil {
		return nil, nil, fmt.Errorf("mcptools: get_period_totals: loading period %s..%s: %w", period.Start, period.End, err)
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

	// Sorted explicitly (not just relying on storage's own "ordered by
	// date" contract) so best/worst-day tie-breaking is deterministic
	// regardless of which storage.Querier implementation is behind q —
	// this is exactly the kind of assumption a fake Querier in a test can
	// silently get away with violating while a live one doesn't.
	sort.Slice(days, func(i, j int) bool { return days[i].Date.Before(days[j].Date) })

	grossBySourceCents := make(map[string]int64)
	var commissionsCents, refundsCents, inputCostsCents, marginCents int64
	var refs []reconcile.SourceRowRef
	var bestDate, worstDate string
	var bestMarginCents, worstMarginCents int64

	for i, d := range days {
		for source, cents := range d.GrossSalesBySource {
			grossBySourceCents[source] += cents
		}
		commissionsCents += d.CommissionsCents
		refundsCents += d.RefundsCents
		inputCostsCents += d.InputCostsCents
		marginCents += d.MarginCents
		refs = append(refs, d.SourceRowRefs...)

		// Strict > / < only, so the first (earliest, thanks to the sort
		// above) day encountered wins any exact tie.
		if i == 0 || d.MarginCents > bestMarginCents {
			bestMarginCents = d.MarginCents
			bestDate = d.Date.Format(dateLayout)
		}
		if i == 0 || d.MarginCents < worstMarginCents {
			worstMarginCents = d.MarginCents
			worstDate = d.Date.Format(dateLayout)
		}
	}

	grossBySource := make(map[string]string, len(grossBySourceCents))
	var deliveryTotalCents int64
	for source, cents := range grossBySourceCents {
		grossBySource[source] = money.FormatCents(cents)
		if source != "pos" {
			deliveryTotalCents += cents
		}
	}

	// len(days) is at least 1 here: period.parse() already refused end <
	// start, and the insufficient_data check above already refused any
	// calendar day in [start, end] — including a single-day period —
	// having no persisted row. DivRoundHalfUp is still used rather than a
	// bare integer divide so the rounding convention matches every other
	// ratio in this codebase (never truncate-toward-zero, never float64).
	avgDailyMarginCents := money.DivRoundHalfUp(marginCents, int64(len(days)))

	return &PeriodTotalsResult{
		Start:                   period.Start,
		End:                     period.End,
		DaysIncluded:            len(days),
		GrossSalesBySource:      grossBySource,
		TotalDeliveryGrossSales: money.FormatCents(deliveryTotalCents),
		Commissions:             money.FormatCents(commissionsCents),
		Refunds:                 money.FormatCents(refundsCents),
		InputCosts:              money.FormatCents(inputCostsCents),
		MarginTotal:             money.FormatCents(marginCents),
		AvgDailyMargin:          money.FormatCents(avgDailyMarginCents),
		BestDay:                 DayMarginRef{Date: bestDate, Margin: money.FormatCents(bestMarginCents)},
		WorstDay:                DayMarginRef{Date: worstDate, Margin: money.FormatCents(worstMarginCents)},
		SourceRowRefs:           refs,
	}, nil, nil
}

// GetPeriodTotalsTool is the mcp-go Tool definition for get_period_totals
// per contracts/mcp-tools.md.
func GetPeriodTotalsTool() mcp.Tool {
	return mcp.NewTool("get_period_totals",
		mcp.WithDescription("Total and rank an ENTIRE period's reconciled figures in one call: gross sales by source, total delivery gross sales, commissions, refunds, input costs, margin total, average daily margin, and which single day was the best/worst by margin — each carrying source-row provenance. Call this tool directly for ANY question about a period's totals or about which day was highest/lowest (e.g. \"total supplier cost for the two-week period\", \"which day had the most profit and why\") — never answer by calling get_daily_summary once per day and adding the results yourself. Returns a typed insufficient_data error, never a total computed against partial data, if any calendar day in the period has no computed reconciliation."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithObject("period",
			mcp.Required(),
			mcp.Description("{start, end} as YYYY-MM-DD, inclusive."),
			mcp.Properties(periodPropertySchema),
		),
	)
}

// GetPeriodTotalsHandler adapts GetPeriodTotals into an MCP
// ToolHandlerFunc, following the same convention as
// ComparePlatformEconomicsHandler/GetPromotionRoiHandler.
func GetPeriodTotalsHandler(store storage.Querier) server.ToolHandlerFunc {
	return mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args GetPeriodTotalsArgs) (*mcp.CallToolResult, error) {
		result, toolErr, err := GetPeriodTotals(ctx, store, args.Period)
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

// registerPeriodTools registers get_period_totals on s. Unexported, like
// registerReconciliationTools/registerPromoTools/
// registerPlatformComparisonTool — this package's only public surface for
// building a server is RegisterMCPServer (server.go), which is the sole
// caller of this function.
func registerPeriodTools(s *server.MCPServer, store storage.Querier) {
	s.AddTool(GetPeriodTotalsTool(), GetPeriodTotalsHandler(store))
}
