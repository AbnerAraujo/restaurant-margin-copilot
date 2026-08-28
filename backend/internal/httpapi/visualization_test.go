package httpapi

import (
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
)

func inv(name, resultJSON string) explain.ToolInvocation {
	return explain.ToolInvocation{Name: name, ResultJSON: resultJSON}
}

const (
	discrepanciesJSONFixture = `{"days_checked":14,"days":[
		{"date":"2026-08-08","flags":[{"type":"missing_delivery_source","detail":"no delivery-platform rows for this date"}]},
		{"date":"2026-08-03","flags":[{"type":"duplicate_order_removed","detail":"order 4412 appeared twice"},{"type":"commission_mismatch","detail":"stated 8.10, recomputed 8.35"}]}
	]}`

	cleanDiscrepanciesJSONFixture = `{"days_checked":7,"days":[]}`

	marginDeltaJSONFixture = `{
		"period_a":{"start":"2026-08-01","end":"2026-08-07","days_included":7,"margin_total":"104.61"},
		"period_b":{"start":"2026-08-08","end":"2026-08-14","days_included":7,"margin_total":"377.02"},
		"delta_margin_total":"272.41"}`

	dailySummaryAug07JSONFixture = `{"date":"2026-08-07","gross_sales_by_source":{"pos":"266.25","ifood":"74.25","just_eat_takeaway":"65.50"},
		"total_delivery_gross_sales":"139.75","commissions":"20.96","refunds":"0.00","input_costs":"9.22","margin":"375.82",
		"discrepancy_flags":[],"source_row_refs":[]}`

	dailySummaryAug08JSONFixture = `{"date":"2026-08-08","gross_sales_by_source":{"pos":"487.50"},
		"total_delivery_gross_sales":"0.00","commissions":"0.00","refunds":"0.00","input_costs":"335.00","margin":"152.50",
		"discrepancy_flags":[{"type":"missing_delivery_source","detail":"none"}],"source_row_refs":[]}`

	promotionsJSONFixture = `{"promotions":[
		{"platform":"iFood","campaign_id":"IFOOD-CAMP-BOOST01","period":{"start":"2026-08-01","end":"2026-08-07"},
		 "spend":"180.00","attributed_incremental_orders":6,"attributed_incremental_revenue":"214.00","roi":"34.00","flagged_negative":false},
		{"platform":"Just Eat Takeaway","campaign_id":"JET-CAMP-LUNCHFIX","period":{"start":"2026-08-04","end":"2026-08-10"},
		 "spend":"220.00","attributed_incremental_orders":2,"attributed_incremental_revenue":"55.00","roi":"-165.00","flagged_negative":true},
		{"platform":"iFood","campaign_id":"IFOOD-CAMP-WEEKEND","period":{"start":"2026-08-08","end":"2026-08-09"},
		 "spend":"95.00","attributed_incremental_orders":null,"attributed_incremental_revenue":null,"roi":null,
		 "reason":"attribution_unavailable","flagged_negative":false}
	]}`

	singlePromotionJSONFixture = `{"promotions":[
		{"platform":"iFood","campaign_id":"IFOOD-CAMP-BOOST01","period":{"start":"2026-08-01","end":"2026-08-07"},
		 "spend":"180.00","attributed_incremental_orders":6,"attributed_incremental_revenue":"214.00","roi":"34.00","flagged_negative":false}
	]}`
)

