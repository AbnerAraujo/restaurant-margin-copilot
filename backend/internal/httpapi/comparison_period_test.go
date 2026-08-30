package httpapi

import (
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
)

func date(s string) time.Time {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDerivePriorPeriod_CalendarMonth(t *testing.T) {
	tests := []struct {
		name         string
		start, end   string
		wantS, wantE string
	}{
		{"31-day month", "2026-07-01", "2026-07-31", "2026-06-01", "2026-06-30"},
		{"30-day month", "2026-06-01", "2026-06-30", "2026-05-01", "2026-05-31"},
		{"February non-leap", "2025-03-01", "2025-03-31", "2025-02-01", "2025-02-28"},
		{"February leap", "2024-03-01", "2024-03-31", "2024-02-01", "2024-02-29"},
		{"January crosses year boundary", "2026-01-01", "2026-01-31", "2025-12-01", "2025-12-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotS, gotE := derivePriorPeriod(date(tt.start), date(tt.end))
			if gotS.Format(dateLayout) != tt.wantS || gotE.Format(dateLayout) != tt.wantE {
				t.Errorf("derivePriorPeriod(%s, %s) = (%s, %s), want (%s, %s)",
					tt.start, tt.end, gotS.Format(dateLayout), gotE.Format(dateLayout), tt.wantS, tt.wantE)
			}
		})
	}
}

func TestDerivePriorPeriod_CalendarYear(t *testing.T) {
	gotS, gotE := derivePriorPeriod(date("2026-01-01"), date("2026-12-31"))
	if gotS.Format(dateLayout) != "2025-01-01" || gotE.Format(dateLayout) != "2025-12-31" {
		t.Errorf("prior year = (%s, %s), want (2025-01-01, 2025-12-31)", gotS.Format(dateLayout), gotE.Format(dateLayout))
	}

	// A leap year's prior period is still the plain prior calendar year,
	// not a shifted 366-day span.
	gotS, gotE = derivePriorPeriod(date("2024-01-01"), date("2024-12-31"))
	if gotS.Format(dateLayout) != "2023-01-01" || gotE.Format(dateLayout) != "2023-12-31" {
		t.Errorf("prior leap year = (%s, %s), want (2023-01-01, 2023-12-31)", gotS.Format(dateLayout), gotE.Format(dateLayout))
	}
}

func TestDerivePriorPeriod_ArbitraryCustomRange(t *testing.T) {
	// A 14-day window: the prior period is the immediately
	// preceding 14 days, no gap, no overlap — not calendar-month-aware
	// since this isn't a full calendar month.
	gotS, gotE := derivePriorPeriod(date("2026-08-01"), date("2026-08-14"))
	if gotS.Format(dateLayout) != "2026-07-18" || gotE.Format(dateLayout) != "2026-07-31" {
		t.Errorf("prior 14-day period = (%s, %s), want (2026-07-18, 2026-07-31)", gotS.Format(dateLayout), gotE.Format(dateLayout))
	}

	// A single day: the prior period is just the day before.
	gotS, gotE = derivePriorPeriod(date("2026-08-07"), date("2026-08-07"))
	if gotS.Format(dateLayout) != "2026-08-06" || gotE.Format(dateLayout) != "2026-08-06" {
		t.Errorf("prior single day = (%s, %s), want (2026-08-06, 2026-08-06)", gotS.Format(dateLayout), gotE.Format(dateLayout))
	}
}

func TestDerivePriorPeriod_CanFallOutsideDataRange(t *testing.T) {
	// This function has no knowledge of the real data's min/max range by
	// design (spec.md: FR-005's refusal happens through the real ask path,
	// not a check here) — it must still produce a well-formed period even
	// when that period will later be refused as out of range.
	dataStart := date("2024-08-01")

	gotS, gotE := derivePriorPeriod(date("2024-08-01"), date("2024-08-31"))
	if !gotE.Before(dataStart) {
		t.Fatalf("expected the derived prior period to end before the real data start for this test's premise to hold, got end=%s", gotE.Format(dateLayout))
	}
	if gotS.Format(dateLayout) != "2024-07-01" || gotE.Format(dateLayout) != "2024-07-31" {
		t.Errorf("prior period = (%s, %s), want (2024-07-01, 2024-07-31) even though it is out of range", gotS.Format(dateLayout), gotE.Format(dateLayout))
	}
}

func TestDeriveResolvedPeriod_FromPeriodTotals(t *testing.T) {
	invocations := []explain.ToolInvocation{
		{Name: "get_period_totals", ResultJSON: `{"start":"2026-08-01","end":"2026-08-14","days_included":14}`},
	}
	got := deriveResolvedPeriod(invocations)
	if got == nil || got.Start != "2026-08-01" || got.End != "2026-08-14" {
		t.Fatalf("deriveResolvedPeriod = %+v, want {2026-08-01 2026-08-14}", got)
	}
}

func TestDeriveResolvedPeriod_FromDailySummary(t *testing.T) {
	invocations := []explain.ToolInvocation{
		{Name: "get_daily_summary", ResultJSON: `{"date":"2026-08-07","commissions":"12.34"}`},
	}
	got := deriveResolvedPeriod(invocations)
	if got == nil || got.Start != "2026-08-07" || got.End != "2026-08-07" {
		t.Fatalf("deriveResolvedPeriod = %+v, want {2026-08-07 2026-08-07} (single day, start==end)", got)
	}
}

func TestDeriveResolvedPeriod_NilWhenNoPeriodShapedToolRan(t *testing.T) {
	invocations := []explain.ToolInvocation{
		{Name: "list_discrepancies", ResultJSON: `{"days_checked":14,"days":[]}`},
	}
	got := deriveResolvedPeriod(invocations)
	if got != nil {
		t.Fatalf("deriveResolvedPeriod = %+v, want nil — list_discrepancies has no top-level period field", got)
	}
}

func TestDeriveResolvedPeriod_NilOnUnparsableJSON(t *testing.T) {
	invocations := []explain.ToolInvocation{
		{Name: "get_period_totals", ResultJSON: `not valid json`},
	}
	got := deriveResolvedPeriod(invocations)
	if got != nil {
		t.Fatalf("deriveResolvedPeriod = %+v, want nil on unparsable JSON", got)
	}
}
