package answerverify

// Unit tests over the matching policy. The cases marked "real corpus" are
// verbatim narration/tool-result pairs the live evaluation harness
// produced against the seeded dataset — they are the actual reason the
// policy is shaped the way it is, and a regression in any of them means
// this check has started refusing correct answers.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// dailySummary is the real get_daily_summary payload for 2024-08-01, the
// dataset's first hand-authored day.
const dailySummary = `{
  "date": "2024-08-01",
  "gross_sales_by_source": {"ifood": "196.50", "just_eat_takeaway": "178.25", "pos": "736.50"},
  "total_delivery_gross_sales": "374.75",
  "commissions": "80.85",
  "refunds": "0.00",
  "refunds_by_source": {},
  "input_costs": "328.50",
  "margin": "701.90",
  "discrepancy_flags": [],
  "source_row_refs": [{"file": "data/live/delivery_platform_export.csv", "row": 2}]
}`

// promoROI is the real get_promotion_roi payload for JET-CAMP-LUNCHFIX,
// the dataset's negative-ROI campaign.
const promoROI = `{
  "platform": "Just Eat Takeaway",
  "campaign_id": "JET-CAMP-LUNCHFIX",
  "period": {"start": "2024-08-04", "end": "2024-08-10"},
  "spend": "610.00",
  "attributed_incremental_orders": 4,
  "attributed_incremental_revenue": "159.25",
  "roi": "-450.75",
  "flagged_negative": true,
  "source_row_refs": [{"file": "data/live/promotion_ad_spend_export.csv", "row": 3}]
}`

func TestVerify_GroundedFiguresPass(t *testing.T) {
	answer := "On 2024-08-01, total delivery gross sales came to **$374.75** (iFood: $196.50 + Just Eat Takeaway: $178.25), per that day's reconciliation."
	report := Verify(answer, []string{dailySummary})

	require.False(t, report.Blocking(), "grounded figures must pass: %s", report.Summary())
	require.Equal(t, 3, report.CheckedCurrency, "all three money figures must actually be checked, not skipped")
	require.Empty(t, report.Dates)
}

// TestVerify_PlainTextSourceWidensTheAllowedSet pins the fix that lets
// internal/explain widen the allowed set with the immediately preceding
// turn's own served answer text — a natural sentence, not a tool-result
// JSON payload. Before this fix, a non-JSON source was silently dropped
// (json.Decode fails, collectFromJSON returned early), so a follow-up
// restating a figure the product had already verified and served last turn
// was refused as if it were a brand-new, unverifiable claim.
func TestVerify_PlainTextSourceWidensTheAllowedSet(t *testing.T) {
	priorAnswer := "Margin on 2026-08-29 was $3,225.06."
	answer := "The day before that, margin was $3,225.06."

	// $3,225.06 is grounded ONLY in the prior answer, not in dailySummary.
	report := Verify(answer, []string{dailySummary, priorAnswer})
	require.False(t, report.Blocking(), "a figure restated from a plain-text prior-answer source must be allowed: %s", report.Summary())

	// Without the plain-text source, the same figure is correctly refused —
	// proving the widening is real, not a coincidental pass.
	reportWithoutPrior := Verify(answer, []string{dailySummary})
	require.True(t, reportWithoutPrior.Blocking(), "the same figure must still be refused when it isn't grounded in any source")
}

func TestVerify_FabricatedFigureIsCaught(t *testing.T) {
	report := Verify("On 2024-08-01, total delivery gross sales came to $999.99.", []string{dailySummary})

	require.True(t, report.Blocking())
	require.Len(t, report.Currency, 1)
	require.Equal(t, "$999.99", report.Currency[0].Text)
}

// TestVerify_CentsLevelAlterationIsCaught is the failure mode that
// motivates this package: not an obviously invented figure, but the right
// one moved by a few cents, which nothing else in the system would notice.
func TestVerify_CentsLevelAlterationIsCaught(t *testing.T) {
	for _, altered := range []string{"$374.57", "$374.74", "$374.76", "$374.85"} {
		report := Verify("Delivery came to "+altered+" that day.", []string{dailySummary})
		require.True(t, report.Blocking(), "%s must not pass as a restatement of $374.75", altered)
	}
}

