-- name: RecordClientErrorReport :one
-- Backs POST /api/client-errors (frontend/src/components/ErrorBoundary.tsx).
-- A pure insert of what the browser reported about itself — no
-- interpretation, no join against any other table. See
-- migrations/000006_client_error_report.up.sql for why this is its own
-- table rather than folded into an existing one.
INSERT INTO client_error_report (message, component, stack, url, user_agent)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
