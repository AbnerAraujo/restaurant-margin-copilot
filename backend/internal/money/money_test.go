package money

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseFixedPoint covers the parser's refuse-rather-than-guess
// boundary (Constitution Principle II): malformed input must be an error,
// never a silently-wrong integer.
func TestParseFixedPoint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		decimals int
		want     int64
		wantErr  bool
	}{
		{name: "plain integer", input: "23", decimals: 2, want: 2300},
		{name: "two decimal places", input: "34.50", decimals: 2, want: 3450},
		{name: "negative value", input: "-7.94", decimals: 2, want: -794},
		{name: "leading plus", input: "+12.00", decimals: 2, want: 1200},
		{name: "too many decimal places rejected", input: "34.505", decimals: 2, wantErr: true},
		{
			// The review's flagged bug: a trailing dot with no fractional
			// digits ("34.") is malformed input, not a valid zero-fraction
			// number, and must be rejected rather than silently parsed as
			// "34.00" (3400).
			name:     "trailing dot with no fractional digits rejected",
			input:    "34.",
			decimals: 2,
			wantErr:  true,
		},
		{
			name:     "trailing dot on negative value rejected",
			input:    "-34.",
			decimals: 2,
			wantErr:  true,
		},
		{name: "sign-only input minus rejected", input: "-", decimals: 2, wantErr: true},
		{name: "sign-only input plus rejected", input: "+", decimals: 2, wantErr: true},
		{name: "empty string rejected", input: "", decimals: 2, wantErr: true},
		{name: "whitespace-only string rejected", input: "   ", decimals: 2, wantErr: true},
		{
			// int64 overflow must be a parse error, never a silent wraparound.
			name:     "int64 overflow rejected",
			input:    "99999999999999999999999999.00",
			decimals: 2,
			wantErr:  true,
		},
		{name: "garbage input rejected", input: "12x.34", decimals: 2, wantErr: true},
		{name: "zero decimals, plain integer", input: "5", decimals: 0, want: 5},
		{name: "zero decimals with fraction rejected", input: "5.5", decimals: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFixedPoint(tt.input, tt.decimals)
			if tt.wantErr {
				require.Error(t, err, "ParseFixedPoint(%q, %d) should have been rejected, got %d", tt.input, tt.decimals, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestParseCents confirms ParseCents is exactly ParseFixedPoint(s, 2), and
// exercises it against the package doc's own headline example: the
// commission math this package exists to make exact.
func TestParseCents(t *testing.T) {
	got, err := ParseCents("34.50")
	require.NoError(t, err)
	require.Equal(t, int64(3450), got)

	_, err = ParseCents("34.")
	require.Error(t, err, "ParseCents must reject a trailing dot with no fractional digits")
}

// TestFormatCents confirms cents render back into the "-12.34"-style string
// FormatCents documents, including the negative-sign and zero-padding cases.
func TestFormatCents(t *testing.T) {
	require.Equal(t, "34.50", FormatCents(3450))
	require.Equal(t, "-7.94", FormatCents(-794))
	require.Equal(t, "0.00", FormatCents(0))
	require.Equal(t, "0.05", FormatCents(5))
}

// TestDivRoundHalfUp_PackageDocExample is the package doc comment's own
// stated reason for existing: 34.50 * 23% = 7.935 must round to 7.94
// (round-half-up), not 7.93 (which naive float64 rounding, or truncating
// integer division, would produce).
func TestDivRoundHalfUp_PackageDocExample(t *testing.T) {
	subtotalCents := int64(3450) // 34.50
	rateBps := int64(2300)       // 23.00%

	// subtotal * rate / (100 * 100) = 3450 * 2300 / 10000 = 793.5 -> 794 (7.94)
	numerator := subtotalCents * rateBps
	got := DivRoundHalfUp(numerator, 10000)
	require.Equal(t, int64(794), got, "34.50 * 23%% must round to 7.94 (round-half-up), not 7.93")
}

// TestDivRoundHalfUp covers both positive and negative operand combinations:
// the review flagged that the negative branch was previously exercised only
// transitively, via a single real data row.
func TestDivRoundHalfUp(t *testing.T) {
	tests := []struct {
		name        string
		numerator   int64
		denominator int64
		want        int64
	}{
		{name: "zero denominator returns zero", numerator: 100, denominator: 0, want: 0},
		{name: "exact division, no rounding needed", numerator: 100, denominator: 10, want: 10},
		{name: "positive/positive rounds half up", numerator: 15, denominator: 10, want: 2},   // 1.5 -> 2
		{name: "positive/positive truncates below half", numerator: 14, denominator: 10, want: 1}, // 1.4 -> 1
		{name: "package doc example: 793.5 -> 794", numerator: 7935, denominator: 10, want: 794},

		// Negative numerator, positive denominator: result rounds half away
		// from zero (more negative), mirroring the positive case in magnitude.
		{name: "negative numerator rounds half away from zero", numerator: -15, denominator: 10, want: -2},
		{name: "negative numerator truncates below half", numerator: -14, denominator: 10, want: -1},

		// Positive numerator, negative denominator: sign of result follows
		// the mismatched signs, magnitude still rounds half up.
		{name: "negative denominator rounds half away from zero", numerator: 15, denominator: -10, want: -2},

		// Both negative: signs cancel, magnitude rounds half up as if both
		// were positive.
		{name: "both negative rounds half up on magnitude", numerator: -15, denominator: -10, want: 2},
		{name: "both negative, exact division", numerator: -100, denominator: -10, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DivRoundHalfUp(tt.numerator, tt.denominator)
			require.Equal(t, tt.want, got)
		})
	}
}
