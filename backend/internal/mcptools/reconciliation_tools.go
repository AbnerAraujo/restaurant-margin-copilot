package mcptools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// --- Result shapes -----------------------------------------------------
//
// Every money field below is rendered as a "-12.34"-style decimal string
// via internal/money.FormatCents, matching the convention every other
// money value in this codebase's JSON surfaces already uses (see
// internal/storage/reconciliation.go's marshalCentsMap) — the model
// receives numbers already formatted the way a human reads them, never a
// raw integer-cents value it might misinterpret as dollars.

// DailySummaryResult backs get_daily_summary's success response — the
// DailyReconciliation row for one date, in full, including its provenance.
type DailySummaryResult struct {
	Date               string            `json:"date"`
	GrossSalesBySource map[string]string `json:"gross_sales_by_source"`
	// TotalDeliveryGrossSales is the deterministic sum of every
	// GrossSalesBySource entry EXCEPT "pos" — i.e. iFood + Just Eat
	// Takeaway + any other delivery platform, but not in-house dine-in/
	// takeaway sales. Computed here in Go, not left for the narration model
	// to add up itself, per the constitution's deterministic/probabilistic
	// split (Principle I). Without this field, a "what was delivery revenue"
	// question forced the model to either sum the per-source map itself (a
	// real violation of that split) or omit the combined figure entirely.
	// Naming it "TotalGrossSales" instead would be wrong: that would silently
	// fold in POS (non-delivery) revenue and inflate the delivery figure.
	TotalDeliveryGrossSales string                      `json:"total_delivery_gross_sales"`
	Commissions             string                      `json:"commissions"`
	Refunds                 string                      `json:"refunds"`
	InputCosts              string                      `json:"input_costs"`
	Margin                  string                      `json:"margin"`
	DiscrepancyFlags        []reconcile.DiscrepancyFlag `json:"discrepancy_flags"`
	SourceRowRefs           []reconcile.SourceRowRef    `json:"source_row_refs"`
}

// PeriodMarginResult is one side of a get_margin_delta response — a
// period's summed margin plus every source row that fed it, so each side
// of the delta carries its own provenance per contracts/mcp-tools.md.
type PeriodMarginResult struct {
	Start         string                   `json:"start"`
	End           string                   `json:"end"`
	DaysIncluded  int                      `json:"days_included"`
	MarginTotal   string                   `json:"margin_total"`
	SourceRowRefs []reconcile.SourceRowRef `json:"source_row_refs"`
}

// MarginDeltaResult backs get_margin_delta's success response.
type MarginDeltaResult struct {
	PeriodA PeriodMarginResult `json:"period_a"`
	PeriodB PeriodMarginResult `json:"period_b"`
	// DeltaMarginTotal is PeriodB's margin total minus PeriodA's — positive
	// means period_b improved on period_a. contracts/mcp-tools.md does not
	// prescribe a sign convention, so this is documented here as this
	// package's own deliberate choice, matching the natural reading of "how
	// did period_b do compared to period_a".
	DeltaMarginTotal string `json:"delta_margin_total"`
}

// DayDiscrepancies is one day's worth of list_discrepancies output. Days
// with zero discrepancy flags are omitted by ListDiscrepancies — the tool
// surfaces exceptions, not a full calendar.
type DayDiscrepancies struct {
	Date          string                      `json:"date"`
	Flags         []reconcile.DiscrepancyFlag `json:"flags"`
	SourceRowRefs []reconcile.SourceRowRef    `json:"source_row_refs"`
}

// DiscrepanciesResult backs list_discrepancies' success response.
// DaysChecked is how many persisted DailyReconciliation rows were actually
// found in the requested range (which may be fewer than the calendar span
// if some days were never ingested) — reported so the model can state that
// honestly rather than implying full calendar coverage.
type DiscrepanciesResult struct {
	DaysChecked int                `json:"days_checked"`
	Days        []DayDiscrepancies `json:"days"`
}

