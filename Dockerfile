# =============================================================================
# Build stage
# =============================================================================
FROM golang:1.27.1-alpine AS build

WORKDIR /src

# Cache dependencies separately from source.
COPY go.mod go.sum ./
RUN go mod download

# Copy the source.
COPY . .

# Static binary. CGO is off so the runtime image does not need libc.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/mywebapp \
    ./cmd/api

# =============================================================================
# Runtime stage
# =============================================================================
FROM alpine:3.23

# ca-certificates for HTTPS calls (Cloudflare R2).
# tzdata so timestamps render in the container's timezone.
# wget for the HEALTHCHECK directive below; it is not in the base
# image.
RUN apk add --no-cache ca-certificates tzdata wget

# Run as a non-root user. The uid is arbitrary; 10001 avoids conflicts
# with any Alpine-provided user.
RUN addgroup -g 10001 app && \
    adduser -D -u 10001 -G app app

WORKDIR /app

# The binary.
COPY --from=build /out/mywebapp /app/mywebapp

# Static assets and templates. These are read at runtime, so they must
# be in the image, not built into the binary.
COPY static /app/static
COPY internal/templates /app/internal/templates

# The encrypted secrets file. It is committed in encrypted form; the
# age key that decrypts it is injected at runtime as a Fly Secret.
COPY secrets/secrets.enc.yaml /app/secrets/secrets.enc.yaml

# Non-secret configuration. Fly overrides these via [env] in fly.toml,
# but defaults here make the image runnable standalone with docker run.
#
# SOPS_PATH is the absolute path to the encrypted secrets file. It
# matches the COPY destination above. Setting it here means the image
# is self-contained; fly.toml's [env] block will override it with the
# same value.
ENV APP_ENV=production \
    PORT=8080 \
    LOG_FORMAT=json \
    LOG_LEVEL=info \
    GIN_MODE=release \
    SOPS_PATH=/app/secrets/secrets.enc.yaml

USER app

EXPOSE 8080

# Liveness probe. The health endpoint does no I/O, so this is safe to
# poll frequently. Fly does its own health checking against the
# [[http_service]] port; this directive matters when running the image
# outside Fly (docker run, docker compose, Kubernetes without its own
# probe).
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/mywebapp"]