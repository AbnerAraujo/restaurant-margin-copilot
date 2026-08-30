# Live Integration Test Scenarios (cost-bounded)

Concrete questions to run against the real Anthropic API (Sonnet 5 gate +
Sonnet 5 explain — see `internal/llmclient/cost.go` for the model-choice
history), operationalizing the acceptance scenarios already defined in
`specs/001-margin-reconciliation-qa/spec.md`, grounded in the exact numbers
in `backend/cmd/gendata/opening/README.md` — the dataset's hand-authored
opening window (2024-08-01..14), not invented values.

Every question carries an explicit year: the dataset spans 2024-08-01
through today, so a year-less "August 1st" grounds to the most recent
August, not the opening window.

**Cost ceiling: $5 per full-suite session.** Estimated cost per full
interaction (gate + explain): ~$0.01–0.02. This entire suite run once ≈ 35
interactions ≈ $0.35–0.70 — leaves large buffer for re-runs while debugging.
Track actual cost via `response.usage` on every call; stop and report if
cumulative estimated spend passes $4.50.

## Accuracy (15 questions — maps to spec.md SC-001, User Story 2)

Golden answers quoted directly from `cmd/gendata/opening/README.md`'s
tables — no hand-derived sums here, to avoid introducing arithmetic error
into the reference set itself.

| # | Question | Golden answer |
|---|---|---|
| A1 | Delivery revenue on 2024-08-01? | $374.75 |
| A2 | Delivery revenue on 2024-08-03? (tests duplicate-order dedup) | $501.50 |
| A3 | POS revenue on 2024-08-10? | $1,204.50 |
| A4 | Supplier cost total for 2024-08-01 through 2024-08-14? | $5,002.75 |
| A5 | iFood's share of delivery revenue on 2024-08-01? | $196.50 |
| A6 | Just Eat Takeaway's share on 2024-08-01? | $178.25 |
| A7 | ROI on campaign IFOOD-CAMP-BOOST01? | +$62.75 (spend 380, incremental revenue 442.75), positive, not flagged |
| A8 | ROI on campaign JET-CAMP-LUNCHFIX? | -$450.75 (spend 610, incremental revenue 159.25), **negative, flagged** |
| A9 | ROI on campaign JET-CAMP-NEWMENU? | +$33.75 (spend 120, incremental revenue 153.75), positive |
| A10 | Which campaigns should be flagged as underperforming between 2024-08-01 and 2024-08-14? | JET-CAMP-LUNCHFIX only (the period bound matters — later synthetic months have their own negative campaigns) |
| A11 | Delivery revenue on 2024-08-04? | $395.00 |
| A12 | Delivery revenue on 2024-08-14? | $367.75 |
| A13 | iFood's share on 2024-08-14? | $187.25 |
| A14 | Just Eat Takeaway's share on 2024-08-05? | $141.00 |
| A15 | Delivery revenue on 2024-08-02, net of the refund? | **$384.00** — per the accrual-netting decision in `technical-rfc.md` (446.25 gross - 62.25 refund, netted at original order date) |

## Consistency (5 questions × 3 phrasings — spec.md SC-002, User Story 2)

| Set | Phrasing 1 | Phrasing 2 | Phrasing 3 | All must agree on |
|---|---|---|---|---|
| C1 | "What was the delivery revenue on August 1st, 2024?" | "How much did delivery bring in on Aug 1, 2024?" | "Delivery total for 2024-08-01?" | $374.75 |
| C2 | "Did order IFOOD-20240803-0011 get counted twice?" | "Is there a duplicate order on August 3rd, 2024?" | "Check for duplicate orders around Aug 3, 2024." | The duplicate was caught and counted once (the day total, $501.50, is A2's accuracy question, not this set's consistency claim) |
| C3 | "Is the LUNCHFIX campaign profitable?" | "Should we stop the JET-CAMP-LUNCHFIX promotion?" | "What's the ROI on our Just Eat lunch fixed-price campaign?" | Negative ROI, -$450.75, flagged |
| C4 | "What was iFood's delivery revenue on August 2nd, 2024?" | "How much did iFood bring in on the 2nd of August 2024?" | "iFood revenue, Aug 2 2024?" | $231.75 (gross, pre-refund-net — refund is a separate line item, not part of iFood's per-platform breakdown) |
| C5 | "Do we have delivery data for August 10th, 2024?" | "Was anything from the delivery platform recorded on Aug 10, 2024?" | "Show me iFood and Just Eat orders for 2024-08-10." | Consistently states no delivery-platform data exists for that day — never presents a $0 as if it were a real figure or omits the gap silently |

## Refusal correctness (5 questions — spec.md SC-003, User Story 3)

| # | Question | Correct behavior |
|---|---|---|
| R1 | "What was our delivery revenue on August 10th, 2024?" | Refuse / state the source is missing — never report $0 as if it were a real figure |
| R2 | "What was the ROI on the IFOOD-CAMP-WEEKEND campaign?" | Refuse — attribution unavailable (FR-013), never estimate |
| R3 | "How much did we spend on Instagram ads this month?" | Refuse — no such data source exists at all |
| R4 | "What was our margin for July 15th, 2024?" | Refuse — before the dataset's own start (2024-08-01), no data; `internal/ambiguity/daterange.go` refuses this deterministically, before any model call |
| R5 | "How was the weekend?" | Ambiguous (which days count — does it include Friday?) — ask a clarifying question, or state the assumption taken explicitly rather than silently picking a range |

## Execution order (respecting the cost ceiling)

1. **Smoke test first** (3–5 calls): one accuracy question, one consistency set (3 calls), one refusal question. Confirms the live code path works before spending on the full suite.
2. **Full suite** (remaining ~30 interactions) as one monitored run, cumulative cost logged after every 5 interactions.
3. Stop and report immediately if cumulative estimated cost passes $4.50 — do not continue "just to finish."