func TestDeriveVisualizationKind(t *testing.T) {
	tests := []struct {
		name        string
		invocations []explain.ToolInvocation
		wantKind    string // "" means no visualization at all
		wantTool    string
		wantPoints  int
		wantRows    int
	}{
		{
			name:        "no tool calls at all yields no chart",
			invocations: nil,
			wantKind:    "",
		},
		{
			name:        "unrecognized tool yields no chart",
			invocations: []explain.ToolInvocation{inv("some_future_tool", `{"whatever":1}`)},
			wantKind:    "",
		},
		{
			name:        "list_discrepancies becomes a table, one row per flagged day",
			invocations: []explain.ToolInvocation{inv("list_discrepancies", discrepanciesJSONFixture)},
			wantKind:    VizKindTable,
			wantTool:    "list_discrepancies",
			wantRows:    2,
		},
		{
			name:        "a clean period yields no table rather than an empty one",
			invocations: []explain.ToolInvocation{inv("list_discrepancies", cleanDiscrepanciesJSONFixture)},
			wantKind:    "",
		},
		{
			name:        "get_margin_delta becomes a two-bar comparison, never three bars",
			invocations: []explain.ToolInvocation{inv("get_margin_delta", marginDeltaJSONFixture)},
			wantKind:    VizKindBar,
			wantTool:    "get_margin_delta",
			wantPoints:  2,
		},
		{
			name:        "a single day with three revenue sources becomes a pie",
			invocations: []explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug07JSONFixture)},
			wantKind:    VizKindPie,
			wantTool:    "get_daily_summary",
			wantPoints:  3,
		},
		{
			name:        "a single day with one revenue source yields no chart",
			invocations: []explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug08JSONFixture)},
			wantKind:    "",
		},
		{
			name: "two days looked up in one interaction become a margin bar chart",
			invocations: []explain.ToolInvocation{
				inv("get_daily_summary", dailySummaryAug08JSONFixture),
				inv("get_daily_summary", dailySummaryAug07JSONFixture),
			},
			wantKind:   VizKindBar,
			wantTool:   "get_daily_summary",
			wantPoints: 2,
		},
		{
			name:        "several campaigns become a bar chart of ROI per campaign",
			invocations: []explain.ToolInvocation{inv("get_promotion_roi", promotionsJSONFixture)},
			wantKind:    VizKindBar,
			wantTool:    "get_promotion_roi",
			wantPoints:  3,
		},
		{
			name:        "a single campaign becomes a detail table, never a one-bar chart",
			invocations: []explain.ToolInvocation{inv("get_promotion_roi", singlePromotionJSONFixture)},
			wantKind:    VizKindTable,
			wantTool:    "get_promotion_roi",
			wantRows:    1,
		},
		{
			name:        "list_negative_roi_promotions uses the same shape and records its own source tool",
			invocations: []explain.ToolInvocation{inv("list_negative_roi_promotions", promotionsJSONFixture)},
			wantKind:    VizKindBar,
			wantTool:    "list_negative_roi_promotions",
			wantPoints:  3,
		},
		{
			name:        "malformed tool JSON is skipped rather than crashing or half-rendering",
			invocations: []explain.ToolInvocation{inv("get_margin_delta", `{not json`)},
			wantKind:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveVisualization(tt.invocations)

			if tt.wantKind == "" {
				if got != nil {
					t.Fatalf("expected no visualization, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected a %s visualization, got none", tt.wantKind)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if got.SourceTool != tt.wantTool {
				t.Errorf("SourceTool = %q, want %q", got.SourceTool, tt.wantTool)
			}
			if tt.wantPoints > 0 && len(got.Points) != tt.wantPoints {
				t.Errorf("len(Points) = %d, want %d", len(got.Points), tt.wantPoints)
			}
			if tt.wantRows > 0 && len(got.Rows) != tt.wantRows {
				t.Errorf("len(Rows) = %d, want %d", len(got.Rows), tt.wantRows)
			}
		})
	}
}

func TestDeriveVisualizationPriorityIsFixedNotCallOrder(t *testing.T) {
	// The same set of tool calls must produce the same chart regardless of
	// the order the model happened to make them in.
	forward := deriveVisualization([]explain.ToolInvocation{
		inv("get_daily_summary", dailySummaryAug07JSONFixture),
		inv("get_margin_delta", marginDeltaJSONFixture),
		inv("get_promotion_roi", promotionsJSONFixture),
	})
	reversed := deriveVisualization([]explain.ToolInvocation{
		inv("get_promotion_roi", promotionsJSONFixture),
		inv("get_margin_delta", marginDeltaJSONFixture),
		inv("get_daily_summary", dailySummaryAug07JSONFixture),
	})

	if forward == nil || reversed == nil {
		t.Fatal("expected a visualization from both orderings")
	}
	if forward.SourceTool != "get_promotion_roi" || reversed.SourceTool != "get_promotion_roi" {
		t.Fatalf("promotions must win the fixed priority; got %q and %q", forward.SourceTool, reversed.SourceTool)
	}
}

