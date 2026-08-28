# Live Integration Test Scenarios (cost-bounded)

Concrete questions to run against the real Anthropic API (Haiku 4.5 gate +
Sonnet 5 explain), operationalizing the acceptance scenarios already defined
in `specs/001-margin-reconciliation-qa/spec.md`, grounded in the exact
numbers in `backend/fixtures/README.md` — not invented values.

**Cost ceiling: $5 for this build session, against $20 Console credit.**
Estimated cost per full interaction (gate + explain): ~$0.01–0.02. This
entire suite run once ≈ 35 interactions ≈ $0.35–0.70 — leaves large buffer
for re-runs while debugging. Track actual cost via `response.usage` on every
call; stop and report if cumulative estimated spend passes $4.50.

## Accuracy (15 questions — maps to spec.md SC-001, User Story 2)

Golden answers quoted directly from `fixtures/README.md`'s tables — no
hand-derived sums, to avoid introducing arithmetic error into the reference
set itself.

| # | Question | Golden answer |
|---|---|---|
| A1 | Delivery revenue on 2026-08-01? | $145.75 |
| A2 | Delivery revenue on 2026-08-03? (tests duplicate-order dedup) | $120.50 |
| A3 | POS revenue on 2026-08-08? | $487.50 |
| A4 | Supplier cost total for the two-week period? | $4,335.75 |
| A5 | iFood's share of delivery revenue on 2026-08-01? | $69.50 |
| A6 | Just Eat Takeaway's share on 2026-08-01? | $76.25 |
| A7 | ROI on campaign IFOOD-CAMP-BOOST01? | +$34.00 (spend 180, incremental revenue 214), positive, not flagged |
| A8 | ROI on campaign JET-CAMP-LUNCHFIX? | -$165.00 (spend 220, incremental revenue 55), **negative, flagged** |
| A9 | ROI on campaign JET-CAMP-NEWMENU? | +$19.50 (spend 60, incremental revenue 79.50), positive |
| A10 | Which campaigns should be flagged as underperforming? | JET-CAMP-LUNCHFIX only |
| A11 | Delivery revenue on 2026-08-04? | $125.50 |
| A12 | Delivery revenue on 2026-08-14? | $140.50 |
| A13 | iFood's share on 2026-08-14? | $66.75 |
| A14 | Just Eat Takeaway's share on 2026-08-05? | $71.25 |
| A15 | Delivery revenue on 2026-08-02, net of the refund? | **$119.75** — per the accrual-netting decision in `technical-rfc.md` (154.25 gross - 34.50 refund, netted at original order date) |

## Consistency (5 questions × 3 phrasings — spec.md SC-002, User Story 2)

| Set | Phrasing 1 | Phrasing 2 | Phrasing 3 | All must agree on |
|---|---|---|---|---|
| C1 | "What was the delivery revenue on August 1st?" | "How much did delivery bring in on Aug 1?" | "Delivery total for 2026-08-01?" | $145.75 |
| C2 | "Did order IFOOD-20260803-0011 get counted twice?" | "Is there a duplicate order on August 3rd?" | "Check for duplicate orders around Aug 3." | Counted once; $120.50 for the day |
| C3 | "Is the LUNCHFIX campaign profitable?" | "Should we stop the JET-CAMP-LUNCHFIX promotion?" | "What's the ROI on our Just Eat lunch fixed-price campaign?" | Negative ROI, -$165.00, flagged |
| C4 | "What was iFood's delivery revenue on August 2nd?" | "How much did iFood bring in on the 2nd?" | "iFood revenue, Aug 2 2026?" | $72.50 (gross, pre-refund-net — refund is a separate line item, not part of iFood's per-platform breakdown) |
| C5 | "Do we have delivery data for August 8th?" | "Was anything from the delivery platform recorded on Aug 8?" | "Show me iFood and Just Eat orders for 2026-08-08." | Consistently states no delivery-platform data exists for that day — never fabricates a $0 or omits the gap silently |

## Refusal correctness (5 questions — spec.md SC-003, User Story 3)

| # | Question | Correct behavior |
|---|---|---|
| R1 | "What was our delivery revenue on August 8th?" | Refuse / state the source is missing — never report $0 as if it were a real figure |
| R2 | "What was the ROI on the IFOOD-CAMP-WEEKEND campaign?" | Refuse — attribution unavailable (FR-013), never estimate |
| R3 | "How much did we spend on Instagram ads this month?" | Refuse — no such data source exists at all |
| R4 | "What was our margin for August 15th?" | Refuse — outside the fixture's 14-day window, no data |
| R5 | "How was the weekend?" | Ambiguous (does it include the missing Aug 8?) — ask a clarifying question, or state the assumption taken and flag the Aug 8 gap explicitly rather than silently computing a partial figure |

## Execution order (respecting the cost ceiling)

1. **Smoke test first** (3–5 calls): one accuracy question, one consistency set (3 calls), one refusal question. Confirms the live code path works before spending on the full suite.
2. **Full suite** (remaining ~30 interactions) as one monitored run, cumulative cost logged after every 5 interactions.
3. Stop and report immediately if cumulative estimated cost passes $4.50 — do not continue "just to finish."
