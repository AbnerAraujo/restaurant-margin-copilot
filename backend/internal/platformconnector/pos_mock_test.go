package platformconnector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

func posDay() time.Time {
	return time.Date(2026, 8, 20, 0, 0, 0, 0, merchantZone)
}

// The pt-BR amount trap. money.ParseCents reads "1.234,56" as $1.23 — a
// plausible string silently understated by three orders of magnitude, with
// no error anywhere. A day's POS revenue would collapse and margin would
// follow it down, and nothing in the product would look wrong.
func TestPOSAdapter_PtBRAmountsNormalize(t *testing.T) {
	t.Run("accepted shapes", func(t *testing.T) {
		for _, tc := range []struct {
			in   string
			want int64
		}{
			{"42,00", 4200},
			{"7,50", 750},
			{"1.234,56", 123456},
			{"1.000.000,00", 100000000},
		} {
			got, err := normalizePtBRAmount(tc.in)
			require.NoError(t, err, "input %q", tc.in)
			require.Equal(t, tc.want, got, "input %q", tc.in)
		}
	})

	// Refusals, not best-effort parses. A string this terminal would never
	// emit is a signal the integration is broken, and parsing it anyway is
	// the cheapest possible way to turn that into a wrong margin figure.
	t.Run("refused shapes", func(t *testing.T) {
		for _, in := range []string{
			"",           // nothing
			"1234.56",    // the OTHER locale's notation — reading it here would be a 100x error
			"1.234.56",   // no decimal comma at all
			"1,234,56",   // two commas
			"42,0",       // one decimal digit
			"12.345,678", // three decimal digits
			"1.2345,00",  // not grouped in thousands
			"abc,de",     // not a number
		} {
			_, err := normalizePtBRAmount(in)
			require.Error(t, err, "input %q must be refused, not guessed at", in)
		}
	})

	// Round trip: whatever the mock emits, the adapter reads back exactly.
	t.Run("round trip", func(t *testing.T) {
		for _, cents := range []int64{1, 99, 100, 4200, 99999, 123456, 100000000} {
			got, err := normalizePtBRAmount(formatPtBRAmount(cents))
			require.NoError(t, err)
			require.Equal(t, cents, got)
		}
	})
}

// The zone-less-timestamp trap, and the reason it is dangerous rather than
// obvious: reading "2026-08-20 19:35:00" with time.Parse yields UTC, the
// calendar DATE still comes out right for every ticket this mock emits, and
// nothing downstream looks wrong — while every ticket TIME has silently
// moved by three hours, disabling every amount-and-time match in the
// product and letting duplicate revenue through.
func TestPOSAdapter_TicketTimeIsReadInTheMerchantZone(t *testing.T) {
	orders, err := NewPOSClient().FetchPOSOrders(context.Background(), posDay())
	require.NoError(t, err)
	require.NotEmpty(t, orders)

	for _, o := range orders {
		_, offset := o.PlacedAt.Zone()
		_, want := posDay().Zone()
		require.Equal(t, want, offset, "ticket %s was parsed outside the merchant's zone", o.Record.OrderID)

		// The wall clock the adapter recorded must equal the wall clock
		// the terminal wrote. This is the assertion a time.Parse would
		// fail, and checkPOSContract enforces the same thing on every
		// fetch.
		require.Equal(t, o.PlacedAt.Format("15:04"), o.Record.OrderTime)

		// Every ticket falls in a plausible service window, which is what
		// makes the three-hour shift detectable at all.
		hour := o.PlacedAt.Hour()
		require.True(t, hour >= 11 && hour <= 21, "ticket %s at %02d:00 is outside any service window", o.Record.OrderID, hour)
	}
}