// TestVerify_RoundedRestatementPasses — real corpus (accuracy A8,
// consistency C3c). "$610" and "about $159" and "roughly $451" are all
// correct restatements of 610.00, 159.25 and -450.75, and a cents-exact
// matcher would refuse all three correct answers.
func TestVerify_RoundedRestatementPasses(t *testing.T) {
	answer := "In plain terms: this campaign cost $610 and only drove about $159 in extra sales — a net loss of roughly $451 for the week."
	report := Verify(answer, []string{promoROI})

	require.False(t, report.Blocking(), "rounded restatements must pass: %s", report.Summary())
	require.Equal(t, 3, report.CheckedCurrency)
}

// TestVerify_WholeDollarClaimStillHasToLandNearSomethingReal proves the
// rounding tolerance is a precision rule, not an open door: at whole-dollar
// precision a figure still has to be within a dollar of a real value.
func TestVerify_WholeDollarClaimStillHasToLandNearSomethingReal(t *testing.T) {
	require.True(t, Verify("about $700 in extra sales", []string{promoROI}).Blocking(),
		"$700 is nowhere near 610.00, 159.25 or 450.75 and must be caught")
	require.False(t, Verify("about $160 in extra sales", []string{promoROI}).Blocking(),
		"$160 is how 159.25 rounds at whole-dollar precision")
	require.True(t, Verify("about $161 in extra sales", []string{promoROI}).Blocking(),
		"$161 is a full dollar past anything 159.25 could be written as")
	require.True(t, Verify("about $611 in spend", []string{promoROI}).Blocking(),
		"610.00 is written as $610 at whole-dollar precision, never as $611")
}

// TestVerify_SignIsCarriedByProseNotSymbols — real corpus (consistency
// C3a): "a net loss of $450.75" states, without a minus sign, a value the
// tool returned as "-450.75". Matching on absolute value is what keeps
// that correct answer servable.
func TestVerify_SignIsCarriedByProseNotSymbols(t *testing.T) {
	require.False(t, Verify("a net loss of $450.75 over the week", []string{promoROI}).Blocking())
	require.False(t, Verify("**ROI:** **-$450.75** (flagged negative)", []string{promoROI}).Blocking())
	require.False(t, Verify("ROI: –$450.75 (flagged as negative)", []string{promoROI}).Blocking(),
		"an en dash is a minus sign in narration too")
}

// TestVerify_ModelComputedSharePercentagePasses — real corpus (accuracy
// A5). No tool returns a share percentage; the model divided 196.50 by
// 374.75. The check recomputes that ratio in Go rather than either
// trusting it blindly or refusing a correct answer.
func TestVerify_ModelComputedSharePercentagePasses(t *testing.T) {
	answer := "iFood brought in $196.50 of the total $374.75 in delivery-platform sales that day — that's about 52% of your delivery revenue."
	report := Verify(answer, []string{dailySummary})

	require.False(t, report.Blocking(), "a share percentage Go can rederive must pass: %s", report.Summary())
	require.Equal(t, 1, report.CheckedPercent)
}

// TestVerify_OneStepComparisonPasses — real corpus, and a real regression
// this package caused before it was fixed. Asked "iFood's share on
// 2024-08-14?", the model answered with the correct headline figure and
// added that iFood was $6.75 ahead of Just Eat Takeaway. No tool returns
// $6.75; the model subtracted 180.50 from 187.25. The first version of
// this check refused that entire correct answer over the aside. Go
// rederives the subtraction instead.
func TestVerify_OneStepComparisonPasses(t *testing.T) {
	aug14 := `{"date": "2024-08-14",
	  "gross_sales_by_source": {"ifood": "187.25", "just_eat_takeaway": "180.50", "pos": "712.75"},
	  "total_delivery_gross_sales": "367.75"}`

	report := Verify("iFood brought in $187.25, with Just Eat Takeaway at $180.50 — iFood was $6.75 ahead.", []string{aug14})
	require.False(t, report.Blocking(), "a difference Go can rederive in one step must pass: %s", report.Summary())

	require.False(t, Verify("iFood and JET came to $367.75 together.", []string{aug14}).Blocking(),
		"a sum Go can rederive in one step must pass too")
}

// TestVerify_PerUnitFigureAndRhetoricalDollarPass — both caught live,
// both refusing a correct answer. "$153 an order" is 610.00 over 4
// orders; "for every $1 spent" is a rhetorical unit, and 610.00/610.00
// is as grounded as a figure gets.
func TestVerify_PerUnitFigureAndRhetoricalDollarPass(t *testing.T) {
	require.False(t, Verify("$610.00 across 4 orders — about $153 an order.", []string{promoROI}).Blocking(),
		"a per-unit figure Go can rederive by one division must pass")
	require.False(t, Verify("For every $1 spent, it brought back about 26 cents.", []string{promoROI}).Blocking(),
		"a rhetorical per-dollar unit must never refuse an answer")
}