// --- Core logic ----------------------------------------------------------

// GetDailySummary backs the get_daily_summary tool contract: the
// DailyReconciliation for one calendar date, or a typed no_data error if
// none was ever computed for it — never a partial or estimated summary.
func GetDailySummary(ctx context.Context, q storage.Querier, date string) (*DailySummaryResult, *ToolError, error) {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return nil, invalidInput(fmt.Sprintf("date %q is not YYYY-MM-DD: %v", date, err)), nil
	}

	day, err := storage.LoadDailyReconciliation(ctx, q, d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ToolError{
				Error:   "no_data",
				Missing: []string{fmt.Sprintf("daily_reconciliation row for %s", date)},
			}, nil
		}
		return nil, nil, fmt.Errorf("mcptools: get_daily_summary(%s): %w", date, err)
	}
	return NewDailySummaryResult(day), nil, nil
}

// GetMarginDelta backs the get_margin_delta tool contract: the margin
// delta between two periods, each carrying its own source_row_refs.
// Returns a typed insufficient_data error if either period has a calendar
// day with no computed reconciliation, rather than computing a delta
// against partial data.
func GetMarginDelta(ctx context.Context, q storage.Querier, periodA, periodB Period) (*MarginDeltaResult, *ToolError, error) {
	a, toolErr, err := periodMargin(ctx, q, periodA)
	if err != nil || toolErr != nil {
		return nil, toolErr, err
	}
	b, toolErr, err := periodMargin(ctx, q, periodB)
	if err != nil || toolErr != nil {
		return nil, toolErr, err
	}

	deltaCents := b.marginCents - a.marginCents
	return &MarginDeltaResult{
		PeriodA:          a.result,
		PeriodB:          b.result,
		DeltaMarginTotal: money.FormatCents(deltaCents),
	}, nil, nil
}

// ListDiscrepancies backs the list_discrepancies tool contract. Exactly
// one of date or period must be provided (period may be nil; date empty
// means "not provided") — providing both or neither is an invalid_input
// error, not a best-guess about which the caller meant.
func ListDiscrepancies(ctx context.Context, q storage.Querier, date string, period *Period) (*DiscrepanciesResult, *ToolError, error) {
	switch {
	case date != "" && period != nil:
		return nil, invalidInput("provide exactly one of date or period, not both"), nil

	case date == "" && period == nil:
		return nil, invalidInput("provide either date or period"), nil

	case date != "":
		d, err := time.Parse(dateLayout, date)
		if err != nil {
			return nil, invalidInput(fmt.Sprintf("date %q is not YYYY-MM-DD: %v", date, err)), nil
		}
		day, err := storage.LoadDailyReconciliation(ctx, q, d)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, &ToolError{
					Error:   "no_data",
					Missing: []string{fmt.Sprintf("daily_reconciliation row for %s", date)},
				}, nil
			}
			return nil, nil, fmt.Errorf("mcptools: list_discrepancies(%s): %w", date, err)
		}
		return discrepanciesFromDays([]reconcile.DailyReconciliation{day}), nil, nil

	default: // period != nil
		start, end, err := period.parse()
		if err != nil {
			return nil, invalidInput(err.Error()), nil
		}
		days, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, start, end)
		if err != nil {
			return nil, nil, fmt.Errorf("mcptools: list_discrepancies(%s..%s): %w", period.Start, period.End, err)
		}
		return discrepanciesFromDays(days), nil, nil
	}
}

// --- shared helpers --------------------------------------------------

type periodMarginInternal struct {
	result      PeriodMarginResult
	marginCents int64
}

