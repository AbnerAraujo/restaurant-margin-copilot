package platformconnector

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, merchantZone)
}

func requireErrorContaining(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

// cmd/gendata's own contract — "deterministic — same seed, same dataset,
// every regen" — applied to the connector. An evaluator re-running the
// demo, or a test re-running a sync, must see the numbers the demo showed.
//
// The second half of this test is the part cmd/gendata's single-stream
// seed would fail: a connector fetch is random access, so the same day
// must return the same orders whether it was fetched alone, fetched as
// part of a longer range, or fetched after the other platform. See
// dayRNG's doc comment.
func TestFetchDeliveryRevenue_IsDeterministicPerPlatformDay(t *testing.T) {
	ctx := context.Background()
	proxy := NewSimulatedProxy()
	target := day(2026, 8, 20)

	t.Run("same day fetched twice is identical", func(t *testing.T) {
		first, err := proxy.FetchRange(ctx, target, target, AllPlatforms)
		if err != nil {
			t.Fatalf("first fetch: %v", err)
		}
		second, err := proxy.FetchRange(ctx, target, target, AllPlatforms)
		if err != nil {
			t.Fatalf("second fetch: %v", err)
		}
		assertRecordsEqual(t, first.Records, second.Records)
		if len(first.Records) == 0 {
			t.Fatal("fetched zero records — a determinism test over an empty result proves nothing")
		}
	})

	t.Run("a day is unaffected by what was fetched before it", func(t *testing.T) {
		alone, err := proxy.FetchRange(ctx, target, target, AllPlatforms)
		if err != nil {
			t.Fatalf("single-day fetch: %v", err)
		}
		// The same day, but reached at the END of a five-day range and
		// after both platforms have already been walked. With a shared
		// random stream this is exactly where the numbers would diverge.
		inRange, err := proxy.FetchRange(ctx, day(2026, 8, 16), target, AllPlatforms)
		if err != nil {
			t.Fatalf("range fetch: %v", err)
		}
		assertRecordsEqual(t, alone.Records, recordsOn(inRange.Records, target))
	})

	t.Run("a day is unaffected by which platform was fetched first", func(t *testing.T) {
		forward, err := proxy.FetchRange(ctx, target, target, []Platform{PlatformIFood, PlatformJustEatTakeaway})
		if err != nil {
			t.Fatalf("forward: %v", err)
		}
		reversed, err := proxy.FetchRange(ctx, target, target, []Platform{PlatformJustEatTakeaway, PlatformIFood})
		if err != nil {
			t.Fatalf("reversed: %v", err)
		}
		assertRecordsEqual(t,
			recordsFor(forward.Records, PlatformIFood),
			recordsFor(reversed.Records, PlatformIFood))
		assertRecordsEqual(t,
			recordsFor(forward.Records, PlatformJustEatTakeaway),
			recordsFor(reversed.Records, PlatformJustEatTakeaway))
	})
}

// A simulated day must look like a real restaurant day at this dataset's
// scale, not like an obvious outlier sitting next to CSV-ingested days on
// the Close page (spec FR-006). This asserts the band, not a figure —
// pinning an exact total would make every future tuning change a test
// failure for no correctness reason.
func TestSimulatedDay_LandsAtTheDatasetsOwnScale(t *testing.T) {
	res, err := NewSimulatedProxy().FetchRange(context.Background(), day(2026, 8, 20), day(2026, 8, 20), AllPlatforms)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	var grossCents int64
	for _, rec := range res.Records {
		if rec.Status == "completed" {
			grossCents += rec.SubtotalCents
		}
	}
	// Two platforms at 16-29 orders each, ~$32 mean ticket: roughly
	// $1,000-$2,400 of delivery gross on a day. cmd/gendata's own curve
	// puts the two platforms together at about a third of a $4,100 day.
	if grossCents < 80_000 || grossCents > 300_000 {
		t.Errorf("simulated delivery gross for one day is %d cents, outside the plausible $800-$3,000 band — the scale constants have drifted from cmd/gendata's", grossCents)
	}
}

func TestFetchRange_Refusals(t *testing.T) {
	ctx := context.Background()
	proxy := NewSimulatedProxy()

	t.Run("inverted range", func(t *testing.T) {
		_, err := proxy.FetchRange(ctx, day(2026, 8, 20), day(2026, 8, 18), AllPlatforms)
		requireErrorContaining(t, err, "runs backwards")
	})

	t.Run("range longer than the cap", func(t *testing.T) {
		from := day(2026, 1, 1)
		_, err := proxy.FetchRange(ctx, from, from.AddDate(0, 0, maxSyncDays), AllPlatforms)
		requireErrorContaining(t, err, "more than the 31-day limit")
	})

	t.Run("no platforms requested", func(t *testing.T) {
		_, err := proxy.FetchRange(ctx, day(2026, 8, 20), day(2026, 8, 20), nil)
		requireErrorContaining(t, err, "no platforms requested")
	})

	t.Run("unregistered platform", func(t *testing.T) {
		_, err := proxy.FetchRange(ctx, day(2026, 8, 20), day(2026, 8, 20), []Platform{"deliveroo"})
		requireErrorContaining(t, err, `no connector registered for platform "deliveroo"`)
	})

	t.Run("one platform failing fails the whole range", func(t *testing.T) {
		// Committing a partial range would replace that range's delivery
		// revenue with a fraction of it: margin down, no flag, no way for
		// anything downstream to tell that from a genuinely bad week.
		partial, err := NewProxy(NewIFoodClient(), failingClient{platform: PlatformJustEatTakeaway})
		if err != nil {
			t.Fatalf("NewProxy: %v", err)
		}
		_, err = partial.FetchRange(ctx, day(2026, 8, 20), day(2026, 8, 20), AllPlatforms)
		requireErrorContaining(t, err, "upstream is unavailable")
	})
}

func TestNewProxy_RefusesDuplicatePlatforms(t *testing.T) {
	_, err := NewProxy(NewIFoodClient(), NewIFoodClient())
	requireErrorContaining(t, err, `two clients registered for platform "ifood"`)
}

// ParsePlatform is what turns an API request's platform string into a
// fetch. An unknown key must be named, not skipped — a silently smaller
// result set is indistinguishable from "that platform had no orders".
func TestParsePlatform(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Platform
	}{
		{"ifood", PlatformIFood},
		{"  iFood ", PlatformIFood},
		{"just_eat_takeaway", PlatformJustEatTakeaway},
	} {
		got, err := ParsePlatform(tc.in)
		if err != nil {
			t.Errorf("ParsePlatform(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParsePlatform(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	_, err := ParsePlatform("uber_eats")
	requireErrorContaining(t, err, `unknown platform "uber_eats"`)
}

// The contract check is what makes Client a contract rather than a
// convention: the day a real client replaces a mock, its first mistake
// must surface at the boundary by name, not downstream as a wrong margin.
//
// Each case below is a real failure mode, not a synthetic one — every one
// of them is something the two mocks in this package genuinely could get
// wrong, given that they disagree on the wire about all of them.
func TestProxy_RefusesContractViolations(t *testing.T) {
	ctx := context.Background()
	target := day(2026, 8, 20)
	base := ingest.DeliveryRecord{
		Ref:               ingest.SourceRowRef{File: "simulated://test/orders?date=2026-08-20&page=1", Row: 1},
		Platform:          "iFood",
		OrderID:           "IFOOD-SIM-TEST-0001",
		OrderDate:         target,
		OrderTime:         "19:35",
		SubtotalCents:     4200,
		CommissionRateBps: 2300,
		CommissionCents:   966,
		NetPayoutCents:    3234,
		Status:            "completed",
	}

	for _, tc := range []struct {
		name    string
		mutate  func(r *ingest.DeliveryRecord)
		wantErr string
	}{
		{
			name:    "wrong platform name opens a third revenue bucket",
			mutate:  func(r *ingest.DeliveryRecord) { r.Platform = "IFood Brasil" },
			wantErr: `platform is "IFood Brasil", expected "iFood"`,
		},
		{
			name:    "record filed under a day that was not requested",
			mutate:  func(r *ingest.DeliveryRecord) { r.OrderDate = target.AddDate(0, 0, 1) },
			wantErr: "order date is 2026-08-21 but 2026-08-20 was requested",
		},
		{
			name:    "provenance that does not disclose the simulation",
			mutate:  func(r *ingest.DeliveryRecord) { r.Ref.File = "delivery_platform_export.csv" },
			wantErr: "does not carry the simulated:// scheme",
		},
		{
			name:    "commission that disagrees with the reported rate",
			mutate:  func(r *ingest.DeliveryRecord) { r.CommissionCents = 840 },
			wantErr: "does not match subtotal",
		},
		{
			name:    "payout that is not subtotal minus commission",
			mutate:  func(r *ingest.DeliveryRecord) { r.NetPayoutCents = 4200 },
			wantErr: "is not subtotal",
		},
		{
			name: "a refund left positive — the sign trap",
			mutate: func(r *ingest.DeliveryRecord) {
				refund := target
				r.Status = "refunded"
				r.RefundDate = &refund
				// Subtotal deliberately left positive: exactly what the
				// iFood adapter would produce if it forgot to negate.
			},
			wantErr: "a reversal must be negative",
		},
		{
			name: "a refund with no refund date",
			mutate: func(r *ingest.DeliveryRecord) {
				r.Status = "refunded"
				r.SubtotalCents, r.CommissionCents, r.NetPayoutCents = -4200, -966, -3234
			},
			wantErr: "no refund date is set",
		},
		{
			name:    "an untranslated upstream status",
			mutate:  func(r *ingest.DeliveryRecord) { r.Status = "CONCLUDED" },
			wantErr: `status "CONCLUDED" is neither completed nor refunded`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := base
			tc.mutate(&rec)

			proxy, err := NewProxy(fixedClient{platform: PlatformIFood, records: []ingest.DeliveryRecord{rec}})
			if err != nil {
				t.Fatalf("NewProxy: %v", err)
			}
			_, err = proxy.FetchRange(ctx, target, target, []Platform{PlatformIFood})
			requireErrorContaining(t, err, tc.wantErr)
		})
	}

	t.Run("the unmutated record passes", func(t *testing.T) {
		proxy, err := NewProxy(fixedClient{platform: PlatformIFood, records: []ingest.DeliveryRecord{base}})
		if err != nil {
			t.Fatalf("NewProxy: %v", err)
		}
		if _, err := proxy.FetchRange(ctx, target, target, []Platform{PlatformIFood}); err != nil {
			t.Fatalf("a contract-abiding record was rejected: %v", err)
		}
	})
}

// --- Test doubles -----------------------------------------------------------

type fixedClient struct {
	platform Platform
	records  []ingest.DeliveryRecord
}

func (c fixedClient) Platform() Platform { return c.platform }
func (c fixedClient) Describe() Description {
	return Description{Platform: c.platform, Simulated: true}
}
func (c fixedClient) FetchDeliveryRevenue(context.Context, time.Time) ([]ingest.DeliveryRecord, error) {
	return c.records, nil
}

type failingClient struct{ platform Platform }

func (c failingClient) Platform() Platform { return c.platform }
func (c failingClient) Describe() Description {
	return Description{Platform: c.platform, Simulated: true}
}
func (c failingClient) FetchDeliveryRevenue(context.Context, time.Time) ([]ingest.DeliveryRecord, error) {
	return nil, errUpstreamUnavailable
}

var errUpstreamUnavailable = errUnavailable{}

type errUnavailable struct{}

func (errUnavailable) Error() string { return "platformconnector: upstream is unavailable" }

// --- Helpers ----------------------------------------------------------------

func assertRecordsEqual(t *testing.T, got, want []ingest.DeliveryRecord) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("record count differs: %d vs %d", len(got), len(want))
	}
	for i := range got {
		if !recordsIdentical(got[i], want[i]) {
			t.Fatalf("record %d differs:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func recordsIdentical(a, b ingest.DeliveryRecord) bool {
	if (a.RefundDate == nil) != (b.RefundDate == nil) {
		return false
	}
	if a.RefundDate != nil && !a.RefundDate.Equal(*b.RefundDate) {
		return false
	}
	return a.Ref == b.Ref &&
		a.Platform == b.Platform &&
		a.OrderID == b.OrderID &&
		a.OrderDate.Equal(b.OrderDate) &&
		a.OrderTime == b.OrderTime &&
		a.SubtotalCents == b.SubtotalCents &&
		a.CommissionRateBps == b.CommissionRateBps &&
		a.CommissionCents == b.CommissionCents &&
		a.NetPayoutCents == b.NetPayoutCents &&
		a.Status == b.Status &&
		a.CampaignID == b.CampaignID &&
		a.Notes == b.Notes
}

func recordsOn(records []ingest.DeliveryRecord, date time.Time) []ingest.DeliveryRecord {
	var out []ingest.DeliveryRecord
	key := date.Format(dateLayout)
	for _, r := range records {
		if r.OrderDate.Format(dateLayout) == key {
			out = append(out, r)
		}
	}
	return out
}

func recordsFor(records []ingest.DeliveryRecord, platform Platform) []ingest.DeliveryRecord {
	var out []ingest.DeliveryRecord
	for _, r := range records {
		if r.Platform == platform.DisplayName() {
			out = append(out, r)
		}
	}
	return out
}
