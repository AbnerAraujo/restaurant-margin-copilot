package platformconnector

import (
	"encoding/json"
	"testing"
	"time"
)

// The whole justification for this feature is that a proxy reconciling two
// genuinely different upstream APIs is worth building and worth showing.
// If the two mocks ever drifted toward the same shape — a well-meaning
// refactor extracting "the common order DTO", say — the proxy would still
// pass every other test in this package while having nothing left to do.
//
// This test asserts on the RAW JSON BYTES, not on Go types, because the
// heterogeneity has to exist on the wire for the normalization to be real
// work rather than a struct copy. It is deliberately brittle in exactly
// one direction: it fails the moment the two formats converge.
func TestMockUpstreams_EmitGenuinelyDifferentWireShapes(t *testing.T) {
	date := time.Date(2026, 8, 20, 0, 0, 0, 0, merchantZone)

	ifoodRaw, err := ifoodUpstream{}.getOrders(date, 1)
	if err != nil {
		t.Fatalf("iFood upstream: %v", err)
	}
	jetRaw, err := jetUpstream{}.getOrders(date, "")
	if err != nil {
		t.Fatalf("Just Eat Takeaway upstream: %v", err)
	}

	var ifoodEnvelope, jetEnvelope map[string]any
	if err := json.Unmarshal(ifoodRaw, &ifoodEnvelope); err != nil {
		t.Fatalf("iFood payload is not JSON: %v", err)
	}
	if err := json.Unmarshal(jetRaw, &jetEnvelope); err != nil {
		t.Fatalf("Just Eat Takeaway payload is not JSON: %v", err)
	}

	t.Run("envelope and pagination differ", func(t *testing.T) {
		requireKey(t, ifoodEnvelope, "orders")
		requireKey(t, ifoodEnvelope, "page")
		requireNoKey(t, ifoodEnvelope, "data")
		requireNoKey(t, ifoodEnvelope, "cursor")

		requireKey(t, jetEnvelope, "data")
		requireKey(t, jetEnvelope, "cursor")
		requireNoKey(t, jetEnvelope, "orders")
		requireNoKey(t, jetEnvelope, "page")
	})

	ifoodOrder := firstItem(t, ifoodEnvelope, "orders")
	jetOrder := firstItem(t, jetEnvelope, "data")

	t.Run("field naming differs", func(t *testing.T) {
		requireKey(t, ifoodOrder, "id")            // snake_case, short
		requireKey(t, ifoodOrder, "created_at")    //
		requireKey(t, ifoodOrder, "net_payout")    //
		requireKey(t, jetOrder, "orderReference")  // camelCase
		requireKey(t, jetOrder, "placedAtEpochMs") //
		requireKey(t, jetOrder, "payoutMinor")     //
	})

	t.Run("money representation differs", func(t *testing.T) {
		// iFood: a nested {currency, amount} object whose amount is a
		// decimal STRING.
		total, ok := ifoodOrder["total"].(map[string]any)
		if !ok {
			t.Fatalf("iFood total is %T, expected a nested object", ifoodOrder["total"])
		}
		if _, ok := total["amount"].(string); !ok {
			t.Fatalf("iFood total.amount is %T, expected a decimal string", total["amount"])
		}

		// Just Eat Takeaway: a bare integer in minor units.
		gross, ok := jetOrder["grossAmountMinor"].(float64) // JSON numbers decode as float64
		if !ok {
			t.Fatalf("JET grossAmountMinor is %T, expected a JSON number", jetOrder["grossAmountMinor"])
		}
		if gross != float64(int64(gross)) {
			t.Fatalf("JET grossAmountMinor %v is not a whole number of minor units", gross)
		}
	})

	t.Run("timestamp encoding differs", func(t *testing.T) {
		createdAt, ok := ifoodOrder["created_at"].(string)
		if !ok {
			t.Fatalf("iFood created_at is %T, expected an RFC 3339 string", ifoodOrder["created_at"])
		}
		if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
			t.Fatalf("iFood created_at %q is not RFC 3339: %v", createdAt, err)
		}
		if _, ok := jetOrder["placedAtEpochMs"].(float64); !ok {
			t.Fatalf("JET placedAtEpochMs is %T, expected epoch milliseconds as a number", jetOrder["placedAtEpochMs"])
		}
	})

	t.Run("commission rate is reported by one platform and not the other", func(t *testing.T) {
		commission, ok := ifoodOrder["commission"].(map[string]any)
		if !ok {
			t.Fatalf("iFood commission is %T, expected a nested object", ifoodOrder["commission"])
		}
		requireKey(t, commission, "rate_percent")

		// This is the one the JET adapter has to derive. If a rate ever
		// appears here, jet_mock.go's derivation stops being exercised and
		// the reason it exists stops being demonstrated.
		requireNoKey(t, jetOrder, "commissionRatePercent")
		requireNoKey(t, jetOrder, "commissionRate")
		requireNoKey(t, jetOrder, "rate_percent")
	})

	t.Run("status vocabularies do not overlap", func(t *testing.T) {
		ifoodStatus, _ := ifoodOrder["status"].(string)
		jetStatus, _ := jetOrder["fulfilmentState"].(string)
		if ifoodStatus != "CONCLUDED" && ifoodStatus != "CANCELLED" {
			t.Fatalf("iFood status %q is outside its documented vocabulary", ifoodStatus)
		}
		if jetStatus != "DELIVERED" && jetStatus != "REFUNDED" {
			t.Fatalf("JET fulfilmentState %q is outside its documented vocabulary", jetStatus)
		}
		if ifoodStatus == jetStatus {
			t.Fatalf("both platforms reported status %q — the vocabularies have converged", ifoodStatus)
		}
	})
}