// periodMargin loads every day in [p.Start, p.End] and sums margin and
// provenance across them, refusing (insufficient_data) if any calendar day
// in the range has no persisted reconciliation — the actual enforcement of
// get_margin_delta's "never compute a delta against partial data" rule.
func periodMargin(ctx context.Context, q storage.Querier, p Period) (*periodMarginInternal, *ToolError, error) {
	start, end, err := p.parse()
	if err != nil {
		return nil, invalidInput(err.Error()), nil
	}

	days, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, start, end)
	if err != nil {
		return nil, nil, fmt.Errorf("mcptools: loading period %s..%s: %w", p.Start, p.End, err)
	}

	present := make(map[string]reconcile.DailyReconciliation, len(days))
	for _, d := range days {
		present[d.Date.Format(dateLayout)] = d
	}

	var (
		missing     []string
		marginCents int64
		refs        []reconcile.SourceRowRef
	)
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		key := cur.Format(dateLayout)
		d, ok := present[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		marginCents += d.MarginCents
		refs = append(refs, d.SourceRowRefs...)
	}
	if len(missing) > 0 {
		return nil, &ToolError{Error: "insufficient_data", Missing: missing}, nil
	}

	return &periodMarginInternal{
		marginCents: marginCents,
		result: PeriodMarginResult{
			Start:         p.Start,
			End:           p.End,
			DaysIncluded:  len(days),
			MarginTotal:   money.FormatCents(marginCents),
			SourceRowRefs: refs,
		},
	}, nil, nil
}

func discrepanciesFromDays(days []reconcile.DailyReconciliation) *DiscrepanciesResult {
	out := &DiscrepanciesResult{DaysChecked: len(days)}
	for _, d := range days {
		if len(d.DiscrepancyFlags) == 0 {
			continue
		}
		out.Days = append(out.Days, DayDiscrepancies{
			Date:          d.Date.Format(dateLayout),
			Flags:         d.DiscrepancyFlags,
			SourceRowRefs: d.SourceRowRefs,
		})
	}
	return out
}

// NewDailySummaryResult renders one already-computed
// reconcile.DailyReconciliation into the JSON-facing shape. Exported so the
// plain REST surface (internal/httpapi's GET /api/reconciliation) serves the
// EXACT same shape, with the same money formatting and the same derived
// TotalDeliveryGrossSales, as the get_daily_summary MCP tool — one
// conversion, so the page and the model can never disagree about the same
// day's numbers.
func NewDailySummaryResult(d reconcile.DailyReconciliation) *DailySummaryResult {
	gross := make(map[string]string, len(d.GrossSalesBySource))
	var deliveryTotalCents int64
	for source, cents := range d.GrossSalesBySource {
		gross[source] = money.FormatCents(cents)
		if source != "pos" {
			deliveryTotalCents += cents
		}
	}
	return &DailySummaryResult{
		Date:                    d.Date.Format(dateLayout),
		GrossSalesBySource:      gross,
		TotalDeliveryGrossSales: money.FormatCents(deliveryTotalCents),
		Commissions:             money.FormatCents(d.CommissionsCents),
		Refunds:                 money.FormatCents(d.RefundsCents),
		InputCosts:              money.FormatCents(d.InputCostsCents),
		Margin:                  money.FormatCents(d.MarginCents),
		DiscrepancyFlags:        d.DiscrepancyFlags,
		SourceRowRefs:           d.SourceRowRefs,
	}
}

// --- MCP tool registration -------------------------------------------

var periodPropertySchema = map[string]any{
	"start": map[string]any{"type": "string", "description": "Inclusive start date, YYYY-MM-DD."},
	"end":   map[string]any{"type": "string", "description": "Inclusive end date, YYYY-MM-DD."},
}

