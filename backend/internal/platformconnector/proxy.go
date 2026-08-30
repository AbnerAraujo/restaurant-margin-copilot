package platformconnector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

const (
	// maxSyncDays bounds a single sync. Constitution: an explicit cap,
	// not an unbounded loop over whatever range a caller sends. A month
	// covers every demo and every realistic "catch up after a holiday"
	// case; a longer backfill is explicitly out of scope (spec.md
	// Assumptions).
	maxSyncDays = 31

	// maxPagesPerDay bounds each adapter's pagination loop. At the mocks'
	// page sizes (12 and 9) a normal day is two or three pages, so 20 is
	// generous headroom while still being a real bound — the difference
	// between "cannot spin forever" and "probably will not".
	maxPagesPerDay = 20

	// commissionToleranceCents is the slack allowed between an upstream's
	// reported commission and this package's recomputation of it. One
	// cent, matching internal/reconcile.computeOneDay's own
	// commission_mismatch tolerance exactly — a different tolerance here
	// would mean the proxy could pass a record that reconciliation then
	// flags, or flag one reconciliation would have accepted.
	commissionToleranceCents = 1
)

// Proxy is the single entry point the rest of the product uses to fetch
// delivery-platform revenue. It dispatches to the right Client per
// platform, walks a date range in a stable order, enforces the caps, and
// — the part that earns its keep — verifies that whatever came back
// actually honors Client's output contract before letting it reach
// internal/reconcile.
//
// The verification is not paranoia about code in this same package. It is
// what makes Client a contract rather than a convention: the day a real
// iFood client replaces the mock, its first mistake surfaces here, by
// name, at the boundary — instead of surfacing as a wall of
// commission_mismatch flags, or worse, as a refund that quietly raised a
// day's margin. Checks 3 and 4 below are spec FR-010's refusal.
type Proxy struct {
	clients    map[Platform]Client
	posClients map[Platform]POSClient
	order      []Platform
}

// NewSimulatedProxy is the production constructor for this prototype: two
// delivery platforms and the in-house POS, all three emulated. Named for
// what it is. A caller that wants a real connector will construct NewProxy
// with a real client and the name at every call site will stop saying
// "simulated", which is the point.
func NewSimulatedProxy() *Proxy {
	p, err := NewProxy(NewIFoodClient(), NewJustEatTakeawayClient(), NewPOSClient())
	if err != nil {
		// Unreachable: the two constructors above return distinct,
		// non-nil platforms. Panicking rather than returning an error
		// keeps the production call site free of an error branch that
		// cannot be taken, the same posture internal/livedata takes for
		// its own impossible init failure.
		panic("platformconnector: NewSimulatedProxy: " + err.Error())
	}
	return p
}

// NewProxy registers connectors in the order given. Each argument must be
// a Client (a delivery platform) or a POSClient; anything else is a wiring
// bug and is refused by name.
//
// Duplicate platforms are refused rather than last-write-wins: two clients
// for one platform is a wiring bug too, and silently picking one would make
// which orders the product sees depend on argument order.
//
// The parameter is `...Connector` rather than two typed slices so a call
// site reads as one list of sources in the order they will be fetched,
// which is also the order dedup.go sees them in.
func NewProxy(clients ...Connector) (*Proxy, error) {
	if len(clients) == 0 {
		return nil, fmt.Errorf("platformconnector: NewProxy needs at least one client")
	}
	p := &Proxy{
		clients:    make(map[Platform]Client, len(clients)),
		posClients: make(map[Platform]POSClient, 1),
	}
	for _, c := range clients {
		platform := c.Platform()
		if p.registered(platform) {
			return nil, fmt.Errorf("platformconnector: two clients registered for platform %q", platform)
		}
		switch typed := c.(type) {
		case Client:
			p.clients[platform] = typed
		case POSClient:
			p.posClients[platform] = typed
		default:
			return nil, fmt.Errorf("platformconnector: connector for platform %q implements neither Client nor POSClient", platform)
		}
		p.order = append(p.order, platform)
	}
	return p, nil
}

