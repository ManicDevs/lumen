# =============================================================================
# Lumen — Multi-stage Docker build
# =============================================================================
# Build:  docker build -t lumen .
# Run:    docker run --rm -it --network=host lumen --chat
# =============================================================================

# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Build
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w \
      -X gitlab.torproject.org/cerberus-droid/lumen/internal/version.Version=${VERSION} \
      -X gitlab.torproject.org/cerberus-droid/lumen/internal/version.Commit=${COMMIT} \
      -X gitlab.torproject.org/cerberus-droid/lumen/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /lumen ./cmd/lumen/

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata

# Non-root user
RUN addgroup -S lumen && adduser -S lumen -G lumen
USER lumen
WORKDIR /home/lumen

COPY --from=builder /lumen /usr/local/bin/lumen

ENTRYPOINT ["lumen"]
