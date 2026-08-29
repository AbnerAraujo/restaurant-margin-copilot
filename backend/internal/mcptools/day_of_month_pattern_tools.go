// This file backs get_expense_pattern_by_day_of_month (contracts/
// mcp-tools.md's eighth entry): groups every reconciled day in a period by
// its DAY-OF-MONTH (1st through 31st) and averages total expense
// (commissions + refunds + input costs — the full deduction side of
// margin, mirroring how MarginCents itself is defined) across however many
// months in the period actually contain that day-of-month, then ranks
// which position in the month runs the most/least expensive on average.
//
// Added deliberately, not by default: a real live question ("is the 15th
// typically my worst day?" / "which day of the month costs the most on
// average?") asked for exactly this grouping, and no existing tool
// computes it — get_period_totals ranks by SPECIFIC calendar date within
// one period, not by RECURRING position across many months. The gate
// correctly refused to approximate this by calling get_daily_summary
// repeatedly and averaging client-side, per Constitution Principle I (all
// arithmetic in Go, never assembled by the model across calls) — this file
// is that missing Go computation, not a workaround for the refusal.
//
// Follows period_tools.go's/platform_comparison_tools.go's current
// convention: a plain "core" function with the (*Result, *ToolError,
// error) three-way return, plus mcp.NewTypedToolHandler /
// mcp.NewToolResultStructuredOnly as the MCP adapter.
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

// DayOfMonthExpense is one day-of-month's (1-31) average total expense
// across however many months in the requested period actually contain
// that day-of-month. Occurrences is always reported alongside the average
// — the 29th/30th/31st don't exist in every month, and this project's own
// "never let an average silently misrepresent its sample size" discipline
// (see get_period_totals' own doc comment on refusing partial data)
// applies just as much to a small sample size as to missing data outright.
type DayOfMonthExpense struct {
	DayOfMonth  int    `json:"day_of_month"`
	AvgExpense  string `json:"avg_expense"`
	Occurrences int    `json:"occurrences"`
}

// ExpensePatternByDayOfMonthResult backs
// get_expense_pattern_by_day_of_month's success response: one
// DayOfMonthExpense per day-of-month that actually occurs at least once in
// the period (sorted ascending by day_of_month — never all 31, since a
// short period may not touch every position), plus which single
// day-of-month ran highest/lowest on average.
type ExpensePatternByDayOfMonthResult struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	DaysIncluded int    `json:"days_included"`
	// Pattern is ordered by day_of_month ascending (1, 2, 3, ...) — a
	// stable, predictable order for the model to narrate directly, not an
	// order derived from ranking (that's what HighestExpenseDay/
	// LowestExpenseDay are for).
	Pattern []DayOfMonthExpense `json:"pattern"`
	// HighestExpenseDay/LowestExpenseDay: on an exact tie in AvgExpense,
	// the SMALLER day-of-month number wins both slots — a deterministic,
	// documented tie-break this tool owns itself, the same way
	// get_period_totals documents its own "earliest date wins" tie-break
	// for BestDay/WorstDay.
	HighestExpenseDay DayOfMonthExpense        `json:"highest_expense_day"`
	LowestExpenseDay  DayOfMonthExpense        `json:"lowest_expense_day"`
	SourceRowRefs     []reconcile.SourceRowRef `json:"source_row_refs"`
}

// GetExpensePatternByDayOfMonthArgs is get_expense_pattern_by_day_of_month's
// input per contracts/mcp-tools.md: one required {start, end} period.
type GetExpensePatternByDayOfMonthArgs struct {
	Period Period `json:"period"`
}