func (p *Proxy) registered(platform Platform) bool {
	if _, ok := p.clients[platform]; ok {
		return true
	}
	_, ok := p.posClients[platform]
	return ok
}

// Platforms returns the registered platforms in registration order.
func (p *Proxy) Platforms() []Platform {
	out := make([]Platform, len(p.order))
	copy(out, p.order)
	return out
}

// Describe returns every registered connector's wire-format facts, in
// registration order — what GET /api/connectors/platforms serves. Delivery
// platforms and the POS describe themselves through the same method, so
// the API and the UI need no special case for the new source.
func (p *Proxy) Describe() []Description {
	out := make([]Description, 0, len(p.order))
	for _, platform := range p.order {
		if c, ok := p.clients[platform]; ok {
			out = append(out, c.Describe())
			continue
		}
		out = append(out, p.posClients[platform].Describe())
	}
	return out
}

// PlatformDayTotals is one platform's reported activity for one calendar
// day: what a preview shows, and what the sync response summarizes. Every
// figure is a plain sum over the normalized records — no separate
// computation path, and nothing here feeds a margin (internal/reconcile
// derives that from the records themselves).
type PlatformDayTotals struct {
	Platform        Platform  `json:"platform"`
	PlatformName    string    `json:"platform_name"`
	Date            time.Time `json:"-"`
	OrderCount      int       `json:"order_count"`
	RefundCount     int       `json:"refund_count"`
	GrossCents      int64     `json:"-"`
	RefundsCents    int64     `json:"-"`
	CommissionCents int64     `json:"-"`

	// DuplicatesRemoved and UnresolvedOverlaps are populated on the POS
	// row only, because only POS tickets are ever removed (spec 012
	// FR-013). They are here rather than in a separate structure so a
	// preview table can show, on the row whose count changed, exactly why
	// it changed.
	DuplicatesRemoved  int `json:"duplicates_removed"`
	UnresolvedOverlaps int `json:"unresolved_overlaps"`

	// TradingNote is the simulated day's statable cause when it had one —
	// "Severe weather — couriers scarce and almost no walk-in trade" — and
	// "" on an ordinary day. See seed.go's trading-day condition model.
	//
	// It is a property of the DATE, so every source's row for the same day
	// carries the same note; that repetition is the point, because it is
	// what says the storm hit the whole restaurant rather than one feed.
	// Without it, the variance this model adds would reach the owner as an
	// unexplained dip, which is the one thing this product is not allowed
	// to show them.
	TradingNote string `json:"trading_note,omitempty"`
}

// FetchResult is one range fetch: every normalized record, plus the
// per-platform-per-day summary a human reads before committing anything.
//
// Records and POSRecords are POST-deduplication — what will actually land.
// Computing the totals before the matcher ran would give the owner a
// preview the commit then contradicts, which is a worse failure than
// showing no preview at all.
type FetchResult struct {
	From time.Time
	To   time.Time

	// Records are the delivery-platform records. The matcher never
	// removes or alters one.
	Records []ingest.DeliveryRecord

	// POSRecords are the in-house tickets that survived deduplication,
	// in the exact type ingest.ParsePOSExport produces.
	POSRecords []ingest.POSRecord

	// Decisions is every outcome the matcher reached, including the ones
	// where it deliberately did nothing. Empty when the fetch covered
	// fewer than two sources, because there was nothing to compare.
	Decisions []DedupDecision

	Totals []PlatformDayTotals
}

// DuplicatesRemoved is how many POS tickets the matcher folded into a
// delivery-platform record across the whole fetch.
func (r *FetchResult) DuplicatesRemoved() int {
	n := 0
	for _, d := range r.Decisions {
		if d.Kind.Merged() {
			n++
		}
	}
	return n
}

