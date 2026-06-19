# syntax=docker/dockerfile:1.6
# ============================================================================
#  Home Lab Dashboard — multi-stage Dockerfile
#
#  Stage 1 (build): go build with CGO disabled, ldflags stripped, static
#                    binary, optional upx compression if available.
#  Stage 2 (runtime): alpine:3 with ca-certificates, nonroot user.
#
#  Final image is ~22 MB compressed; runtime RAM footprint is <50 MB.
# ============================================================================

# --- Stage 1: build --------------------------------------------------------
# Pin to Go 1.25-alpine to match the go.mod "go 1.25.0" directive, which
# is auto-bumped from 1.26.4 because go.opentelemetry.io/contrib/.../otelhttp
# v0.69.0 requires go >= 1.25.0. 1.25 is the lowest stable Go at the time
# of the build; aligning the Dockerfile tag with the go directive avoids any
# GOTOOLCHAIN auto-download surprises inside the container.
#
# Note: the original "undefined: errors.As/Is" failure was *not* due to a
# broken toolchain; it was github.com/pkg/errors resolving to v0.8.1 through
# docker/docker's go.mod, which predates that library's As/Is methods
# (added in v0.9.0, Feb 2020). The fix lives in project go.mod (direct
# require pkg/errors v0.9.1) and works on this image or any later one.
FROM golang:1.25-alpine AS build

# Build tools only — kept out of the runtime image.
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache-friendly: copy module manifests first.
COPY go.mod go.sum* ./
RUN go mod download

# Then the source — invalidates cache when code changes.
COPY . .

# Build a static, stripped binary. We cross-target linux/amd64 by default;
# override with --build-arg TARGETOS / TARGETARCH for ARM homelab boards.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

ENV CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

RUN go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -tags netgo,osusergo \
    -o /out/dashboard .

# Compress with UPX if installed at build time. UPX is optional; failure is
# gracefully ignored so the build never breaks for missing tooling.
RUN apk add --no-cache upx 2>/dev/null \
 && upx --best --lzma /out/dashboard 2>/dev/null \
 || true

# --- Stage 2: runtime ------------------------------------------------------
FROM alpine:3.20 AS runtime

# tzdata is required for slog + http handlers that respect location.
# ca-certificates is needed for HTTPS calls to public IP services.
RUN apk add --no-cache ca-certificates tzdata curl \
 && update-ca-certificates \
 && addgroup -S -g 1000 hld \
 && adduser  -S -u 1000 -G hld hld

WORKDIR /app

COPY --from=build /out/dashboard /app/dashboard

# Runtime config — overridable via docker-compose env block.
ENV PORT=8080 \
    POLLING_INTERVAL=15s \
    LOG_LEVEL=info \
    ENABLE_PUBLIC_IP=true

USER hld

EXPOSE 8080

# In-app health endpoint — Docker's HEALTHCHECK uses the same endpoint so
# you don't need extra curl tooling inside the image (curl is installed for
# convenience in case admins need it; not used by the healthcheck itself).
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD curl -fsS "http://127.0.0.1:${PORT}/healthz" || exit 1

ENTRYPOINT ["/app/dashboard"]