// GetExpensePatternByDayOfMonth backs get_expense_pattern_by_day_of_month's
// core logic. Refuses (insufficient_data) rather than partially compute a
// pattern, the exact same policy every other period-taking tool in this
// package already enforces — if any calendar day in [period.Start,
// period.End] has no persisted DailyReconciliation row, nothing is
// grouped, averaged, or ranked, and every missing date is named.
func GetExpensePatternByDayOfMonth(ctx context.Context, q storage.Querier, period Period) (*ExpensePatternByDayOfMonthResult, *ToolError, error) {
	start, end, err := period.parse()
	if err != nil {
		return nil, invalidInput(err.Error()), nil
	}

	days, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, start, end)
	if err != nil {
		return nil, nil, fmt.Errorf("mcptools: get_expense_pattern_by_day_of_month: loading period %s..%s: %w", period.Start, period.End, err)
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

	// dayOfMonth -> running (sum of expense cents, count of occurrences).
	type accumulator struct {
		sumCents int64
		count    int
	}
	byDayOfMonth := make(map[int]*accumulator)
	var refs []reconcile.SourceRowRef

	for _, d := range days {
		expenseCents := d.CommissionsCents + d.RefundsCents + d.InputCostsCents
		dom := d.Date.Day()
		acc, ok := byDayOfMonth[dom]
		if !ok {
			acc = &accumulator{}
			byDayOfMonth[dom] = acc
		}
		acc.sumCents += expenseCents
		acc.count++
		refs = append(refs, d.SourceRowRefs...)
	}

	domKeys := make([]int, 0, len(byDayOfMonth))
	for dom := range byDayOfMonth {
		domKeys = append(domKeys, dom)
	}
	sort.Ints(domKeys)

	pattern := make([]DayOfMonthExpense, 0, len(domKeys))
	var highest, lowest DayOfMonthExpense
	var highestCents, lowestCents int64
	for i, dom := range domKeys {
		acc := byDayOfMonth[dom]
		avgCents := money.DivRoundHalfUp(acc.sumCents, int64(acc.count))
		entry := DayOfMonthExpense{
			DayOfMonth:  dom,
			AvgExpense:  money.FormatCents(avgCents),
			Occurrences: acc.count,
		}
		pattern = append(pattern, entry)

		// Strict > / < only against the running best/worst so the FIRST
		// day-of-month encountered (domKeys is sorted ascending, so this
		// means the smaller day-of-month number) wins any exact tie —
		// the documented tie-break.
		if i == 0 || avgCents > highestCents {
			highest = entry
			highestCents = avgCents
		}
		if i == 0 || avgCents < lowestCents {
			lowest = entry
			lowestCents = avgCents
		}
	}

	return &ExpensePatternByDayOfMonthResult{
		Start:             period.Start,
		End:               period.End,
		DaysIncluded:      len(days),
		Pattern:           pattern,
		HighestExpenseDay: highest,
		LowestExpenseDay:  lowest,
		SourceRowRefs:     collapseSourceRowRefsByFile(refs),
	}, nil, nil
}

// GetExpensePatternByDayOfMonthTool is the mcp-go Tool definition for
// get_expense_pattern_by_day_of_month per contracts/mcp-tools.md.
func GetExpensePatternByDayOfMonthTool() mcp.Tool {
	return mcp.NewTool("get_expense_pattern_by_day_of_month",
		mcp.WithDescription("Groups every reconciled day in a period by its DAY-OF-MONTH (1st through 31st) and averages total expense (commissions + refunds + input costs) for each day-of-month across however many months in the period actually contain it, ranking which position in the month runs highest/lowest on average. This is a DIFFERENT grouping from get_period_totals, which ranks by one SPECIFIC calendar date within a single period — call THIS tool for any question about a RECURRING pattern by position in the month (e.g. \"is the 1st typically my worst day\", \"which day of the month costs the most on average\", \"do I spend more around the 15th\"). Each day-of-month's average discloses its occurrences count, since the 29th/30th/31st don't exist in every month. Never reconstruct this by calling get_daily_summary once per day and averaging the results yourself. Returns a typed insufficient_data error, never a pattern computed against partial data, if any calendar day in the period has no computed reconciliation."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithObject("period",
			mcp.Required(),
			mcp.Description("{start, end} as YYYY-MM-DD, inclusive. Should span multiple months for this tool to be meaningful — a single-month period still works, but every day-of-month will show exactly 1 occurrence."),
			mcp.Properties(periodPropertySchema),
		),
	)
}

// GetExpensePatternByDayOfMonthHandler adapts
// GetExpensePatternByDayOfMonth into an MCP ToolHandlerFunc, following the
// same convention as GetPeriodTotalsHandler.
func GetExpensePatternByDayOfMonthHandler(store storage.Querier) server.ToolHandlerFunc {
	return mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args GetExpensePatternByDayOfMonthArgs) (*mcp.CallToolResult, error) {
		result, toolErr, err := GetExpensePatternByDayOfMonth(ctx, store, args.Period)
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

// registerDayOfMonthPatternTools registers get_expense_pattern_by_day_of_month
// on s. Unexported, like every other per-file registrar in this package —
// RegisterMCPServer (server.go) is the sole caller.
func registerDayOfMonthPatternTools(s *server.MCPServer, store storage.Querier) {
	s.AddTool(GetExpensePatternByDayOfMonthTool(), GetExpensePatternByDayOfMonthHandler(store))
}
