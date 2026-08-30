package platformconnector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// --- The simulated in-house POS terminal API --------------------------------
//
// Shaped as a plausible restaurant POS "day close" export endpoint.
// Nothing here is real: no terminal, no network, no vendor. See the
// package doc.
//
// Its format choices disagree with BOTH delivery mocks on every decision
// either of them makes:
//
//   - NDJSON — newline-delimited JSON, one ticket per line, with NO
//     envelope at all (iFood has an orders envelope, JET has data+cursor)
//   - NO pagination of any kind: a terminal is a local box and hands over
//     the whole business day in one response (iFood pages by number, JET
//     by opaque cursor)
//   - money as pt-BR DECIMAL STRINGS: "1.234,56" — thousands dot, decimal
//     comma (iFood uses "1234.56", JET uses integer minor units)
//   - timestamps as ZONE-LESS LOCAL wall clock: "2026-08-20 19:35:00"
//     (iFood carries an explicit offset, JET sends epoch millis in UTC)
//   - status vocabulary PAID / VOID (CONCLUDED/CANCELLED, DELIVERED/REFUNDED)
//   - a service_type field neither delivery mock has, plus a nested
//     delivery_partner block that is this feature's whole reason to exist
//
// Two of those are traps with real teeth.
//
// THE pt-BR AMOUNT. money.ParseCents reads "1234.56" correctly and reads
// "1.234,56" as $1.23 — a plausible-looking string silently understated by
// three orders of magnitude, with no error anywhere. A day's POS revenue
// would collapse and margin would follow it down. normalizePtBRAmount
// converts explicitly and REFUSES anything that does not fit the shape,
// rather than best-effort parsing whatever it was handed.
//
// THE ZONE-LESS TIMESTAMP. time.Parse with a layout carrying no zone
// yields UTC. Every ticket this mock emits falls between 11:00 and 21:29
// local, so the calendar DATE survives that mistake — which is exactly what
// makes it dangerous, because nothing downstream would look wrong. What it
// destroys is the merchant's three-hour offset in every ticket TIME, and
// ticket time is the input to dedup.go's matching window. Every
// amount-and-time match in the product would stop firing, duplicate revenue
// would flow straight through, and gross sales would inflate with nothing
// to flag it. time.ParseInLocation is the fix; proxy.go's checkPOSContract
// and TestPOSAdapter_TicketTimeIsReadInTheMerchantZone are the proof.

// maxTicketsPerDay bounds the response this adapter will walk.
//
// The delivery adapters bound a pagination LOOP (maxPagesPerDay); this
// upstream has no pagination, so the analogous unbounded input is the
// number of lines in one response. Same Constitution requirement, same
// refusal rather than a silent truncation: a POS day quietly cut off at
// line N would understate in-house revenue and inflate margin. 400 is
// generous headroom over a simulated day's ~100 tickets while still being
// a real bound.
const maxTicketsPerDay = 400

const posTerminalID = "SIMULATED-TERMINAL-02"

// posTicketDTO is one line of the mock's NDJSON response.
type posTicketDTO struct {
	TicketNumber string `json:"ticket_number"`
	// OpenedAt is local wall clock with NO zone: "2026-08-20 19:35:00".
	OpenedAt string `json:"opened_at"`
	// ServiceType is DINE_IN | COUNTER | DELIVERY_PARTNER.
	ServiceType string `json:"service_type"`
	// TotalBrl is a pt-BR decimal string: "42,00", "1.234,56".
	TotalBrl string `json:"total_brl"`
	Tender   string `json:"tender"`
	// State is PAID | VOID.
	State string `json:"state"`
	// Partner is present only on a DELIVERY_PARTNER ticket. Its
	// partner_order_ref is omitted when the integration did not record
	// one — a pointer-free empty string, because "absent" and "empty" mean
	// the same thing here and distinguishing them would be ceremony.
	Partner *posPartnerDTO `json:"delivery_partner,omitempty"`
}

