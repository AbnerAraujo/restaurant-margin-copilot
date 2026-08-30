# Phase 1 Data Model: Daily Margin & Growth Copilot

Entities from `spec.md`'s Key Entities section, with fields and validation
rules derived from the Functional Requirements. Every entity that reaches
the user carries a provenance reference, per Constitution Principle IV.

## DailyReconciliation

Deterministic output of `internal/reconcile/`. One row per restaurant per day.

| Field | Type | Notes |
|---|---|---|
| `date` | date | Primary key component |
| `gross_sales_by_source` | jsonb | Keyed by source (delivery platform, POS) |
| `commissions` | numeric | Computed from delivery-platform export rows |
| `refunds` | numeric | Netted per FR-002 |
| `input_costs` | numeric | From supplier cost sheet |
| `margin` | numeric | Computed, never model-generated (Principle I) |
| `discrepancy_flags` | jsonb | e.g. missing source, anomaly threshold exceeded (FR-003) |
| `source_row_refs` | jsonb | File + row references — provenance (FR-005) |

**Validation**: `margin` MUST be derivable by re-running the reconciliation
function against `source_row_refs` — no field is ever written by the model
layer.

## PromotionRoiRecord

Deterministic output of `internal/reconcile/`, one row per promotion/campaign
per period.

| Field | Type | Notes |
|---|---|---|
| `platform` | text | e.g. Just Eat Takeaway, iFood |
| `campaign_id` | text | |
| `period` | daterange | |
| `spend` | numeric | From promotion/ad-spend export |
| `attributed_incremental_orders` | integer | Nullable — see below |
| `attributed_incremental_revenue` | numeric | Nullable — see below |
| `roi` | numeric | `NULL` when attribution is missing (FR-013) — never estimated |
| `flagged_negative` | boolean | `roi < 0` when `roi` is not null |
| `source_row_refs` | jsonb | Provenance |

**Validation**: If `attributed_incremental_revenue` is null,
`roi` MUST be null and the tool layer MUST surface this as "cannot attribute"
rather than a computed value (spec Edge Cases, FR-013).

## QuestionInteraction

One row per user question, written by `internal/instrumentation/` alongside
whichever of `internal/ambiguity/` / `internal/explain/` ran.

| Field | Type | Notes |
|---|---|---|
| `question_text` | text | |
| `resolved_period` | daterange | Nullable if refused before resolution |
| `ambiguity_gate_result` | enum | `answerable` \| `ambiguous` \| `unanswerable` |
| `clarification_fired` | boolean | |
| `refusal_fired` | boolean | |
| `answer_text` | text | Nullable if refused |
| `provenance_refs` | jsonb | Points into `DailyReconciliation`/`PromotionRoiRecord` rows used |
| `model_used` | text | e.g. `claude-haiku-4-5`, `claude-sonnet-5` |
| `input_tokens` / `output_tokens` | integer | |
| `estimated_cost_usd` | numeric | |
| `latency_ms` | integer | |

**Validation**: `refusal_fired = true` implies `answer_text IS NULL` and
`provenance_refs = '[]'` — a refusal never carries a fabricated citation.

## SourceDataFile (design-time, not a DB table)

The four CSV shapes `internal/ingest/` parses: delivery-platform export, POS
export, supplier cost sheet, promotion/ad-spend export. Each source file
deliberately includes the irregularities named in `spec.md`'s Edge Cases
(duplicate order, refund, missing day, inconsistent date format, incomplete
promotion attribution). Column-name matching tolerates realistic real-world
variance per the Research decision on real-file compatibility — this is a
parsing contract, not a stored entity.

## Relationships

- `DailyReconciliation` and `PromotionRoiRecord` are independent outputs of
  the same ingestion pipeline; a `QuestionInteraction` may cite rows from
  either or both, never neither when `refusal_fired = false`.
- No entity here is ever written by `internal/ambiguity/` or
  `internal/explain/` — those packages only read and narrate.