// UnresolvedOverlaps is how many POS tickets the matcher believed might be
// duplicates but declined to merge. Each one is a possible double-count
// the owner has been told about explicitly.
func (r *FetchResult) UnresolvedOverlaps() int {
	n := 0
	for _, d := range r.Decisions {
		if d.Kind == DedupUnresolvedAmbiguous || d.Kind == DedupUnresolvedNoCounterpart {
			n++
		}
	}
	return n
}

// FetchRange fetches every requested platform for every calendar date in
// [from, to] inclusive.
//
// Ordering is platform-major, then date, then the upstream's own order —
// fixed, so two runs of the same request produce byte-identical output
// (spec FR-005 / SC-002), which is what makes a re-synced day reconcile to
// the same margin.
//
// Any single platform's failure fails the whole call. That is deliberate:
// returning iFood's orders alone for a range would, on commit, replace
// that range's delivery revenue with half of it — Just Eat Takeaway's
// third of gross sales silently gone, margin down, and no flag anywhere
// saying why, because from reconciliation's point of view it simply
// received the records it received. Refusing the range is the only outcome
// that cannot be mistaken for a real business result (spec FR-011).
func (p *Proxy) FetchRange(ctx context.Context, from, to time.Time, platforms []Platform) (*FetchResult, error) {
	from = truncateToDay(from)
	to = truncateToDay(to)

	if to.Before(from) {
		return nil, fmt.Errorf("platformconnector: date range %s..%s runs backwards — the start date must not be after the end date", from.Format(dateLayout), to.Format(dateLayout))
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if days > maxSyncDays {
		return nil, fmt.Errorf("platformconnector: date range %s..%s covers %d days, more than the %d-day limit on a single sync — sync a shorter range", from.Format(dateLayout), to.Format(dateLayout), days, maxSyncDays)
	}
	if len(platforms) == 0 {
		return nil, fmt.Errorf("platformconnector: no platforms requested — name at least one of %s", strings.Join(platformKeys(), ", "))
	}

	// Fetch first, dedupe second, summarize third. The three stages are
	// separated because only the first can fail on an upstream, only the
	// second makes a judgement, and only the third is for display — and
	// keeping them apart is what lets the totals report the post-dedup
	// truth rather than a figure the commit contradicts.
	result := &FetchResult{From: from, To: to}

	fetchedDelivery := make(map[Platform]bool, len(platforms))
	posOrdersByDay := make(map[string][]POSOrder)
	var posDays []string

	for _, platform := range platforms {
		if posClient, ok := p.posClients[platform]; ok {
			for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
				orders, err := posClient.FetchPOSOrders(ctx, day)
				if err != nil {
					return nil, err
				}
				for i, order := range orders {
					if err := checkPOSContract(platform, day, order); err != nil {
						return nil, err
					}
					orders[i] = order
				}
				key := day.Format(dateLayout)
				posOrdersByDay[key] = orders
				posDays = append(posDays, key)
			}
			continue
		}

		client, ok := p.clients[platform]
		if !ok {
			return nil, fmt.Errorf("platformconnector: no connector registered for platform %q", platform)
		}
		fetchedDelivery[platform] = true

		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			records, err := client.FetchDeliveryRevenue(ctx, day)
			if err != nil {
				return nil, err
			}

			totals := PlatformDayTotals{
				Platform:     platform,
				PlatformName: platform.DisplayName(),
				Date:         day,
				TradingNote:  TradingNoteForDate(day),
			}
			for _, rec := range records {
				if err := checkContract(platform, day, rec); err != nil {
					return nil, err
				}
				totals.OrderCount++
				totals.CommissionCents += rec.CommissionCents
				if rec.Status == "refunded" {
					totals.RefundCount++
					totals.RefundsCents += -rec.SubtotalCents
				} else {
					totals.GrossCents += rec.SubtotalCents
				}
			}

			result.Records = append(result.Records, records...)
			result.Totals = append(result.Totals, totals)
		}
	}

	// Deduplication runs per calendar day, over that day's delivery
	// records and that day's POS tickets. Per day rather than per range
	// because matching is scoped to one calendar day (dedup.go), so
	// comparing across days would only widen the candidate sets and make
	// ambiguity — and therefore refusals to merge — more likely for no
	// gain.
	for _, key := range posDays {
		day, err := time.ParseInLocation(dateLayout, key, merchantZone)
		if err != nil {
			return nil, fmt.Errorf("platformconnector: %w", err)
		}

		dayDelivery := recordsOnDay(result.Records, key)
		kept, decisions := dedupeAcrossSources(dayDelivery, posOrdersByDay[key], fetchedDelivery)

		totals := PlatformDayTotals{
			Platform:     PlatformPOS,
			PlatformName: PlatformPOS.DisplayName(),
			Date:         day,
			OrderCount:   len(kept),
			TradingNote:  TradingNoteForDate(day),
		}
		for _, rec := range kept {
			// A voided ticket contributes no gross, exactly as
			// reconcile.computeOneDay will treat it. Counting it here
			// would make the preview disagree with the commit.
			if rec.Status == "" || rec.Status == "completed" {
				totals.GrossCents += rec.GrossCents
			}
		}
		for _, d := range decisions {
			switch {
			case d.Kind.Merged():
				totals.DuplicatesRemoved++
			case d.Kind == DedupUnresolvedAmbiguous || d.Kind == DedupUnresolvedNoCounterpart:
				totals.UnresolvedOverlaps++
			}
		}

		result.POSRecords = append(result.POSRecords, kept...)
		result.Decisions = append(result.Decisions, decisions...)
		result.Totals = append(result.Totals, totals)
	}

	return result, nil
}

