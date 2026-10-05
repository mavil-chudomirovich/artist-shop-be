# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build stage: compile the three commands into static binaries.
# Migrations are embedded via go:embed, so the runtime needs no extra files.
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Cache module downloads separately from the source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0 GOOS=linux

RUN go build -trimpath -ldflags="-s -w" -o /out/api    ./cmd/api && \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    go build -trimpath -ldflags="-s -w" -o /out/seed    ./cmd/seed

# ---------------------------------------------------------------------------
# Runtime stage.
# ---------------------------------------------------------------------------
FROM alpine:3.22

# ca-certificates: outbound TLS (SMTP, HTTPS APIs)
# tzdata: correct timestamps for non-UTC deployments
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 -h /app app

WORKDIR /app
COPY --from=build /out/api    /app/api
COPY --from=build /out/migrate /app/migrate
COPY --from=build /out/seed    /app/seed

USER app
EXPOSE 8080

# /readyz reports ready only when PostgreSQL, the schema and Redis are usable.
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/readyz > /dev/null || exit 1

ENTRYPOINT ["/app/api"]