// TestVerify_TwoStepDerivationIsNotAdmitted holds the line at ONE step: a
// figure needing two combinations is not cheaply reproducible from the
// deterministic layer and stays a mismatch.
func TestVerify_TwoStepDerivationIsNotAdmitted(t *testing.T) {
	values := `{"a": "100.00", "b": "20.00", "c": "3.00"}`

	require.False(t, Verify("that's $120.00 in total", []string{values}).Blocking(), "one step (100+20)")
	require.True(t, Verify("that's $123.00 in total", []string{values}).Blocking(),
		"100+20+3 is two steps — not admissible")
}

// TestVerify_ProvenanceRowNumbersAreNotMoney: every tool result carries a
// source_row_refs array of {file, row} pairs, and those row indices are
// small integers no narration ever states as money. Admitting them made
// every real figure plus or minus a few dollars verifiable once one-step
// derivation was on — measured, not hypothesised (see provenanceKey).
func TestVerify_ProvenanceRowNumbersAreNotMoney(t *testing.T) {
	// dailySummary carries "row": 2, and 374.75 + 2.00 = 376.75.
	require.True(t, Verify("Delivery came to $376.75 that day.", []string{dailySummary}).Blocking(),
		"a row index must never become an admissible money value")
}

// TestVerify_PercentageStatedToTheTenthIsCheckedAtThatPrecision: the tools
// state some percentages themselves, inside anomaly-flag prose. Those are
// admissible directly, and a neighbouring value is not.
func TestVerify_ToolStatedPercentagePasses(t *testing.T) {
	flagged := `{"days": [{"date": "2024-08-05", "flags": [{"type": "anomaly_threshold_exceeded",
	  "detail": "gross revenue 901.75 deviates 28.9% from the trailing 3-day average 1268.16 (threshold 20%)"}]}]}`

	require.False(t, Verify("that day's gross revenue ($901.75) came in 28.9% below the trailing 3-day average", []string{flagged}).Blocking(),
		"a figure the tool itself stated, inside flag prose, is grounded")
	require.True(t, Verify("that day came in 41.3% below the trailing average", []string{flagged}).Blocking(),
		"41.3% is neither stated by the tool nor a ratio of any two values it returned")
}

// TestVerify_DateVarianceIsAdvisoryNeverBlocking pins the deliberate
// narrowness of the date pass: it reports, it never refuses.
func TestVerify_DateVarianceIsAdvisoryNeverBlocking(t *testing.T) {
	report := Verify("On 2024-08-09, delivery came to $374.75.", []string{dailySummary})

	require.False(t, report.Blocking(), "a date that doesn't match must never block an answer")
	require.Len(t, report.Dates, 1)
	require.Equal(t, KindDate, report.Dates[0].Kind)
	require.Equal(t, "2024-08-09", report.Dates[0].Text)
}

func TestVerify_MatchingDateIsNotReported(t *testing.T) {
	report := Verify("On 2024-08-01, delivery came to $374.75.", []string{dailySummary})
	require.Empty(t, report.Dates)
	require.Equal(t, 1, report.CheckedDates)
}

// TestVerify_PartialDateReferenceIsNotGuessedAt: "Aug 1–14, 2024" and
// "Aug 5" carry no unambiguous single date, so the date pass skips them
// rather than inventing one — the same "a form I cannot recognize with
// certainty is left alone" discipline internal/ambiguity/daterange.go
// applies. Here the cost of guessing is log noise, which is its own harm.
func TestVerify_PartialDateReferenceIsNotGuessedAt(t *testing.T) {
	report := Verify("For August 1–14, 2024 your costs came to $374.75. One other flag turned up on Aug 5.", []string{dailySummary})
	require.Zero(t, report.CheckedDates)
	require.Empty(t, report.Dates)
}

// TestVerify_MalformedToolOutputDoesNotItselfCauseARefusal: a payload this
// package cannot parse contributes nothing to the allowed set, but it must
// not turn into a refusal on its own — the same best-effort discipline
// internal/explain.collectProvenance already applies. (With a second,
// well-formed result present, the answer still verifies.)
func TestVerify_MalformedToolOutputIsSkippedNotFatal(t *testing.T) {
	report := Verify("Delivery came to $374.75.", []string{"{not json at all", dailySummary})
	require.False(t, report.Blocking())
}