// recordsOnDay filters already-fetched delivery records down to one
// calendar day. A linear scan per day rather than a prebuilt index: a
// 31-day sync over three sources is a few thousand records, and an index
// would trade readable code for microseconds nobody is waiting on.
func recordsOnDay(records []ingest.DeliveryRecord, dateKey string) []ingest.DeliveryRecord {
	out := make([]ingest.DeliveryRecord, 0, len(records))
	for _, rec := range records {
		if rec.OrderDate.Format(dateLayout) == dateKey {
			out = append(out, rec)
		}
	}
	return out
}

// checkContract enforces Client's documented output contract on a single
// record. Every failure here is a refusal, never a correction: a proxy
// that silently repaired an upstream's numbers would be estimating, and
// this product does not estimate money (Constitution Principle II).
func checkContract(platform Platform, date time.Time, rec ingest.DeliveryRecord) error {
	reject := func(format string, args ...any) error {
		return fmt.Errorf("platformconnector: %s connector returned a record that violates the connector contract (order %s, %s): %s",
			platform.DisplayName(), rec.OrderID, rec.Ref.File, fmt.Sprintf(format, args...))
	}

	if rec.Platform != platform.DisplayName() {
		// Not cosmetic: internal/reconcile derives its GrossSalesBySource
		// keys from this string, so a wrong value opens a third revenue
		// bucket that every platform-comparison surface would then
		// under-report from without erroring.
		return reject("platform is %q, expected %q", rec.Platform, platform.DisplayName())
	}
	if got, want := rec.OrderDate.Format(dateLayout), date.Format(dateLayout); got != want {
		return reject("order date is %s but %s was requested", got, want)
	}
	if !strings.HasPrefix(rec.Ref.File, "simulated://") {
		// While these upstreams are emulated, provenance must say so.
		// This is the check that makes the honesty requirement
		// structural rather than a habit someone has to remember.
		return reject("provenance %q does not carry the simulated:// scheme", rec.Ref.File)
	}
	if rec.Ref.Row < 1 {
		return reject("provenance row %d is not a 1-based position", rec.Ref.Row)
	}

	recomputed := money.DivRoundHalfUp(rec.SubtotalCents*rec.CommissionRateBps, 10000)
	if abs64(recomputed-rec.CommissionCents) > commissionToleranceCents {
		return reject("reported commission %s does not match subtotal %s at %s bps (recomputed %s)",
			money.FormatCents(rec.CommissionCents), money.FormatCents(rec.SubtotalCents),
			money.FormatCents(rec.CommissionRateBps), money.FormatCents(recomputed))
	}
	if want := rec.SubtotalCents - rec.CommissionCents; rec.NetPayoutCents != want {
		return reject("reported payout %s is not subtotal %s minus commission %s (%s)",
			money.FormatCents(rec.NetPayoutCents), money.FormatCents(rec.SubtotalCents),
			money.FormatCents(rec.CommissionCents), money.FormatCents(want))
	}

	switch rec.Status {
	case "completed":
		if rec.RefundDate != nil {
			return reject("status is completed but a refund date is set")
		}
		if rec.SubtotalCents <= 0 {
			return reject("status is completed but subtotal %s is not positive", money.FormatCents(rec.SubtotalCents))
		}
	case "refunded":
		// The sharp edge. The two mocks disagree about the sign of a
		// refund on the wire; this is where an adapter that failed to
		// normalize is caught, before internal/reconcile turns a
		// positive "refund" into revenue.
		if rec.RefundDate == nil {
			return reject("status is refunded but no refund date is set")
		}
		if rec.SubtotalCents > 0 {
			return reject("status is refunded but subtotal %s is positive — a reversal must be negative in this product's delivery-record convention", money.FormatCents(rec.SubtotalCents))
		}
	default:
		return reject("status %q is neither completed nor refunded", rec.Status)
	}
	return nil
}