// The controlled-overlap mechanism (spec 012 FR-004, US4.2): echoed
// tickets must be echoes of the ACTUAL orders the iFood mock returns for
// the same date. If they were independently generated, a "duplicate" the
// matcher found would be a coincidence the mock arranged, and the whole
// exercise would be circular.
func TestPOSMock_EchoesRealIFoodOrdersAndNeverJETOrders(t *testing.T) {
	ctx := context.Background()
	date := posDay()

	ifood, err := NewIFoodClient().FetchDeliveryRevenue(ctx, date)
	require.NoError(t, err)
	jet, err := NewJustEatTakeawayClient().FetchDeliveryRevenue(ctx, date)
	require.NoError(t, err)
	tickets, err := NewPOSClient().FetchPOSOrders(ctx, date)
	require.NoError(t, err)

	ifoodByID := map[string]int64{}
	for _, rec := range ifood {
		ifoodByID[rec.OrderID] = abs64(rec.SubtotalCents)
	}
	jetIDs := map[string]bool{}
	for _, rec := range jet {
		jetIDs[rec.OrderID] = true
	}

	var echoed, withRef int
	for _, tk := range tickets {
		if tk.DeliveryPlatform == "" {
			require.Empty(t, tk.PartnerOrderRef, "an in-house ticket must never carry a partner reference")
			continue
		}
		echoed++
		require.Equal(t, PlatformIFood, tk.DeliveryPlatform,
			"only iFood is integrated into this POS — a JET echo would remove the matcher's control group")

		if tk.PartnerOrderRef == "" {
			continue
		}
		withRef++
		require.False(t, jetIDs[tk.PartnerOrderRef], "a POS ticket referenced a Just Eat Takeaway order")
		subtotal, ok := ifoodByID[tk.PartnerOrderRef]
		require.True(t, ok, "POS ticket %s references %s, which is not in the iFood feed for %s — the echo is not an echo",
			tk.Record.OrderID, tk.PartnerOrderRef, date.Format(dateLayout))

		// The amounts agree except where a platform campaign discounted
		// the customer's price below the menu price the POS rang. Both
		// outcomes are real; a third would mean the echo had drifted.
		if tk.Record.GrossCents != subtotal {
			grossedUp := divRoundHalfUp(subtotal*10000, 10000-posCampaignDiscountBps)
			require.Equal(t, grossedUp, tk.Record.GrossCents,
				"POS ticket %s disagrees with iFood order %s for a reason the model does not describe",
				tk.Record.OrderID, tk.PartnerOrderRef)
		}
	}

	require.Equal(t, len(ifood), echoed, "every iFood order for the day should have reached the POS")
	require.Greater(t, withRef, 0, "no echoed ticket carried a reference — the reference tier would never run")
	require.Less(t, withRef, echoed, "every echoed ticket carried a reference — the amount-and-time tier would be decoration")
}

// The control group (spec 012 FR-005, SC-002): a large majority of the
// day's tickets are genuine in-house business the matcher must never
// touch. Without them a rule that over-matches would pass every test.
func TestPOSMock_MajorityOfTicketsAreInHouse(t *testing.T) {
	tickets, err := NewPOSClient().FetchPOSOrders(context.Background(), posDay())
	require.NoError(t, err)

	inHouse := 0
	for _, tk := range tickets {
		if tk.DeliveryPlatform == "" {
			inHouse++
			require.Contains(t, []string{"dine_in", "counter"}, tk.Record.Channel)
		}
	}
	require.Greater(t, inHouse*100/len(tickets), 60,
		"in-house tickets should be the clear majority — cmd/gendata puts POS at 66%% of gross against one platform's 17%%")
}

// The mock's own contract, on the same terms the proxy enforces it.
func TestPOSMock_HonorsTheConnectorContract(t *testing.T) {
	date := posDay()
	orders, err := NewPOSClient().FetchPOSOrders(context.Background(), date)
	require.NoError(t, err)
	require.NotEmpty(t, orders)

	for i, o := range orders {
		require.NoError(t, checkPOSContract(PlatformPOS, date, o))
		require.Equal(t, i+1, o.Record.Ref.Row, "provenance rows must be 1-based and contiguous")
		require.True(t, strings.HasPrefix(o.Record.Ref.File, "simulated://"),
			"provenance %q must be self-evidently synthetic", o.Record.Ref.File)
		require.Contains(t, []string{"completed", "void"}, o.Record.Status)
	}
}

