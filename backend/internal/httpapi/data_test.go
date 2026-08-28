package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(dateLayout, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return d
}

func TestParseOptionalPeriod(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantStart string // "" means "the wide-open sentinel"
		wantEnd   string
		wantErr   bool
	}{
		{
			name:  "no parameters means everything persisted, not nothing",
			query: "",
		},
		{
			name:      "both bounds are honoured",
			query:     "?start=2026-08-01&end=2026-08-14",
			wantStart: "2026-08-01",
			wantEnd:   "2026-08-14",
		},
		{
			name:      "a start alone leaves the end wide open",
			query:     "?start=2026-08-08",
			wantStart: "2026-08-08",
		},
		{
			name:    "a malformed date is refused, never coerced",
			query:   "?start=08%2F01%2F2026",
			wantErr: true,
		},
		{
			name:    "an end before its start is refused, never silently swapped",
			query:   "?start=2026-08-14&end=2026-08-01",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/reconciliation"+tt.query, nil)
			start, end, err := parseOptionalPeriod(request)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantStart != "" && start.Format(dateLayout) != tt.wantStart {
				t.Errorf("start = %s, want %s", start.Format(dateLayout), tt.wantStart)
			}
			if tt.wantStart == "" && start.Year() != 1900 {
				t.Errorf("start = %s, want the wide-open sentinel", start.Format(dateLayout))
			}
			if tt.wantEnd != "" && end.Format(dateLayout) != tt.wantEnd {
				t.Errorf("end = %s, want %s", end.Format(dateLayout), tt.wantEnd)
			}
			if tt.wantEnd == "" && end.Year() != 2999 {
				t.Errorf("end = %s, want the wide-open sentinel", end.Format(dateLayout))
			}
		})
	}
}

func TestServedBoundReportsRealCoverageNotSentinels(t *testing.T) {
	days := []reconcile.DailyReconciliation{
		{Date: mustParseDate(t, "2026-08-01")},
		{Date: mustParseDate(t, "2026-08-14")},
	}
	wideStart := mustParseDate(t, "1900-01-01")
	wideEnd := mustParseDate(t, "2999-12-31")

	if got := servedBound(wideStart, days, true); got != "2026-08-01" {
		t.Errorf("unbounded start served as %q, want the data's own first date", got)
	}
	if got := servedBound(wideEnd, days, false); got != "2026-08-14" {
		t.Errorf("unbounded end served as %q, want the data's own last date", got)
	}

	requested := mustParseDate(t, "2026-08-05")
	if got := servedBound(requested, days, true); got != "2026-08-05" {
		t.Errorf("explicit start served as %q, want it echoed back", got)
	}

	// An empty result must not invent a range for a period that returned
	// nothing.
	if got := servedBound(wideStart, nil, true); got != "" {
		t.Errorf("empty result served start %q, want an empty string", got)
	}
	if got := servedBound(wideEnd, nil, false); got != "" {
		t.Errorf("empty result served end %q, want an empty string", got)
	}
}
