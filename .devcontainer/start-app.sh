#!/usr/bin/env bash
# Starts the whole app inside a GitHub Codespace (see devcontainer.json):
# Postgres from the repo's docker-compose.yml, then the same single-URL image
# a Railway deploy runs, listening on port 8080 of the codespace.
set -euo pipefail
cd "$(dirname "$0")/.."

# The docker-in-docker daemon starts alongside the container; wait for it
# rather than racing it.
for _ in $(seq 1 60); do
  docker info >/dev/null 2>&1 && break
  sleep 2
done

docker compose up -d --wait postgres

docker build -t margin-copilot .

docker rm -f margin-copilot-app >/dev/null 2>&1 || true
# Host networking: the app reaches Postgres on localhost:5432 and its own
# port 8080 is the codespace's, which Codespaces forwards. ANTHROPIC_API_KEY
# is passed by name only, so its value never appears in a process listing.
docker run -d --name margin-copilot-app --network host \
  -e PORT=8080 \
  -e DATABASE_URL="postgres://margin_copilot:margin_copilot@localhost:5432/margin_copilot?sslmode=disable" \
  -e ANTHROPIC_API_KEY \
  margin-copilot >/dev/null

echo "Restaurant Margin Copilot is starting on port 8080 (Ports tab)."
echo "Follow its startup with: docker logs -f margin-copilot-app"