// Determinism, on the same terms the delivery mocks are held to: the same
// date fetched twice returns identical tickets, so a re-synced day
// reconciles to the same margin.
func TestPOSMock_IsDeterministicPerDay(t *testing.T) {
	ctx := context.Background()
	first, err := NewPOSClient().FetchPOSOrders(ctx, posDay())
	require.NoError(t, err)
	second, err := NewPOSClient().FetchPOSOrders(ctx, posDay())
	require.NoError(t, err)
	require.Equal(t, first, second)

	other, err := NewPOSClient().FetchPOSOrders(ctx, posDay().AddDate(0, 0, 1))
	require.NoError(t, err)
	require.NotEqual(t, first, other, "two different days returning identical tickets would mean the seed is not keyed by date")
}

// An adapter that refuses is only useful if it refuses the right things.
func TestPOSAdapter_RefusesRatherThanGuesses(t *testing.T) {
	adapter := posAdapter{}
	ref := ingest.SourceRowRef{File: posEndpoint(), Row: 1}

	t.Run("unknown state", func(t *testing.T) {
		_, err := adapter.normalize(posTicketDTO{
			TicketNumber: "POS-X", OpenedAt: "2026-08-20 19:35:00",
			ServiceType: "DINE_IN", TotalBrl: "42,00", State: "PENDING",
		}, ref)
		require.ErrorContains(t, err, "unrecognized state")
	})

	t.Run("unknown service type", func(t *testing.T) {
		_, err := adapter.normalize(posTicketDTO{
			TicketNumber: "POS-X", OpenedAt: "2026-08-20 19:35:00",
			ServiceType: "CATERING", TotalBrl: "42,00", State: "PAID",
		}, ref)
		require.ErrorContains(t, err, "unrecognized service type")
	})

	// A delivery ticket with no partner block must NOT be quietly
	// reclassified as in-house: that would exempt it from duplicate
	// detection permanently, which is the silent double-count this
	// feature exists to prevent.
	t.Run("delivery ticket with no partner block", func(t *testing.T) {
		_, err := adapter.normalize(posTicketDTO{
			TicketNumber: "POS-X", OpenedAt: "2026-08-20 19:35:00",
			ServiceType: "DELIVERY_PARTNER", TotalBrl: "42,00", State: "PAID",
		}, ref)
		require.ErrorContains(t, err, "no partner block")
	})

	t.Run("unknown delivery partner", func(t *testing.T) {
		_, err := adapter.normalize(posTicketDTO{
			TicketNumber: "POS-X", OpenedAt: "2026-08-20 19:35:00",
			ServiceType: "DELIVERY_PARTNER", TotalBrl: "42,00", State: "PAID",
			Partner: &posPartnerDTO{Name: "Deliveroo"},
		}, ref)
		require.ErrorContains(t, err, "unknown delivery partner")
	})

	t.Run("non-positive total", func(t *testing.T) {
		_, err := adapter.normalize(posTicketDTO{
			TicketNumber: "POS-X", OpenedAt: "2026-08-20 19:35:00",
			ServiceType: "DINE_IN", TotalBrl: "-42,00", State: "PAID",
		}, ref)
		require.ErrorContains(t, err, "not positive")
	})
}

// The wire really is NDJSON with no envelope — asserted on the raw bytes,
// so the format cannot converge on the delivery mocks' over time.
func TestPOSUpstream_EmitsNDJSONWithNoEnvelope(t *testing.T) {
	raw, err := posUpstream{}.dayClose(posDay())
	require.NoError(t, err)

	// A single json.Unmarshal must FAIL: the payload is many documents,
	// not one. That is the structural difference from both delivery mocks.
	var whole map[string]any
	require.Error(t, json.Unmarshal(raw, &whole), "the POS payload decoded as a single JSON object — the NDJSON shape has gone")

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Greater(t, len(lines), 1)
	for _, line := range lines {
		var ticket map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &ticket), "every line must be one complete JSON object")
		require.Contains(t, ticket, "ticket_number")
		require.Contains(t, ticket, "service_type")
		require.NotContains(t, ticket, "cursor")
		require.NotContains(t, ticket, "page")
	}
}
