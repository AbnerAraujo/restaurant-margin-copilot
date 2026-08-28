package httpapi

// Chart-type selection for a chat answer.
//
// The load-bearing constraint: WHICH chart an answer gets is decided here, in
// plain Go, from (a) which typed MCP tool actually ran and (b) the shape of
// the deterministic result it returned. It is never a second model call and
// never a field the narration model fills in. Constitution Principle I draws
// the line at "the model narrates an already-computed result"; asking a model
// "should this be a bar or a pie?" would put presentation of a financial
// figure back on the probabilistic side of that line for no benefit — the
// tool name already tells us the question's shape with certainty, which is
// exactly the kind of thing deterministic code should decide.
//
// Money crosses the wire the way it does everywhere else in this codebase: as
// an already-formatted decimal string (internal/money.FormatCents), never raw
// cents. Points additionally carry a float64 `value` in dollars, which exists
// ONLY so the client can compute bar heights and pie arcs; `display` is the
// authoritative rendering and is what the UI must actually print. Deriving
// `value` here rather than parsing money strings in TypeScript keeps every
// money conversion in the one language that already owns the money type.
//
// Mapping rationale, per tool (form chosen by the data's job, per the dataviz
// form heuristic — magnitude comparison -> bar, part-to-whole -> pie, more
// than a couple of columns of detail -> table):
//
//   - compare_platform_economics -> bar, 4 marks: one pair per platform
//     (commission-only, commission+promo combined). This is a magnitude
//     comparison across named categories (spec 003-platform-comparator),
//     the same job get_margin_delta's bar already does — reused rather than
//     inventing a grouped/2-series bar component, since a flat list of 4
//     labelled points fits the existing single-series contract exactly
//     (plan.md's own chart-mapping decision). Commission-only and combined
//     are two SEPARATE points, never merged into one bar, per FR-004's
//     "shown as distinct, separately-sourced figures".
//   - list_discrepancies      -> table. Each flagged day carries a date, one
//     or more flag types, and free-text detail: three columns of mixed text,
//     which is a table's job, not a chart's.
//   - get_margin_delta        -> bar, two marks (period A vs period B). The
//     delta itself is deliberately NOT a third bar: it is a different measure
//     from the two totals, and putting a difference on the same axis as the
//     magnitudes it was derived from double-counts the same money visually.
//     It is stated as the subtitle instead.
//   - get_promotion_roi /
//     list_negative_roi_promotions
//     -> bar of ROI per campaign when 2+ campaigns are in the result
//     (comparing magnitudes across named categories); -> table for a single
//     campaign, where the interesting content is that campaign's several
//     columns of detail and a one-bar bar chart is an anti-pattern.
//   - get_daily_summary       -> nothing for a single day: the narrated
//     sentence already carries the figure, and a one-value chart adds no
//     information. Two exceptions, both real jobs rather than decoration:
//     2+ days looked up in one interaction -> bar of margin per day (a
//     magnitude comparison the reader explicitly asked for); a single day
//     whose revenue splits across 3+ sources -> pie of gross sales by source
//     (a genuine part-to-whole, capped at 6 slices; a 2-slice pie is an
//     anti-pattern and is deliberately not produced).
//
// When several recognized tools ran in one interaction, the fixed priority
// below applies rather than "whichever ran last": priority is deterministic
// and testable, whereas call order depends on the model's own sequencing and
// would make the same question render differently on different runs.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// Visualization kinds. An answer with no suitable shape carries no
// Visualization at all (the field is omitted) rather than an empty one — the
// client must never have to distinguish "no chart" from "an empty chart".
const (
	VizKindTable = "table"
	VizKindBar   = "bar"
	VizKindPie   = "pie"
)

// MaxPieSlices caps a pie at the point where slices stop being separable.
// Past it the same data is rendered as a table instead.
const MaxPieSlices = 6

// MinPieSlices is the floor below which a pie is the wrong form: a two-slice
// pie communicates strictly less than the two numbers written out.
const MinPieSlices = 3

