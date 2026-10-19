# syntax=docker/dockerfile:1
#
# Deploy image: one container, one public URL. The Go API also serves the
# built React app (cmd/server -frontend), so the browser never makes a
# cross-origin call. Local development does not use this file; see the
# README's "Getting started" for the two-server dev setup.

FROM node:22-bookworm-slim AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Empty, not unset: src/lib/api.ts then calls /api/* on whatever origin
# served the page instead of falling back to http://localhost:8080.
ENV VITE_API_BASE_URL=
RUN npm run build

FROM golang:1.27-bookworm AS backend
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# No -trimpath, on purpose: internal/livedata anchors the dataset directory
# (where cost-sheet uploads and connector syncs write) to its own
# compile-time source path. The runtime stage therefore works from the same
# /app/backend this stage builds in.
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/gendata ./cmd/gendata \
 && CGO_ENABLED=0 GOBIN=/out go install -tags pgx5 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /app/backend
COPY --from=backend /out/server /out/gendata /out/migrate /usr/local/bin/
COPY backend/migrations ./migrations
COPY --from=frontend /app/frontend/dist /app/frontend/dist
COPY deploy/start.sh /usr/local/bin/start.sh
ENV PORT=8080
EXPOSE 8080
CMD ["start.sh"]