type posPartnerDTO struct {
	Name            string `json:"name"`
	PartnerOrderRef string `json:"partner_order_ref,omitempty"`
}

// posUpstream is the mock terminal. Like the two delivery mocks it returns
// raw bytes and its adapter parses them back; see ifoodUpstream's doc for
// why that round trip is deliberate rather than wasteful.
type posUpstream struct{}

// dayClose is the simulated GET /pos/v1/terminals/{id}/day-close?date=YYYY-MM-DD.
// It returns NDJSON: one JSON object per line, no wrapper, no trailing
// count, no cursor.
func (u posUpstream) dayClose(date time.Time) ([]byte, error) {
	tickets := simulatePOSDay(date)

	var buf bytes.Buffer
	for _, t := range tickets {
		dto := posTicketDTO{
			TicketNumber: fmt.Sprintf("POS-SIM-%s-%04d", date.Format("20060102"), t.Seq),
			OpenedAt:     t.PlacedAt.Format("2006-01-02 15:04:05"),
			ServiceType:  posServiceType(t.Channel),
			TotalBrl:     formatPtBRAmount(t.GrossCents),
			Tender:       t.Payment,
			State:        "PAID",
		}
		if t.Voided {
			dto.State = "VOID"
		}
		if t.EchoOf != "" {
			dto.Partner = &posPartnerDTO{
				Name:            t.EchoOf.DisplayName(),
				PartnerOrderRef: t.PartnerOrderRef,
			}
		}
		line, err := json.Marshal(dto)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// posServiceType maps the day model's channel onto the terminal's own
// vocabulary. A delivery-partner ticket loses the platform's name here and
// carries it in the nested block instead — which is how a real POS models
// it, and which is also why the adapter has to read two fields to answer
// "did this arrive through a delivery channel".
func posServiceType(channel string) string {
	switch channel {
	case "dine_in":
		return "DINE_IN"
	case "counter":
		return "COUNTER"
	default:
		return "DELIVERY_PARTNER"
	}
}

// formatPtBRAmount renders integer cents as this terminal's pt-BR decimal
// string: thousands separated by ".", decimals by ",".
func formatPtBRAmount(cents int64) string {
	canonical := money.FormatCents(cents) // "1234.56"
	neg := strings.HasPrefix(canonical, "-")
	canonical = strings.TrimPrefix(canonical, "-")

	whole, frac, _ := strings.Cut(canonical, ".")

	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}

	out := grouped.String() + "," + frac
	if neg {
		out = "-" + out
	}
	return out
}

// normalizePtBRAmount is the inverse, and the one function in this file
// that must not be lenient.
//
// It refuses rather than guesses. A string this terminal would never emit
// is a signal the integration is broken, and the cheapest possible way to
// turn that into a wrong margin figure is to parse it anyway and get a
// number that looks fine. There is exactly one accepted shape: optional
// sign, digits with optional "." group separators, a "," and exactly two
// decimal digits.
func normalizePtBRAmount(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("amount is empty")
	}

	body := strings.TrimPrefix(trimmed, "-")
	whole, frac, found := strings.Cut(body, ",")
	if !found {
		return 0, fmt.Errorf("amount %q has no decimal comma — this terminal reports amounts in pt-BR notation (\"1.234,56\"), and reading it as anything else would understate the ticket", s)
	}
	if strings.Contains(frac, ",") || strings.Contains(frac, ".") {
		return 0, fmt.Errorf("amount %q has more than one decimal separator", s)
	}
	if len(frac) != 2 {
		return 0, fmt.Errorf("amount %q does not have exactly two decimal digits", s)
	}

	// Group separators are stripped, but only after confirming they are
	// where a group separator belongs — "1.2345,00" is not a number this
	// terminal produces, and silently accepting it would mean accepting
	// that a "." might have been a decimal point after all.
	if strings.Contains(whole, ".") {
		groups := strings.Split(whole, ".")
		if len(groups[0]) == 0 || len(groups[0]) > 3 {
			return 0, fmt.Errorf("amount %q is not grouped in thousands", s)
		}
		for _, g := range groups[1:] {
			if len(g) != 3 {
				return 0, fmt.Errorf("amount %q is not grouped in thousands", s)
			}
		}
		whole = strings.ReplaceAll(whole, ".", "")
	}

	canonical := whole + "." + frac
	if strings.HasPrefix(trimmed, "-") {
		canonical = "-" + canonical
	}
	cents, err := money.ParseCents(canonical)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	return cents, nil
}

