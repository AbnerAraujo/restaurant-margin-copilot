package badges

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

func day(dateStr string, flags ...string) reconcile.DailyReconciliation {
	d, err := time.Parse(dateLayout, dateStr)
	if err != nil {
		panic(err)
	}
	out := reconcile.DailyReconciliation{Date: d}
	for _, f := range flags {
		out.DiscrepancyFlags = append(out.DiscrepancyFlags, reconcile.DiscrepancyFlag{Type: f, Detail: "detail"})
	}
	return out
}

func TestEvaluatePoints(t *testing.T) {
	tests := []struct {
		name          string
		days          []reconcile.DailyReconciliation
		wantTotal     int
		wantLineCount int
	}{
		{
			name:      "no reconciled days earns nothing, not a placeholder balance",
			days:      nil,
			wantTotal: 0,
		},
		{
			name:          "a clean day is worth PointsCleanClose",
			days:          []reconcile.DailyReconciliation{day("2026-08-01")},
			wantTotal:     PointsCleanClose,
			wantLineCount: 1,
		},
		{
			name:          "a flagged day is worth PointsDiscrepancyCatcher",
			days:          []reconcile.DailyReconciliation{day("2026-08-03", reconcile.FlagDuplicateOrderRemoved)},
			wantTotal:     PointsDiscrepancyCatcher,
			wantLineCount: 1,
		},
		{
			name: "a day with several flags still earns exactly one badge's points",
			days: []reconcile.DailyReconciliation{
				day("2026-08-03", reconcile.FlagDuplicateOrderRemoved, reconcile.FlagCommissionMismatch),
			},
			wantTotal:     PointsDiscrepancyCatcher,
			wantLineCount: 1,
		},
		{
			name: "a mixed period sums both categories",
			days: []reconcile.DailyReconciliation{
				day("2026-08-01"),
				day("2026-08-02"),
				day("2026-08-03", reconcile.FlagDuplicateOrderRemoved),
				day("2026-08-08", reconcile.FlagMissingDeliverySource),
			},
			wantTotal:     2*PointsCleanClose + 2*PointsDiscrepancyCatcher,
			wantLineCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluatePoints(EvaluateReconciliationBadges(tt.days))

			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", got.Total, tt.wantTotal)
			}
			if len(got.Breakdown) != tt.wantLineCount {
				t.Fatalf("len(Breakdown) = %d, want %d", len(got.Breakdown), tt.wantLineCount)
			}

			// The breakdown must always reconcile to the stated total —
			// a balance an owner cannot audit is a balance they cannot trust.
			sum := 0
			for _, line := range got.Breakdown {
				if line.Points != line.Count*line.PointsEach {
					t.Errorf("line %s: Points = %d, want Count*PointsEach = %d", line.Code, line.Points, line.Count*line.PointsEach)
				}
				if line.Name == "" {
					t.Errorf("line %s: Name must not be empty", line.Code)
				}
				sum += line.Points
			}
			if sum != got.Total {
				t.Errorf("breakdown sums to %d but Total = %d", sum, got.Total)
			}
		})
	}
}

func TestEvaluatePointsBreakdownOrderIsStable(t *testing.T) {
	days := []reconcile.DailyReconciliation{
		day("2026-08-03", reconcile.FlagDuplicateOrderRemoved),
		day("2026-08-01"),
	}
	for i := 0; i < 25; i++ {
		got := EvaluatePoints(EvaluateReconciliationBadges(days))
		if got.Breakdown[0].Code != CodeCleanClose || got.Breakdown[1].Code != CodeDiscrepancyCatcher {
			t.Fatalf("breakdown order drifted on run %d: %+v", i, got.Breakdown)
		}
	}
}

// TestApplySpent_NeverReportsNegativeAvailable is the QA round 4 regression:
// GET /api/badges?start&end scopes Total (via the Reconciliation-category
// days it's built from — see RegisterBadgeHandler's own doc comment) while
// Spent is always all-time, so a narrow enough period used to make
// Available = Total - Spent go negative, contradicting the Points struct's
// own "Never negative" claim.
func TestApplySpent_NeverReportsNegativeAvailable(t *testing.T) {
	points := Points{Total: 10}

	got := applySpent(points, 25)

	if got.Spent != 25 {
		t.Fatalf("Spent = %d, want 25 (recorded as-is, not clamped)", got.Spent)
	}
	if got.Available != 0 {
		t.Fatalf("Available = %d, want 0 — Total (10) - Spent (25) must clamp at zero, never go negative", got.Available)
	}
}

func TestApplySpent_OrdinaryCaseIsPlainSubtraction(t *testing.T) {
	got := applySpent(Points{Total: 100}, 40)

	if got.Spent != 40 || got.Available != 60 {
		t.Fatalf("got Spent=%d Available=%d, want Spent=40 Available=60", got.Spent, got.Available)
	}
}

// TestBuildResponseWireContract pins the exact JSON shape the frontend's
// points card reads.
func TestBuildResponseWireContract(t *testing.T) {
	body, err := json.Marshal(BuildResponse([]reconcile.DailyReconciliation{
		day("2026-08-01"),
		day("2026-08-03", reconcile.FlagDuplicateOrderRemoved),
	}, nil, nil))
	if err != nil {
		t.Fatalf("marshalling response: %v", err)
	}

	var decoded struct {
		Badges []Badge `json:"badges"`
		Points struct {
			Total     int `json:"total"`
			Breakdown []struct {
				Code       string `json:"code"`
				Name       string `json:"name"`
				Count      int    `json:"count"`
				PointsEach int    `json:"points_each"`
				Points     int    `json:"points"`
			} `json:"breakdown"`
		} `json:"points"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(decoded.Badges) != 2 {
		t.Errorf("len(badges) = %d, want 2", len(decoded.Badges))
	}
	if decoded.Points.Total != PointsCleanClose+PointsDiscrepancyCatcher {
		t.Errorf("points.total = %d, want %d", decoded.Points.Total, PointsCleanClose+PointsDiscrepancyCatcher)
	}
	if len(decoded.Points.Breakdown) != 2 {
		t.Fatalf("len(points.breakdown) = %d, want 2", len(decoded.Points.Breakdown))
	}
	if decoded.Points.Breakdown[0].Name != "Clean Close" {
		t.Errorf("first breakdown line Name = %q, want %q", decoded.Points.Breakdown[0].Name, "Clean Close")
	}
}