// VizPoint is one mark in a bar or pie.
type VizPoint struct {
	Label string `json:"label"`
	// Value is the figure in DOLLARS, for geometry only (bar height, pie
	// arc). Display is what the UI prints.
	Value   float64 `json:"value"`
	Display string  `json:"display"`
	// Unavailable marks a category the deterministic layer refused to produce
	// a figure for (FR-013's unattributable promotion ROI). Such a point must
	// be rendered as an explicit "no figure" state — never as a zero-height
	// mark, which would read as "broke even" rather than "unknown".
	Unavailable bool   `json:"unavailable,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// Visualization is the optional structured rendering that accompanies an
// answer. Exactly one of (Columns+Rows) or Points is populated, per Kind.
type Visualization struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// ValueLabel names the measure a bar/pie axis carries (e.g. "Margin (USD)").
	ValueLabel string `json:"value_label,omitempty"`
	// SourceTool is the MCP tool whose deterministic result this was derived
	// from — provenance for the CHART itself, in the same spirit as
	// Principle IV's provenance on every number.
	SourceTool string `json:"source_tool"`

	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
	Points  []VizPoint `json:"points,omitempty"`
}

// deriveVisualization picks at most one visualization for an interaction.
// Returns nil when no tool result has a shape worth drawing — the common and
// correct outcome for a plain single-day question.
func deriveVisualization(invocations []explain.ToolInvocation) *Visualization {
	byTool := map[string][]string{}
	for _, inv := range invocations {
		byTool[inv.Name] = append(byTool[inv.Name], inv.ResultJSON)
	}

	// Fixed priority: the narrowest subject wins. A question that reached a
	// promotions tool is about promotions even if a daily summary was also
	// pulled for context.
	if viz := platformComparisonVisualization(byTool["compare_platform_economics"]); viz != nil {
		return viz
	}
	if viz := promotionVisualization(byTool); viz != nil {
		return viz
	}
	if viz := discrepancyVisualization(byTool["list_discrepancies"]); viz != nil {
		return viz
	}
	if viz := marginDeltaVisualization(byTool["get_margin_delta"]); viz != nil {
		return viz
	}
	return dailySummaryVisualization(byTool["get_daily_summary"])
}

// --- list_discrepancies -> table --------------------------------------

type discrepancyFlagJSON struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

type discrepancyDayJSON struct {
	Date  string                `json:"date"`
	Flags []discrepancyFlagJSON `json:"flags"`
}

type discrepanciesJSON struct {
	DaysChecked int                  `json:"days_checked"`
	Days        []discrepancyDayJSON `json:"days"`
}

func discrepancyVisualization(results []string) *Visualization {
	var days []discrepancyDayJSON
	daysChecked := 0
	seen := map[string]bool{}
	for _, raw := range results {
		var parsed discrepanciesJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		daysChecked += parsed.DaysChecked
		for _, day := range parsed.Days {
			if seen[day.Date] {
				continue
			}
			seen[day.Date] = true
			days = append(days, day)
		}
	}
	// A clean period is a real, good answer — but there is nothing to
	// tabulate, and an empty table reads as a failure rather than as "no
	// discrepancies found".
	if len(days) == 0 {
		return nil
	}

	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })

	rows := make([][]string, 0, len(days))
	for _, day := range days {
		types := make([]string, 0, len(day.Flags))
		details := make([]string, 0, len(day.Flags))
		for _, flag := range day.Flags {
			types = append(types, humanizeFlagType(flag.Type))
			if flag.Detail != "" {
				details = append(details, flag.Detail)
			}
		}
		rows = append(rows, []string{
			day.Date,
			strings.Join(types, ", "),
			strings.Join(details, "; "),
		})
	}

	return &Visualization{
		Kind:       VizKindTable,
		Title:      "Flagged days",
		Subtitle:   fmt.Sprintf("%d of %d reconciled days carried a discrepancy flag", len(days), daysChecked),
		SourceTool: "list_discrepancies",
		Columns:    []string{"Date", "Discrepancy", "Detail"},
		Rows:       rows,
	}
}

// humanizeFlagType turns reconcile.Flag* snake_case constants into display
// text. Kept as a plain replacement rather than a lookup table so a new flag
// type added in internal/reconcile still renders sensibly here instead of
// falling through to an empty cell.
func humanizeFlagType(t string) string {
	if t == "" {
		return ""
	}
	words := strings.Split(t, "_")
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}

// --- get_margin_delta -> bar ------------------------------------------

type periodMarginJSON struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	DaysIncluded int    `json:"days_included"`
	MarginTotal  string `json:"margin_total"`
}

type marginDeltaJSON struct {
	PeriodA          periodMarginJSON `json:"period_a"`
	PeriodB          periodMarginJSON `json:"period_b"`
	DeltaMarginTotal string           `json:"delta_margin_total"`
}

func marginDeltaVisualization(results []string) *Visualization {
	if len(results) == 0 {
		return nil
	}
	var parsed marginDeltaJSON
	if err := json.Unmarshal([]byte(results[0]), &parsed); err != nil {
		return nil
	}

	pointA, okA := moneyPoint(periodLabel(parsed.PeriodA), parsed.PeriodA.MarginTotal)
	pointB, okB := moneyPoint(periodLabel(parsed.PeriodB), parsed.PeriodB.MarginTotal)
	if !okA || !okB {
		return nil
	}

	subtitle := ""
	if parsed.DeltaMarginTotal != "" {
		subtitle = "Change: " + signedMoney(parsed.DeltaMarginTotal)
	}

	return &Visualization{
		Kind:       VizKindBar,
		Title:      "Margin by period",
		Subtitle:   subtitle,
		ValueLabel: "Margin (USD)",
		SourceTool: "get_margin_delta",
		Points:     []VizPoint{pointA, pointB},
	}
}

func periodLabel(p periodMarginJSON) string {
	if p.Start == p.End {
		return p.Start
	}
	return p.Start + " → " + p.End
}

// --- compare_platform_economics -> bar ---------------------------------

type platformEconomicsJSON struct {
	Source                string  `json:"source"`
	DisplayName           string  `json:"display_name"`
	CommissionPaid        string  `json:"commission_paid"`
	CombinedCost          string  `json:"combined_cost"`
	EffectiveRate         *string `json:"effective_rate"`
	CombinedEffectiveRate *string `json:"combined_effective_rate"`
}

type platformComparisonJSON struct {
	Platforms []platformEconomicsJSON `json:"platforms"`
}

// platformComparisonVisualization renders compare_platform_economics' result
// as 4 points: one platform's commission-only cost, then that same
// platform's commission+promo combined cost, repeated for the second
// platform — never fewer, so a platform with zero activity in the period
// still gets its own (zero-valued) pair rather than being dropped (FR-003).
func platformComparisonVisualization(results []string) *Visualization {
	if len(results) == 0 {
		return nil
	}
	var parsed platformComparisonJSON
	if err := json.Unmarshal([]byte(results[0]), &parsed); err != nil || len(parsed.Platforms) == 0 {
		return nil
	}

	points := make([]VizPoint, 0, len(parsed.Platforms)*2)
	subtitleParts := make([]string, 0, len(parsed.Platforms))
	for _, platform := range parsed.Platforms {
		name := platform.DisplayName
		if name == "" {
			name = platform.Source
		}

		commissionOnly, ok := moneyPoint(name+" — commission only", platform.CommissionPaid)
		if !ok {
			continue
		}
		combined, ok := moneyPoint(name+" — commission + promo", platform.CombinedCost)
		if !ok {
			continue
		}
		points = append(points, commissionOnly, combined)

		if platform.EffectiveRate != nil {
			subtitleParts = append(subtitleParts, fmt.Sprintf("%s: %s commission rate", name, *platform.EffectiveRate))
		} else {
			subtitleParts = append(subtitleParts, fmt.Sprintf("%s: no sales this period", name))
		}
	}
	if len(points) == 0 {
		return nil
	}

	return &Visualization{
		Kind:       VizKindBar,
		Title:      "Platform economics: commission vs. commission + promo spend",
		Subtitle:   strings.Join(subtitleParts, " · "),
		ValueLabel: "Cost (USD)",
		SourceTool: "compare_platform_economics",
		Points:     points,
	}
}

// --- promotion tools -> bar or table ----------------------------------

type promotionJSON struct {
	Platform                     string     `json:"platform"`
	CampaignID                   string     `json:"campaign_id"`
	Period                       periodJSON `json:"period"`
	Spend                        string     `json:"spend"`
	AttributedIncrementalOrders  *int       `json:"attributed_incremental_orders"`
	AttributedIncrementalRevenue *string    `json:"attributed_incremental_revenue"`
	ROI                          *string    `json:"roi"`
	Reason                       string     `json:"reason"`
	FlaggedNegative              bool       `json:"flagged_negative"`
}

type periodJSON struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type promotionsJSON struct {
	Promotions []promotionJSON `json:"promotions"`
}

func promotionVisualization(byTool map[string][]string) *Visualization {
	// Both promotion tools return the identical shape, so they share one
	// derivation; the SourceTool recorded is whichever actually ran.
	sourceTool := ""
	var raws []string
	for _, name := range []string{"list_negative_roi_promotions", "get_promotion_roi"} {
		if results, ok := byTool[name]; ok && len(results) > 0 {
			if sourceTool == "" {
				sourceTool = name
			}
			raws = append(raws, results...)
		}
	}
	if len(raws) == 0 {
		return nil
	}

	var promotions []promotionJSON
	seen := map[string]bool{}
	for _, raw := range raws {
		var parsed promotionsJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		for _, promo := range parsed.Promotions {
			key := promo.CampaignID + "|" + promo.Period.Start
			if seen[key] {
				continue
			}
			seen[key] = true
			promotions = append(promotions, promo)
		}
	}
	if len(promotions) == 0 {
		return nil
	}
	sort.Slice(promotions, func(i, j int) bool {
		return promotions[i].CampaignID < promotions[j].CampaignID
	})

	if len(promotions) == 1 {
		return singlePromotionTable(promotions[0], sourceTool)
	}

	points := make([]VizPoint, 0, len(promotions))
	for _, promo := range promotions {
		if promo.ROI == nil {
			// FR-013: attribution unavailable. Carried through as an explicit
			// unavailable mark so the chart refuses in the same voice the
			// tool did, instead of drawing a zero.
			points = append(points, VizPoint{
				Label:       promo.CampaignID,
				Display:     "Unattributable",
				Unavailable: true,
				Reason:      defaultString(promo.Reason, "attribution_unavailable"),
			})
			continue
		}
		point, ok := moneyPoint(promo.CampaignID, *promo.ROI)
		if !ok {
			continue
		}
		points = append(points, point)
	}
	if len(points) == 0 {
		return nil
	}

	return &Visualization{
		Kind:       VizKindBar,
		Title:      "Promotion ROI by campaign",
		Subtitle:   "Attributed incremental revenue minus spend",
		ValueLabel: "Net ROI (USD)",
		SourceTool: sourceTool,
		Points:     points,
	}
}

func singlePromotionTable(promo promotionJSON, sourceTool string) *Visualization {
	roi := "Unattributable — refusing to estimate"
	if promo.ROI != nil {
		roi = signedMoney(*promo.ROI)
	}
	revenue := "Unattributable"
	if promo.AttributedIncrementalRevenue != nil {
		revenue = "$" + *promo.AttributedIncrementalRevenue
	}
	orders := "—"
	if promo.AttributedIncrementalOrders != nil {
		orders = fmt.Sprintf("%d", *promo.AttributedIncrementalOrders)
	}

	return &Visualization{
		Kind:       VizKindTable,
		Title:      "Campaign " + promo.CampaignID,
		SourceTool: sourceTool,
		Columns:    []string{"Platform", "Period", "Spend", "Incremental orders", "Incremental revenue", "Net ROI"},
		Rows: [][]string{{
			promo.Platform,
			promo.Period.Start + " → " + promo.Period.End,
			"$" + promo.Spend,
			orders,
			revenue,
			roi,
		}},
	}
}

// --- get_daily_summary -> bar (multi-day) or pie (source mix) ---------

type dailySummaryJSON struct {
	Date               string            `json:"date"`
	GrossSalesBySource map[string]string `json:"gross_sales_by_source"`
	Margin             string            `json:"margin"`
}

func dailySummaryVisualization(results []string) *Visualization {
	var days []dailySummaryJSON
	seen := map[string]bool{}
	for _, raw := range results {
		var parsed dailySummaryJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed.Date == "" {
			continue
		}
		if seen[parsed.Date] {
			continue
		}
		seen[parsed.Date] = true
		days = append(days, parsed)
	}
	switch {
	case len(days) == 0:
		return nil
	case len(days) == 1:
		return grossSalesPie(days[0])
	default:
		return marginByDayBar(days)
	}
}

func marginByDayBar(days []dailySummaryJSON) *Visualization {
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })

	points := make([]VizPoint, 0, len(days))
	for _, day := range days {
		point, ok := moneyPoint(day.Date, day.Margin)
		if !ok {
			continue
		}
		points = append(points, point)
	}
	if len(points) < 2 {
		return nil
	}
	return &Visualization{
		Kind:       VizKindBar,
		Title:      "Margin by day",
		ValueLabel: "Margin (USD)",
		SourceTool: "get_daily_summary",
		Points:     points,
	}
}

// grossSalesPie renders one day's revenue mix — the single genuine
// part-to-whole in this data model. Gated at 3..6 non-zero sources: below
// that a pie says less than the sentence already does, above it the slices
// stop being separable.
func grossSalesPie(day dailySummaryJSON) *Visualization {
	sources := make([]string, 0, len(day.GrossSalesBySource))
	for source, amount := range day.GrossSalesBySource {
		cents, err := money.ParseCents(amount)
		if err != nil || cents <= 0 {
			continue
		}
		sources = append(sources, source)
	}
	if len(sources) < MinPieSlices || len(sources) > MaxPieSlices {
		return nil
	}
	sort.Strings(sources)

	points := make([]VizPoint, 0, len(sources))
	for _, source := range sources {
		point, ok := moneyPoint(humanizeSource(source), day.GrossSalesBySource[source])
		if !ok {
			continue
		}
		points = append(points, point)
	}
	if len(points) < MinPieSlices {
		return nil
	}

	return &Visualization{
		Kind:       VizKindPie,
		Title:      "Where the day's revenue came from",
		Subtitle:   "Gross sales by source, " + day.Date,
		ValueLabel: "Gross sales (USD)",
		SourceTool: "get_daily_summary",
		Points:     points,
	}
}

// humanizeSource maps internal/reconcile's normalized source keys to the
// names an owner actually uses for those platforms.
func humanizeSource(source string) string {
	switch source {
	case "pos":
		return "In-house POS"
	case "ifood":
		return "iFood"
	case "just_eat_takeaway":
		return "Just Eat Takeaway"
	default:
		return source
	}
}

// --- shared helpers ----------------------------------------------------

// moneyPoint converts one already-formatted decimal money string into a
// point. Parsing goes through internal/money so the dollars figure the client
// uses for geometry is derived by the same fixed-point code that produced the
// string, never by a float parse that could round differently.
func moneyPoint(label, decimal string) (VizPoint, bool) {
	cents, err := money.ParseCents(decimal)
	if err != nil {
		return VizPoint{}, false
	}
	return VizPoint{
		Label:   label,
		Value:   float64(cents) / 100,
		Display: signedMoney(decimal),
	}, true
}

// signedMoney renders a decimal money string for display: a leading minus
// stays outside the currency symbol ("−$12.34"), matching how the existing
// chart components format a loss.
func signedMoney(decimal string) string {
	if strings.HasPrefix(decimal, "-") {
		return "−$" + strings.TrimPrefix(decimal, "-")
	}
	return "$" + decimal
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
