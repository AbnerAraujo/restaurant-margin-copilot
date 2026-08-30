// Package platformconnector is the product's delivery-platform integration
// layer: one internal interface over iFood and Just Eat Takeaway, with both
// upstreams currently EMULATED.
//
// # This is a simulation, and it says so everywhere
//
// This project has no iFood partner-API credentials and no Just Eat
// Takeaway partner-API credentials, and will not have them. Every order
// this package returns is generated locally by a seeded pseudorandom
// model (seed.go). Nothing here talks to a network, and no number here is
// a real settlement figure.
//
// That fact is disclosed in five independent places, deliberately
// redundantly, because this product's entire claim is that it would rather
// refuse than be confidently wrong (CLAUDE.md): the package doc you are
// reading; every record's provenance string, which carries a
// "simulated://" scheme (enforced, not merely intended — see
// proxy.go's checkContract); the API responses, which
// carry a top-level "simulated": true; the UI tab label; and a persistent
// notice above the UI controls. Remove any one of them and the disclosure
// is still present.
//
// # What is real
//
// The connector layer itself. The two mock upstreams emit genuinely
// different wire formats — different envelopes, pagination, money
// representations, timestamp encodings, and status vocabularies (see
// ifood_mock.go and jet_mock.go) — and each adapter does the real work of
// normalizing its own format into internal/ingest.DeliveryRecord, the
// exact type ingest.ParseDeliveryExport already produces from a CSV. When
// real credentials exist, a real client replaces a mock behind the same
// Client interface and nothing downstream changes.
//
// # What this package never does
//
// No arithmetic that reaches a margin figure. Records leave here and go
// straight into internal/reconcile, unchanged and indistinguishable from
// CSV-sourced records except by their provenance. No model call, anywhere,
// at any point (Constitution Principle I).
package platformconnector

import (
	"fmt"
	"strings"
	"time"
)

// dateLayout is the YYYY-MM-DD convention every date string in this
// product uses (internal/mcptools, internal/httpapi, cmd/gendata).
const dateLayout = "2006-01-02"

// merchantZone is the restaurant's own wall-clock zone, and the zone every
// calendar date in this product is expressed in.
//
// It matters more than it looks. internal/reconcile groups a day by
// DeliveryRecord.OrderDate, so "which day did this order belong to" is
// decided here, at normalization time, and cannot be corrected later. The
// two upstreams hand that decision over in incompatible forms: the iFood
// mock sends a timestamp that already carries this offset, while the Just
// Eat Takeaway mock sends epoch milliseconds in UTC. Reading a 21:30 local
// order out of the JET feed without converting first would file it under
// the NEXT calendar day (00:30 UTC), moving real revenue between two days'
// margins with nothing anywhere to flag it. jet_mock.go converts; there is
// a test for exactly that order (TestJETAdapter_LateEveningOrderKeepsItsLocalDay).
//
// A fixed offset rather than a tzdata location because this is a
// prototype with one restaurant in one place, and a fixed zone has no
// dependency on the host's tzdata being present.
var merchantZone = time.FixedZone("BRT", -3*60*60)

// Platform identifies one delivery platform. Exactly two exist; this is
// deliberately a closed set rather than a plugin registry (spec.md
// Assumptions — the requirement names two platforms, and a generic
// registry for two entries would be architecture for its own sake, which
// CLAUDE.md lists as a non-goal).
type Platform string

const (
	PlatformIFood           Platform = "ifood"
	PlatformJustEatTakeaway Platform = "just_eat_takeaway"
)

// AllPlatforms is the full, ordered set — ordered so a sync over "every
// platform" produces records in a stable sequence run to run, which is
// part of what makes a re-run byte-identical (spec FR-005).
var AllPlatforms = []Platform{PlatformIFood, PlatformJustEatTakeaway}

// DisplayName is the string written into ingest.DeliveryRecord.Platform.
//
// These two values are load-bearing and must not be prettified.
// internal/reconcile.normalizeSourceName lowercases and replaces spaces
// with underscores to derive the keys of GrossSalesBySource,
// CommissionsBySource, and RefundsBySource — so "iFood" becomes "ifood"
// and "Just Eat Takeaway" becomes "just_eat_takeaway", which are exactly
// the keys the CSV-ingested dataset already produces (its own
// delivery_platform_export.csv uses these same two display strings).
// Writing "IFood", "iFood Brasil", or "JustEat" here would silently open a
// THIRD revenue bucket: the platform comparison page, the
// compare_platform_economics MCP tool, and every chat answer about
// platform economics would all keep working, and all quietly report a
// platform that had half its orders missing.
func (p Platform) DisplayName() string {
	switch p {
	case PlatformIFood:
		return "iFood"
	case PlatformJustEatTakeaway:
		return "Just Eat Takeaway"
	default:
		return string(p)
	}
}

// ParsePlatform resolves a platform key from an API request. Unknown keys
// are refused by name rather than ignored: a sync request naming a
// platform this product cannot fetch would otherwise return a smaller
// result set than the caller asked for, and the caller would have no way
// to tell that from "that platform had no orders" (Constitution Principle
// II).
func ParsePlatform(s string) (Platform, error) {
	key := strings.ToLower(strings.TrimSpace(s))
	for _, p := range AllPlatforms {
		if string(p) == key {
			return p, nil
		}
	}
	return "", fmt.Errorf("platformconnector: unknown platform %q — this product connects to %s only", s, strings.Join(platformKeys(), " and "))
}

func platformKeys() []string {
	keys := make([]string, 0, len(AllPlatforms))
	for _, p := range AllPlatforms {
		keys = append(keys, string(p))
	}
	return keys
}
