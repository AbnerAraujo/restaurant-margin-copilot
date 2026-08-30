// Package money implements exact fixed-point decimal arithmetic for
// currency and percentage values, so the deterministic reconciliation
// engine (Constitution Principle I) never routes money math through
// float64. Every amount is represented internally as an integer scaled by
// 100 ("cents") — the same fixed-point trick real accounting systems use —
// specifically to avoid the binary floating-point rounding error a naive
// float64 computation would introduce.
//
// This is not theoretical: backend/fixtures/README.md documents that
// delivery-platform commission is computed as subtotal * commission_rate_pct
// / 100, and 34.50 * 23% is exactly 7.935 — a value that Go's float64 (and
// Python's default round()) can push to 7.93 instead of the fixture's
// correct round-half-up 7.94, producing a false "commission mismatch" flag
// that has nothing to do with real data. Money and DivRoundHalfUp exist to
// make that false positive impossible.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseFixedPoint parses a decimal string (e.g. "34.50", "-7.94", "23") into
// an integer scaled by 10^decimals (decimals=2 turns "34.50" into 3450). It
// never goes through float64, so it cannot introduce binary floating-point
// rounding error on the way in. An empty or malformed value is an error, not
// a silent zero — ingestion refuses rather than guesses (Constitution
// Principle II) on malformed source data.
func ParseFixedPoint(s string, decimals int) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("money: empty value")
	}

	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("money: invalid value %q", s)
	}

	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if hasFrac {
		// A trailing dot with nothing after it ("34.") is malformed input,
		// not a valid zero-fraction number — silently treating it as "34.00"
		// would misparse a truncated/corrupt value into a confident number
		// (Constitution Principle II: refuse rather than guess).
		if frac == "" {
			return 0, fmt.Errorf("money: %q has a trailing decimal point with no digits after it", s)
		}
		if len(frac) > decimals {
			return 0, fmt.Errorf("money: %q has more than %d decimal places", s, decimals)
		}
		frac += strings.Repeat("0", decimals-len(frac))
	} else {
		frac = strings.Repeat("0", decimals)
	}

	digits := whole + frac
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		// Deliberately not %w-wrapping strconv's error here: it names the
		// cents-padded internal `digits` string ("abc00"), not the raw input
		// the owner actually typed, and its own text ("strconv.ParseInt:
		// parsing ...: invalid syntax") is an implementation detail no
		// end user should see — the same "clean, human-readable, no
		// internal stack trace" treatment ingest/date.go's parse() already
		// gives a malformed date.
		return 0, fmt.Errorf("money: %q is not a valid amount", s)
	}
	if neg {
		v = -v
	}
	return v, nil
}

// ParseCents parses a currency amount string into integer cents.
func ParseCents(s string) (int64, error) {
	return ParseFixedPoint(s, 2)
}

// FormatCents renders integer cents back into a "-12.34"-style decimal
// string, e.g. for discrepancy-flag messages and CLI output.
func FormatCents(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// DivRoundHalfUp divides numerator by denominator, rounding half away from
// zero — the convention this project's delivery-platform fixture data uses
// for commission math (see the package doc). Plain integer division
// truncates toward zero, which would silently under-count commission by a
// cent on exact .5 cases such as 34.50 * 23% = 7.935.
func DivRoundHalfUp(numerator, denominator int64) int64 {
	if denominator == 0 {
		return 0
	}
	neg := (numerator < 0) != (denominator < 0)
	n, d := abs64(numerator), abs64(denominator)
	q := (n + d/2) / d
	if neg {
		return -q
	}
	return q
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
