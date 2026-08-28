-- name: RecordUsageEvent :one
-- Backs FR-003: one real, timestamped usage event per real app-open. The
-- ON CONFLICT no-op is the actual mechanism behind "no double-counting
-- within a calendar day, no manual dedup required of the caller" (spec 002
-- User Story 2 Acceptance Scenario 3) — occurred_on is a generated column
-- with a unique index (migrations/000003), so a second ping on the same UTC
-- day can never insert a second row. DO NOTHING has no RETURNING row on a
-- conflict, so the Go caller (internal/httpapi) does its own
-- already-recorded-today check rather than relying on this query's return
-- value alone; see storage/usage_event.go's RecordUsageEvent wrapper.
INSERT INTO usage_event (occurred_at)
VALUES (now())
ON CONFLICT (occurred_on) DO NOTHING
RETURNING *;

-- name: ListDistinctUsageDays :many
-- Backs Engagement badge evaluation (FR-004): every distinct UTC calendar
-- day the app has genuinely been opened on, oldest first. The unique index
-- on occurred_on already guarantees one row per day, so this is a plain
-- ordered read, not a DISTINCT/GROUP BY doing real deduplication work.
SELECT occurred_on FROM usage_event ORDER BY occurred_on;
