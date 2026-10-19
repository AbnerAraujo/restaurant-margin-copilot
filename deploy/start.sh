#!/bin/sh
# Container entrypoint for the deploy image (see /Dockerfile).
#
# Runs the same steps as the README's "Getting started", in the same order,
# on every start: schema migration, dataset generation, both ingests, then
# serve. Each step is safe to repeat. Migrations skip what is applied,
# gendata is deterministic, and both ingests upsert, so a restart converges
# on the same reconciled numbers instead of duplicating them.
#
# Consequence worth knowing: the dataset directory lives in the container,
# so a cost sheet uploaded through the app survives in Postgres only until
# the next restart re-ingests the generated data over it.
set -eu

: "${DATABASE_URL:?DATABASE_URL must be set (on Railway, reference the Postgres service: \${{Postgres.DATABASE_URL}})}"
PORT="${PORT:-8080}"

if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo "start.sh: ANTHROPIC_API_KEY is not set; every page works, but the Ask chat will answer with an error until it is." >&2
fi

# golang-migrate's pgx v5 driver reads the same URL under its own scheme;
# pgx defaults to sslmode=prefer, matching what the Go server itself uses.
MIGRATE_URL=$(printf '%s' "$DATABASE_URL" | sed -E 's#^postgres(ql)?://#pgx5://#')

cd /app/backend
migrate -path migrations -database "$MIGRATE_URL" up
gendata -out data/live
server -ingest data/live
server -ingest-promo data/live
exec server -serve ":$PORT" -frontend /app/frontend/dist