// checkPOSContract enforces POSClient's documented output contract on a
// single ticket. Same posture as checkContract: every failure is a
// refusal, never a correction.
//
// The rule with teeth here is the last one. PlacedAt is the input to
// dedup.go's matching window, and an adapter that read a zone-less
// timestamp with time.Parse instead of time.ParseInLocation would shift
// every ticket by the merchant's UTC offset while leaving the calendar
// date intact — so nothing downstream would look wrong, no amount-and-time
// match would ever fire again, and duplicate revenue would flow straight
// into gross sales. Checking that PlacedAt's wall clock agrees with the
// OrderTime string is what makes that failure loud.
func checkPOSContract(platform Platform, date time.Time, order POSOrder) error {
	rec := order.Record
	reject := func(format string, args ...any) error {
		return fmt.Errorf("platformconnector: %s connector returned a ticket that violates the connector contract (ticket %s, %s): %s",
			platform.DisplayName(), rec.OrderID, rec.Ref.File, fmt.Sprintf(format, args...))
	}

	if got, want := rec.OrderDate.Format(dateLayout), date.Format(dateLayout); got != want {
		return reject("ticket date is %s but %s was requested", got, want)
	}
	if !strings.HasPrefix(rec.Ref.File, "simulated://") {
		return reject("provenance %q does not carry the simulated:// scheme", rec.Ref.File)
	}
	if rec.Ref.Row < 1 {
		return reject("provenance row %d is not a 1-based position", rec.Ref.Row)
	}
	if rec.GrossCents <= 0 {
		return reject("gross %s is not positive — this connector models no POS refund", money.FormatCents(rec.GrossCents))
	}
	if order.DeliveryPlatform != "" && !order.DeliveryPlatform.IsDelivery() {
		return reject("claims to have arrived through %q, which is not a delivery platform", order.DeliveryPlatform)
	}
	if order.DeliveryPlatform == "" && order.PartnerOrderRef != "" {
		return reject("carries a delivery-partner order reference %q but names no delivery platform — a reference the matcher cannot attribute is worse than none, because it looks like evidence", order.PartnerOrderRef)
	}
	if got, want := order.PlacedAt.Format(dateLayout), date.Format(dateLayout); got != want {
		return reject("ticket time %s falls on %s, not the requested %s", order.PlacedAt.Format(time.RFC3339), got, want)
	}
	if got := order.PlacedAt.Format("15:04"); got != rec.OrderTime {
		return reject("ticket time %s disagrees with the recorded order time %s — the timestamp was almost certainly read in the wrong zone, which would silently disable duplicate detection", got, rec.OrderTime)
	}
	return nil
}

func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, merchantZone)
}