// A day's orders must span more than one page in both mocks, or the
// pagination code paths (page numbers in one, opaque cursors in the other)
// are never exercised by any other test in this package.
func TestMockUpstreams_PaginateAcrossMultiplePages(t *testing.T) {
	date := time.Date(2026, 8, 20, 0, 0, 0, 0, merchantZone)

	ifoodRaw, err := ifoodUpstream{}.getOrders(date, 1)
	if err != nil {
		t.Fatalf("iFood upstream: %v", err)
	}
	var ifoodResp ifoodOrdersResponse
	if err := json.Unmarshal(ifoodRaw, &ifoodResp); err != nil {
		t.Fatalf("decoding iFood: %v", err)
	}
	if ifoodResp.Page.TotalPages < 2 {
		t.Fatalf("iFood reported %d page(s) for %s — pagination is not being exercised", ifoodResp.Page.TotalPages, date.Format(dateLayout))
	}

	jetRaw, err := jetUpstream{}.getOrders(date, "")
	if err != nil {
		t.Fatalf("JET upstream: %v", err)
	}
	var jetResp jetOrdersResponse
	if err := json.Unmarshal(jetRaw, &jetResp); err != nil {
		t.Fatalf("decoding JET: %v", err)
	}
	if !jetResp.Cursor.HasMore {
		t.Fatalf("JET reported no further pages for %s — cursor pagination is not being exercised", date.Format(dateLayout))
	}
	if jetResp.Cursor.Next == "" {
		t.Fatal("JET reported hasMore with an empty cursor")
	}
}

func requireKey(t *testing.T, obj map[string]any, key string) {
	t.Helper()
	if _, ok := obj[key]; !ok {
		t.Fatalf("expected key %q in payload, got keys %v", key, keysOf(obj))
	}
}

func requireNoKey(t *testing.T, obj map[string]any, key string) {
	t.Helper()
	if _, ok := obj[key]; ok {
		t.Fatalf("key %q must NOT be present — the two wire formats have converged", key)
	}
}

func firstItem(t *testing.T, envelope map[string]any, key string) map[string]any {
	t.Helper()
	list, ok := envelope[key].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("expected a non-empty %q array", key)
	}
	item, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("expected %q[0] to be an object, got %T", key, list[0])
	}
	return item
}

func keysOf(obj map[string]any) []string {
	out := make([]string, 0, len(obj))
	for k := range obj {
		out = append(out, k)
	}
	return out
}