// TestVerify_DateStringsAreNotDecomposedIntoMoney guards a specific way a
// naive walker leaks: splitting "2024-08-01" into 2024/8/1 would make a
// fabricated "$2,024" verifiable.
func TestVerify_DateStringsAreNotDecomposedIntoMoney(t *testing.T) {
	require.True(t, Verify("Your margin was $2,024.00.", []string{dailySummary}).Blocking(),
		"a year lifted out of a date string must never become an admissible money value")
}

// TestVerify_ZeroToolResultsFlagsEverything documents the contract
// internal/explain relies on when it declines to call Verify at all for a
// zero-tool-call interaction: with nothing to check against, this package
// reports every figure, which is an absence of evidence rather than a
// finding, and belongs to that path's own guard.
func TestVerify_ZeroToolResultsFlagsEverything(t *testing.T) {
	report := Verify("Your margin was $374.75.", nil)
	require.True(t, report.Blocking())
}

// TestVerify_IsPure re-runs the same input and requires an identical
// report — no map-iteration order, no clock, no accumulation.
func TestVerify_IsPure(t *testing.T) {
	answer := "On 2024-08-01, delivery came to $374.75 (iFood $196.50, JET $178.25) — about 52% from iFood."
	first := Verify(answer, []string{dailySummary})
	for i := 0; i < 20; i++ {
		require.Equal(t, first, Verify(answer, []string{dailySummary}), "run %d disagreed with the first", i)
	}
}

func TestParseHundredths(t *testing.T) {
	cases := []struct {
		in       string
		want     int64
		decimals int
		ok       bool
	}{
		{"374.75", 37475, 2, true},
		{"1,204.50", 120450, 2, true},
		{"-450.75", -45075, 2, true},
		{"+610.00", 61000, 2, true},
		{"610", 61000, 0, true},
		{"52.4", 5240, 1, true},
		{"33.756", 3376, 3, true}, // rounded into hundredths, not rejected
		{"", 0, 0, false},
		{"12x.34", 0, 0, false},
		{"34.", 0, 0, false},
		{"-", 0, 0, false},
	}
	for _, c := range cases {
		got, decimals, ok := parseHundredths(c.in)
		require.Equal(t, c.ok, ok, "parseHundredths(%q) ok", c.in)
		if !c.ok {
			continue
		}
		require.Equal(t, c.want, got, "parseHundredths(%q) value", c.in)
		require.Equal(t, c.decimals, decimals, "parseHundredths(%q) decimals", c.in)
	}
}

func TestFindCurrencyFigures_DoesNotMatchDatesOrPercentages(t *testing.T) {
	found := findCurrencyFigures("On 2024-08-01 margin rose 28.9% to $374.75 across 14 days.")
	require.Len(t, found, 1)
	require.Equal(t, "$374.75", found[0].Text)
}

func TestFindPercentFigures(t *testing.T) {
	found := findPercentFigures("up 28.9% — call it 29 percent")
	require.Len(t, found, 2)
	require.Equal(t, 2890, int(found[0].Hundredths))
	require.Equal(t, 1, found[0].Decimals)
	require.Equal(t, 2900, int(found[1].Hundredths))
	require.Equal(t, 0, found[1].Decimals)
}

// TestVerify_LargeToolResultSkipsPercentChecking documents the deliberate
// escape hatch: past maxValuesForDerivation, percentage checking is
// skipped rather than run over a set so large it would either be slow or
// admit everything. Skipping can only fail to refuse, never refuse
// wrongly — money checking is unaffected.
func TestVerify_LargeToolResultSkipsPercentChecking(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"pattern": [`)
	for i := 0; i <= maxValuesForDerivation; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"avg_expense": "%d.%02d"}`, 1000+i, i%100)
	}
	b.WriteString(`]}`)

	report := Verify("Expenses averaged 71.4% of revenue, and $1,000.00 on the first.", []string{b.String()})
	require.True(t, report.PercentSkipped)
	require.Zero(t, report.CheckedPercent)
	require.False(t, report.Blocking(), "money checking must still work, and the skipped percentage must not block")
}

