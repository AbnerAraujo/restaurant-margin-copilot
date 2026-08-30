// Package platformconnector is the product's revenue-source integration
// layer: one internal interface over iFood, Just Eat Takeaway and the
// in-house POS, with all three upstreams currently EMULATED.
//
// # This is a simulation, and it says so everywhere
//
// This project has no iFood partner-API credentials, no Just Eat
// Takeaway partner-API credentials, and no POS terminal to poll, and will
// not have them. Every order this package returns is generated locally by
// a seeded pseudorandom model (seed.go). Nothing here talks to a network,
// and no number here is a real settlement figure.
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
// The connector layer itself. The three mock upstreams emit genuinely
// different wire formats — different envelopes, pagination, money
// representations, timestamp encodings, and status vocabularies (see
// ifood_mock.go, jet_mock.go and pos_mock.go) — and each adapter does the
// real work of normalizing its own format into internal/ingest's
// DeliveryRecord or POSRecord, the exact types
// ingest.ParseDeliveryExport and ingest.ParsePOSExport already produce
// from a CSV. When real credentials exist, a real client replaces a mock
// behind the same interface and nothing downstream changes.
//
// The cross-source deduplication in dedup.go is also real, and is the
// reason the POS upstream exists as more than a third data feed. A POS
// that integrates with a delivery aggregator records the aggregator's
// orders as its own tickets, so the same real-world order arrives twice —
// once in the platform's settlement feed, once in the POS's ticket feed.
// Summing both inflates gross sales every single day. dedup.go finds
// those pairs with plain, auditable Go, refuses to guess when the
// evidence is ambiguous, and never removes a ticket without saying so
// (specs/012-pos-connector-dedup).
//
// # What this package never does
//
// No arithmetic that reaches a margin figure. Records leave here and go
// straight into internal/reconcile, unchanged and indistinguishable from
// CSV-sourced records except by their provenance. No model call, anywhere,
// at any point — the matcher is integer comparison and string equality,
// not a similarity score (Constitution Principle I).
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

// Platform identifies one revenue source this connector can fetch from —
// two delivery platforms and the in-house POS. Exactly three exist; this
// is deliberately a closed set rather than a plugin registry (spec 010
// Assumptions, carried forward by spec 012 — a generic registry for three
// entries would be architecture for its own sake, which CLAUDE.md lists
// as a non-goal).
type Platform string

const (
	PlatformIFood           Platform = "ifood"
	PlatformJustEatTakeaway Platform = "just_eat_takeaway"

	// PlatformPOS is the in-house point-of-sale terminal: dine-in,
	// counter, and — the reason dedup.go exists — the delivery-platform
	// orders an integrated POS records a second time as its own tickets.
	// It is not a delivery platform and never produces a
	// DeliveryRecord; see client.go's POSClient for why it is a peer of
	// Client rather than an implementation of it.
	PlatformPOS Platform = "pos"
)

// AllPlatforms is the full, ordered set — ordered so a sync over "every
// platform" produces records in a stable sequence run to run, which is
// part of what makes a re-run byte-identical (spec 010 FR-005).
//
// POS is LAST on purpose. dedup.go resolves a duplicate in favour of the
// delivery-platform record (spec 012 FR-013), and having the delivery
// feeds already in hand when the POS answers keeps the fetch order and
// the resolution order telling the same story.
var AllPlatforms = []Platform{PlatformIFood, PlatformJustEatTakeaway, PlatformPOS}

// DeliveryPlatforms is AllPlatforms minus the POS: the sources that
// produce an ingest.DeliveryRecord. Used where "a delivery platform" is
// the meaningful set — the dedup matcher's channel vocabulary, and the
// POS mock's own model of which aggregator it is integrated with.
var DeliveryPlatforms = []Platform{PlatformIFood, PlatformJustEatTakeaway}

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
// FOURTH revenue bucket: the platform comparison page, the
// compare_platform_economics MCP tool, and every chat answer about
// platform economics would all keep working, and all quietly report a
// platform that had half its orders missing.
//
// "POS" is here for the same reason and with the same constraint —
// normalizeSourceName lowercases it to "pos", the key
// reconcile.computeOneDay already writes in-house revenue under for the
// CSV path — even though a POS record never carries a Platform field of
// its own. It is what the UI and the sync summary display.
func (p Platform) DisplayName() string {
	switch p {
	case PlatformIFood:
		return "iFood"
	case PlatformJustEatTakeaway:
		return "Just Eat Takeaway"
	case PlatformPOS:
		return "POS"
	default:
		return string(p)
	}
}

// IsDelivery reports whether this source produces delivery-platform
// records (and therefore commission, payouts and refunds) rather than
// in-house POS tickets.
func (p Platform) IsDelivery() bool { return p != PlatformPOS }

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