func TestUnattributablePromotionIsMarkedUnavailableNotZero(t *testing.T) {
	viz := deriveVisualization([]explain.ToolInvocation{inv("get_promotion_roi", promotionsJSONFixture)})
	if viz == nil {
		t.Fatal("expected a visualization")
	}

	var weekend *VizPoint
	for i := range viz.Points {
		if viz.Points[i].Label == "IFOOD-CAMP-WEEKEND" {
			weekend = &viz.Points[i]
		}
	}
	if weekend == nil {
		t.Fatal("IFOOD-CAMP-WEEKEND missing from the chart")
	}
	if !weekend.Unavailable {
		t.Error("an unattributable campaign must be marked Unavailable (FR-013)")
	}
	if weekend.Value != 0 || weekend.Display == "$0.00" {
		t.Errorf("an unattributable campaign must never be rendered as a $0 figure; got value=%v display=%q", weekend.Value, weekend.Display)
	}
	if weekend.Reason == "" {
		t.Error("an unavailable point must carry the reason the tool gave")
	}
}

func TestMoneyCrossesTheWireAsFormattedStringsAndGeometryDollars(t *testing.T) {
	viz := deriveVisualization([]explain.ToolInvocation{inv("get_margin_delta", marginDeltaJSONFixture)})
	if viz == nil {
		t.Fatal("expected a visualization")
	}

	tests := []struct {
		label       string
		wantDisplay string
		wantValue   float64
	}{
		{"2026-08-01 → 2026-08-07", "$104.61", 104.61},
		{"2026-08-08 → 2026-08-14", "$377.02", 377.02},
	}
	for i, want := range tests {
		got := viz.Points[i]
		if got.Label != want.label {
			t.Errorf("point %d Label = %q, want %q", i, got.Label, want.label)
		}
		if got.Display != want.wantDisplay {
			t.Errorf("point %d Display = %q, want %q", i, got.Display, want.wantDisplay)
		}
		if got.Value != want.wantValue {
			t.Errorf("point %d Value = %v, want %v", i, got.Value, want.wantValue)
		}
	}
	if viz.Subtitle != "Change: $272.41" {
		t.Errorf("Subtitle = %q, want the delta stated rather than drawn as a third bar", viz.Subtitle)
	}
}

func TestNegativeMoneyRendersWithATypographicMinusOutsideTheCurrencySymbol(t *testing.T) {
	viz := deriveVisualization([]explain.ToolInvocation{inv("get_promotion_roi", promotionsJSONFixture)})
	if viz == nil {
		t.Fatal("expected a visualization")
	}

	for _, point := range viz.Points {
		if point.Label != "JET-CAMP-LUNCHFIX" {
			continue
		}
		if point.Display != "−$165.00" {
			t.Errorf("Display = %q, want %q", point.Display, "−$165.00")
		}
		if point.Value != -165 {
			t.Errorf("Value = %v, want -165", point.Value)
		}
		return
	}
	t.Fatal("JET-CAMP-LUNCHFIX missing from the chart")
}

func TestHumanizeFlagType(t *testing.T) {
	tests := []struct{ in, want string }{
		{"duplicate_order_removed", "Duplicate order removed"},
		{"missing_delivery_source", "Missing delivery source"},
		{"anomaly_threshold_exceeded", "Anomaly threshold exceeded"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := humanizeFlagType(tt.in); got != tt.want {
			t.Errorf("humanizeFlagType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