// posAdapter implements POSClient over posUpstream.
type posAdapter struct {
	upstream posUpstream
}

// NewPOSClient returns the simulated in-house POS connector.
func NewPOSClient() POSClient { return posAdapter{} }

func (a posAdapter) Platform() Platform { return PlatformPOS }

func (a posAdapter) Describe() Description {
	return Description{
		Platform:   PlatformPOS,
		Name:       PlatformPOS.DisplayName(),
		Simulated:  true,
		WireFormat: "newline-delimited JSON with no envelope and no pagination, amounts in pt-BR notation (\"1.234,56\"), zone-less local timestamps, PAID/VOID states, delivery-channel tickets carrying the partner's own order reference",
		// The POS charges no commission at all. Empty rather than "0.00",
		// because "0.00%" invites the reader to think of the POS as a
		// platform that happens to be free, and that framing is what
		// leaks a "pos" key into the platform comparator.
		CommissionRatePct: "",
		Endpoint:          posEndpoint(),
	}
}

func posEndpoint() string {
	return "simulated://pos-terminal-api/pos/v1/terminals/" + posTerminalID + "/day-close"
}

// FetchPOSOrders reads the simulated terminal's NDJSON day close and
// normalizes every ticket into ingest.POSRecord plus the two matching
// signals dedup.go needs.
func (a posAdapter) FetchPOSOrders(ctx context.Context, date time.Time) ([]POSOrder, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("platformconnector: POS fetch for %s: %w", date.Format(dateLayout), err)
	}

	raw, err := a.upstream.dayClose(date)
	if err != nil {
		return nil, fmt.Errorf("platformconnector: POS upstream for %s: %w", date.Format(dateLayout), err)
	}

	src := fmt.Sprintf("%s?date=%s", posEndpoint(), date.Format(dateLayout))

	var out []POSOrder
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	row := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		row++
		if row > maxTicketsPerDay {
			// A refusal, not a truncation. See maxTicketsPerDay.
			return nil, fmt.Errorf("platformconnector: POS reported more than %d tickets for %s — refusing rather than returning a partial day", maxTicketsPerDay, date.Format(dateLayout))
		}

		var dto posTicketDTO
		if err := json.Unmarshal(line, &dto); err != nil {
			return nil, fmt.Errorf("platformconnector: decoding POS ticket on line %d of %s: %w", row, date.Format(dateLayout), err)
		}

		order, err := a.normalize(dto, ingest.SourceRowRef{File: src, Row: row})
		if err != nil {
			return nil, err
		}
		out = append(out, order)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("platformconnector: reading POS response for %s: %w", date.Format(dateLayout), err)
	}
	return out, nil
}