// registerReconciliationTools adds get_daily_summary, get_margin_delta, and
// list_discrepancies to s, per contracts/mcp-tools.md. Called from
// RegisterMCPServer (server.go).
func registerReconciliationTools(s *server.MCPServer, q storage.Querier) {
	s.AddTool(
		mcp.NewTool("get_daily_summary",
			mcp.WithDescription("Return the deterministic DailyReconciliation for one calendar date: gross sales by source, commissions, refunds, input costs, margin, discrepancy flags, and full source-row provenance. Returns a typed no_data error, never a partial or estimated summary, if no reconciliation has been computed for that date."),
			mcp.WithString("date", mcp.Required(), mcp.Description("Calendar date in YYYY-MM-DD format.")),
		),
		handleGetDailySummary(q),
	)

	s.AddTool(
		mcp.NewTool("get_margin_delta",
			mcp.WithDescription("Compute the margin delta between two date ranges (period_b's margin total minus period_a's), each carrying its own source-row provenance and day count. Returns a typed insufficient_data error, never a delta computed against partial data, if either period has a calendar day with no computed reconciliation."),
			mcp.WithObject("period_a", mcp.Required(), mcp.Description("Baseline period."), mcp.Properties(periodPropertySchema)),
			mcp.WithObject("period_b", mcp.Required(), mcp.Description("Comparison period."), mcp.Properties(periodPropertySchema)),
		),
		handleGetMarginDelta(q),
	)

	s.AddTool(
		mcp.NewTool("list_discrepancies",
			mcp.WithDescription("List discrepancy_flags entries (duplicate orders collapsed, refunds netted, missing sources, anomaly-threshold breaches) for one date or one period, each day carrying its full source-row provenance. Provide exactly one of date or period, not both."),
			mcp.WithString("date", mcp.Description("Calendar date in YYYY-MM-DD format. Provide this OR period, not both.")),
			mcp.WithObject("period", mcp.Description("Date range. Provide this OR date, not both."), mcp.Properties(periodPropertySchema)),
		),
		handleListDiscrepancies(q),
	)
}

func handleGetDailySummary(q storage.Querier) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Date string `json:"date"`
		}
		if err := req.BindArguments(&args); err != nil {
			return errorResult(*invalidInput("could not parse arguments: " + err.Error()))
		}

		result, toolErr, err := GetDailySummary(ctx, q, args.Date)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get_daily_summary failed", err), nil
		}
		if toolErr != nil {
			return errorResult(*toolErr)
		}
		return jsonResult(result)
	}
}

func handleGetMarginDelta(q storage.Querier) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PeriodA Period `json:"period_a"`
			PeriodB Period `json:"period_b"`
		}
		if err := req.BindArguments(&args); err != nil {
			return errorResult(*invalidInput("could not parse arguments: " + err.Error()))
		}

		result, toolErr, err := GetMarginDelta(ctx, q, args.PeriodA, args.PeriodB)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get_margin_delta failed", err), nil
		}
		if toolErr != nil {
			return errorResult(*toolErr)
		}
		return jsonResult(result)
	}
}

func handleListDiscrepancies(q storage.Querier) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Date   string  `json:"date"`
			Period *Period `json:"period"`
		}
		if err := req.BindArguments(&args); err != nil {
			return errorResult(*invalidInput("could not parse arguments: " + err.Error()))
		}

		result, toolErr, err := ListDiscrepancies(ctx, q, args.Date, args.Period)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list_discrepancies failed", err), nil
		}
		if toolErr != nil {
			return errorResult(*toolErr)
		}
		return jsonResult(result)
	}
}

// jsonResult and errorResult are the two shapes every tool handler in this
// package returns: a plain JSON success payload, or a ToolError rendered
// as JSON with IsError set. Per mcp-go's own documented convention ("errors
// that originate from the tool SHOULD be reported inside the result
// object... so the LLM can see it and self-correct"), a typed business
// error is still a normal CallToolResult — never a Go `error` return, which
// this package reserves for genuine infrastructure failures.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultJSON(v)
}

func errorResult(e ToolError) (*mcp.CallToolResult, error) {
	r, err := mcp.NewToolResultJSON(e)
	if err != nil {
		return nil, err
	}
	r.IsError = true
	return r, nil
}