// TestFindCurrencyFigures_WordDenominatedDollars pins the fix for a real
// blind spot: a narration is free to spell money out in words instead of
// using "$", and before this fix currencyInAnswer extracted nothing at all
// for "375 dollars" (no symbol, no decimal point for the bare-decimal
// branch to require) — meaning a WRONG figure phrased this way would never
// have been checked against anything, the exact silent-hole failure mode
// worse than a false refusal.
func TestFindCurrencyFigures_WordDenominatedDollars(t *testing.T) {
	found := findCurrencyFigures("the campaign spent 610 dollars and returned 159.25 dollars")
	// "610 dollars" has no decimal point, so only the new word-denominated
	// loop extracts it. "159.25 dollars" has one, so the pre-existing
	// bare-decimal branch AND the new loop both extract it — a harmless
	// duplicate (same value, same decimals), never a wrong one, since both
	// readings agree on what the figure is.
	require.Len(t, found, 3)
	var sawWholeDollars, sawDecimalDollarsCount int
	for _, f := range found {
		switch f.Hundredths {
		case 61000:
			require.Equal(t, 0, f.Decimals)
			sawWholeDollars++
		case 15925:
			require.Equal(t, 2, f.Decimals)
			sawDecimalDollarsCount++
		default:
			t.Fatalf("unexpected figure %+v", f)
		}
	}
	require.Equal(t, 1, sawWholeDollars, "610 dollars: only the word-denominated loop can extract a whole number with no decimal point")
	require.Equal(t, 2, sawDecimalDollarsCount, "159.25 dollars: both the bare-decimal branch and the word-denominated loop extract it, harmlessly")
}

// TestFindCurrencyFigures_WordDenominatedCentsConvertUnitsCorrectly pins
// the other half of the same blind spot, and its sharpest failure mode: a
// figure stated as "26 cents" must resolve to $0.26, not $26.00 — the unit
// conversion this loop gets by design, never by reusing parseHundredths's
// dollars assumption unmodified.
func TestFindCurrencyFigures_WordDenominatedCentsConvertUnitsCorrectly(t *testing.T) {
	found := findCurrencyFigures("for every $1 spent it brought back 26 cents")
	require.Len(t, found, 2, "the $1 and the 26 cents are both money figures")
	var centsFigure *figure
	for i := range found {
		if found[i].Text == "26 cents" {
			centsFigure = &found[i]
		}
	}
	require.NotNil(t, centsFigure, "must extract the word-denominated cents figure")
	require.Equal(t, int64(26), centsFigure.Hundredths, "26 cents is $0.26 (26 hundredths), not $26.00 (2600 hundredths)")
	require.Equal(t, 2, centsFigure.Decimals, "a cents statement is always exact-cent precision")
}

// TestFindCurrencyFigures_DecimalCentsDoesNotDoubleCountAsWholeDollars
// guards the interaction between the two loops: "26.00 cents" must resolve
// ONLY to $0.26 via the dedicated cents-word conversion, never also to
// $26.00 via the plain bare-decimal branch above it — that second,
// unconverted reading would be a real dollar value nothing in this product
// ever states, and admitting it would let a genuinely wrong $26.00 claim
// hide behind a coincidentally-matching "26.00 cents" in the same answer.
func TestFindCurrencyFigures_DecimalCentsDoesNotDoubleCountAsWholeDollars(t *testing.T) {
	found := findCurrencyFigures("it returned 26.00 cents per dollar spent")
	require.Len(t, found, 1)
	require.Equal(t, int64(26), found[0].Hundredths)
}

// TestFindPercentFigures_PercentagePoints pins the other half of Bug 5: a
// week-over-week or platform-comparison delta is routinely phrased in
// points ("margin improved by 3 percentage points"), and "percent\b" alone
// never matches "percentage" — no word boundary follows "percent" inside
// it — so this phrasing previously extracted nothing at all.
func TestFindPercentFigures_PercentagePoints(t *testing.T) {
	found := findPercentFigures("margin improved by 3 percentage points, and iFood's effective rate rose 1 percentage point")
	require.Len(t, found, 2)
	require.Equal(t, 300, int(found[0].Hundredths))
	require.Equal(t, 100, int(found[1].Hundredths))
}

// TestVerify_WordDenominatedFigureStillHasToBeReal proves this is a
// genuine check, not just an extraction fix: a WRONG figure phrased in
// words must still be caught.
func TestVerify_WordDenominatedFigureStillHasToBeReal(t *testing.T) {
	require.True(t, Verify("the campaign spent 999 dollars", []string{promoROI}).Blocking(),
		"999 dollars is nowhere near the real 610.00 spend and must be caught, not silently ignored for lacking a $ sign")
}