// normalize converts one POS wire ticket into the shared record type plus
// its matching signals.
//
// Four real conversions, none of them a rename:
//   - pt-BR decimal string -> integer cents (normalizePtBRAmount)
//   - zone-less local timestamp -> an instant in the merchant's own zone
//     (ParseInLocation, see this file's header comment)
//   - DELIVERY_PARTNER + a partner name -> a typed Platform the matcher
//     can compare against, refusing a name it does not recognize
//   - PAID/VOID -> the "completed"/non-completed vocabulary
//     reconcile.computeOneDay already branches on
func (a posAdapter) normalize(dto posTicketDTO, ref ingest.SourceRowRef) (POSOrder, error) {
	fail := func(field string, err error) (POSOrder, error) {
		return POSOrder{}, fmt.Errorf("platformconnector: POS ticket %s: %s: %w", dto.TicketNumber, field, err)
	}

	// ParseInLocation, never Parse. See this file's header comment: Parse
	// would silently shift every ticket by the merchant's offset and
	// disable cross-source matching entirely.
	openedAt, err := time.ParseInLocation("2006-01-02 15:04:05", dto.OpenedAt, merchantZone)
	if err != nil {
		return fail("opened_at", err)
	}

	gross, err := normalizePtBRAmount(dto.TotalBrl)
	if err != nil {
		return fail("total_brl", err)
	}
	if gross <= 0 {
		return fail("total_brl", fmt.Errorf("ticket total %s is not positive — this connector models no POS refund and will not guess whether a non-positive ticket is a reversal or a data error", money.FormatCents(gross)))
	}

	y, m, d := openedAt.Date()

	order := POSOrder{
		Record: ingest.POSRecord{
			Ref:           ref,
			OrderID:       dto.TicketNumber,
			OrderDate:     time.Date(y, m, d, 0, 0, 0, 0, merchantZone),
			OrderTime:     openedAt.Format("15:04"),
			GrossCents:    gross,
			PaymentMethod: dto.Tender,
		},
		PlacedAt: openedAt,
	}

	switch dto.State {
	case "PAID":
		order.Record.Status = "completed"
	case "VOID":
		// Not "refunded": internal/reconcile has no POS refund concept
		// and excludes any non-completed POS row from gross with its
		// existing pos_non_completed_row_excluded flag. Passing the
		// terminal's own word through keeps that flag's detail line
		// readable back to the source.
		order.Record.Status = "void"
	default:
		return fail("state", fmt.Errorf("unrecognized state %q — refusing rather than guessing whether this ticket counts as revenue", dto.State))
	}

	switch dto.ServiceType {
	case "DINE_IN":
		order.Record.Channel = "dine_in"
	case "COUNTER":
		order.Record.Channel = "counter"
	case "DELIVERY_PARTNER":
		if dto.Partner == nil {
			return fail("delivery_partner", fmt.Errorf("service_type is DELIVERY_PARTNER but no partner block was returned — refusing rather than treating a delivery order as an in-house one, which would exempt it from duplicate detection"))
		}
		platform, ok := platformByDisplayName(dto.Partner.Name)
		if !ok {
			// A partner this product does not connect to means its
			// orders are NOT in the fetch, so this ticket cannot be
			// matched and must not be silently reclassified as
			// in-house — that would remove it from the matcher's
			// attention permanently. Refuse instead.
			return fail("delivery_partner.name", fmt.Errorf("unknown delivery partner %q — this connector recognizes %s", dto.Partner.Name, strings.Join(deliveryPlatformNames(), " and ")))
		}
		order.Record.Channel = string(platform)
		order.DeliveryPlatform = platform
		order.PartnerOrderRef = strings.TrimSpace(dto.Partner.PartnerOrderRef)
	default:
		return fail("service_type", fmt.Errorf("unrecognized service type %q — refusing rather than guessing whether this ticket could be a delivery duplicate", dto.ServiceType))
	}

	return order, nil
}

// platformByDisplayName resolves a delivery platform from the name the POS
// wrote on the ticket. Display names, not keys, because a POS records what
// the aggregator calls itself, not this product's internal identifier.
func platformByDisplayName(name string) (Platform, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, p := range DeliveryPlatforms {
		if strings.ToLower(p.DisplayName()) == want {
			return p, true
		}
	}
	return "", false
}

func deliveryPlatformNames() []string {
	names := make([]string, 0, len(DeliveryPlatforms))
	for _, p := range DeliveryPlatforms {
		names = append(names, p.DisplayName())
	}
	return names
}
